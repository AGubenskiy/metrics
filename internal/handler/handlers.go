package handler

import (
	"bytes"
	"context"
	"errors"
	"html/template"
	"log"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/AGubenskiy/metrics/internal/audit"
	models "github.com/AGubenskiy/metrics/internal/model"
	"github.com/go-chi/chi/v5"
	gojson "github.com/goccy/go-json"
)

var (
	errMetricNameRequired    = errors.New("metric name is required")
	errGaugeValueRequired    = errors.New("gauge value is required")
	errCounterDeltaRequired  = errors.New("counter delta is required")
	errUnsupportedMetricType = errors.New("unsupported metric type")
)

type Handler struct {
	service        MetricsService
	pinger         Pinger
	auditPublisher AuditPublisher
}

type gaugeMetricRow struct {
	Name  string
	Value float64
}

type counterMetricRow struct {
	Name  string
	Value int64
}

type metricsPageData struct {
	Gauges   []gaugeMetricRow
	Counters []counterMetricRow
}

var metricsPageTmpl = template.Must(template.New("metrics-page").Parse(`
<!DOCTYPE html>
<html lang="en">
<head>
	<meta charset="UTF-8">
	<title>Metrics</title>
	<style>
		body {
			font-family: Arial, sans-serif;
			padding: 10px;
		}
		h1 {
			margin-bottom: 10px;
		}
		table {
			border-collapse: collapse;
			margin-bottom: 30px;
			min-width: 400px;
		}
		th, td {
			border: 1px solid #ccc;
			padding: 8px 12px;
			text-align: left;
		}
		th {
			background-color: #CEE0CC;
		}
	</style>
</head>
<body>
<h1>Metrics</h1>

<h2>Gauge</h2>
<table><tr><th>Name</th><th>Value</th></tr>
{{range .Gauges}}
	<tr><td>{{.Name}}</td><td>{{printf "%f" .Value}}</td></tr>
{{end}}
</table>

<h2>Counter</h2>
<table><tr><th>Name</th><th>Value</th></tr>
{{range .Counters}}
	<tr><td>{{.Name}}</td><td>{{.Value}}</td></tr>
{{end}}
</table>
</body>
</html>
`))

func NewHandler(s MetricsService) *Handler {
	return newHandler(s, nil, nil)
}

func NewHandlerWithPinger(s MetricsService, pinger Pinger) *Handler {
	return newHandler(s, pinger, nil)
}

func NewHandlerWithAudit(s MetricsService, pinger Pinger, publisher AuditPublisher) *Handler {
	return newHandler(s, pinger, publisher)
}

func newHandler(s MetricsService, pinger Pinger, publisher AuditPublisher) *Handler {
	if publisher == nil {
		publisher = audit.NewPublisher()
	}

	return &Handler{
		service:        s,
		pinger:         pinger,
		auditPublisher: publisher,
	}
}

func (h *Handler) UpdateMetric(w http.ResponseWriter, r *http.Request) {
	metricType := chi.URLParam(r, "type")
	metricName := chi.URLParam(r, "name")
	metricValue := chi.URLParam(r, "value")

	if metricName == "" {
		http.NotFound(w, r)
		return
	}

	switch metricType {
	case models.Gauge:
		value, err := strconv.ParseFloat(metricValue, 64)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if err = h.service.UpdateGauge(r.Context(), metricName, value); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

	case models.Counter:
		value, err := strconv.ParseInt(metricValue, 10, 64)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if err = h.service.UpdateCounter(r.Context(), metricName, value); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

	default:
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	h.publishAudit(metricNamesFromString(metricName), requestIPAddress(r))

	w.WriteHeader(http.StatusOK)
	if _, err := w.Write([]byte("OK")); err != nil {
		return
	}
}

func (h *Handler) UpdateMetricJSON(w http.ResponseWriter, r *http.Request) {
	defer func() {
		_ = r.Body.Close()
	}()

	var metric models.Metrics
	if err := gojson.NewDecoder(r.Body).Decode(&metric); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if metric.ID == "" {
		http.Error(w, "metric name is required", http.StatusNotFound)
		return
	}

	if statusCode, err := validateUpdateMetric(metric); err != nil {
		http.Error(w, err.Error(), statusCode)
		return
	}

	switch metric.MType {
	case models.Gauge:
		if err := h.service.UpdateGauge(r.Context(), metric.ID, *metric.Value); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		value, _ := h.service.GetGauge(r.Context(), metric.ID)
		metric.Value = &value
		metric.Delta = nil
	case models.Counter:
		if err := h.service.UpdateCounter(r.Context(), metric.ID, *metric.Delta); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		delta, _ := h.service.GetCounter(r.Context(), metric.ID)
		metric.Delta = &delta
		metric.Value = nil
	}

	h.publishAudit(metricNamesFromString(metric.ID), requestIPAddress(r))

	w.Header().Set("Content-Type", "application/json")
	if err := gojson.NewEncoder(w).Encode(metric); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func (h *Handler) UpdateMetricsJSON(w http.ResponseWriter, r *http.Request) {
	defer func() {
		_ = r.Body.Close()
	}()

	var metrics []models.Metrics
	if err := gojson.NewDecoder(r.Body).Decode(&metrics); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if len(metrics) == 0 {
		http.Error(w, "metrics batch is empty", http.StatusBadRequest)
		return
	}

	for _, metric := range metrics {
		if statusCode, err := validateUpdateMetric(metric); err != nil {
			http.Error(w, err.Error(), statusCode)
			return
		}
	}

	if err := h.service.UpdateMetrics(r.Context(), metrics); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	h.publishAudit(metricNamesFromBatch(metrics), requestIPAddress(r))

	w.WriteHeader(http.StatusOK)
}

// Инкремент 3
func (h *Handler) GetMetricValue(w http.ResponseWriter, r *http.Request) {
	metricType := chi.URLParam(r, "type")
	metricName := chi.URLParam(r, "name")

	switch metricType {
	case models.Gauge:
		value, ok := h.service.GetGauge(r.Context(), metricName)
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte(strconv.FormatFloat(value, 'f', -1, 64))); err != nil {
			return
		}

	case models.Counter:
		value, ok := h.service.GetCounter(r.Context(), metricName)
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte(strconv.FormatInt(value, 10))); err != nil {
			return
		}

	default:
		w.WriteHeader(http.StatusBadRequest)
	}
}

func (h *Handler) GetMetricValueJSON(w http.ResponseWriter, r *http.Request) {
	defer func() {
		_ = r.Body.Close()
	}()

	var metric models.Metrics
	if err := gojson.NewDecoder(r.Body).Decode(&metric); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if metric.ID == "" {
		http.NotFound(w, r)
		return
	}

	switch metric.MType {
	case models.Gauge:
		value, ok := h.service.GetGauge(r.Context(), metric.ID)
		if !ok {
			http.NotFound(w, r)
			return
		}
		metric.Value = &value
		metric.Delta = nil
	case models.Counter:
		value, ok := h.service.GetCounter(r.Context(), metric.ID)
		if !ok {
			http.NotFound(w, r)
			return
		}
		metric.Delta = &value
		metric.Value = nil
	default:
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := gojson.NewEncoder(w).Encode(metric); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func (h *Handler) Ping(w http.ResponseWriter, r *http.Request) {
	if h.pinger == nil {
		http.Error(w, "database connection is not configured", http.StatusInternalServerError)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), time.Second)
	defer cancel()

	if err := h.pinger.PingContext(ctx); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (h *Handler) GetAllMetrics(w http.ResponseWriter, r *http.Request) {
	gauges, counters := h.service.GetAll(r.Context())

	gaugeNames := make([]string, 0, len(gauges))
	for name := range gauges {
		gaugeNames = append(gaugeNames, name)
	}
	sort.Strings(gaugeNames)

	counterNames := make([]string, 0, len(counters))
	for name := range counters {
		counterNames = append(counterNames, name)
	}
	sort.Strings(counterNames)

	pageData := metricsPageData{
		Gauges:   make([]gaugeMetricRow, 0, len(gaugeNames)),
		Counters: make([]counterMetricRow, 0, len(counterNames)),
	}
	for _, name := range gaugeNames {
		pageData.Gauges = append(pageData.Gauges, gaugeMetricRow{
			Name:  name,
			Value: gauges[name],
		})
	}
	for _, name := range counterNames {
		pageData.Counters = append(pageData.Counters, counterMetricRow{
			Name:  name,
			Value: counters[name],
		})
	}

	var buf bytes.Buffer
	if err := metricsPageTmpl.Execute(&buf, pageData); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(buf.Bytes()); err != nil {
		return
	}
}

func validateUpdateMetric(metric models.Metrics) (int, error) {
	if metric.ID == "" {
		return http.StatusNotFound, errMetricNameRequired
	}

	switch metric.MType {
	case models.Gauge:
		if metric.Value == nil {
			return http.StatusBadRequest, errGaugeValueRequired
		}
	case models.Counter:
		if metric.Delta == nil {
			return http.StatusBadRequest, errCounterDeltaRequired
		}
	default:
		return http.StatusBadRequest, errUnsupportedMetricType
	}

	return http.StatusOK, nil
}

func (h *Handler) publishAudit(metricNames []string, ipAddress string) {
	if len(metricNames) == 0 {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	event := audit.Event{
		TS:        time.Now().Unix(),
		Metrics:   metricNames,
		IPAddress: ipAddress,
	}

	if err := h.auditPublisher.Notify(ctx, event); err != nil {
		log.Printf("cannot publish audit event: %v", err)
	}
}

func metricNamesFromString(metricName string) []string {
	if metricName == "" {
		return nil
	}
	return []string{metricName}
}

func metricNamesFromBatch(metrics []models.Metrics) []string {
	names := make([]string, 0, len(metrics))
	for _, metric := range metrics {
		if metric.ID != "" {
			names = append(names, metric.ID)
		}
	}
	return names
}

func requestIPAddress(r *http.Request) string {
	if r == nil {
		return ""
	}

	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err == nil {
		return host
	}

	return strings.TrimSpace(r.RemoteAddr)
}
