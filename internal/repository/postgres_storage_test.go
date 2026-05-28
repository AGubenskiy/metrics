package repository

import (
	"errors"
	"testing"
	"time"

	"github.com/lib/pq"
)

func TestIsRetriablePostgresError(t *testing.T) {
	if !isRetriablePostgresError(&pq.Error{Code: "08006"}) {
		t.Fatal("expected Class 08 error to be retriable")
	}

	if isRetriablePostgresError(&pq.Error{Code: "23505"}) {
		t.Fatal("expected non-Class 08 error to be non-retriable")
	}
}

func TestRetry_RetriesClass08Errors(t *testing.T) {
	storage := &PostgresStorage{
		retryDelays: []time.Duration{0, 0, 0},
		sleep:       func(time.Duration) {},
	}

	attempts := 0
	err := storage.retry(func() error {
		attempts++
		if attempts <= 2 {
			return &pq.Error{Code: "08006"}
		}
		return nil
	})

	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if attempts != 3 {
		t.Fatalf("expected 3 attempts, got %d", attempts)
	}
}

func TestRetry_StopsOnNonRetriableError(t *testing.T) {
	storage := &PostgresStorage{
		retryDelays: []time.Duration{0, 0, 0},
		sleep:       func(time.Duration) {},
	}

	attempts := 0
	nonRetriableErr := errors.New("no retriable")
	err := storage.retry(func() error {
		attempts++
		return nonRetriableErr
	})

	if !errors.Is(err, nonRetriableErr) {
		t.Fatalf("expected %v, got %v", nonRetriableErr, err)
	}
	if attempts != 1 {
		t.Fatalf("expected 1 attempt, got %d", attempts)
	}
}
