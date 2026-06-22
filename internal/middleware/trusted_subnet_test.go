package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestTrustedSubnet(t *testing.T) {
	tests := []struct {
		name       string
		cidr       string
		path       string
		realIP     string
		wantStatus int
	}{
		{
			name:       "empty subnet disables check",
			cidr:       "",
			path:       "/update",
			wantStatus: http.StatusNoContent,
		},
		{
			name:       "allows matching real ip",
			cidr:       "192.168.1.0/24",
			path:       "/updates/",
			realIP:     "192.168.1.42",
			wantStatus: http.StatusNoContent,
		},
		{
			name:       "rejects missing real ip",
			cidr:       "192.168.1.0/24",
			path:       "/update",
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "rejects real ip outside subnet",
			cidr:       "192.168.1.0/24",
			path:       "/updates",
			realIP:     "10.0.0.7",
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "skips unprotected path",
			cidr:       "192.168.1.0/24",
			path:       "/value",
			wantStatus: http.StatusNoContent,
		},
		{
			name:       "does not match partial path segment",
			cidr:       "192.168.1.0/24",
			path:       "/updated",
			wantStatus: http.StatusNoContent,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mw, err := TrustedSubnet(tt.cidr, "/update", "/updates")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			handler := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNoContent)
			}))

			req := httptest.NewRequest(http.MethodPost, tt.path, nil)
			if tt.realIP != "" {
				req.Header.Set(realIPHeader, tt.realIP)
			}
			rr := httptest.NewRecorder()

			handler.ServeHTTP(rr, req)

			if rr.Code != tt.wantStatus {
				t.Fatalf("expected status %d, got %d", tt.wantStatus, rr.Code)
			}
		})
	}
}

func TestTrustedSubnetRejectsInvalidCIDR(t *testing.T) {
	if _, err := TrustedSubnet("192.168.1.0"); err == nil {
		t.Fatal("expected invalid CIDR error")
	}
}

func TestTrustedSubnetLogsWhenDisabled(t *testing.T) {
	core, logs := observer.New(zap.InfoLevel)

	mw, err := TrustedSubnetWithLogger(zap.New(core), "", "/update", "/updates")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/update", nil))

	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected status %d, got %d", http.StatusNoContent, rr.Code)
	}
	if logs.Len() != 1 {
		t.Fatalf("expected one startup log entry, got %d", logs.Len())
	}
	if got := logs.All()[0].Message; got != "trusted subnet middleware disabled" {
		t.Fatalf("unexpected log message: %q", got)
	}
}
