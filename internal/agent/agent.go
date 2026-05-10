package agent

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	models "github.com/AGubenskiy/metrics/internal/model"
	"github.com/AGubenskiy/metrics/internal/signing"
	gojson "github.com/goccy/go-json"
	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/mem"
	"go.uber.org/zap"
)

// Agent collects runtime and system metrics and reports them to the metrics server.
type Agent struct {
	serverAddr     string
	pollInterval   time.Duration
	reportInterval time.Duration
	rateLimit      int

	mu          sync.RWMutex
	gauges      map[string]float64
	counters    map[string]int64
	httpClient  *http.Client
	logger      *zap.Logger
	retryDelays []time.Duration
	sleep       func(time.Duration)
	key         string

	readMemStats      func(*runtime.MemStats)
	readVirtualMemory func() (*mem.VirtualMemoryStat, error)
	readCPUPercent    func() ([]float64, error)

	batchDisabled atomic.Bool
}

// NewAgent creates an agent with single-request reporting mode.
func NewAgent(serverAddr string, reportInterval int, pollInterval int, key string) *Agent {
	return NewAgentWithRateLimit(serverAddr, reportInterval, pollInterval, 1, key)
}

// NewAgentWithRateLimit creates an agent with a bounded number of concurrent outgoing requests.
func NewAgentWithRateLimit(serverAddr string, reportInterval int, pollInterval int, rateLimit int, key string) *Agent {
	if rateLimit <= 0 {
		rateLimit = 1
	}

	return &Agent{
		serverAddr:     serverAddr,
		pollInterval:   time.Duration(pollInterval) * time.Second,
		reportInterval: time.Duration(reportInterval) * time.Second,
		rateLimit:      rateLimit,
		gauges:         make(map[string]float64),
		counters:       make(map[string]int64),
		httpClient: &http.Client{
			Timeout: 5 * time.Second,
		},
		logger:       zap.NewNop(),
		retryDelays:  []time.Duration{1 * time.Second, 3 * time.Second, 5 * time.Second},
		sleep:        time.Sleep,
		key:          key,
		readMemStats: runtime.ReadMemStats,
		readVirtualMemory: func() (*mem.VirtualMemoryStat, error) {
			return mem.VirtualMemory()
		},
		readCPUPercent: func() ([]float64, error) {
			return cpu.Percent(0, true)
		},
	}
}

// SetLogger overrides the logger used by the agent.
func (a *Agent) SetLogger(logger *zap.Logger) {
	if logger == nil {
		return
	}
	a.logger = logger
}

// Run starts metric collection and reporting loops and blocks until ctx is cancelled.
func (a *Agent) Run(ctx context.Context) {
	jobs := make(chan sendJob, a.rateLimit*2)

	var workersWG sync.WaitGroup
	for i := 0; i < a.rateLimit; i++ {
		workersWG.Add(1)
		go a.runWorker(jobs, &workersWG)
	}

	var loopsWG sync.WaitGroup
	loopsWG.Add(3)
	go a.runRuntimeCollector(ctx, &loopsWG)
	go a.runSystemCollector(ctx, &loopsWG)
	go a.runReporter(ctx, jobs, &loopsWG)

	loopsWG.Wait()
	close(jobs)
	workersWG.Wait()
}

func (a *Agent) poll() {
	var m runtime.MemStats
	a.readMemStats(&m)

	a.mu.Lock()
	defer a.mu.Unlock()

	a.gauges["Alloc"] = float64(m.Alloc)
	a.gauges["BuckHashSys"] = float64(m.BuckHashSys)
	a.gauges["Frees"] = float64(m.Frees)
	a.gauges["GCCPUFraction"] = m.GCCPUFraction
	a.gauges["GCSys"] = float64(m.GCSys)
	a.gauges["HeapAlloc"] = float64(m.HeapAlloc)
	a.gauges["HeapIdle"] = float64(m.HeapIdle)
	a.gauges["HeapInuse"] = float64(m.HeapInuse)
	a.gauges["HeapObjects"] = float64(m.HeapObjects)
	a.gauges["HeapReleased"] = float64(m.HeapReleased)
	a.gauges["HeapSys"] = float64(m.HeapSys)
	a.gauges["LastGC"] = float64(m.LastGC)
	a.gauges["Lookups"] = float64(m.Lookups)
	a.gauges["MCacheInuse"] = float64(m.MCacheInuse)
	a.gauges["MCacheSys"] = float64(m.MCacheSys)
	a.gauges["MSpanInuse"] = float64(m.MSpanInuse)
	a.gauges["MSpanSys"] = float64(m.MSpanSys)
	a.gauges["Mallocs"] = float64(m.Mallocs)
	a.gauges["NextGC"] = float64(m.NextGC)
	a.gauges["NumForcedGC"] = float64(m.NumForcedGC)
	a.gauges["NumGC"] = float64(m.NumGC)
	a.gauges["OtherSys"] = float64(m.OtherSys)
	a.gauges["PauseTotalNs"] = float64(m.PauseTotalNs)
	a.gauges["StackInuse"] = float64(m.StackInuse)
	a.gauges["StackSys"] = float64(m.StackSys)
	a.gauges["Sys"] = float64(m.Sys)
	a.gauges["TotalAlloc"] = float64(m.TotalAlloc)
	a.gauges["RandomValue"] = rand.Float64()
	a.counters["PollCount"]++
}

func (a *Agent) pollSystem() {
	virtualMemory, err := a.readVirtualMemory()
	if err != nil {
		a.logger.Error("failed to collect virtual memory metrics", zap.Error(err))
		return
	}

	cpuPercentages, err := a.readCPUPercent()
	if err != nil {
		a.logger.Error("failed to collect cpu utilization metrics", zap.Error(err))
		return
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	a.gauges["TotalMemory"] = float64(virtualMemory.Total)
	a.gauges["FreeMemory"] = float64(virtualMemory.Free)

	for name := range a.gauges {
		if strings.HasPrefix(name, "CPUutilization") {
			delete(a.gauges, name)
		}
	}

	for i, value := range cpuPercentages {
		a.gauges[fmt.Sprintf("CPUutilization%d", i+1)] = value
	}
}

func (a *Agent) report() {
	metrics := a.collectMetricsBatch()
	if len(metrics) == 0 {
		return
	}

	if a.sendMetricsBatch(metrics) {
		for _, metric := range metrics {
			a.sendMetric(metric)
		}
	}
}

func (a *Agent) collectMetricsBatch() []models.Metrics {
	a.mu.RLock()
	defer a.mu.RUnlock()

	metrics := make([]models.Metrics, 0, len(a.gauges)+len(a.counters))
	gaugeValues := make([]float64, len(a.gauges))
	gaugeIndex := 0

	for name, value := range a.gauges {
		gaugeValues[gaugeIndex] = value
		metrics = append(metrics, models.Metrics{
			ID:    name,
			MType: models.Gauge,
			Value: &gaugeValues[gaugeIndex],
		})
		gaugeIndex++
	}

	counterValues := make([]int64, len(a.counters))
	counterIndex := 0
	for name, value := range a.counters {
		counterValues[counterIndex] = value
		metrics = append(metrics, models.Metrics{
			ID:    name,
			MType: models.Counter,
			Delta: &counterValues[counterIndex],
		})
		counterIndex++
	}

	return metrics
}

func (a *Agent) runRuntimeCollector(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()

	ticker := time.NewTicker(a.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.poll()
		}
	}
}

func (a *Agent) runSystemCollector(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()

	ticker := time.NewTicker(a.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.pollSystem()
		}
	}
}

func (a *Agent) runReporter(ctx context.Context, jobs chan<- sendJob, wg *sync.WaitGroup) {
	defer wg.Done()

	ticker := time.NewTicker(a.reportInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.reportWithWorkerPool(ctx, jobs)
		}
	}
}

func (a *Agent) reportWithWorkerPool(ctx context.Context, jobs chan<- sendJob) {
	metrics := a.collectMetricsBatch()
	if len(metrics) == 0 {
		return
	}

	if !a.batchDisabled.Load() {
		resultCh := make(chan sendResult, 1)
		if !a.enqueueJob(ctx, jobs, sendJob{batch: metrics, result: resultCh}) {
			return
		}

		select {
		case <-ctx.Done():
			return
		case result := <-resultCh:
			if result.fallbackToSingle {
				a.batchDisabled.Store(true)
				a.enqueueSingleMetricJobs(ctx, jobs, metrics)
			}
		}

		return
	}

	a.enqueueSingleMetricJobs(ctx, jobs, metrics)
}

func (a *Agent) enqueueSingleMetricJobs(ctx context.Context, jobs chan<- sendJob, metrics []models.Metrics) {
	for _, metric := range metrics {
		metricCopy := metric
		if !a.enqueueJob(ctx, jobs, sendJob{metric: &metricCopy}) {
			return
		}
	}
}

func (a *Agent) enqueueJob(ctx context.Context, jobs chan<- sendJob, job sendJob) bool {
	select {
	case <-ctx.Done():
		return false
	case jobs <- job:
		return true
	}
}

func (a *Agent) runWorker(jobs <-chan sendJob, wg *sync.WaitGroup) {
	defer wg.Done()

	for job := range jobs {
		result := a.processJob(job)
		if job.result != nil {
			job.result <- result
		}
	}
}

func (a *Agent) processJob(job sendJob) sendResult {
	if job.metric != nil {
		a.sendMetric(*job.metric)
		return sendResult{}
	}

	return sendResult{fallbackToSingle: a.sendMetricsBatch(job.batch)}
}

// sendMetricsBatch returns true when caller should fallback to old single-metric API.
func (a *Agent) sendMetricsBatch(metrics []models.Metrics) bool {
	logFields := []zap.Field{
		zap.Int("metrics_count", len(metrics)),
	}

	body, err := gojson.Marshal(metrics)
	if err != nil {
		a.logger.Error("failed to marshal metrics batch", append(logFields, zap.Error(err))...)
		return false
	}

	statusCode, err := a.sendCompressedJSONWithRetry("/updates/", body)
	if err != nil {
		a.logger.Error("failed to send metrics batch", append(logFields, zap.Error(err))...)
		return false
	}

	if statusCode == http.StatusNotFound || statusCode == http.StatusMethodNotAllowed {
		a.logger.Warn(
			"batch endpoint is not supported, fallback to single metric updates",
			append(logFields, zap.Int("status_code", statusCode))...,
		)
		return true
	}

	if statusCode != http.StatusOK {
		a.logger.Warn(
			"server returned not OK status for metrics batch",
			append(logFields, zap.Int("status_code", statusCode))...,
		)
	}

	return false
}

func (a *Agent) sendMetric(metric models.Metrics) {
	logFields := []zap.Field{
		zap.String("metric_id", metric.ID),
		zap.String("metric_type", metric.MType),
	}

	body, err := gojson.Marshal(metric)
	if err != nil {
		a.logger.Error("failed to marshal metric", append(logFields, zap.Error(err))...)
		return
	}

	statusCode, err := a.sendCompressedJSONWithRetry("/update", body)
	if err != nil {
		a.logger.Error("failed to send metric", append(logFields, zap.Error(err))...)
		return
	}

	if statusCode != http.StatusOK {
		a.logger.Warn("server returned not OK status", append(logFields, zap.Int("status_code", statusCode))...)
	}
}

func (a *Agent) sendCompressedJSON(path string, body []byte) (int, error) {
	compressedBody, err := gzipPayload(body)
	if err != nil {
		return 0, err
	}

	endpointURL := a.buildURL(path)
	req, err := http.NewRequest(http.MethodPost, endpointURL, bytes.NewReader(compressedBody))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Content-Encoding", "gzip")
	req.Header.Set("Accept-Encoding", "gzip")
	if a.key != "" {
		req.Header.Set(signing.HeaderName, signing.Hash(body, a.key))
	}

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	if resp == nil {
		return 0, fmt.Errorf("received nil response for url %s", endpointURL)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	responseBody := io.Reader(resp.Body)
	if hasGzipEncoding(resp.Header.Get("Content-Encoding")) {
		gzipReader, gzipErr := gzip.NewReader(resp.Body)
		if gzipErr != nil {
			return 0, gzipErr
		}
		defer func() {
			_ = gzipReader.Close()
		}()
		responseBody = gzipReader
	}

	if _, err = io.Copy(io.Discard, responseBody); err != nil {
		return 0, err
	}

	return resp.StatusCode, nil
}

func (a *Agent) sendCompressedJSONWithRetry(path string, body []byte) (int, error) {
	statusCode, err := a.sendCompressedJSON(path, body)
	if err == nil || !isRetriableAgentError(err) {
		return statusCode, err
	}

	lastErr := err
	for _, delay := range a.retryDelays {
		a.sleep(delay)
		statusCode, err = a.sendCompressedJSON(path, body)
		if err == nil {
			return statusCode, nil
		}
		if !isRetriableAgentError(err) {
			return 0, err
		}
		lastErr = err
	}

	return 0, lastErr
}

func (a *Agent) buildURL(path string) string {
	return strings.TrimRight(a.serverAddr, "/") + path
}

func gzipPayload(body []byte) ([]byte, error) {
	var compressedBody bytes.Buffer
	gzipWriter := gzip.NewWriter(&compressedBody)
	if _, err := gzipWriter.Write(body); err != nil {
		closeErr := gzipWriter.Close()
		if closeErr != nil {
			return nil, errors.Join(err, closeErr)
		}
		return nil, err
	}

	if err := gzipWriter.Close(); err != nil {
		return nil, err
	}

	return compressedBody.Bytes(), nil
}

func hasGzipEncoding(headerValue string) bool {
	for _, part := range strings.Split(headerValue, ",") {
		value := strings.TrimSpace(part)
		value = strings.SplitN(value, ";", 2)[0]
		if strings.EqualFold(value, "gzip") {
			return true
		}
	}
	return false
}

func isRetriableAgentError(err error) bool {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return isRetriableAgentError(urlErr.Err)
	}

	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return true
	}

	var netErr net.Error
	if errors.As(err, &netErr) {
		if netErr.Timeout() {
			return true
		}

		type temporary interface {
			Temporary() bool
		}

		if tempErr, ok := any(netErr).(temporary); ok && tempErr.Temporary() {
			return true
		}
	}

	return false
}

type sendJob struct {
	batch  []models.Metrics
	metric *models.Metrics
	result chan sendResult
}

type sendResult struct {
	fallbackToSingle bool
}
