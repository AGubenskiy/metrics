package main

import (
	"github.com/AGubenskiy/metrics/internal/handler"
	"github.com/AGubenskiy/metrics/internal/storage"
	"github.com/go-chi/chi/v5"
	"log"
	"net/http"
)

func main() {
	store := storage.NewMemStorage()
	handler := handler.NewHandler(store)
	r := chi.NewRouter()
	r.Post("/update/{type}/{name}/{value}", handler.UpdateMetric)
	r.Get("/value/{type}/{name}", handler.GetMetricValue)
	r.Get("/", handler.GetAllMetrics)
	//mux := http.NewServeMux()
	//mux.HandleFunc("/update/", handler.UpdateMetric)
	//mux.HandleFunc("/value/", handler.GetMetricValue)
	//mux.HandleFunc("/", handler.GetAllMetrics)

	log.Println("Server started on http://localhost:8080")
	//log.Fatal(http.ListenAndServe(":8080", mux))
	log.Fatal(http.ListenAndServe(":8080", r))
}
