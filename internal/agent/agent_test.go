package agent

import "testing"

func TestPollIncrementsCounter(t *testing.T) {
	a := NewAgent("http://localhost:8080")

	a.poll()
	a.poll()

	if a.counters["PollCount"] != 2 {
		t.Fatalf("expected PollCount = 2, got %d", a.counters["PollCount"])
	}
}
