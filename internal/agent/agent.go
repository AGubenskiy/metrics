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
	"time"

	models "github.com/AGubenskiy/metrics/internal/model"
	gojson "github.com/goccy/go-json"
	"go.uber.org/zap"
)

type Agent struct {
	serverAddr     string
	pollInterval   time.Duration
	reportInterval time.Duration

	gauges      map[string]float64
	counters    map[string]int64
	httpClient  *http.Client // перенес в структуру
	logger      *zap.Logger
	retryDelays []time.Duration
	sleep       func(time.Duration)
}

func NewAgent(serverAddr string, reportInterval int, pollInterval int) *Agent {
	return &Agent{
		serverAddr:     serverAddr,
		pollInterval:   time.Duration(pollInterval) * time.Second,
		reportInterval: time.Duration(reportInterval) * time.Second,
		gauges:         make(map[string]float64),
		counters:       make(map[string]int64),
		//создаем агента
		httpClient: &http.Client{
			Timeout: 5 * time.Second,
		},
		logger:      zap.NewNop(),
		retryDelays: []time.Duration{1 * time.Second, 3 * time.Second, 5 * time.Second},
		sleep:       time.Sleep,
	}
}

func (a *Agent) SetLogger(logger *zap.Logger) {
	if logger == nil {
		return
	}
	a.logger = logger
}

func (a *Agent) Run(ctx context.Context) {
	pollTicker := time.NewTicker(a.pollInterval)
	reportTicker := time.NewTicker(a.reportInterval)
	defer pollTicker.Stop()
	defer reportTicker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		//ожидание что в любой канал тикера придет значение
		case <-pollTicker.C:
			a.poll()
		case <-reportTicker.C:

			a.report()
		}
	}

}
func (a *Agent) poll() {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

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
	metrics := make([]models.Metrics, 0, len(a.gauges)+len(a.counters))

	for name, value := range a.gauges {
		gaugeValue := value
		metrics = append(metrics, models.Metrics{
			ID:    name,
			MType: models.Gauge,
			Value: &gaugeValue,
		})
	}

	for name, value := range a.counters {
		counterDelta := value
		metrics = append(metrics, models.Metrics{
			ID:    name,
			MType: models.Counter,
			Delta: &counterDelta,
		})
	}

	return metrics
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
