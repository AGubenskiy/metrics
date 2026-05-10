package handler

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	models "github.com/AGubenskiy/metrics/internal/model"
	"github.com/AGubenskiy/metrics/internal/service"
	"github.com/AGubenskiy/metrics/internal/storage"
	gojson "github.com/goccy/go-json"
)

func BenchmarkHandlerUpdateMetricsJSON(b *testing.B) {
	handler := NewHandler(service.NewMetrics(storage.NewMemStorage()))
	payload := mustMarshalMetrics(b, benchmarkBatchMetrics(256, 32))

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodPost, "/updates", bytes.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = "192.168.0.42:54321"

		rr := httptest.NewRecorder()
		handler.UpdateMetricsJSON(rr, req)

		if rr.Code != http.StatusOK {
			b.Fatalf("expected status %d, got %d", http.StatusOK, rr.Code)
		}
	}
}

func mustMarshalMetrics(b *testing.B, metrics []models.Metrics) []byte {
	b.Helper()

	payload, err := gojson.Marshal(metrics)
	if err != nil {
		b.Fatalf("failed to marshal : %v", err)
	}

	return payload
}

func benchmarkBatchMetrics(gaugeCount, counterCount int) []models.Metrics {
	metrics := make([]models.Metrics, 0, gaugeCount+counterCount)

	gaugeValues := make([]float64, gaugeCount)
	for i := 0; i < gaugeCount; i++ {
		gaugeValues[i] = float64(i) + 0.5
		metrics = append(metrics, models.Metrics{
			ID:    "gauge-" + strconv.Itoa(i),
			MType: models.Gauge,
			Value: &gaugeValues[i],
		})
	}

	counterValues := make([]int64, counterCount)
	for i := 0; i < counterCount; i++ {
		counterValues[i] = int64(i + 1)
		metrics = append(metrics, models.Metrics{
			ID:    "counter-" + strconv.Itoa(i),
			MType: models.Counter,
			Delta: &counterValues[i],
		})
	}

	return metrics
}
