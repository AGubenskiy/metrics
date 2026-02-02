package handler

import (
	"fmt"
	"github.com/AGubenskiy/metrics/internal/storage"
	"github.com/go-chi/chi/v5"
	"net/http"
	"strconv"
)

type Handler struct {
	storage storage.Storage
}

func NewHandler(s storage.Storage) *Handler {
	return &Handler{storage: s}
}

func (h *Handler) UpdateMetric(w http.ResponseWriter, r *http.Request) {
	metricType := chi.URLParam(r, "type")
	metricName := chi.URLParam(r, "name")
	metricValue := chi.URLParam(r, "value")

	if metricName == "" {
		http.NotFound(w, r)
		return
	}

	switch metricType {
	case "gauge":
		value, err := strconv.ParseFloat(metricValue, 64)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		_ = h.storage.UpdateGauge(metricName, value)

	case "counter":
		value, err := strconv.ParseInt(metricValue, 10, 64)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		_ = h.storage.UpdateCounter(metricName, value)

	default:
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

// Инкремент 3
func (h *Handler) GetMetricValue(w http.ResponseWriter, r *http.Request) {
	metricType := chi.URLParam(r, "type")
	metricName := chi.URLParam(r, "name")

	switch metricType {
	case "gauge":
		value, ok := h.storage.GetGauge(metricName)
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(strconv.FormatFloat(value, 'f', -1, 64)))

	case "counter":
		value, ok := h.storage.GetCounter(metricName)
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(strconv.FormatInt(value, 10)))

	default:
		w.WriteHeader(http.StatusBadRequest)
	}
}
func (h *Handler) GetAllMetrics(w http.ResponseWriter, r *http.Request) {
	gauges, counters := h.storage.GetAll()

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)

	fmt.Fprint(w, `
<!DOCTYPE html>
<html lang="en">
<head>
	<meta charset="UTF-8">
	<title>Metrics</title>
	<style>
		body {
			font-family: Arial, sans-serif;
			padding: 10px;
		}
		h1 {
			margin-bottom: 10px;
		}
		table {
			border-collapse: collapse;
			margin-bottom: 30px;
			min-width: 400px;
		}
		th, td {
			border: 1px solid #ccc;
			padding: 8px 12px;
			text-align: left;
		}
		th {
			background-color: #CEE0CC;
		}
	</style>
</head>
<body>

<h1>Metrics</h1>
`)

	// Gauge
	fmt.Fprint(w, "<h2>Gauge</h2>")
	fmt.Fprint(w, "<table><tr><th>Name</th><th>Value</th></tr>")
	for name, value := range gauges {
		fmt.Fprintf(
			w,
			"<tr><td>%s</td><td>%f</td></tr>",
			name,
			value,
		)
	}
	fmt.Fprint(w, "</table>")

	// Counter
	fmt.Fprint(w, "<h2>Counter</h2>")
	fmt.Fprint(w, "<table><tr><th>Name</th><th>Value</th></tr>")
	for name, value := range counters {
		fmt.Fprintf(
			w,
			"<tr><td>%s</td><td>%d</td></tr>",
			name,
			value,
		)
	}
	fmt.Fprint(w, "</table>")

	fmt.Fprint(w, `
</body>
</html>
`)
}
