package main

import (
	"github.com/AGubenskiy/metrics/internal/handler"
	"github.com/AGubenskiy/metrics/internal/storage"
	"log"
	"net/http"
)

func main() {
	store := storage.NewMemStorage()
	handler := handler.NewHandler(store)

	mux := http.NewServeMux()
	mux.HandleFunc("/update/", handler.UpdateMetric)
	mux.HandleFunc("/value/", handler.GetMetricValue)
	mux.HandleFunc("/", handler.GetAllMetrics)

	log.Println("Server started on http://localhost:8080")
	log.Fatal(http.ListenAndServe(":8080", mux))
}
