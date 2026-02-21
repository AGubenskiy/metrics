package agent

import (
	"bytes"
	"compress/gzip"
	"fmt"
	models "github.com/AGubenskiy/metrics/internal/model"
	gojson "github.com/goccy/go-json"
	"io"
	"math/rand"
	"net/http"
	"runtime"
	"strings"
	"time"
)

type Agent struct {
	serverAddr     string
	pollInterval   time.Duration
	reportInterval time.Duration

	gauges     map[string]float64
	counters   map[string]int64
	httpClient *http.Client // перенес в структуру
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
	}
}

func (a *Agent) Run() {
	pollTicker := time.NewTicker(a.pollInterval)
	reportTicker := time.NewTicker(a.reportInterval)
	for {
		select {
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
	for name, value := range a.gauges {
		gaugeValue := value
		a.sendMetric(models.Metrics{
			ID:    name,
			MType: models.Gauge,
			Value: &gaugeValue,
		})
	}

	for name, value := range a.counters {
		counterDelta := value
		a.sendMetric(models.Metrics{
			ID:    name,
			MType: models.Counter,
			Delta: &counterDelta,
		})
	}
}

func (a *Agent) sendMetric(metric models.Metrics) {
	body, err := gojson.Marshal(metric)
	if err != nil {
		return
	}

	var compressedBody bytes.Buffer
	gzipWriter := gzip.NewWriter(&compressedBody)
	if _, err = gzipWriter.Write(body); err != nil {
		return
	}
	if err = gzipWriter.Close(); err != nil {
		return
	}

	url := fmt.Sprintf("%s/update", a.serverAddr)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(compressedBody.Bytes()))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Content-Encoding", "gzip")
	req.Header.Set("Accept-Encoding", "gzip")

	resp, err := a.httpClient.Do(req)
	if err != nil || resp == nil {
		return
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	responseBody := io.Reader(resp.Body)
	if hasGzipEncoding(resp.Header.Get("Content-Encoding")) {
		gzipReader, gzipErr := gzip.NewReader(resp.Body)
		if gzipErr != nil {
			return
		}
		defer func() {
			_ = gzipReader.Close()
		}()
		responseBody = gzipReader
	}

	if _, err = io.Copy(io.Discard, responseBody); err != nil {
		return
	}
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
