package main

import (
	"flag"
	"github.com/AGubenskiy/metrics/internal/handler"
	"github.com/AGubenskiy/metrics/internal/storage"
	"github.com/go-chi/chi/v5"
	"log"
	"net/http"
	"os"
)

func main() {
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	addr := flag.String("a", "localhost:8080", "HTTP server address")
	flag.Parse()

	store := storage.NewMemStorage()
	handler := handler.NewHandler(store)
	r := chi.NewRouter()
	r.Post("/update/{type}/{name}/{value}", handler.UpdateMetric)
	r.Get("/value/{type}/{name}", handler.GetMetricValue)
	r.Get("/", handler.GetAllMetrics)

	log.Printf("Server started on http://%s\n", *addr)
	log.Fatal(http.ListenAndServe(*addr, r))
}
