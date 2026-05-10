package main

import (
	"context"
	"flag"
	"log"
	"os"
	"path/filepath"
	"runtime"
	rpprof "runtime/pprof"
	"strconv"

	"github.com/AGubenskiy/metrics/internal/storage"
)

func main() {
	var (
		outputPath = flag.String("output", filepath.Join("profiles", "profile.pprof"), "path to heap profile output")
		iterations = flag.Int("iterations", 300, "number of SaveToFile iterations")
		gauges     = flag.Int("gauges", 4096, "number of gauge metrics in the dataset")
		counters   = flag.Int("counters", 512, "number of counter metrics in the dataset")
	)
	flag.Parse()

	if *iterations <= 0 || *gauges < 0 || *counters < 0 {
		log.Fatal("iterations must be > 0; gauges and counters must be >= 0")
	}

	runtime.MemProfileRate = 1

	store := storage.NewMemStorage()
	ctx := context.Background()
	for i := 0; i < *gauges; i++ {
		if err := store.UpdateGauge(ctx, "gauge-"+strconv.Itoa(i), float64(i)*1.5); err != nil {
			log.Fatalf("populate gauge metrics: %v", err)
		}
	}
	for i := 0; i < *counters; i++ {
		if err := store.UpdateCounter(ctx, "counter-"+strconv.Itoa(i), int64(i+1)); err != nil {
			log.Fatalf("populate counter metrics: %v", err)
		}
	}

	tempDir, err := os.MkdirTemp("", "metrics-profile-*")
	if err != nil {
		log.Fatalf("create temp data dir: %v", err)
	}
	defer func() {
		_ = os.RemoveAll(tempDir)
	}()

	for i := 0; i < *iterations; i++ {
		dataFilePath := filepath.Join(tempDir, "metrics-profile-"+strconv.Itoa(i)+".json")
		if err = store.SaveToFile(dataFilePath); err != nil {
			log.Fatalf("save snapshot iteration %d: %v", i, err)
		}
	}

	if err = os.MkdirAll(filepath.Dir(*outputPath), 0o755); err != nil {
		log.Fatalf("create output directory: %v", err)
	}

	profileFile, err := os.Create(*outputPath)
	if err != nil {
		log.Fatalf("create profile file: %v", err)
	}
	defer func() {
		if closeErr := profileFile.Close(); closeErr != nil {
			log.Printf("close profile file: %v", closeErr)
		}
	}()

	runtime.GC()
	if err = rpprof.WriteHeapProfile(profileFile); err != nil {
		log.Fatalf("write heap profile: %v", err)
	}

	log.Printf("wrote heap profile to %s", *outputPath)
}
