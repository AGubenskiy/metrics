package grpcmetrics

import (
	"context"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestTrustedSubnetUnaryInterceptor(t *testing.T) {
	tests := []struct {
		name     string
		cidr     string
		realIP   string
		wantCode codes.Code
	}{
		{
			name:     "empty subnet disables check",
			wantCode: codes.OK,
		},
		{
			name:     "allows matching real ip",
			cidr:     "192.168.1.0/24",
			realIP:   "192.168.1.42",
			wantCode: codes.OK,
		},
		{
			name:     "rejects missing real ip",
			cidr:     "192.168.1.0/24",
			wantCode: codes.PermissionDenied,
		},
		{
			name:     "rejects real ip outside subnet",
			cidr:     "192.168.1.0/24",
			realIP:   "10.0.0.7",
			wantCode: codes.PermissionDenied,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			interceptor, err := TrustedSubnetUnaryInterceptor(tt.cidr)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			ctx := context.Background()
			if tt.realIP != "" {
				ctx = metadata.NewIncomingContext(ctx, metadata.Pairs(RealIPMetadataKey, tt.realIP))
			}

			_, err = interceptor(ctx, nil, &grpc.UnaryServerInfo{}, func(context.Context, any) (any, error) {
				return &struct{}{}, nil
			})
			if status.Code(err) != tt.wantCode {
				t.Fatalf("expected code %v, got %v (%v)", tt.wantCode, status.Code(err), err)
			}
		})
	}
}

func TestTrustedSubnetUnaryInterceptorRejectsInvalidCIDR(t *testing.T) {
	if _, err := TrustedSubnetUnaryInterceptor("192.168.1.0"); err == nil {
		t.Fatal("expected invalid CIDR error")
	}
}
