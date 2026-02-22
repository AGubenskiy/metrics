package storage

import (
	"os"
	"path/filepath"
	"sort"
	"sync"

	models "github.com/AGubenskiy/metrics/internal/model"
	gojson "github.com/goccy/go-json"
)

type Storage interface {
	UpdateGauge(name string, value float64) error
	UpdateCounter(name string, value int64) error
	//Инкремент 3
	GetGauge(name string) (float64, bool)
	GetCounter(name string) (int64, bool)
	GetAll() (map[string]float64, map[string]int64)
}

type MemStorage struct {
	mu       sync.Mutex
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

func (m *MemStorage) UpdateGauge(name string, value float64) error {
	m.mu.Lock()
	m.gauges[name] = value
	onUpdate := m.onUpdate
	m.mu.Unlock()

	if onUpdate != nil {
		return onUpdate()
	}
	return nil
}

func (m *MemStorage) UpdateCounter(name string, value int64) error {
	m.mu.Lock()
	m.counters[name] += value
	onUpdate := m.onUpdate
	m.mu.Unlock()

	if onUpdate != nil {
		return onUpdate()
	}
	return nil
}

// Инкремент 3
func (m *MemStorage) GetGauge(name string) (float64, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	value, ok := m.gauges[name]
	return value, ok
}
func (m *MemStorage) GetCounter(name string) (int64, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	value, ok := m.counters[name]
	return value, ok
}
func (m *MemStorage) GetAll() (map[string]float64, map[string]int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
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
	m.mu.Lock()
	defer m.mu.Unlock()

	gaugeNames := make([]string, 0, len(m.gauges))
	for name := range m.gauges {
		gaugeNames = append(gaugeNames, name)
	}
	sort.Strings(gaugeNames)

	counterNames := make([]string, 0, len(m.counters))
	for name := range m.counters {
		counterNames = append(counterNames, name)
	}
	sort.Strings(counterNames)

	metrics := make([]models.Metrics, 0, len(gaugeNames)+len(counterNames))

	for _, name := range gaugeNames {
		value := m.gauges[name]
		metrics = append(metrics, models.Metrics{
			ID:    name,
			MType: models.Gauge,
			Value: &value,
		})
	}

	for _, name := range counterNames {
		delta := m.counters[name]
		metrics = append(metrics, models.Metrics{
			ID:    name,
			MType: models.Counter,
			Delta: &delta,
		})
	}

	return metrics
}
