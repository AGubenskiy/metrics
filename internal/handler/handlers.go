package handler

import (
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
		w.WriteHeader(http.StatusMethodNotAllowed)
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
