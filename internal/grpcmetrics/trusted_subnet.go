package grpcmetrics

import (
	"context"
	"fmt"
	"net"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const RealIPMetadataKey = "x-real-ip"

func TrustedSubnetUnaryInterceptor(cidr string) (grpc.UnaryServerInterceptor, error) {
	cidr = strings.TrimSpace(cidr)
	if cidr == "" {
		return func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
			return handler(ctx, req)
		}, nil
	}

	_, subnet, err := net.ParseCIDR(cidr)
	if err != nil {
		return nil, fmt.Errorf("invalid trusted_subnet %q: %w", cidr, err)
	}

	return func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if !metadataIPAllowed(ctx, subnet) {
			return nil, status.Error(codes.PermissionDenied, "permission denied")
		}
		return handler(ctx, req)
	}, nil
}

func metadataIPAllowed(ctx context.Context, subnet *net.IPNet) bool {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return false
	}

	for _, value := range md.Get(RealIPMetadataKey) {
		ip := net.ParseIP(strings.TrimSpace(value))
		if ip != nil && subnet.Contains(ip) {
			return true
		}
	}
	return false
}
