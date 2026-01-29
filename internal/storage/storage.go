package storage

import "sync"

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
}

func NewMemStorage() *MemStorage {
	return &MemStorage{
		gauges:   make(map[string]float64),
		counters: make(map[string]int64),
	}
}

func (m *MemStorage) UpdateGauge(name string, value float64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.gauges[name] = value
	return nil
}

func (m *MemStorage) UpdateCounter(name string, value int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.counters[name] += value
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
	return m.gauges, m.counters

}
