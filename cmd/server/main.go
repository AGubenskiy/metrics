package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/AGubenskiy/metrics/internal/audit"
	"github.com/AGubenskiy/metrics/internal/buildinfo"
	appconfig "github.com/AGubenskiy/metrics/internal/config"
	"github.com/AGubenskiy/metrics/internal/cryptoutil"
	"github.com/AGubenskiy/metrics/internal/grpcmetrics"
	"github.com/AGubenskiy/metrics/internal/handler"
	loggerMiddleware "github.com/AGubenskiy/metrics/internal/logger"
	"github.com/AGubenskiy/metrics/internal/middleware"
	pb "github.com/AGubenskiy/metrics/internal/proto"
	"github.com/AGubenskiy/metrics/internal/repository"
	"github.com/AGubenskiy/metrics/internal/service"
	"github.com/AGubenskiy/metrics/internal/storage"
	"github.com/go-chi/chi/v5"
	_ "github.com/lib/pq"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
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
		defaultGRPCAddr      = ""
		defaultStoreInterval = 300
		defaultRestore       = true
		defaultDatabaseDSN   = ""
	)
	defaultFileStoragePath := filepath.Join(os.TempDir(), "metrics-db.json")

	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	addr := flag.String("a", defaultAddr, "HTTP server address")
	grpcAddr := flag.String("grpc-a", defaultGRPCAddr, "gRPC server address")
	flag.StringVar(grpcAddr, "grpc-address", defaultGRPCAddr, "gRPC server address")
	storeInterval := flag.Int("i", defaultStoreInterval, "store interval in seconds")
	fileStoragePath := flag.String("f", defaultFileStoragePath, "file storage path")
	restore := flag.Bool("r", defaultRestore, "restore metrics from file at startup")
	databaseDSN := flag.String("d", defaultDatabaseDSN, "database connection DSN")
	key := flag.String("k", "", "hash key")
	cryptoKey := flag.String("crypto-key", "", "path to private crypto key")
	grpcCertFile := flag.String("grpc-cert-file", "", "path to gRPC TLS certificate PEM file")
	grpcKeyFile := flag.String("grpc-key-file", "", "path to gRPC TLS private key PEM file")
	trustedSubnet := flag.String("t", "", "trusted subnet in CIDR notation")
	auditFile := flag.String("audit-file", "", "path to audit log file")
	auditURL := flag.String("audit-url", "", "audit receiver URL")
	configPath := ""
	flag.StringVar(&configPath, "c", "", "path to JSON config file")
	flag.StringVar(&configPath, "config", "", "path to JSON config file")
	flag.Parse()

	setFlags := map[string]bool{}
	flag.CommandLine.Visit(func(f *flag.Flag) {
		setFlags[f.Name] = true
	})

	finalConfigPath := appconfig.ResolveString([]string{"CONFIG"}, setFlags["c"] || setFlags["config"], configPath, nil, "")
	fileConfig, err := appconfig.LoadServer(finalConfigPath)
	if err != nil {
		log.Fatal(err)
	}

	finalAddr := appconfig.ResolveString([]string{"ADDRESS"}, setFlags["a"], *addr, fileConfig.Address, defaultAddr)
	finalGRPCAddr := appconfig.ResolveString(
		[]string{"GRPC_ADDRESS"},
		setFlags["grpc-a"] || setFlags["grpc-address"],
		*grpcAddr,
		fileConfig.GRPCAddress,
		defaultGRPCAddr,
	)

	finalStoreInterval, err := appconfig.ResolveSeconds([]string{"STORE_INTERVAL"}, setFlags["i"], *storeInterval, fileConfig.StoreInterval, defaultStoreInterval)
	if err != nil {
		log.Fatal(err)
	}
	if finalStoreInterval < 0 {
		log.Fatalf("store interval cannot be negative: %d", finalStoreInterval)
	}

	finalFileStoragePath := appconfig.ResolveString(
		[]string{"STORE_FILE", "FILE_STORAGE_PATH"},
		setFlags["f"],
		*fileStoragePath,
		fileConfig.StoreFileValue(),
		defaultFileStoragePath,
	)

	finalRestore, err := appconfig.ResolveBool([]string{"RESTORE"}, setFlags["r"], *restore, fileConfig.Restore, defaultRestore)
	if err != nil {
		log.Fatal(err)
	}

	finalDatabaseDSN := appconfig.ResolveString([]string{"DATABASE_DSN"}, setFlags["d"], *databaseDSN, fileConfig.DatabaseDSN, defaultDatabaseDSN)
	finalKey := appconfig.ResolveString([]string{"KEY"}, setFlags["k"], *key, fileConfig.Key, "")
	finalCryptoKey := appconfig.ResolveString([]string{"CRYPTO_KEY"}, setFlags["crypto-key"], *cryptoKey, fileConfig.CryptoKey, "")
	finalGRPCCertFile := appconfig.ResolveString([]string{"GRPC_CERT_FILE"}, setFlags["grpc-cert-file"], *grpcCertFile, fileConfig.GRPCCertFile, "")
	finalGRPCKeyFile := appconfig.ResolveString([]string{"GRPC_KEY_FILE"}, setFlags["grpc-key-file"], *grpcKeyFile, fileConfig.GRPCKeyFile, "")
	finalTrustedSubnet := appconfig.ResolveString([]string{"TRUSTED_SUBNET"}, setFlags["t"], *trustedSubnet, fileConfig.TrustedSubnet, "")
	finalAuditFile := appconfig.ResolveString([]string{"AUDIT_FILE"}, setFlags["audit-file"], *auditFile, fileConfig.AuditFile, "")
	finalAuditURL := appconfig.ResolveString([]string{"AUDIT_URL"}, setFlags["audit-url"], *auditURL, fileConfig.AuditURL, "")
	fileStorageConfigured := appconfig.HasNonEmptyEnv("STORE_FILE", "FILE_STORAGE_PATH") ||
		appconfig.HasNonEmptyEnv("STORE_INTERVAL") ||
		appconfig.HasNonEmptyEnv("RESTORE") ||
		setFlags["f"] ||
		setFlags["i"] ||
		setFlags["r"] ||
		fileConfig.HasFileStorageSettings()

	logger, err := zap.NewProduction()
	if err != nil {
		log.Fatalf("cannot initialize logger: %v", err)
	}
	defer func() {
		if err := logger.Sync(); err != nil {
			log.Printf("logger sync error: %v", err)
		}
	}()

	trustedSubnetMiddleware, err := middleware.TrustedSubnetWithLogger(logger, finalTrustedSubnet, "/update", "/updates")
	if err != nil {
		log.Fatal(err)
	}

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
	grpcServer, grpcErrCh, err := startGRPCServer(finalGRPCAddr, finalTrustedSubnet, finalGRPCCertFile, finalGRPCKeyFile, metricsService, logger)
	if err != nil {
		log.Fatal(err)
	}
	defer func() {
		if grpcServer != nil {
			grpcServer.Stop()
		}
	}()

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
	r := chi.NewRouter()
	r.Use(loggerMiddleware.WithLogging(logger))
	r.Use(trustedSubnetMiddleware)
	if finalCryptoKey != "" {
		privateKey, err := cryptoutil.LoadPrivateKey(finalCryptoKey)
		if err != nil {
			log.Fatalf("cannot load private crypto key: %v", err)
		}
		r.Use(middleware.Decrypt(privateKey))
	}
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
		"Server started on http://%s (mode=%s, store_interval=%ds, file=%s, restore=%t, db=%t, crypto=%t, trusted_subnet=%t, audit_file=%t, audit_url=%t)\n",
		finalAddr,
		storageMode,
		finalStoreInterval,
		finalFileStoragePath,
		finalRestore,
		finalDatabaseDSN != "",
		finalCryptoKey != "",
		finalTrustedSubnet != "",
		finalAuditFile != "",
		finalAuditURL != "",
	)
	if finalGRPCAddr != "" {
		log.Printf("gRPC server started on %s with TLS", finalGRPCAddr)
	}

	server := &http.Server{
		Addr:    finalAddr,
		Handler: r,
	}

	signalCtx, stopSignals := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT, syscall.SIGQUIT)
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
	case err := <-grpcErrCh:
		if err != nil {
			log.Printf("gRPC server stopped with error: %v", err)
		}
	case <-signalCtx.Done():
		log.Printf("shutdown signal-stopping HTTP server")

		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()

		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Printf("graceful shutdown failed: %v", err)
		}
		shutdownGRPCServer(grpcServer, 5*time.Second)
	}
}

func startGRPCServer(addr, trustedSubnet, certFile, keyFile string, metricsService grpcmetrics.MetricsService, logger *zap.Logger) (*grpc.Server, <-chan error, error) {
	if addr == "" {
		return nil, nil, nil
	}

	if certFile == "" || keyFile == "" {
		return nil, nil, fmt.Errorf("gRPC TLS certificate and private key files are required when gRPC server is enabled")
	}

	tlsCredentials, err := credentials.NewServerTLSFromFile(certFile, keyFile)
	if err != nil {
		return nil, nil, fmt.Errorf("load gRPC TLS credentials: %w", err)
	}

	trustedSubnetInterceptor, err := grpcmetrics.TrustedSubnetUnaryInterceptor(trustedSubnet)
	if err != nil {
		return nil, nil, err
	}

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, nil, err
	}

	server := grpc.NewServer(
		grpc.Creds(tlsCredentials),
		grpc.UnaryInterceptor(trustedSubnetInterceptor),
	)
	metricsServer := grpcmetrics.NewServer(metricsService)
	metricsServer.SetLogger(logger)
	pb.RegisterMetricsServer(server, metricsServer)

	errCh := make(chan error, 1)
	go func() {
		err := server.Serve(listener)
		if err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			errCh <- err
		}
		close(errCh)
	}()

	return server, errCh, nil
}

func shutdownGRPCServer(server *grpc.Server, timeout time.Duration) {
	if server == nil {
		return
	}

	done := make(chan struct{})
	go func() {
		server.GracefulStop()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(timeout):
		server.Stop()
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
