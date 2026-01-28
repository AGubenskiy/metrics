package storage

import "sync"

type Storage interface {
	UpdateGauge(name string, value float64) error
	UpdateCounter(name string, value int64) error
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
