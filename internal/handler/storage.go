package handler

import (
	"context"

	models "github.com/AGubenskiy/metrics/internal/model"
)

type MetricsService interface {
	UpdateGauge(ctx context.Context, name string, value float64) error
	UpdateCounter(ctx context.Context, name string, value int64) error
	UpdateMetrics(ctx context.Context, metrics []models.Metrics) error
	GetGauge(ctx context.Context, name string) (float64, bool)
	GetCounter(ctx context.Context, name string) (int64, bool)
	GetAll(ctx context.Context) (map[string]float64, map[string]int64)
}

type Pinger interface {
	PingContext(ctx context.Context) error
}
