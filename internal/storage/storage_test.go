package storage

import "testing"

func TestCounterAccumulation(t *testing.T) {
	s := NewMemStorage()

	s.UpdateCounter("c", 10)
	s.UpdateCounter("c", 5)

	if s.counters["c"] != 15 {
		t.Fatalf("expected 15, got %d", s.counters["c"])
	}
}
