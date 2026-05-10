package handler

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"github.com/AGubenskiy/metrics/internal/audit"
	"github.com/AGubenskiy/metrics/internal/middleware"
	models "github.com/AGubenskiy/metrics/internal/model"
	"github.com/AGubenskiy/metrics/internal/service"
	"github.com/AGubenskiy/metrics/internal/signing"
	"github.com/go-chi/chi/v5"
	gojson "github.com/goccy/go-json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/AGubenskiy/metrics/internal/storage"
)

type mockPinger struct {
	err error
}

type recordingAuditObserver struct {
	events []audit.Event
}

func (m *mockPinger) PingContext(_ context.Context) error {
	return m.err
}

func (o *recordingAuditObserver) Update(_ context.Context, event audit.Event) error {
	o.events = append(o.events, event)
	return nil
}

func setupRouter() http.Handler {
	store := storage.NewMemStorage()
	h := NewHandler(service.NewMetrics(store))

	r := chi.NewRouter()
	r.Use(middleware.Gzip)
	r.Post("/update/{type}/{name}/{value}", h.UpdateMetric)
	r.Post("/update", h.UpdateMetricJSON)
	r.Post("/update/", h.UpdateMetricJSON)
	r.Post("/updates", h.UpdateMetricsJSON)
	r.Post("/updates/", h.UpdateMetricsJSON)
	r.Get("/value/{type}/{name}", h.GetMetricValue)
	r.Post("/value", h.GetMetricValueJSON)
	r.Post("/value/", h.GetMetricValueJSON)
	r.Get("/", h.GetAllMetrics)
	r.Get("/ping", h.Ping)

	return r
}

func setupRouterWithKey(key string) http.Handler {
	store := storage.NewMemStorage()
	h := NewHandler(service.NewMetrics(store))

	r := chi.NewRouter()
	r.Use(middleware.Gzip)
	r.Use(middleware.Hash(key))
	r.Post("/update/{type}/{name}/{value}", h.UpdateMetric)
	r.Post("/update", h.UpdateMetricJSON)
	r.Post("/update/", h.UpdateMetricJSON)
	r.Post("/updates", h.UpdateMetricsJSON)
	r.Post("/updates/", h.UpdateMetricsJSON)
	r.Get("/value/{type}/{name}", h.GetMetricValue)
	r.Post("/value", h.GetMetricValueJSON)
	r.Post("/value/", h.GetMetricValueJSON)
	r.Get("/", h.GetAllMetrics)
	r.Get("/ping", h.Ping)

	return r
}

func setupRouterWithPinger(pinger Pinger) http.Handler {
	store := storage.NewMemStorage()
	h := NewHandlerWithPinger(service.NewMetrics(store), pinger)

	r := chi.NewRouter()
	r.Get("/ping", h.Ping)

	return r
}

func setupRouterWithAudit(observer audit.Observer) http.Handler {
	store := storage.NewMemStorage()
	publisher := audit.NewPublisher(observer)
	h := NewHandlerWithAudit(service.NewMetrics(store), nil, publisher)

	r := chi.NewRouter()
	r.Use(middleware.Gzip)
	r.Post("/update/{type}/{name}/{value}", h.UpdateMetric)
	r.Post("/update", h.UpdateMetricJSON)
	r.Post("/update/", h.UpdateMetricJSON)
	r.Post("/updates", h.UpdateMetricsJSON)
	r.Post("/updates/", h.UpdateMetricsJSON)

	return r
}

func TestUpdateMetric(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		url        string
		wantStatus int
	}{
		{
			name:       "valid gauge",
			method:     http.MethodPost,
			url:        "/update/gauge/testGauge/10.5",
			wantStatus: http.StatusOK,
		},
		{
			name:       "valid counter",
			method:     http.MethodPost,
			url:        "/update/counter/testCounter/3",
			wantStatus: http.StatusOK,
		},
		{
			name:       "unknown metric type",
			method:     http.MethodPost,
			url:        "/update/unknown/test/10",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "invalid gauge value",
			method:     http.MethodPost,
			url:        "/update/gauge/test/abc",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "invalid counter value",
			method:     http.MethodPost,
			url:        "/update/counter/test/abc",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "missing value",
			method:     http.MethodPost,
			url:        "/update/gauge/test/",
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "wrong http method",
			method:     http.MethodGet,
			url:        "/update/gauge/test/10",
			wantStatus: http.StatusMethodNotAllowed,
		},
	}

	router := setupRouter()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.url, nil)
			rr := httptest.NewRecorder()

			router.ServeHTTP(rr, req)

			if rr.Code != tt.wantStatus {
				t.Fatalf("expected status %d, got %d", tt.wantStatus, rr.Code)
			}
		})
	}
}

func TestGetMetricValue(t *testing.T) {
	router := setupRouter()

	// сначала создаём метрики
	req1 := httptest.NewRequest(http.MethodPost, "/update/gauge/testGauge/1.23", nil)
	req2 := httptest.NewRequest(http.MethodPost, "/update/counter/testCounter/10", nil)

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req1)
	router.ServeHTTP(rr, req2)

	tests := []struct {
		name       string
		url        string
		wantStatus int
	}{
		{
			name:       "get existing gauge",
			url:        "/value/gauge/testGauge",
			wantStatus: http.StatusOK,
		},
		{
			name:       "get existing counter",
			url:        "/value/counter/testCounter",
			wantStatus: http.StatusOK,
		},
		{
			name:       "unknown gauge metric",
			url:        "/value/gauge/unknown",
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "unknown counter metric",
			url:        "/value/counter/unknown",
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "unknown metric type",
			url:        "/value/unknown/test",
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.url, nil)
			rr := httptest.NewRecorder()

			router.ServeHTTP(rr, req)

			if rr.Code != tt.wantStatus {
				t.Fatalf("expected status %d, got %d", tt.wantStatus, rr.Code)
			}
		})
	}
}

func TestGetAllMetrics(t *testing.T) {
	router := setupRouter()

	router.ServeHTTP(
		httptest.NewRecorder(),
		httptest.NewRequest(http.MethodPost, "/update/gauge/testGauge/5.5", nil),
	)
	router.ServeHTTP(
		httptest.NewRecorder(),
		httptest.NewRequest(http.MethodPost, "/update/counter/testCounter/7", nil),
	)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rr.Code)
	}

	if ct := rr.Header().Get("Content-Type"); ct == "" {
		t.Fatalf("expected Content-Type header to be set")
	}
}

func TestUpdateMetricJSON(t *testing.T) {
	router := setupRouter()

	gaugeValue := 10.5
	body, _ := gojson.Marshal(models.Metrics{
		ID:    "testGauge",
		MType: models.Gauge,
		Value: &gaugeValue,
	})

	req := httptest.NewRequest(http.MethodPost, "/update", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("expected application/json content type, got %q", ct)
	}

	var got models.Metrics
	if err := gojson.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("failed to decode JSON response: %v", err)
	}
	if got.ID != "testGauge" || got.MType != models.Gauge || got.Value == nil || *got.Value != gaugeValue {
		t.Fatalf("unexpected response: %+v", got)
	}
}

func TestGetMetricValueJSON(t *testing.T) {
	router := setupRouter()

	initialValue := 42.25
	updateBody, _ := gojson.Marshal(models.Metrics{
		ID:    "gaugeJSON",
		MType: models.Gauge,
		Value: &initialValue,
	})
	updateReq := httptest.NewRequest(http.MethodPost, "/update", bytes.NewReader(updateBody))
	updateReq.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(httptest.NewRecorder(), updateReq)

	valueBody, _ := gojson.Marshal(models.Metrics{
		ID:    "gaugeJSON",
		MType: models.Gauge,
	})
	valueReq := httptest.NewRequest(http.MethodPost, "/value", bytes.NewReader(valueBody))
	valueReq.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, valueReq)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("expected application/json content type, got %q", ct)
	}

	var got models.Metrics
	if err := gojson.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("failed to decode JSON response: %v", err)
	}
	if got.Value == nil || *got.Value != initialValue {
		t.Fatalf("expected value %f, got %+v", initialValue, got)
	}
}

func TestUpdateMetricJSONWithGzipBody(t *testing.T) {
	router := setupRouter()

	gaugeValue := 7.7
	body, err := gojson.Marshal(models.Metrics{
		ID:    "gzipGauge",
		MType: models.Gauge,
		Value: &gaugeValue,
	})
	if err != nil {
		t.Fatalf("failed to marshal request body: %v", err)
	}

	compressedBody := gzipBytes(t, body)

	req := httptest.NewRequest(http.MethodPost, "/update", bytes.NewReader(compressedBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Content-Encoding", "gzip")
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rr.Code)
	}
}

func TestUpdateMetricJSONRejectsInvalidHash(t *testing.T) {
	router := setupRouterWithKey("test-key")

	gaugeValue := 7.7
	body, err := gojson.Marshal(models.Metrics{
		ID:    "signedGauge",
		MType: models.Gauge,
		Value: &gaugeValue,
	})
	if err != nil {
		t.Fatalf("failed to marshal request body: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/update", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(signing.HeaderName, "invalid")
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, rr.Code)
	}
}

func TestUpdateMetricJSONAcceptsValidHash(t *testing.T) {
	router := setupRouterWithKey("test-key")

	gaugeValue := 7.7
	body, err := gojson.Marshal(models.Metrics{
		ID:    "signedGauge",
		MType: models.Gauge,
		Value: &gaugeValue,
	})
	if err != nil {
		t.Fatalf("failed to marshal request body: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/update", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(signing.HeaderName, signing.Hash(body, "test-key"))
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rr.Code)
	}
}

func TestGzipResponseForJSON(t *testing.T) {
	router := setupRouter()

	gaugeValue := 11.11
	updateBody, _ := gojson.Marshal(models.Metrics{
		ID:    "jsonCompressed",
		MType: models.Gauge,
		Value: &gaugeValue,
	})
	updateReq := httptest.NewRequest(http.MethodPost, "/update", bytes.NewReader(updateBody))
	updateReq.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(httptest.NewRecorder(), updateReq)

	valueBody, _ := gojson.Marshal(models.Metrics{
		ID:    "jsonCompressed",
		MType: models.Gauge,
	})
	valueReq := httptest.NewRequest(http.MethodPost, "/value", bytes.NewReader(valueBody))
	valueReq.Header.Set("Content-Type", "application/json")
	valueReq.Header.Set("Accept-Encoding", "gzip")
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, valueReq)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rr.Code)
	}
	if got := rr.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("expected Content-Encoding gzip, got %q", got)
	}

	decompressed := gunzipBytes(t, rr.Body.Bytes())
	var got models.Metrics
	if err := gojson.Unmarshal(decompressed, &got); err != nil {
		t.Fatalf("failed to decode gzipped json response: %v", err)
	}
	if got.Value == nil || *got.Value != gaugeValue {
		t.Fatalf("expected value %f, got %+v", gaugeValue, got)
	}
}

func TestJSONResponseContainsHashHeaderWhenKeyConfigured(t *testing.T) {
	router := setupRouterWithKey("test-key")

	gaugeValue := 11.11
	updateBody, _ := gojson.Marshal(models.Metrics{
		ID:    "jsonSigned",
		MType: models.Gauge,
		Value: &gaugeValue,
	})
	updateReq := httptest.NewRequest(http.MethodPost, "/update", bytes.NewReader(updateBody))
	updateReq.Header.Set("Content-Type", "application/json")
	updateReq.Header.Set(signing.HeaderName, signing.Hash(updateBody, "test-key"))
	router.ServeHTTP(httptest.NewRecorder(), updateReq)

	valueBody, _ := gojson.Marshal(models.Metrics{
		ID:    "jsonSigned",
		MType: models.Gauge,
	})
	valueReq := httptest.NewRequest(http.MethodPost, "/value", bytes.NewReader(valueBody))
	valueReq.Header.Set("Content-Type", "application/json")
	valueReq.Header.Set(signing.HeaderName, signing.Hash(valueBody, "test-key"))
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, valueReq)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rr.Code)
	}

	responseHash := rr.Header().Get(signing.HeaderName)
	if responseHash == "" {
		t.Fatalf("expected %s header to be set", signing.HeaderName)
	}

	if expected := signing.Hash(rr.Body.Bytes(), "test-key"); responseHash != expected {
		t.Fatalf("expected %s %q, got %q", signing.HeaderName, expected, responseHash)
	}
}

func TestUpdateMetricsJSON(t *testing.T) {
	router := setupRouter()

	gaugeValue := 5.5
	counterDelta := int64(7)
	requestMetrics := []models.Metrics{
		{
			ID:    "batchGauge",
			MType: models.Gauge,
			Value: &gaugeValue,
		},
		{
			ID:    "batchCounter",
			MType: models.Counter,
			Delta: &counterDelta,
		},
	}

	body, err := gojson.Marshal(requestMetrics)
	if err != nil {
		t.Fatalf("failed to marshal request body: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/updates/", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rr.Code)
	}

	valueGaugeReq, _ := gojson.Marshal(models.Metrics{ID: "batchGauge", MType: models.Gauge})
	gaugeReq := httptest.NewRequest(http.MethodPost, "/value/", bytes.NewReader(valueGaugeReq))
	gaugeReq.Header.Set("Content-Type", "application/json")
	gaugeRR := httptest.NewRecorder()
	router.ServeHTTP(gaugeRR, gaugeReq)
	if gaugeRR.Code != http.StatusOK {
		t.Fatalf("expected status %d for gauge value, got %d", http.StatusOK, gaugeRR.Code)
	}

	valueCounterReq, _ := gojson.Marshal(models.Metrics{ID: "batchCounter", MType: models.Counter})
	counterReq := httptest.NewRequest(http.MethodPost, "/value/", bytes.NewReader(valueCounterReq))
	counterReq.Header.Set("Content-Type", "application/json")
	counterRR := httptest.NewRecorder()
	router.ServeHTTP(counterRR, counterReq)
	if counterRR.Code != http.StatusOK {
		t.Fatalf("expected status %d for counter value, got %d", http.StatusOK, counterRR.Code)
	}
}

func TestUpdateMetricsJSONWithGzipBody(t *testing.T) {
	router := setupRouter()

	gaugeValue := 8.8
	requestMetrics := []models.Metrics{
		{
			ID:    "gzipBatchGauge",
			MType: models.Gauge,
			Value: &gaugeValue,
		},
	}

	body, err := gojson.Marshal(requestMetrics)
	if err != nil {
		t.Fatalf("failed to marshal request body: %v", err)
	}

	compressedBody := gzipBytes(t, body)
	req := httptest.NewRequest(http.MethodPost, "/updates/", bytes.NewReader(compressedBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Content-Encoding", "gzip")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rr.Code)
	}
}

func TestUpdateMetricsJSONPublishesAuditEvent(t *testing.T) {
	observer := &recordingAuditObserver{}
	router := setupRouterWithAudit(observer)

	gaugeValue := 8.8
	counterDelta := int64(3)
	requestMetrics := []models.Metrics{
		{
			ID:    "auditGauge",
			MType: models.Gauge,
			Value: &gaugeValue,
		},
		{
			ID:    "auditCounter",
			MType: models.Counter,
			Delta: &counterDelta,
		},
	}

	body, err := gojson.Marshal(requestMetrics)
	if err != nil {
		t.Fatalf("failed to marshal request body: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/updates", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "192.168.0.42:12345"
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rr.Code)
	}

	if len(observer.events) != 1 {
		t.Fatalf("expected 1 audit event, got %d", len(observer.events))
	}

	event := observer.events[0]
	if event.IPAddress != "192.168.0.42" {
		t.Fatalf("expected IP %q, got %q", "192.168.0.42", event.IPAddress)
	}
	if len(event.Metrics) != 2 {
		t.Fatalf("expected 2 metrics in audit event, got %d", len(event.Metrics))
	}
	if event.Metrics[0] != "auditGauge" || event.Metrics[1] != "auditCounter" {
		t.Fatalf("unexpected metric names in audit event: %#v", event.Metrics)
	}
	if event.TS == 0 {
		t.Fatal("expected non-zero audit timestamp")
	}
}

func TestGzipResponseForHTML(t *testing.T) {
	router := setupRouter()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rr.Code)
	}
	if got := rr.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("expected Content-Encoding gzip, got %q", got)
	}

	decompressed := gunzipBytes(t, rr.Body.Bytes())
	if !strings.Contains(string(decompressed), "<h1>Metrics</h1>") {
		t.Fatalf("expected html body with metrics title, got %q", string(decompressed))
	}
}

func TestNoGzipForUnsupportedContentType(t *testing.T) {
	router := setupRouter()

	req := httptest.NewRequest(http.MethodPost, "/update/gauge/plainGauge/1.5", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rr.Code)
	}
	if got := rr.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("expected empty Content-Encoding for text/plain response, got %q", got)
	}
}

func TestPing(t *testing.T) {
	tests := []struct {
		name       string
		pinger     Pinger
		wantStatus int
	}{
		{
			name:       "db not configured",
			pinger:     nil,
			wantStatus: http.StatusInternalServerError,
		},
		{
			name:       "ping ok",
			pinger:     &mockPinger{},
			wantStatus: http.StatusOK,
		},
		{
			name:       "ping fails",
			pinger:     &mockPinger{err: errors.New("db unavailable")},
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := setupRouterWithPinger(tt.pinger)
			req := httptest.NewRequest(http.MethodGet, "/ping", nil)
			rr := httptest.NewRecorder()

			router.ServeHTTP(rr, req)

			if rr.Code != tt.wantStatus {
				t.Fatalf("expected status %d, got %d", tt.wantStatus, rr.Code)
			}
		})
	}
}

func gzipBytes(t *testing.T, body []byte) []byte {
	t.Helper()

	var buf bytes.Buffer
	writer := gzip.NewWriter(&buf)
	if _, err := writer.Write(body); err != nil {
		t.Fatalf("failed to gzip body: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("failed to close gzip writer: %v", err)
	}

	return buf.Bytes()
}

func gunzipBytes(t *testing.T, body []byte) []byte {
	t.Helper()

	reader, err := gzip.NewReader(bytes.NewReader(body))
	if err != nil {
		t.Fatalf("failed to create gzip reader: %v", err)
	}
	defer func() {
		_ = reader.Close()
	}()

	decoded, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("failed to read gzipped body: %v", err)
	}

	return decoded
}
