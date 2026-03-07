package agent

import (
	"bytes"
	"compress/gzip"
	"errors"
	models "github.com/AGubenskiy/metrics/internal/model"
	gojson "github.com/goccy/go-json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"
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
		if r.URL.Path != "/updates/" {
			t.Fatalf("expected path /updates/, got %s", r.URL.Path)
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

		var batch []models.Metrics
		if err := gojson.NewDecoder(gzipReader).Decode(&batch); err != nil {
			t.Fatalf("failed to decode request body: %v", err)
		}

		mu.Lock()
		metrics = append(metrics, batch...)
		mu.Unlock()

		var responseBody bytes.Buffer
		responseWriter := gzip.NewWriter(&responseBody)
		if err := gojson.NewEncoder(responseWriter).Encode(map[string]string{"status": "ok"}); err != nil {
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

func TestReportFallsBackToSingleMetricEndpoint(t *testing.T) {
	var (
		mu            sync.Mutex
		batchRequests int
		singleMetrics []models.Metrics
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

		switch r.URL.Path {
		case "/updates/":
			mu.Lock()
			batchRequests++
			mu.Unlock()
			w.WriteHeader(http.StatusNotFound)
		case "/update":
			gzipReader, err := gzip.NewReader(r.Body)
			if err != nil {
				t.Fatalf("failed to create gzip reader: %v", err)
			}
			defer func() {
				_ = gzipReader.Close()
			}()

			var m models.Metrics
			if err = gojson.NewDecoder(gzipReader).Decode(&m); err != nil {
				t.Fatalf("failed to decode single metric request body: %v", err)
			}

			mu.Lock()
			singleMetrics = append(singleMetrics, m)
			mu.Unlock()
			w.WriteHeader(http.StatusOK)
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	a := NewAgent(srv.URL, 10, 5)
	gv := 1.5
	a.gauges["Alloc"] = gv
	a.counters["PollCount"] = 2

	a.report()

	mu.Lock()
	defer mu.Unlock()

	if batchRequests != 1 {
		t.Fatalf("expected one batch request, got %d", batchRequests)
	}
	if len(singleMetrics) != 2 {
		t.Fatalf("expected 2 fallback metric requests, got %d", len(singleMetrics))
	}
}

func TestSendCompressedJSONWithRetry_RetriesTemporaryNetworkErrors(t *testing.T) {
	attempts := 0

	a := NewAgent("http://example.com", 10, 5)
	a.retryDelays = []time.Duration{0, 0, 0}
	a.sleep = func(time.Duration) {}
	a.httpClient = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			attempts++
			if attempts <= 2 {
				return nil, &url.Error{
					Op:  req.Method,
					URL: req.URL.String(),
					Err: temporaryNetError{},
				}
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(bytes.NewReader(nil)),
				Request:    req,
			}, nil
		}),
	}

	statusCode, err := a.sendCompressedJSONWithRetry("/update", []byte(`{"id":"Alloc","type":"gauge","value":1}`))
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if statusCode != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, statusCode)
	}
	if attempts != 3 {
		t.Fatalf("expected 3 attempts, got %d", attempts)
	}
}

func TestSendCompressedJSONWithRetry_DoesNotRetryNonRetriableErrors(t *testing.T) {
	attempts := 0

	a := NewAgent("http://example.com", 10, 5)
	a.retryDelays = []time.Duration{0, 0, 0}
	a.sleep = func(time.Duration) {}
	a.httpClient = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			attempts++
			return nil, errors.New("non-retriable error")
		}),
	}

	statusCode, err := a.sendCompressedJSONWithRetry("/update", []byte(`{"id":"Alloc","type":"gauge","value":1}`))
	if err == nil {
		t.Fatal("expected non-nil error")
	}
	if statusCode != 0 {
		t.Fatalf("expected status 0 on error, got %d", statusCode)
	}
	if attempts != 1 {
		t.Fatalf("expected 1 attempt, got %d", attempts)
	}
}

type roundTripFunc func(req *http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type temporaryNetError struct{}

func (temporaryNetError) Error() string {
	return "temporary network error"
}

func (temporaryNetError) Timeout() bool {
	return true
}

func (temporaryNetError) Temporary() bool {
	return true
}
