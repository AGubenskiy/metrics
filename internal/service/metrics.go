package service

import (
	"context"

	models "github.com/AGubenskiy/metrics/internal/model"
)

// Repository describes storage operations required by the metrics service.
type Repository interface {
	UpdateGauge(ctx context.Context, name string, value float64) error
	UpdateCounter(ctx context.Context, name string, value int64) error
	UpdateMetrics(ctx context.Context, metrics []models.Metrics) error
	GetGauge(ctx context.Context, name string) (float64, bool)
	GetCounter(ctx context.Context, name string) (int64, bool)
	GetAll(ctx context.Context) (map[string]float64, map[string]int64)
}

// Metrics provides application-level operations over metric storage.
type Metrics struct {
	repo Repository
}

// NewMetrics creates a service backed by repo.
func NewMetrics(repo Repository) *Metrics {
	return &Metrics{repo: repo}
}

// UpdateGauge stores a gauge metric value.
func (s *Metrics) UpdateGauge(ctx context.Context, name string, value float64) error {
	return s.repo.UpdateGauge(ctx, name, value)
}

// UpdateCounter increments a counter metric.
func (s *Metrics) UpdateCounter(ctx context.Context, name string, value int64) error {
	return s.repo.UpdateCounter(ctx, name, value)
}

// UpdateMetrics stores a batch of metrics in one call.
func (s *Metrics) UpdateMetrics(ctx context.Context, metrics []models.Metrics) error {
	return s.repo.UpdateMetrics(ctx, metrics)
}

// GetGauge returns a gauge metric by name.
func (s *Metrics) GetGauge(ctx context.Context, name string) (float64, bool) {
	return s.repo.GetGauge(ctx, name)
}

// GetCounter returns a counter metric by name.
func (s *Metrics) GetCounter(ctx context.Context, name string) (int64, bool) {
	return s.repo.GetCounter(ctx, name)
}

// GetAll returns copies of all stored gauge and counter metrics.
func (s *Metrics) GetAll(ctx context.Context) (map[string]float64, map[string]int64) {
	return s.repo.GetAll(ctx)
}
