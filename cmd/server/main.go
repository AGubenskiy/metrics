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
	r := chi.NewRouter()
	r.Post("/update/{type}/{name}/{value}", handler.UpdateMetric)
	r.Get("/value/{type}/{name}", handler.GetMetricValue)
	r.Get("/", handler.GetAllMetrics)

	log.Printf("Server started on http://%s\n", finalAddr)
	log.Fatal(http.ListenAndServe(finalAddr, r))
}
