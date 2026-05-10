package storage

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"

	models "github.com/AGubenskiy/metrics/internal/model"
	gojson "github.com/goccy/go-json"
)

type MemStorage struct {
	mu       sync.RWMutex
	gauges   map[string]float64
	counters map[string]int64
	onUpdate func() error
}

func NewMemStorage() *MemStorage {
	return &MemStorage{
		gauges:   make(map[string]float64),
		counters: make(map[string]int64),
	}
}

func (m *MemStorage) UpdateGauge(_ context.Context, name string, value float64) error {
	m.mu.Lock()
	m.gauges[name] = value
	onUpdate := m.onUpdate
	m.mu.Unlock()

	if onUpdate != nil {
		return onUpdate()
	}
	return nil
}

func (m *MemStorage) UpdateCounter(_ context.Context, name string, value int64) error {
	m.mu.Lock()
	m.counters[name] += value
	onUpdate := m.onUpdate
	m.mu.Unlock()

	if onUpdate != nil {
		return onUpdate()
	}
	return nil
}

func (m *MemStorage) UpdateMetrics(_ context.Context, metrics []models.Metrics) error {
	if len(metrics) == 0 {
		return nil
	}

	for _, metric := range metrics {
		if err := validateMetric(metric); err != nil {
			return err
		}
	}

	m.mu.Lock()
	for _, metric := range metrics {
		switch metric.MType {
		case models.Gauge:
			m.gauges[metric.ID] = *metric.Value
		case models.Counter:
			m.counters[metric.ID] += *metric.Delta
		}
	}
	onUpdate := m.onUpdate
	m.mu.Unlock()

	if onUpdate != nil {
		return onUpdate()
	}
	return nil
}

// Инкремент 3
func (m *MemStorage) GetGauge(_ context.Context, name string) (float64, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	value, ok := m.gauges[name]
	return value, ok
}
func (m *MemStorage) GetCounter(_ context.Context, name string) (int64, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	value, ok := m.counters[name]
	return value, ok
}
func (m *MemStorage) GetAll(_ context.Context) (map[string]float64, map[string]int64) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	//return m.gauges, m.counters
	gaugesOut := make(map[string]float64, len(m.gauges))
	for k, v := range m.gauges {
		gaugesOut[k] = v
	}

	countersOut := make(map[string]int64, len(m.counters))
	for k, v := range m.counters {
		countersOut[k] = v
	}

	return gaugesOut, countersOut
}

func (m *MemStorage) SetSyncSaveOnUpdate(callback func() error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onUpdate = callback
}

func (m *MemStorage) SaveToFile(path string) error {
	metrics := m.snapshot()

	data, err := gojson.Marshal(metrics)
	if err != nil {
		return err
	}

	if err = os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	tmpPath := path + ".tmp"
	if err = os.WriteFile(tmpPath, data, 0o644); err != nil {
		return err
	}
	//Наверное можно как то лучше сделать
	// если файл существует — делаем бэкап
	bakPath := path + ".bak"
	if _, statErr := os.Stat(path); statErr == nil {
		//если остался старый.bak удаляю
		_ = os.Remove(bakPath)
		if err = os.Rename(path, bakPath); err != nil {
			//удаляем временный файл и возвращаем ошибку
			_ = os.Remove(tmpPath)
			return err
		}
	}

	// переносим новый файл на место
	if err = os.Rename(tmpPath, path); err != nil {
		// пробуем откатиться, если был бэкап
		//проверяю есть ли bak и возвращаю на место
		if _, statErr := os.Stat(bakPath); statErr == nil {
			_ = os.Rename(bakPath, path)
		}
		_ = os.Remove(tmpPath)
		return err
	}

	// если все прошло хорошо можно удалить бэкап
	_ = os.Remove(bakPath)
	return nil
}

func (m *MemStorage) LoadFromFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	var metrics []models.Metrics
	if len(data) > 0 {
		if err = gojson.Unmarshal(data, &metrics); err != nil {
			return err
		}
	}

	gauges := make(map[string]float64)
	counters := make(map[string]int64)

	for _, metric := range metrics {
		if metric.ID == "" {
			continue
		}

		switch metric.MType {
		case models.Gauge:
			if metric.Value != nil {
				gauges[metric.ID] = *metric.Value
			}
		case models.Counter:
			if metric.Delta != nil {
				counters[metric.ID] = *metric.Delta
			}
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.gauges = gauges
	m.counters = counters
	return nil
}

func (m *MemStorage) snapshot() []models.Metrics {
	m.mu.RLock()
	defer m.mu.RUnlock()

	metrics := make([]models.Metrics, 0, len(m.gauges)+len(m.counters))
	gaugeValues := make([]float64, len(m.gauges))
	gaugeIndex := 0

	for name, value := range m.gauges {
		gaugeValues[gaugeIndex] = value
		metrics = append(metrics, models.Metrics{
			ID:    name,
			MType: models.Gauge,
			Value: &gaugeValues[gaugeIndex],
		})
		gaugeIndex++
	}

	counterValues := make([]int64, len(m.counters))
	counterIndex := 0
	for name, delta := range m.counters {
		counterValues[counterIndex] = delta
		metrics = append(metrics, models.Metrics{
			ID:    name,
			MType: models.Counter,
			Delta: &counterValues[counterIndex],
		})
		counterIndex++
	}

	sort.Slice(metrics, func(i, j int) bool {
		if metrics[i].MType != metrics[j].MType {
			return metrics[i].MType == models.Gauge
		}
		return metrics[i].ID < metrics[j].ID
	})

	return metrics
}

func validateMetric(metric models.Metrics) error {
	if metric.ID == "" {
		return errors.New("metric name is required")
	}

	switch metric.MType {
	case models.Gauge:
		if metric.Value == nil {
			return errors.New("gauge value is required")
		}
	case models.Counter:
		if metric.Delta == nil {
			return errors.New("counter delta is required")
		}
	default:
		return errors.New("unsupported metric type")
	}

	return nil
}
