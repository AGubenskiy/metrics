package agent

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AGubenskiy/metrics/internal/cryptoutil"
	models "github.com/AGubenskiy/metrics/internal/model"
	pb "github.com/AGubenskiy/metrics/internal/proto"
	"github.com/AGubenskiy/metrics/internal/signing"
	gojson "github.com/goccy/go-json"
	"github.com/shirou/gopsutil/v3/mem"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

func runReportWithWorkerPool(t *testing.T, a *Agent) {
	t.Helper()

	jobsBufferSize := len(a.collectMetricsBatch()) + 1
	if jobsBufferSize < a.rateLimit {
		jobsBufferSize = a.rateLimit
	}

	jobs := make(chan sendJob, jobsBufferSize)
	var workersWG sync.WaitGroup
	for i := 0; i < a.rateLimit; i++ {
		workersWG.Add(1)
		go a.runWorker(jobs, &workersWG)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	a.reportWithWorkerPool(ctx, jobs)
	close(jobs)
	workersWG.Wait()
}

func TestPollIncrementsCounter(t *testing.T) {
	a := NewAgent("http://localhost:8080", 10, 5, "")

	a.poll()
	a.poll()

	if a.counters["PollCount"] != 2 {
		t.Fatalf("expected PollCount = 2, got %d", a.counters["PollCount"])
	}
}

func TestPollSystemCollectsGaugeMetrics(t *testing.T) {
	a := NewAgent("http://localhost:8080", 10, 5, "")
	a.readVirtualMemory = func() (*mem.VirtualMemoryStat, error) {
		return &mem.VirtualMemoryStat{
			Total: 1024,
			Free:  256,
		}, nil
	}
	a.readCPUPercent = func() ([]float64, error) {
		return []float64{11.5, 27.25}, nil
	}

	a.pollSystem()

	a.mu.RLock()
	defer a.mu.RUnlock()

	if got := a.gauges["TotalMemory"]; got != 1024 {
		t.Fatalf("expected TotalMemory=1024, got %v", got)
	}
	if got := a.gauges["FreeMemory"]; got != 256 {
		t.Fatalf("expected FreeMemory=256, got %v", got)
	}
	if got := a.gauges["CPUutilization1"]; got != 11.5 {
		t.Fatalf("expected CPUutilization1=11.5, got %v", got)
	}
	if got := a.gauges["CPUutilization2"]; got != 27.25 {
		t.Fatalf("expected CPUutilization2=27.25, got %v", got)
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
		if got := r.Header.Get(signing.HeaderName); got != "" {
			t.Fatalf("expected empty %s header, got %q", signing.HeaderName, got)
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

	a := NewAgent(srv.URL, 10, 5, "")
	gv := 12.5
	a.gauges["Alloc"] = gv
	a.counters["PollCount"] = 3

	runReportWithWorkerPool(t, a)

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

func TestReportSetsRealIPHeader(t *testing.T) {
	var gotRealIP string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotRealIP = r.Header.Get(realIPHeader)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	a := NewAgent(srv.URL, 10, 5, "")
	a.gauges["Alloc"] = 1

	runReportWithWorkerPool(t, a)

	if gotRealIP == "" {
		t.Fatalf("expected %s header to be set", realIPHeader)
	}
	if ip := net.ParseIP(gotRealIP); ip == nil {
		t.Fatalf("expected %s header to contain IP address, got %q", realIPHeader, gotRealIP)
	}
}

func TestReportSendsGRPCBatchWithRealIPMetadata(t *testing.T) {
	client := &captureMetricsClient{}
	a := NewAgent("http://localhost:8080", 10, 5, "")
	a.SetGRPCClient(client, "localhost:9091")

	gaugeValue := 12.5
	counterDelta := int64(3)
	result := a.sendMetricsBatch([]models.Metrics{
		{ID: "Alloc", MType: models.Gauge, Value: &gaugeValue},
		{ID: "PollCount", MType: models.Counter, Delta: &counterDelta},
	})

	if !result.success {
		t.Fatal("expected gRPC batch send to succeed")
	}
	if client.req == nil {
		t.Fatal("expected gRPC request")
	}
	if len(client.req.GetMetrics()) != 2 {
		t.Fatalf("expected 2 metrics, got %d", len(client.req.GetMetrics()))
	}
	if got := client.req.GetMetrics()[0]; got.GetId() != "Alloc" || got.GetType() != pb.Metric_GAUGE || got.GetValue() != gaugeValue {
		t.Fatalf("unexpected gauge metric: %+v", got)
	}
	if got := client.req.GetMetrics()[1]; got.GetId() != "PollCount" || got.GetType() != pb.Metric_COUNTER || got.GetDelta() != counterDelta {
		t.Fatalf("unexpected counter metric: %+v", got)
	}

	realIPValues := client.md.Get("x-real-ip")
	if len(realIPValues) != 1 {
		t.Fatalf("expected x-real-ip metadata, got %v", realIPValues)
	}
	if ip := net.ParseIP(realIPValues[0]); ip == nil {
		t.Fatalf("expected x-real-ip metadata to contain IP address, got %q", realIPValues[0])
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

	a := NewAgent(srv.URL, 10, 5, "")
	gv := 1.5
	a.gauges["Alloc"] = gv
	a.counters["PollCount"] = 2

	runReportWithWorkerPool(t, a)

	mu.Lock()
	defer mu.Unlock()

	if batchRequests != 1 {
		t.Fatalf("expected one batch request, got %d", batchRequests)
	}
	if len(singleMetrics) != 2 {
		t.Fatalf("expected 2 fallback metric requests, got %d", len(singleMetrics))
	}
}

func TestReportWithWorkerPoolRespectsRateLimit(t *testing.T) {
	var (
		currentRequests int32
		maxRequests     int32
		requestsCount   int32
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/updates/" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.URL.Path != "/update" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}

		active := atomic.AddInt32(&currentRequests, 1)
		defer atomic.AddInt32(&currentRequests, -1)
		atomic.AddInt32(&requestsCount, 1)

		for {
			previous := atomic.LoadInt32(&maxRequests)
			if active <= previous || atomic.CompareAndSwapInt32(&maxRequests, previous, active) {
				break
			}
		}

		time.Sleep(50 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	a := NewAgentWithRateLimit(srv.URL, 10, 5, 2, "")
	for i := 0; i < 6; i++ {
		value := float64(i + 1)
		a.gauges[fmt.Sprintf("gauge-%d", i)] = value
	}

	jobs := make(chan sendJob, 12)
	var workersWG sync.WaitGroup
	for i := 0; i < a.rateLimit; i++ {
		workersWG.Add(1)
		go a.runWorker(jobs, &workersWG)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	a.reportWithWorkerPool(ctx, jobs)
	close(jobs)
	workersWG.Wait()

	if got := atomic.LoadInt32(&maxRequests); got > 2 {
		t.Fatalf("expected at most 2 concurrent requests, got %d", got)
	}
	if got := atomic.LoadInt32(&requestsCount); got != 6 {
		t.Fatalf("expected 6 single metric requests after fallback, got %d", got)
	}
}

func TestRunSendsPendingMetricsOnShutdown(t *testing.T) {
	var (
		mu      sync.Mutex
		metrics []models.Metrics
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/updates/" {
			t.Fatalf("unexpected path %s", r.URL.Path)
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
		if err = gojson.NewDecoder(gzipReader).Decode(&batch); err != nil {
			t.Fatalf("failed to decode request body: %v", err)
		}

		mu.Lock()
		metrics = append(metrics, batch...)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	a := NewAgent(srv.URL, 3600, 3600, "")
	a.readMemStats = func(stats *runtime.MemStats) {
		stats.Alloc = 42
	}
	a.poll()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	a.Run(ctx)

	mu.Lock()
	defer mu.Unlock()

	for _, metric := range metrics {
		if metric.ID == "PollCount" && metric.MType == models.Counter && metric.Delta != nil && *metric.Delta == 1 {
			return
		}
	}
	t.Fatalf("expected pending PollCount metric to be sent on shutdown, got %+v", metrics)
}

func TestRunDoesNotResendAlreadyReportedMetricsOnShutdown(t *testing.T) {
	var batchRequests int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/updates/" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}

		atomic.AddInt32(&batchRequests, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	a := NewAgent(srv.URL, 3600, 3600, "")
	a.readMemStats = func(stats *runtime.MemStats) {
		stats.Alloc = 42
	}
	a.poll()
	runReportWithWorkerPool(t, a)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	a.Run(ctx)

	if got := atomic.LoadInt32(&batchRequests); got != 1 {
		t.Fatalf("expected already reported metrics not to be resent on shutdown, got %d batch requests", got)
	}
}

func TestRunRetriesPendingMetricsAfterFailedReportOnShutdown(t *testing.T) {
	var batchRequests int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/updates/" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}

		requestNumber := atomic.AddInt32(&batchRequests, 1)
		if requestNumber == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	a := NewAgent(srv.URL, 3600, 3600, "")
	a.readMemStats = func(stats *runtime.MemStats) {
		stats.Alloc = 42
	}
	a.poll()
	runReportWithWorkerPool(t, a)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	a.Run(ctx)

	if got := atomic.LoadInt32(&batchRequests); got != 2 {
		t.Fatalf("expected failed report to be retried on shutdown, got %d batch requests", got)
	}
}

func TestSendCompressedJSONWithRetry_RetriesTemporaryNetworkErrors(t *testing.T) {
	attempts := 0

	a := NewAgent("http://example.com", 10, 5, "")
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

	a := NewAgent("http://example.com", 10, 5, "")
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

func TestReportSignsRequestBodyWhenKeyConfigured(t *testing.T) {
	var receivedHash string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHash = r.Header.Get(signing.HeaderName)

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

		body, err := io.ReadAll(gzipReader)
		if err != nil {
			t.Fatalf("failed to read request body: %v", err)
		}

		expectedHash := signing.Hash(body, "test-key")
		if receivedHash != expectedHash {
			t.Fatalf("expected %s %q, got %q", signing.HeaderName, expectedHash, receivedHash)
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	a := NewAgent(srv.URL, 10, 5, "test-key")
	value := 3.14
	a.gauges["Alloc"] = value

	runReportWithWorkerPool(t, a)

	if receivedHash == "" {
		t.Fatalf("expected %s header to be set", signing.HeaderName)
	}
}

func TestReportEncryptsRequestBodyWhenPublicKeyConfigured(t *testing.T) {
	privateKey := generateAgentTestPrivateKey(t)
	var decryptedBody []byte

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get(cryptoutil.HeaderName); got != cryptoutil.HeaderValue {
			t.Fatalf("expected %s %q, got %q", cryptoutil.HeaderName, cryptoutil.HeaderValue, got)
		}

		encryptedBody, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("failed to read encrypted body: %v", err)
		}
		if bytes.Contains(encryptedBody, []byte("Alloc")) {
			t.Fatalf("encrypted body contains plaintext metric name")
		}

		compressedBody, err := cryptoutil.Decrypt(encryptedBody, privateKey)
		if err != nil {
			t.Fatalf("failed to decrypt body: %v", err)
		}
		gzipReader, err := gzip.NewReader(bytes.NewReader(compressedBody))
		if err != nil {
			t.Fatalf("failed to create gzip reader: %v", err)
		}
		defer func() {
			_ = gzipReader.Close()
		}()

		decryptedBody, err = io.ReadAll(gzipReader)
		if err != nil {
			t.Fatalf("failed to read decrypted body: %v", err)
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	a := NewAgent(srv.URL, 10, 5, "")
	a.SetEncryptionPublicKey(&privateKey.PublicKey)
	value := 3.14
	a.gauges["Alloc"] = value

	runReportWithWorkerPool(t, a)

	var batch []models.Metrics
	if err := gojson.Unmarshal(decryptedBody, &batch); err != nil {
		t.Fatalf("failed to decode decrypted JSON: %v", err)
	}
	if len(batch) != 1 || batch[0].ID != "Alloc" {
		t.Fatalf("unexpected decrypted metrics batch: %+v", batch)
	}
}

func TestPollUsesInjectedRuntimeReader(t *testing.T) {
	a := NewAgent("http://localhost:8080", 10, 5, "")
	a.readMemStats = func(stats *runtime.MemStats) {
		stats.Alloc = 42
		stats.TotalAlloc = 99
	}

	a.poll()

	a.mu.RLock()
	defer a.mu.RUnlock()

	if got := a.gauges["Alloc"]; got != 42 {
		t.Fatalf("expected Alloc=42, got %v", got)
	}
	if got := a.gauges["TotalAlloc"]; got != 99 {
		t.Fatalf("expected TotalAlloc=99, got %v", got)
	}
}

type roundTripFunc func(req *http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type captureMetricsClient struct {
	req *pb.UpdateMetricsRequest
	md  metadata.MD
}

func (c *captureMetricsClient) UpdateMetrics(ctx context.Context, in *pb.UpdateMetricsRequest, _ ...grpc.CallOption) (*pb.UpdateMetricsResponse, error) {
	c.req = in
	if md, ok := metadata.FromOutgoingContext(ctx); ok {
		c.md = md.Copy()
	}
	return &pb.UpdateMetricsResponse{}, nil
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

func generateAgentTestPrivateKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate RSA key: %v", err)
	}
	return privateKey
}
