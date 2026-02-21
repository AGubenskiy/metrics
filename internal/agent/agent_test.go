package agent

import (
	"bytes"
	"compress/gzip"
	models "github.com/AGubenskiy/metrics/internal/model"
	gojson "github.com/goccy/go-json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

func TestPollIncrementsCounter(t *testing.T) {
	a := NewAgent("http://localhost:8080", 10, 5)

	a.poll()
	a.poll()

	if a.counters["PollCount"] != 2 {
		t.Fatalf("expected PollCount = 2, got %d", a.counters["PollCount"])
	}
}

func TestReportSendsJSONToUpdateEndpoint(t *testing.T) {
	var (
		mu      sync.Mutex
		metrics []models.Metrics
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected method POST, got %s", r.Method)
		}
		if r.URL.Path != "/update" {
			t.Fatalf("expected path /update, got %s", r.URL.Path)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Fatalf("expected Content-Type application/json, got %q", got)
		}
		if got := r.Header.Get("Content-Encoding"); got != "gzip" {
			t.Fatalf("expected Content-Encoding gzip, got %q", got)
		}
		if got := r.Header.Get("Accept-Encoding"); got != "gzip" {
			t.Fatalf("expected Accept-Encoding gzip, got %q", got)
		}

		defer func() {
			_ = r.Body.Close()
		}()

		gzipReader, err := gzip.NewReader(r.Body)
		if err != nil {
			t.Fatalf("failed to create gzip reader: %v", err)
		}
		defer func() {
			_ = gzipReader.Close()
		}()

		var m models.Metrics
		if err := gojson.NewDecoder(gzipReader).Decode(&m); err != nil {
			t.Fatalf("failed to decode request body: %v", err)
		}

		mu.Lock()
		metrics = append(metrics, m)
		mu.Unlock()

		var responseBody bytes.Buffer
		responseWriter := gzip.NewWriter(&responseBody)
		if err := gojson.NewEncoder(responseWriter).Encode(m); err != nil {
			t.Fatalf("failed to encode gzip response body: %v", err)
		}
		if err := responseWriter.Close(); err != nil {
			t.Fatalf("failed to close gzip response writer: %v", err)
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Encoding", "gzip")
		w.WriteHeader(http.StatusOK)
		if _, err := io.Copy(w, &responseBody); err != nil {
			t.Fatalf("failed to write gzip response: %v", err)
		}
	}))
	defer srv.Close()

	a := NewAgent(srv.URL, 10, 5)
	gv := 12.5
	a.gauges["Alloc"] = gv
	a.counters["PollCount"] = 3

	a.report()

	mu.Lock()
	defer mu.Unlock()

	if len(metrics) != 2 {
		t.Fatalf("expected 2 metrics sent, got %d", len(metrics))
	}

	foundGauge := false
	foundCounter := false
	for _, m := range metrics {
		if m.ID == "Alloc" && m.MType == models.Gauge && m.Value != nil && *m.Value == gv {
			foundGauge = true
		}
		if m.ID == "PollCount" && m.MType == models.Counter && m.Delta != nil && *m.Delta == 3 {
			foundCounter = true
		}
	}

	if !foundGauge || !foundCounter {
		t.Fatalf("sent metrics mismatch: %+v", metrics)
	}
}
