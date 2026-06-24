package agent

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rsa"
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

	"github.com/AGubenskiy/metrics/internal/cryptoutil"
	models "github.com/AGubenskiy/metrics/internal/model"
	pb "github.com/AGubenskiy/metrics/internal/proto"
	"github.com/AGubenskiy/metrics/internal/signing"
	"github.com/AGubenskiy/metrics/internal/trustedsubnet"
	gojson "github.com/goccy/go-json"
	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/mem"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
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
	grpcClient  pb.MetricsClient
	logger      *zap.Logger
	retryDelays []time.Duration
	sleep       func(time.Duration)
	key         string
	publicKey   *rsa.PublicKey

	readMemStats      func(*runtime.MemStats)
	readVirtualMemory func() (*mem.VirtualMemoryStat, error)
	readCPUPercent    func() ([]float64, error)
	realIP            string

	batchDisabled   atomic.Bool
	metricsVersion  atomic.Uint64
	reportedVersion atomic.Uint64
}

const realIPHeader = trustedsubnet.RealIPHeader
const grpcRequestTimeout = 5 * time.Second

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
		realIP: detectHostIP(serverAddr),
	}
}

// SetLogger overrides the logger used by the agent.
func (a *Agent) SetLogger(logger *zap.Logger) {
	if logger == nil {
		return
	}
	a.logger = logger
}

// SetEncryptionPublicKey enables encryption for outgoing request bodies.
func (a *Agent) SetEncryptionPublicKey(key *rsa.PublicKey) {
	a.publicKey = key
}

// SetGRPCClient enables gRPC batch reporting.
func (a *Agent) SetGRPCClient(client pb.MetricsClient, serverAddr string) {
	if client == nil {
		return
	}
	a.grpcClient = client
	if ip := detectHostIP(serverAddr); ip != "" {
		a.realIP = ip
	}
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
	a.reportPending(context.Background(), jobs)
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
	a.metricsVersion.Add(1)
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
	a.metricsVersion.Add(1)
}

func (a *Agent) report() {
	metrics, version := a.collectMetricsSnapshot()
	if len(metrics) == 0 {
		return
	}

	result := a.sendMetricsBatch(metrics)
	if result.fallbackToSingle {
		allSent := true
		for _, metric := range metrics {
			if !a.sendMetric(metric) {
				allSent = false
			}
		}
		if !allSent {
			return
		}
	} else if !result.success {
		return
	}
	a.markReported(version)
}

func (a *Agent) collectMetricsBatch() []models.Metrics {
	metrics, _ := a.collectMetricsSnapshot()
	return metrics
}

func (a *Agent) collectMetricsSnapshot() ([]models.Metrics, uint64) {
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

	return metrics, a.metricsVersion.Load()
}

func (a *Agent) reportPending(ctx context.Context, jobs chan<- sendJob) {
	if a.metricsVersion.Load() == a.reportedVersion.Load() {
		return
	}
	a.reportWithWorkerPool(ctx, jobs)
}

func (a *Agent) markReported(version uint64) {
	for {
		current := a.reportedVersion.Load()
		if current >= version {
			return
		}
		if a.reportedVersion.CompareAndSwap(current, version) {
			return
		}
	}
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
			a.reportWithWorkerPool(context.Background(), jobs)
		}
	}
}

func (a *Agent) reportWithWorkerPool(ctx context.Context, jobs chan<- sendJob) bool {
	metrics, version := a.collectMetricsSnapshot()
	if len(metrics) == 0 {
		a.markReported(version)
		return true
	}

	if !a.batchDisabled.Load() {
		resultCh := make(chan sendResult, 1)
		if !a.enqueueJob(ctx, jobs, sendJob{batch: metrics, result: resultCh}) {
			return false
		}

		select {
		case <-ctx.Done():
			return false
		case result := <-resultCh:
			if result.fallbackToSingle {
				a.batchDisabled.Store(true)
				if !a.enqueueSingleMetricJobs(ctx, jobs, metrics) {
					return false
				}
			} else if !result.success {
				return false
			}
		}

		a.markReported(version)
		return true
	}

	if !a.enqueueSingleMetricJobs(ctx, jobs, metrics) {
		return false
	}
	a.markReported(version)
	return true
}

func (a *Agent) enqueueSingleMetricJobs(ctx context.Context, jobs chan<- sendJob, metrics []models.Metrics) bool {
	resultCh := make(chan sendResult, len(metrics))
	for _, metric := range metrics {
		metricCopy := metric
		if !a.enqueueJob(ctx, jobs, sendJob{metric: &metricCopy, result: resultCh}) {
			return false
		}
	}

	allSent := true
	for range metrics {
		select {
		case <-ctx.Done():
			return false
		case result := <-resultCh:
			if !result.success {
				allSent = false
			}
		}
	}
	return allSent
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
		return sendResult{success: a.sendMetric(*job.metric)}
	}

	return a.sendMetricsBatch(job.batch)
}

// sendMetricsBatch returns whether the batch was delivered or should fallback to old single-metric API.
func (a *Agent) sendMetricsBatch(metrics []models.Metrics) sendResult {
	if a.grpcClient != nil {
		return a.sendMetricsBatchGRPC(metrics)
	}

	logFields := []zap.Field{
		zap.Int("metrics_count", len(metrics)),
	}

	body, err := gojson.Marshal(metrics)
	if err != nil {
		a.logger.Error("failed to marshal metrics batch", append(logFields, zap.Error(err))...)
		return sendResult{}
	}

	statusCode, err := a.sendCompressedJSONWithRetry("/updates/", body)
	if err != nil {
		a.logger.Error("failed to send metrics batch", append(logFields, zap.Error(err))...)
		return sendResult{}
	}

	if statusCode == http.StatusNotFound || statusCode == http.StatusMethodNotAllowed {
		a.logger.Warn(
			"batch endpoint is not supported, fallback to single metric updates",
			append(logFields, zap.Int("status_code", statusCode))...,
		)
		return sendResult{fallbackToSingle: true}
	}

	if statusCode != http.StatusOK {
		a.logger.Warn(
			"server returned not OK status for metrics batch",
			append(logFields, zap.Int("status_code", statusCode))...,
		)
		return sendResult{}
	}

	return sendResult{success: true}
}

func (a *Agent) sendMetricsBatchGRPC(metrics []models.Metrics) sendResult {
	logFields := []zap.Field{
		zap.Int("metrics_count", len(metrics)),
	}

	req, err := buildUpdateMetricsRequest(metrics)
	if err != nil {
		a.logger.Error("failed to build gRPC metrics batch", append(logFields, zap.Error(err))...)
		return sendResult{}
	}

	if err = a.updateMetricsGRPCWithRetry(req); err != nil {
		a.logger.Error("failed to send gRPC metrics batch", append(logFields, zap.Error(err))...)
		return sendResult{}
	}

	return sendResult{success: true}
}

func (a *Agent) updateMetricsGRPCWithRetry(req *pb.UpdateMetricsRequest) error {
	err := a.updateMetricsGRPC(req)
	if err == nil || !isRetriableGRPCError(err) {
		return err
	}

	lastErr := err
	for _, delay := range a.retryDelays {
		a.sleep(delay)
		err = a.updateMetricsGRPC(req)
		if err == nil {
			return nil
		}
		if !isRetriableGRPCError(err) {
			return err
		}
		lastErr = err
	}

	return lastErr
}

func (a *Agent) updateMetricsGRPC(req *pb.UpdateMetricsRequest) error {
	ctx, cancel := context.WithTimeout(context.Background(), grpcRequestTimeout)
	defer cancel()

	if a.realIP != "" {
		ctx = metadata.AppendToOutgoingContext(ctx, trustedsubnet.RealIPMetadataKey, a.realIP)
	}

	_, err := a.grpcClient.UpdateMetrics(ctx, req)
	return err
}

func buildUpdateMetricsRequest(metrics []models.Metrics) (*pb.UpdateMetricsRequest, error) {
	protoMetrics := make([]*pb.Metric, 0, len(metrics))

	for _, metric := range metrics {
		protoMetric := pb.Metric_builder{Id: metric.ID}

		switch metric.MType {
		case models.Gauge:
			if metric.Value == nil {
				return nil, fmt.Errorf("gauge value is required")
			}
			protoMetric.Type = pb.Metric_GAUGE
			protoMetric.Value = *metric.Value
		case models.Counter:
			if metric.Delta == nil {
				return nil, fmt.Errorf("counter delta is required")
			}
			protoMetric.Type = pb.Metric_COUNTER
			protoMetric.Delta = *metric.Delta
		default:
			return nil, fmt.Errorf("unsupported metric type %q", metric.MType)
		}

		protoMetrics = append(protoMetrics, protoMetric.Build())
	}

	return pb.UpdateMetricsRequest_builder{Metrics: protoMetrics}.Build(), nil
}

func (a *Agent) sendMetric(metric models.Metrics) bool {
	logFields := []zap.Field{
		zap.String("metric_id", metric.ID),
		zap.String("metric_type", metric.MType),
	}

	body, err := gojson.Marshal(metric)
	if err != nil {
		a.logger.Error("failed to marshal metric", append(logFields, zap.Error(err))...)
		return false
	}

	statusCode, err := a.sendCompressedJSONWithRetry("/update", body)
	if err != nil {
		a.logger.Error("failed to send metric", append(logFields, zap.Error(err))...)
		return false
	}

	if statusCode != http.StatusOK {
		a.logger.Warn("server returned not OK status", append(logFields, zap.Int("status_code", statusCode))...)
		return false
	}
	return true
}

func (a *Agent) sendCompressedJSON(path string, body []byte) (int, error) {
	compressedBody, err := gzipPayload(body)
	if err != nil {
		return 0, err
	}
	requestBody := compressedBody
	encrypted := false
	if a.publicKey != nil {
		requestBody, err = cryptoutil.Encrypt(compressedBody, a.publicKey)
		if err != nil {
			return 0, err
		}
		encrypted = true
	}

	endpointURL := a.buildURL(path)
	req, err := http.NewRequest(http.MethodPost, endpointURL, bytes.NewReader(requestBody))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Content-Encoding", "gzip")
	req.Header.Set("Accept-Encoding", "gzip")
	req.Header.Set(realIPHeader, a.realIP)
	if encrypted {
		req.Header.Set(cryptoutil.HeaderName, cryptoutil.HeaderValue)
	}
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

func isRetriableGRPCError(err error) bool {
	switch status.Code(err) {
	case codes.Unavailable, codes.DeadlineExceeded, codes.ResourceExhausted:
		return true
	default:
		return false
	}
}

func detectHostIP(serverAddr string) string {
	if ip := localIPForServer(serverAddr); ip != "" {
		return ip
	}
	if ip := interfaceIP(false); ip != "" {
		return ip
	}
	if ip := interfaceIP(true); ip != "" {
		return ip
	}
	return "127.0.0.1"
}

func localIPForServer(serverAddr string) string {
	addr := strings.TrimSpace(serverAddr)
	parsedURL, err := url.Parse(addr)
	if err != nil {
		return ""
	}

	host := parsedURL.Hostname()
	port := parsedURL.Port()
	if host == "" && !strings.Contains(addr, "://") {
		host, port = splitHostPort(addr)
	}
	if host == "" {
		return ""
	}
	if strings.EqualFold(host, "localhost") {
		return "127.0.0.1"
	}

	targetIP := net.ParseIP(host)
	if targetIP == nil {
		return ""
	}
	if targetIP.IsLoopback() {
		return normalizedIPString(targetIP)
	}

	if port == "" {
		port = defaultPort(parsedURL.Scheme)
	}

	conn, err := net.DialTimeout("udp", net.JoinHostPort(targetIP.String(), port), 100*time.Millisecond)
	if err != nil {
		return ""
	}
	defer func() {
		_ = conn.Close()
	}()

	udpAddr, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok || udpAddr.IP == nil {
		return ""
	}
	return normalizedIPString(udpAddr.IP)
}

func splitHostPort(address string) (string, string) {
	host, port, err := net.SplitHostPort(address)
	if err == nil {
		return host, port
	}

	return strings.Trim(address, "[]"), ""
}

func defaultPort(scheme string) string {
	if strings.EqualFold(scheme, "https") {
		return "443"
	}
	return "80"
}

func interfaceIP(allowLoopback bool) string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}

	for _, addr := range addrs {
		ipNet, ok := addr.(*net.IPNet)
		if !ok || ipNet.IP == nil {
			continue
		}

		ip := ipNet.IP
		if ip.IsLoopback() != allowLoopback {
			continue
		}
		if !allowLoopback && !ip.IsGlobalUnicast() {
			continue
		}
		if ipv4 := ip.To4(); ipv4 != nil {
			return ipv4.String()
		}
	}

	for _, addr := range addrs {
		ipNet, ok := addr.(*net.IPNet)
		if !ok || ipNet.IP == nil {
			continue
		}

		ip := ipNet.IP
		if ip.IsLoopback() != allowLoopback {
			continue
		}
		if !allowLoopback && !ip.IsGlobalUnicast() {
			continue
		}
		return ip.String()
	}

	return ""
}

func normalizedIPString(ip net.IP) string {
	if ipv4 := ip.To4(); ipv4 != nil {
		return ipv4.String()
	}
	return ip.String()
}

type sendJob struct {
	batch  []models.Metrics
	metric *models.Metrics
	result chan sendResult
}

type sendResult struct {
	fallbackToSingle bool
	success          bool
}
