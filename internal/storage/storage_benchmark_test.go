package storage

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	models "github.com/AGubenskiy/metrics/internal/model"
)

func BenchmarkMemStorageUpdateMetrics(b *testing.B) {
	store := NewMemStorage()
	metrics := makeBenchmarkMetrics(512, 64)
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		if err := store.UpdateMetrics(ctx, metrics); err != nil {
			b.Fatalf("UpdateMetrics failed: %v", err)
		}
	}
}

func BenchmarkMemStorageSnapshotLarge(b *testing.B) {
	store := makeBenchmarkStorage(4096, 512)

	b.ReportAllocs()
	b.ResetTimer()

	total := 0
	for i := 0; i < b.N; i++ {
		total += len(store.snapshot())
	}

	if total == 0 {
		b.Fatal("unexpected zero snapshot size")
	}
}

func BenchmarkMemStorageSaveToFileLarge(b *testing.B) {
	store := makeBenchmarkStorage(4096, 512)
	path := filepath.Join(b.TempDir(), "metrics.json")

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		if err := store.SaveToFile(path); err != nil {
			b.Fatalf("SaveToFile failed: %v", err)
		}
	}
}

func TestProfileMemStorageSnapshotLarge(t *testing.T) {
	if os.Getenv("PROFILE_MEMSTORAGE_SNAPSHOT") != "1" {
		t.Skip("profiling helper test")
	}

	store := makeBenchmarkStorage(4096, 512)
	total := 0

	for i := 0; i < 3000; i++ {
		total += len(store.snapshot())
	}

	if total == 0 {
		t.Fatal("unexpected zero snapshot size")
	}
}

func makeBenchmarkStorage(gaugeCount, counterCount int) *MemStorage {
	store := NewMemStorage()
	store.gauges = make(map[string]float64, gaugeCount)
	store.counters = make(map[string]int64, counterCount)

	for i := 0; i < gaugeCount; i++ {
		store.gauges["gauge-"+strconv.Itoa(i)] = float64(i) * 1.5
	}
	for i := 0; i < counterCount; i++ {
		store.counters["counter-"+strconv.Itoa(i)] = int64(i + 1)
	}

	return store
}

func makeBenchmarkMetrics(gaugeCount, counterCount int) []models.Metrics {
	metrics := make([]models.Metrics, 0, gaugeCount+counterCount)

	gaugeValues := make([]float64, gaugeCount)
	for i := 0; i < gaugeCount; i++ {
		gaugeValues[i] = float64(i) * 1.25
		metrics = append(metrics, models.Metrics{
			ID:    "gauge-" + strconv.Itoa(i),
			MType: models.Gauge,
			Value: &gaugeValues[i],
		})
	}

	counterValues := make([]int64, counterCount)
	for i := 0; i < counterCount; i++ {
		counterValues[i] = int64(i + 1)
		metrics = append(metrics, models.Metrics{
			ID:    "counter-" + strconv.Itoa(i),
			MType: models.Counter,
			Delta: &counterValues[i],
		})
	}

	return metrics
}
