package storage

import (
	models "github.com/AGubenskiy/metrics/internal/model"
	gojson "github.com/goccy/go-json"
	"os"
	"path/filepath"
	"testing"
)

func TestCounterAccumulation(t *testing.T) {
	s := NewMemStorage()

	s.UpdateCounter("c", 10)
	s.UpdateCounter("c", 5)

	if s.counters["c"] != 15 {
		t.Fatalf("expected 15, got %d", s.counters["c"])
	}
}

func TestSaveAndLoadFromFile(t *testing.T) {
	s := NewMemStorage()
	if err := s.UpdateGauge("Alloc", 12.5); err != nil {
		t.Fatalf("failed to update gauge: %v", err)
	}
	if err := s.UpdateCounter("PollCount", 3); err != nil {
		t.Fatalf("failed to update counter: %v", err)
	}

	filePath := filepath.Join(t.TempDir(), "metrics.json")
	if err := s.SaveToFile(filePath); err != nil {
		t.Fatalf("failed to save metrics: %v", err)
	}

	restored := NewMemStorage()
	if err := restored.LoadFromFile(filePath); err != nil {
		t.Fatalf("failed to load metrics: %v", err)
	}

	gauge, ok := restored.GetGauge("Alloc")
	if !ok || gauge != 12.5 {
		t.Fatalf("expected gauge Alloc=12.5, got %v (ok=%v)", gauge, ok)
	}

	counter, ok := restored.GetCounter("PollCount")
	if !ok || counter != 3 {
		t.Fatalf("expected counter PollCount=3, got %v (ok=%v)", counter, ok)
	}
}

func TestLoadFromMissingFileDoesNotFail(t *testing.T) {
	s := NewMemStorage()
	missingPath := filepath.Join(t.TempDir(), "missing.json")

	if err := s.LoadFromFile(missingPath); err != nil {
		t.Fatalf("expected no error for missing file, got %v", err)
	}
}

func TestSyncSaveOnUpdate(t *testing.T) {
	s := NewMemStorage()
	filePath := filepath.Join(t.TempDir(), "sync-metrics.json")

	s.SetSyncSaveOnUpdate(func() error {
		return s.SaveToFile(filePath)
	})

	if err := s.UpdateGauge("RandomValue", 99.9); err != nil {
		t.Fatalf("failed to update gauge with sync save: %v", err)
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("failed to read saved file: %v", err)
	}

	var metrics []models.Metrics
	if err = gojson.Unmarshal(data, &metrics); err != nil {
		t.Fatalf("failed to decode saved metrics: %v", err)
	}
	if len(metrics) != 1 {
		t.Fatalf("expected one metric in file, got %d", len(metrics))
	}
	if metrics[0].ID != "RandomValue" || metrics[0].MType != models.Gauge || metrics[0].Value == nil {
		t.Fatalf("unexpected saved metric: %+v", metrics[0])
	}
}
