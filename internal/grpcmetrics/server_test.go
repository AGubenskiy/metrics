package grpcmetrics

import (
	"context"
	"testing"

	pb "github.com/AGubenskiy/metrics/internal/proto"
	"github.com/AGubenskiy/metrics/internal/service"
	"github.com/AGubenskiy/metrics/internal/storage"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestServerUpdateMetrics(t *testing.T) {
	store := storage.NewMemStorage()
	server := NewServer(service.NewMetrics(store))

	_, err := server.UpdateMetrics(context.Background(), &pb.UpdateMetricsRequest{
		Metrics: []*pb.Metric{
			{Id: "grpcGauge", Type: pb.Metric_GAUGE, Value: 12.5},
			{Id: "grpcCounter", Type: pb.Metric_COUNTER, Delta: 3},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got, ok := store.GetGauge(context.Background(), "grpcGauge"); !ok || got != 12.5 {
		t.Fatalf("unexpected gauge: value=%v ok=%t", got, ok)
	}
	if got, ok := store.GetCounter(context.Background(), "grpcCounter"); !ok || got != 3 {
		t.Fatalf("unexpected counter: value=%v ok=%t", got, ok)
	}
}

func TestServerUpdateMetricsRejectsInvalidBatch(t *testing.T) {
	store := storage.NewMemStorage()
	server := NewServer(service.NewMetrics(store))

	_, err := server.UpdateMetrics(context.Background(), &pb.UpdateMetricsRequest{
		Metrics: []*pb.Metric{{Type: pb.Metric_GAUGE, Value: 1}},
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("expected InvalidArgument, got %v (%v)", status.Code(err), err)
	}
}
