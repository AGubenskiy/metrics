package proto

import (
	"testing"

	goproto "google.golang.org/protobuf/proto"
)

func TestUpdateMetricsRequestProtoRoundTrip(t *testing.T) {
	in := UpdateMetricsRequest_builder{
		Metrics: []*Metric{
			Metric_builder{Id: "Alloc", Type: Metric_GAUGE, Value: 12.5}.Build(),
			Metric_builder{Id: "PollCount", Type: Metric_COUNTER, Delta: 3}.Build(),
		},
	}.Build()

	data, err := goproto.Marshal(in)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var out UpdateMetricsRequest
	if err = goproto.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if len(out.GetMetrics()) != 2 {
		t.Fatalf("expected 2 metrics, got %d", len(out.GetMetrics()))
	}
	if got := out.GetMetrics()[0]; got.GetId() != "Alloc" || got.GetType() != Metric_GAUGE || got.GetValue() != 12.5 {
		t.Fatalf("unexpected gauge metric: %+v", got)
	}
	if got := out.GetMetrics()[1]; got.GetId() != "PollCount" || got.GetType() != Metric_COUNTER || got.GetDelta() != 3 {
		t.Fatalf("unexpected counter metric: %+v", got)
	}
}
