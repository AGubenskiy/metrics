package grpcmetrics

import (
	"context"
	"errors"
	"strings"
	"testing"

	models "github.com/AGubenskiy/metrics/internal/model"
	pb "github.com/AGubenskiy/metrics/internal/proto"
	"github.com/AGubenskiy/metrics/internal/service"
	"github.com/AGubenskiy/metrics/internal/storage"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestServerUpdateMetrics(t *testing.T) {
	store := storage.NewMemStorage()
	server := NewServer(service.NewMetrics(store))

	_, err := server.UpdateMetrics(context.Background(), pb.UpdateMetricsRequest_builder{
		Metrics: []*pb.Metric{
			pb.Metric_builder{Id: "grpcGauge", Type: pb.Metric_GAUGE, Value: 12.5}.Build(),
			pb.Metric_builder{Id: "grpcCounter", Type: pb.Metric_COUNTER, Delta: 3}.Build(),
		},
	}.Build())
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

	_, err := server.UpdateMetrics(context.Background(), pb.UpdateMetricsRequest_builder{
		Metrics: []*pb.Metric{
			pb.Metric_builder{Type: pb.Metric_GAUGE, Value: 1}.Build(),
		},
	}.Build())
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("expected InvalidArgument, got %v (%v)", status.Code(err), err)
	}
}

func TestServerSetLoggerWarnsOnNilLogger(t *testing.T) {
	core, logs := observer.New(zap.WarnLevel)
	server := NewServer(service.NewMetrics(storage.NewMemStorage()))
	server.SetLogger(zap.New(core))

	server.SetLogger(nil)

	if got := logs.Len(); got != 1 {
		t.Fatalf("expected nil logger warning to be logged once, got %d log entries", got)
	}
	if got := logs.All()[0].Message; got != "skipped setting gRPC metrics server logger: logger is nil" {
		t.Fatalf("unexpected log message: %q", got)
	}
}

func TestServerUpdateMetricsHidesInternalErrorDetails(t *testing.T) {
	core, logs := observer.New(zap.ErrorLevel)
	sensitiveErr := errors.New("postgres password=secret host=internal-db")
	server := NewServer(failingMetricsService{err: sensitiveErr})
	server.SetLogger(zap.New(core))

	_, err := server.UpdateMetrics(context.Background(), pb.UpdateMetricsRequest_builder{
		Metrics: []*pb.Metric{
			pb.Metric_builder{Id: "grpcGauge", Type: pb.Metric_GAUGE, Value: 12.5}.Build(),
		},
	}.Build())

	if status.Code(err) != codes.Internal {
		t.Fatalf("expected Internal, got %v (%v)", status.Code(err), err)
	}
	if got := status.Convert(err).Message(); strings.Contains(got, sensitiveErr.Error()) {
		t.Fatalf("client error leaks internal details: %q", got)
	}
	if got := logs.Len(); got != 1 {
		t.Fatalf("expected internal error to be logged once, got %d log entries", got)
	}
}

type failingMetricsService struct {
	err error
}

func (s failingMetricsService) UpdateMetrics(context.Context, []models.Metrics) error {
	return s.err
}
