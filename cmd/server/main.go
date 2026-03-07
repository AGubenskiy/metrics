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
	"syscall"
	"time"

	"github.com/AGubenskiy/metrics/internal/handler"
	loggerMiddleware "github.com/AGubenskiy/metrics/internal/logger"
	"github.com/AGubenskiy/metrics/internal/middleware"
	"github.com/AGubenskiy/metrics/internal/storage"
	"github.com/go-chi/chi/v5"
	_ "github.com/lib/pq"
	"go.uber.org/zap"
)

func main() {
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
	flag.Parse()

	setFlags := map[string]bool{}
	flag.CommandLine.Visit(func(f *flag.Flag) {
		setFlags[f.Name] = true
	})

	finalAddr := defaultAddr
	if envAddr := os.Getenv("ADDRESS"); envAddr != "" {
		finalAddr = envAddr
	} else if setFlags["a"] {
		finalAddr = *addr
	}

	finalStoreInterval := defaultStoreInterval
	if envStoreInterval := os.Getenv("STORE_INTERVAL"); envStoreInterval != "" {
		parsedStoreInterval, err := strconv.Atoi(envStoreInterval)
		if err != nil {
			log.Fatalf("invalid STORE_INTERVAL value %q: %v", envStoreInterval, err)
		}
		finalStoreInterval = parsedStoreInterval
	} else if setFlags["i"] {
		finalStoreInterval = *storeInterval
	}
	if finalStoreInterval < 0 {
		log.Fatalf("store interval cannot be negative: %d", finalStoreInterval)
	}

	finalFileStoragePath := defaultFileStoragePath
	if envFileStoragePath := os.Getenv("FILE_STORAGE_PATH"); envFileStoragePath != "" {
		finalFileStoragePath = envFileStoragePath
	} else if setFlags["f"] {
		finalFileStoragePath = *fileStoragePath
	}

	finalRestore := defaultRestore
	if envRestore := os.Getenv("RESTORE"); envRestore != "" {
		parsedRestore, err := strconv.ParseBool(envRestore)
		if err != nil {
			log.Fatalf("invalid RESTORE value %q: %v", envRestore, err)
		}
		finalRestore = parsedRestore
	} else if setFlags["r"] {
		finalRestore = *restore
	}

	finalDatabaseDSN := defaultDatabaseDSN
	if envDatabaseDSN := os.Getenv("DATABASE_DSN"); envDatabaseDSN != "" {
		finalDatabaseDSN = envDatabaseDSN
	} else if setFlags["d"] {
		finalDatabaseDSN = *databaseDSN
	}

	store := storage.NewMemStorage()
	if finalRestore {
		if err := store.LoadFromFile(finalFileStoragePath); err != nil {
			log.Fatalf("cannot restore metrics from %q: %v", finalFileStoragePath, err)
		}
	}

	defer func() {
		if err := store.SaveToFile(finalFileStoragePath); err != nil {
			log.Printf("cannot final save to %q: %v", finalFileStoragePath, err)
			return
		}
		log.Printf("metrics snapshot saved to %q", finalFileStoragePath)
	}()

	stopPeriodicSave := func() {}
	if finalStoreInterval == 0 {
		store.SetSyncSaveOnUpdate(func() error {
			return store.SaveToFile(finalFileStoragePath)
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
			ticker := time.NewTicker(time.Duration(finalStoreInterval) * time.Second)
			defer ticker.Stop()

			for {
				select {
				case <-saveCtx.Done():
					return
				case <-ticker.C:
					if err := store.SaveToFile(finalFileStoragePath); err != nil {
						log.Printf("cannot save metrics to %q: %v", finalFileStoragePath, err)
					}
				}
			}
		}()
	}
	defer stopPeriodicSave()

	var db *sql.DB
	if finalDatabaseDSN != "" {
		dbConn, err := sql.Open("postgres", finalDatabaseDSN)
		if err != nil {
			log.Fatalf("cannot initialize database connection: %v", err)
		}
		db = dbConn

		defer func() {
			if closeErr := db.Close(); closeErr != nil {
				log.Printf("cannot close database connection: %v", closeErr)
			}
		}()
	}

	h := handler.NewHandlerWithPinger(store, db)
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
	r.Post("/update/{type}/{name}/{value}", h.UpdateMetric)
	r.Post("/update", h.UpdateMetricJSON)
	r.Post("/update/", h.UpdateMetricJSON)
	r.Get("/value/{type}/{name}", h.GetMetricValue)
	r.Post("/value", h.GetMetricValueJSON)
	r.Post("/value/", h.GetMetricValueJSON)
	r.Get("/", h.GetAllMetrics)
	r.Get("/ping", h.Ping)

	log.Printf(
		"Server started on http://%s (store_interval=%ds, file=%s, restore=%t, db=%t)\n",
		finalAddr,
		finalStoreInterval,
		finalFileStoragePath,
		finalRestore,
		finalDatabaseDSN != "",
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
