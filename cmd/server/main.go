package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/AGubenskiy/metrics/internal/audit"
	"github.com/AGubenskiy/metrics/internal/buildinfo"
	"github.com/AGubenskiy/metrics/internal/handler"
	loggerMiddleware "github.com/AGubenskiy/metrics/internal/logger"
	"github.com/AGubenskiy/metrics/internal/middleware"
	"github.com/AGubenskiy/metrics/internal/repository"
	"github.com/AGubenskiy/metrics/internal/service"
	"github.com/AGubenskiy/metrics/internal/storage"
	"github.com/go-chi/chi/v5"
	_ "github.com/lib/pq"
	"go.uber.org/zap"
)

var (
	buildVersion = "N/A"
	buildDate    = "N/A"
	buildCommit  = "N/A"
)

func main() {
	if err := buildinfo.Print(os.Stdout, buildVersion, buildDate, buildCommit); err != nil {
		log.Printf("cannot print build info: %v", err)
	}

	const (
		defaultAddr          = "localhost:8080"
		defaultStoreInterval = 300
		defaultRestore       = true
		defaultDatabaseDSN   = ""
	)
	defaultFileStoragePath := filepath.Join(os.TempDir(), "metrics-db.json")

	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	addr := flag.String("a", defaultAddr, "HTTP server address")
	storeInterval := flag.Int("i", defaultStoreInterval, "store interval in seconds")
	fileStoragePath := flag.String("f", defaultFileStoragePath, "file storage path")
	restore := flag.Bool("r", defaultRestore, "restore metrics from file at startup")
	databaseDSN := flag.String("d", defaultDatabaseDSN, "database connection DSN")
	key := flag.String("k", "", "hash key")
	auditFile := flag.String("audit-file", "", "path to audit log file")
	auditURL := flag.String("audit-url", "", "audit receiver URL")
	flag.Parse()

	setFlags := map[string]bool{}
	flag.CommandLine.Visit(func(f *flag.Flag) {
		setFlags[f.Name] = true
	})

	finalAddr := resolveStringSetting("ADDRESS", setFlags["a"], *addr, defaultAddr)

	finalStoreInterval, err := resolveIntSetting("STORE_INTERVAL", setFlags["i"], *storeInterval, defaultStoreInterval)
	if err != nil {
		log.Fatal(err)
	}
	if finalStoreInterval < 0 {
		log.Fatalf("store interval cannot be negative: %d", finalStoreInterval)
	}

	finalFileStoragePath := resolveStringSetting("FILE_STORAGE_PATH", setFlags["f"], *fileStoragePath, defaultFileStoragePath)

	finalRestore, err := resolveBoolSetting("RESTORE", setFlags["r"], *restore, defaultRestore)
	if err != nil {
		log.Fatal(err)
	}

	finalDatabaseDSN := resolveStringSetting("DATABASE_DSN", setFlags["d"], *databaseDSN, defaultDatabaseDSN)
	finalKey := resolveStringSetting("KEY", setFlags["k"], *key, "")
	finalAuditFile := resolveStringSetting("AUDIT_FILE", setFlags["audit-file"], *auditFile, "")
	finalAuditURL := resolveStringSetting("AUDIT_URL", setFlags["audit-url"], *auditURL, "")
	fileStorageConfigured := isNonEmptyEnv("FILE_STORAGE_PATH") ||
		isNonEmptyEnv("STORE_INTERVAL") ||
		isNonEmptyEnv("RESTORE") ||
		setFlags["f"] ||
		setFlags["i"] ||
		setFlags["r"]

	var (
		store        service.Repository
		pinger       handler.Pinger
		storageMode  string
		stopAndFlush = func() {}
	)

	switch {
	case finalDatabaseDSN != "":
		if err = repository.ApplyMigrations(finalDatabaseDSN); err != nil {
			log.Fatalf("cannot apply database migrations: %v", err)
		}

		db, dbErr := sql.Open("postgres", finalDatabaseDSN)
		if dbErr != nil {
			log.Fatalf("cannot initialize database connection: %v", dbErr)
		}

		pingCtx, pingCancel := context.WithTimeout(context.Background(), 3*time.Second)
		dbErr = db.PingContext(pingCtx)
		pingCancel()
		if dbErr != nil {
			_ = db.Close()
			log.Fatalf("cannot connect to database: %v", dbErr)
		}

		store = repository.NewPostgresStorage(db)
		pinger = db
		storageMode = "postgres"
		stopAndFlush = func() {
			if closeErr := db.Close(); closeErr != nil {
				log.Printf("cannot close database connection: %v", closeErr)
			}
		}
	case fileStorageConfigured:
		fileStore := storage.NewMemStorage()
		if finalRestore {
			if err = fileStore.LoadFromFile(finalFileStoragePath); err != nil {
				log.Fatalf("cannot restore metrics from %q: %v", finalFileStoragePath, err)
			}
		}

		stopAndFlush = setupFilePersistence(fileStore, finalStoreInterval, finalFileStoragePath)
		store = fileStore
		storageMode = "file"
	default:
		store = storage.NewMemStorage()
		storageMode = "memory"
	}
	defer stopAndFlush()

	metricsService := service.NewMetrics(store)
	auditPublisher, stopAudit, err := buildAuditPublisher(finalAuditFile, finalAuditURL)
	if err != nil {
		log.Fatal(err)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if closeErr := stopAudit(shutdownCtx); closeErr != nil {
			log.Printf("cannot stop audit publisher: %v", closeErr)
		}
	}()

	h := handler.NewHandlerWithAudit(metricsService, pinger, auditPublisher)
	logger, err := zap.NewProduction()
	if err != nil {
		log.Fatalf("cannot initialize logger: %v", err)
	}
	defer func() {
		if err := logger.Sync(); err != nil {
			log.Printf("logger sync error: %v", err)
		}
	}()

	r := chi.NewRouter()
	r.Use(loggerMiddleware.WithLogging(logger))
	r.Use(middleware.Gzip)
	r.Use(middleware.Hash(finalKey))
	r.Post("/update/{type}/{name}/{value}", h.UpdateMetric)
	r.Post("/update", h.UpdateMetricJSON)
	r.Post("/update/", h.UpdateMetricJSON)
	r.Post("/updates", h.UpdateMetricsJSON)
	r.Post("/updates/", h.UpdateMetricsJSON)
	r.Get("/value/{type}/{name}", h.GetMetricValue)
	r.Post("/value", h.GetMetricValueJSON)
	r.Post("/value/", h.GetMetricValueJSON)
	r.Get("/", h.GetAllMetrics)
	r.Get("/ping", h.Ping)

	log.Printf(
		"Server started on http://%s (mode=%s, store_interval=%ds, file=%s, restore=%t, db=%t, audit_file=%t, audit_url=%t)\n",
		finalAddr,
		storageMode,
		finalStoreInterval,
		finalFileStoragePath,
		finalRestore,
		finalDatabaseDSN != "",
		finalAuditFile != "",
		finalAuditURL != "",
	)

	server := &http.Server{
		Addr:    finalAddr,
		Handler: r,
	}

	signalCtx, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()

	serverErrCh := make(chan error, 1)
	go func() {
		err := server.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErrCh <- err
		}
		close(serverErrCh)
	}()

	select {
	case err := <-serverErrCh:
		if err != nil {
			log.Printf("server stopped with error: %v", err)
		}
	case <-signalCtx.Done():
		log.Printf("shutdown signal-stopping HTTP server")

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Printf("graceful shutdown failed: %v", err)
		}
	}
}

func setupFilePersistence(store *storage.MemStorage, storeInterval int, fileStoragePath string) func() {
	stopPeriodicSave := func() {}
	if storeInterval == 0 {
		store.SetSyncSaveOnUpdate(func() error {
			return store.SaveToFile(fileStoragePath)
		})
	} else {
		saveCtx, saveCancel := context.WithCancel(context.Background())
		saveDone := make(chan struct{})
		stopPeriodicSave = func() {
			saveCancel()
			<-saveDone
		}

		go func() {
			defer close(saveDone)
			ticker := time.NewTicker(time.Duration(storeInterval) * time.Second)
			defer ticker.Stop()

			for {
				select {
				case <-saveCtx.Done():
					return
				case <-ticker.C:
					if err := store.SaveToFile(fileStoragePath); err != nil {
						log.Printf("cannot save metrics to %q: %v", fileStoragePath, err)
					}
				}
			}
		}()
	}

	return func() {
		stopPeriodicSave()
		if err := store.SaveToFile(fileStoragePath); err != nil {
			log.Printf("cannot final save to %q: %v", fileStoragePath, err)
			return
		}
		log.Printf("metrics snapshot saved to %q", fileStoragePath)
	}
}

func resolveStringSetting(envName string, flagSet bool, flagValue, defaultValue string) string {
	if value, ok := getNonEmptyEnv(envName); ok {
		return value
	}
	if flagSet {
		trimmed := strings.TrimSpace(flagValue)
		if trimmed != "" {
			return trimmed
		}
	}
	return defaultValue
}

func resolveIntSetting(envName string, flagSet bool, flagValue, defaultValue int) (int, error) {
	if value, ok := getNonEmptyEnv(envName); ok {
		parsedValue, err := strconv.Atoi(value)
		if err != nil {
			return 0, errors.New("invalid " + envName + " value " + strconv.Quote(value) + ": " + err.Error())
		}
		return parsedValue, nil
	}
	if flagSet {
		return flagValue, nil
	}
	return defaultValue, nil
}

func resolveBoolSetting(envName string, flagSet bool, flagValue, defaultValue bool) (bool, error) {
	if value, ok := getNonEmptyEnv(envName); ok {
		parsedValue, err := strconv.ParseBool(value)
		if err != nil {
			return false, errors.New("invalid " + envName + " value " + strconv.Quote(value) + ": " + err.Error())
		}
		return parsedValue, nil
	}
	if flagSet {
		return flagValue, nil
	}
	return defaultValue, nil
}

func isNonEmptyEnv(envName string) bool {
	_, ok := getNonEmptyEnv(envName)
	return ok
}

func getNonEmptyEnv(envName string) (string, bool) {
	value, ok := os.LookupEnv(envName)
	if !ok {
		return "", false
	}

	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", false
	}
	return trimmed, true
}

func buildAuditPublisher(auditFilePath, auditURL string) (handler.AuditPublisher, func(context.Context) error, error) {
	publisher := audit.NewPublisher()
	configured := false

	if auditFilePath != "" {
		publisher.Register(audit.NewFileObserver(auditFilePath))
		configured = true
	}

	if auditURL != "" {
		observer, err := audit.NewHTTPObserver(
			auditURL,
			audit.NewRetryHTTPClient(&http.Client{Timeout: 3 * time.Second}),
		)
		if err != nil {
			return nil, nil, err
		}
		publisher.Register(observer)
		configured = true
	}

	if !configured {
		return publisher, func(context.Context) error { return nil }, nil
	}

	asyncPublisher, err := audit.NewAsyncPublisher(publisher, audit.AsyncPublisherConfig{
		QueueSize:       256,
		DeliveryTimeout: 3 * time.Second,
	})
	if err != nil {
		return nil, nil, err
	}

	return asyncPublisher, asyncPublisher.Close, nil
}
