package handler

import (
	"bytes"
	models "github.com/AGubenskiy/metrics/internal/model"
	"github.com/go-chi/chi/v5"
	gojson "github.com/goccy/go-json"
	"html/template"
	"net/http"
	"sort"
	"strconv"
)

type Handler struct {
	storage Storage
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

func NewHandler(s Storage) *Handler {
	return &Handler{storage: s}
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
	case "gauge":
		value, err := strconv.ParseFloat(metricValue, 64)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if err = h.storage.UpdateGauge(metricName, value); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

	case "counter":
		value, err := strconv.ParseInt(metricValue, 10, 64)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if err = h.storage.UpdateCounter(metricName, value); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

	default:
		w.WriteHeader(http.StatusBadRequest)
		return
	}

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

	switch metric.MType {
	case models.Gauge:
		if metric.Value == nil {
			http.Error(w, "gauge value is required", http.StatusBadRequest)
			return
		}
		if err := h.storage.UpdateGauge(metric.ID, *metric.Value); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		value, _ := h.storage.GetGauge(metric.ID)
		metric.Value = &value
		metric.Delta = nil
	case models.Counter:
		if metric.Delta == nil {
			http.Error(w, "counter delta is required", http.StatusBadRequest)
			return
		}
		if err := h.storage.UpdateCounter(metric.ID, *metric.Delta); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		delta, _ := h.storage.GetCounter(metric.ID)
		metric.Delta = &delta
		metric.Value = nil
	default:
		http.Error(w, "unsupported metric type", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := gojson.NewEncoder(w).Encode(metric); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

// Инкремент 3
func (h *Handler) GetMetricValue(w http.ResponseWriter, r *http.Request) {
	metricType := chi.URLParam(r, "type")
	metricName := chi.URLParam(r, "name")

	switch metricType {
	case "gauge":
		value, ok := h.storage.GetGauge(metricName)
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte(strconv.FormatFloat(value, 'f', -1, 64))); err != nil {
			return
		}

	case "counter":
		value, ok := h.storage.GetCounter(metricName)
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
		value, ok := h.storage.GetGauge(metric.ID)
		if !ok {
			http.NotFound(w, r)
			return
		}
		metric.Value = &value
		metric.Delta = nil
	case models.Counter:
		value, ok := h.storage.GetCounter(metric.ID)
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
func (h *Handler) GetAllMetrics(w http.ResponseWriter, _ *http.Request) {
	gauges, counters := h.storage.GetAll()

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
