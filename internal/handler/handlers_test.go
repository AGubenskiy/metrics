package handler

import (
	"github.com/go-chi/chi/v5"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/AGubenskiy/metrics/internal/storage"
)

func setupRouter() http.Handler {
	store := storage.NewMemStorage()
	h := NewHandler(store)

	r := chi.NewRouter()
	r.Post("/update/{type}/{name}/{value}", h.UpdateMetric)
	r.Get("/value/{type}/{name}", h.GetMetricValue)
	r.Get("/", h.GetAllMetrics)

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

	// добавим пару метрик
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
