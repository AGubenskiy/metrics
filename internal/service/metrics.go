package service

import (
	"context"

	models "github.com/AGubenskiy/metrics/internal/model"
)

type Repository interface {
	UpdateGauge(ctx context.Context, name string, value float64) error
	UpdateCounter(ctx context.Context, name string, value int64) error
	UpdateMetrics(ctx context.Context, metrics []models.Metrics) error
	GetGauge(ctx context.Context, name string) (float64, bool)
	GetCounter(ctx context.Context, name string) (int64, bool)
	GetAll(ctx context.Context) (map[string]float64, map[string]int64)
}

type Metrics struct {
	repo Repository
}

func NewMetrics(repo Repository) *Metrics {
	return &Metrics{repo: repo}
}

func (s *Metrics) UpdateGauge(ctx context.Context, name string, value float64) error {
	return s.repo.UpdateGauge(ctx, name, value)
}

func (s *Metrics) UpdateCounter(ctx context.Context, name string, value int64) error {
	return s.repo.UpdateCounter(ctx, name, value)
}

func (s *Metrics) UpdateMetrics(ctx context.Context, metrics []models.Metrics) error {
	return s.repo.UpdateMetrics(ctx, metrics)
}

func (s *Metrics) GetGauge(ctx context.Context, name string) (float64, bool) {
	return s.repo.GetGauge(ctx, name)
}

func (s *Metrics) GetCounter(ctx context.Context, name string) (int64, bool) {
	return s.repo.GetCounter(ctx, name)
}

func (s *Metrics) GetAll(ctx context.Context) (map[string]float64, map[string]int64) {
	return s.repo.GetAll(ctx)
}
