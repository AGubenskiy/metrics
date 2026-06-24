package grpcmetrics

import (
	"context"

	"github.com/AGubenskiy/metrics/internal/trustedsubnet"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const RealIPMetadataKey = trustedsubnet.RealIPMetadataKey

func TrustedSubnetUnaryInterceptor(cidr string) (grpc.UnaryServerInterceptor, error) {
	checker, err := trustedsubnet.NewChecker(cidr)
	if err != nil {
		return nil, err
	}
	if !checker.Enabled() {
		return func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
			return handler(ctx, req)
		}, nil
	}

	return func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if !metadataIPAllowed(ctx, checker) {
			return nil, status.Error(codes.PermissionDenied, "permission denied")
		}
		return handler(ctx, req)
	}, nil
}

func metadataIPAllowed(ctx context.Context, checker *trustedsubnet.Checker) bool {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return false
	}

	for _, value := range md.Get(RealIPMetadataKey) {
		if checker.Allows(value) {
			return true
		}
	}
	return false
}
