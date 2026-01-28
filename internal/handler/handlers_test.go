package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/AGubenskiy/metrics/internal/storage"
)

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
			url:        "/update/gauge/temperature/36.6",
			wantStatus: http.StatusOK,
		},
		{
			name:       "valid counter",
			method:     http.MethodPost,
			url:        "/update/counter/requests/10",
			wantStatus: http.StatusOK,
		},
		{
			name:       "unknown metric type",
			method:     http.MethodPost,
			url:        "/update/unknown/test/1",
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
			url:        "/update/counter/test/1.5",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "missing metric name",
			method:     http.MethodPost,
			url:        "/update/gauge//10",
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "missing value",
			method:     http.MethodPost,
			url:        "/update/gauge/test/",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "wrong http method",
			method:     http.MethodGet,
			url:        "/update/gauge/test/10",
			wantStatus: http.StatusMethodNotAllowed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := storage.NewMemStorage()
			h := NewHandler(store)

			req := httptest.NewRequest(tt.method, tt.url, nil)
			w := httptest.NewRecorder()

			h.UpdateMetric(w, req)

			resp := w.Result()
			defer resp.Body.Close()

			if resp.StatusCode != tt.wantStatus {
				t.Errorf(
					"expected status %d, got %d",
					tt.wantStatus,
					resp.StatusCode,
				)
			}
		})
	}
}
