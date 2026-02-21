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
)

func main() {
	const defaultAddr = "localhost:8080"

	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	addr := flag.String("a", defaultAddr, "HTTP server address")
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

	store := storage.NewMemStorage()
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

	log.Printf("Server started on http://%s\n", finalAddr)
	log.Fatal(http.ListenAndServe(finalAddr, r))
}
