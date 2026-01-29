package handler

import (
	"fmt"
	"github.com/AGubenskiy/metrics/internal/storage"
	"net/http"
	"strconv"
	"strings"
)

type Handler struct {
	storage storage.Storage
}

func NewHandler(s storage.Storage) *Handler {
	return &Handler{storage: s}
}

func (h *Handler) UpdateMetric(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.NotFound(w, r)
		return
	}

	// update/<ТИП_МЕТРИКИ>/<ИМЯ_МЕТРИКИ>/<ЗНАЧЕНИЕ_МЕТРИКИ>
	path := strings.TrimPrefix(r.URL.Path, "/update/")
	parts := strings.Split(path, "/")

	if len(parts) < 3 {
		http.NotFound(w, r)
		return
	}

	metricType := parts[0]
	metricName := parts[1]
	metricValue := parts[2]

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
		//log.Println("Updating metric", metricName, metricType, value)
		_ = h.storage.UpdateGauge(metricName, value)

	case "counter":
		value, err := strconv.ParseInt(metricValue, 10, 64)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		//log.Println("Updating metric", metricName, metricType, value)
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
	if r.Method != http.MethodGet {
		http.NotFound(w, r)
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/value/")
	parts := strings.Split(path, "/")

	if len(parts) != 2 {
		http.NotFound(w, r)
		return
	}

	metricType := parts[0]
	metricName := parts[1]

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
	if r.Method != http.MethodGet {
		http.NotFound(w, r)
		return
	}

	gauges, counters := h.storage.GetAll()

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)

	w.Write([]byte("<html><body><h1>Все метрики:</h1>"))

	for name, value := range gauges {
		w.Write([]byte(
			fmt.Sprintf("<br>gauge %s = %f</br>", name, value),
		))
	}

	for name, value := range counters {
		w.Write([]byte(
			fmt.Sprintf("<br>counter %s = %d</br>", name, value),
		))
	}

	w.Write([]byte("</body></html>"))
}
