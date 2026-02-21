package main

import (
	"flag"
	"github.com/AGubenskiy/metrics/internal/handler"
	loggerMiddleware "github.com/AGubenskiy/metrics/internal/logger"
	"github.com/AGubenskiy/metrics/internal/middleware"
	"github.com/AGubenskiy/metrics/internal/storage"
	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

func main() {
	const (
		defaultAddr          = "localhost:8080"
		defaultStoreInterval = 300
		defaultRestore       = true
	)
	defaultFileStoragePath := filepath.Join(os.TempDir(), "metrics-db.json")

	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	addr := flag.String("a", defaultAddr, "HTTP server address")
	storeInterval := flag.Int("i", defaultStoreInterval, "store interval in seconds")
	fileStoragePath := flag.String("f", defaultFileStoragePath, "file storage path")
	restore := flag.Bool("r", defaultRestore, "restore metrics from file at startup")
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

	store := storage.NewMemStorage()
	if finalRestore {
		if err := store.LoadFromFile(finalFileStoragePath); err != nil {
			log.Fatalf("cannot restore metrics from %q: %v", finalFileStoragePath, err)
		}
	}
	if finalStoreInterval == 0 {
		store.SetSyncSaveOnUpdate(func() error {
			return store.SaveToFile(finalFileStoragePath)
		})
	} else {
		go func() {
			ticker := time.NewTicker(time.Duration(finalStoreInterval) * time.Second)
			defer ticker.Stop()

			for range ticker.C {
				if err := store.SaveToFile(finalFileStoragePath); err != nil {
					log.Printf("cannot save metrics to %q: %v", finalFileStoragePath, err)
				}
			}
		}()
	}

	h := handler.NewHandler(store)
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

	log.Printf(
		"Server started on http://%s (store_interval=%ds, file=%s, restore=%t)\n",
		finalAddr,
		finalStoreInterval,
		finalFileStoragePath,
		finalRestore,
	)
	log.Fatal(http.ListenAndServe(finalAddr, r))
}
