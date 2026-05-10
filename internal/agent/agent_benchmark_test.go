package agent

import (
	"strconv"
	"testing"
)

func BenchmarkAgentCollectMetricsBatch(b *testing.B) {
	agent := NewAgent("http://localhost:8080", 10, 5, "")

	for i := 0; i < 4096; i++ {
		agent.gauges["gauge-"+strconv.Itoa(i)] = float64(i) * 2
	}
	for i := 0; i < 512; i++ {
		agent.counters["counter-"+strconv.Itoa(i)] = int64(i)
	}

	b.ReportAllocs()
	b.ResetTimer()

	total := 0
	for i := 0; i < b.N; i++ {
		total += len(agent.collectMetricsBatch())
	}

	if total == 0 {
		b.Fatal("unexpected zero batch size")
	}
}
