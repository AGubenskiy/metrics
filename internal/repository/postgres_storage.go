package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	models "github.com/AGubenskiy/metrics/internal/model"
)

const queryTimeout = 3 * time.Second

const (
	upsertGaugeQuery = `INSERT INTO metrics (metric_id, metric_type, gauge_value)
	 VALUES ($1, $2, $3)
	 ON CONFLICT (metric_id, metric_type)
	 DO UPDATE SET gauge_value = EXCLUDED.gauge_value, updated_at = NOW()`
	upsertCounterQuery = `INSERT INTO metrics (metric_id, metric_type, counter_value)
	 VALUES ($1, $2, $3)
	 ON CONFLICT (metric_id, metric_type)
	 DO UPDATE SET counter_value = metrics.counter_value + EXCLUDED.counter_value, updated_at = NOW()`
)

type PostgresStorage struct {
	db *sql.DB
}

func NewPostgresStorage(db *sql.DB) *PostgresStorage {
	return &PostgresStorage{db: db}
}

func (s *PostgresStorage) UpdateGauge(name string, value float64) error {
	ctx, cancel := context.WithTimeout(context.Background(), queryTimeout)
	defer cancel()

	return updateGauge(ctx, s.db, name, value)
}

func (s *PostgresStorage) UpdateCounter(name string, value int64) error {
	ctx, cancel := context.WithTimeout(context.Background(), queryTimeout)
	defer cancel()

	return updateCounter(ctx, s.db, name, value)
}

func (s *PostgresStorage) UpdateMetrics(metrics []models.Metrics) (err error) {
	if len(metrics) == 0 {
		return nil
	}

	for _, metric := range metrics {
		if err = validateMetric(metric); err != nil {
			return err
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), queryTimeout)
	defer cancel()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	for _, metric := range metrics {
		switch metric.MType {
		case models.Gauge:
			err = updateGauge(ctx, tx, metric.ID, *metric.Value)
		case models.Counter:
			err = updateCounter(ctx, tx, metric.ID, *metric.Delta)
		}
		if err != nil {
			return err
		}
	}

	err = tx.Commit()
	return err
}

func (s *PostgresStorage) GetGauge(name string) (float64, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), queryTimeout)
	defer cancel()

	var value float64
	err := s.db.QueryRowContext(
		ctx,
		`SELECT gauge_value FROM metrics WHERE metric_id = $1 AND metric_type = $2`,
		name,
		models.Gauge,
	).Scan(&value)
	if err != nil {
		return 0, false
	}

	return value, true
}

func (s *PostgresStorage) GetCounter(name string) (int64, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), queryTimeout)
	defer cancel()

	var value int64
	err := s.db.QueryRowContext(
		ctx,
		`SELECT counter_value FROM metrics WHERE metric_id = $1 AND metric_type = $2`,
		name,
		models.Counter,
	).Scan(&value)
	if err != nil {
		return 0, false
	}

	return value, true
}

func (s *PostgresStorage) GetAll() (map[string]float64, map[string]int64) {
	ctx, cancel := context.WithTimeout(context.Background(), queryTimeout)
	defer cancel()

	rows, err := s.db.QueryContext(
		ctx,
		`SELECT metric_id, metric_type, gauge_value, counter_value FROM metrics`,
	)
	if err != nil {
		return map[string]float64{}, map[string]int64{}
	}
	defer func() {
		_ = rows.Close()
	}()

	gauges := make(map[string]float64)
	counters := make(map[string]int64)

	for rows.Next() {
		var (
			name         string
			metricType   string
			gaugeValue   sql.NullFloat64
			counterValue sql.NullInt64
		)

		if err = rows.Scan(&name, &metricType, &gaugeValue, &counterValue); err != nil {
			return map[string]float64{}, map[string]int64{}
		}

		switch metricType {
		case models.Gauge:
			if gaugeValue.Valid {
				gauges[name] = gaugeValue.Float64
			}
		case models.Counter:
			if counterValue.Valid {
				counters[name] = counterValue.Int64
			}
		}
	}

	if err = rows.Err(); err != nil {
		return map[string]float64{}, map[string]int64{}
	}

	return gauges, counters
}

type execContexter interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

func updateGauge(ctx context.Context, execer execContexter, name string, value float64) error {
	_, err := execer.ExecContext(
		ctx,
		upsertGaugeQuery,
		name,
		models.Gauge,
		value,
	)
	return err
}

func updateCounter(ctx context.Context, execer execContexter, name string, value int64) error {
	_, err := execer.ExecContext(
		ctx,
		upsertCounterQuery,
		name,
		models.Counter,
		value,
	)
	return err
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
