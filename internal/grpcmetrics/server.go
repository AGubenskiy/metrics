package grpcmetrics

import (
	"context"
	"fmt"

	models "github.com/AGubenskiy/metrics/internal/model"
	pb "github.com/AGubenskiy/metrics/internal/proto"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type MetricsService interface {
	UpdateMetrics(ctx context.Context, metrics []models.Metrics) error
}

type Server struct {
	pb.UnimplementedMetricsServer

	service MetricsService
}

func NewServer(service MetricsService) *Server {
	return &Server{service: service}
}

func (s *Server) UpdateMetrics(ctx context.Context, req *pb.UpdateMetricsRequest) (*pb.UpdateMetricsResponse, error) {
	if req == nil || len(req.GetMetrics()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "metrics batch is empty")
	}

	metrics := make([]models.Metrics, 0, len(req.GetMetrics()))
	for i, metric := range req.GetMetrics() {
		converted, err := metricFromProto(metric)
		if err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "metric %d: %v", i, err)
		}
		metrics = append(metrics, converted)
	}

	if err := s.service.UpdateMetrics(ctx, metrics); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &pb.UpdateMetricsResponse{}, nil
}

func metricFromProto(metric *pb.Metric) (models.Metrics, error) {
	if metric == nil {
		return models.Metrics{}, fmt.Errorf("metric is required")
	}
	if metric.GetId() == "" {
		return models.Metrics{}, fmt.Errorf("metric name is required")
	}

	switch metric.GetType() {
	case pb.Metric_GAUGE:
		value := metric.GetValue()
		return models.Metrics{
			ID:    metric.GetId(),
			MType: models.Gauge,
			Value: &value,
		}, nil
	case pb.Metric_COUNTER:
		delta := metric.GetDelta()
		return models.Metrics{
			ID:    metric.GetId(),
			MType: models.Counter,
			Delta: &delta,
		}, nil
	default:
		return models.Metrics{}, fmt.Errorf("unsupported metric type")
	}
}
