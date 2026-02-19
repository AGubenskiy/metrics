package main

import (
	"flag"
	"github.com/AGubenskiy/metrics/internal/handler"
	loggerMiddleware "github.com/AGubenskiy/metrics/internal/logger"
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
	handler := handler.NewHandler(store)
	logger, err := zap.NewProduction()
	if err != nil {
		log.Fatalf("cannot initialize logger: %v", err)
	}
	defer logger.Sync()

	r := chi.NewRouter()
	r.Use(loggerMiddleware.WithLogging(logger))
	r.Post("/update/{type}/{name}/{value}", handler.UpdateMetric)
	r.Post("/update", handler.UpdateMetricJSON)
	r.Get("/value/{type}/{name}", handler.GetMetricValue)
	r.Post("/value", handler.GetMetricValueJSON)
	r.Get("/", handler.GetAllMetrics)

	log.Printf("Server started on http://%s\n", finalAddr)
	log.Fatal(http.ListenAndServe(finalAddr, r))
}
