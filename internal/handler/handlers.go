package handler

import (
	"fmt"
	models "github.com/AGubenskiy/metrics/internal/model"
	"github.com/AGubenskiy/metrics/internal/storage"
	"github.com/go-chi/chi/v5"
	gojson "github.com/goccy/go-json"
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

func (h *Handler) UpdateMetricJSON(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	var metric models.Metrics
	if err := gojson.NewDecoder(r.Body).Decode(&metric); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if metric.ID == "" {
		http.Error(w, "metric name is required", http.StatusNotFound)
		return
	}

	switch metric.MType {
	case models.Gauge:
		if metric.Value == nil {
			http.Error(w, "gauge value is required", http.StatusBadRequest)
			return
		}
		_ = h.storage.UpdateGauge(metric.ID, *metric.Value)
		value, _ := h.storage.GetGauge(metric.ID)
		metric.Value = &value
		metric.Delta = nil
	case models.Counter:
		if metric.Delta == nil {
			http.Error(w, "counter delta is required", http.StatusBadRequest)
			return
		}
		_ = h.storage.UpdateCounter(metric.ID, *metric.Delta)
		delta, _ := h.storage.GetCounter(metric.ID)
		metric.Delta = &delta
		metric.Value = nil
	default:
		http.Error(w, "unsupported metric type", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = gojson.NewEncoder(w).Encode(metric)
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

func (h *Handler) GetMetricValueJSON(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	var metric models.Metrics
	if err := gojson.NewDecoder(r.Body).Decode(&metric); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if metric.ID == "" {
		http.NotFound(w, r)
		return
	}

	switch metric.MType {
	case models.Gauge:
		value, ok := h.storage.GetGauge(metric.ID)
		if !ok {
			http.NotFound(w, r)
			return
		}
		metric.Value = &value
		metric.Delta = nil
	case models.Counter:
		value, ok := h.storage.GetCounter(metric.ID)
		if !ok {
			http.NotFound(w, r)
			return
		}
		metric.Delta = &value
		metric.Value = nil
	default:
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = gojson.NewEncoder(w).Encode(metric)
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
