package handler

import (
	"context"

	models "github.com/AGubenskiy/metrics/internal/model"
)

type Storage interface {
	UpdateGauge(name string, value float64) error
	UpdateCounter(name string, value int64) error
	UpdateMetrics(metrics []models.Metrics) error
	GetGauge(name string) (float64, bool)
	GetCounter(name string) (int64, bool)
	GetAll() (map[string]float64, map[string]int64)
}

type Pinger interface {
	PingContext(ctx context.Context) error
}
