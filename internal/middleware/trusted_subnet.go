package middleware

import (
	"net/http"
	"strings"

	"github.com/AGubenskiy/metrics/internal/trustedsubnet"
	"go.uber.org/zap"
)

const realIPHeader = trustedsubnet.RealIPHeader

// TrustedSubnet allows requests whose X-Real-IP belongs to cidr.
// If cidr is empty, the middleware is disabled. Optional path prefixes limit
// the check to selected endpoints.
func TrustedSubnet(cidr string, pathPrefixes ...string) (func(http.Handler) http.Handler, error) {
	return TrustedSubnetWithLogger(zap.NewNop(), cidr, pathPrefixes...)
}

func TrustedSubnetWithLogger(logger *zap.Logger, cidr string, pathPrefixes ...string) (func(http.Handler) http.Handler, error) {
	if logger == nil {
		logger = zap.NewNop()
	}

	checker, err := trustedsubnet.NewChecker(cidr)
	if err != nil {
		return nil, err
	}
	if !checker.Enabled() {
		logger.Info(
			"trusted subnet middleware disabled",
			zap.String("reason", "trusted subnet is not configured"),
			zap.Strings("path_prefixes", pathPrefixes),
		)
		return func(next http.Handler) http.Handler {
			return next
		}, nil
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !shouldCheckTrustedSubnet(r.URL.Path, pathPrefixes) {
				next.ServeHTTP(w, r)
				return
			}

			if !checker.Allows(r.Header.Get(realIPHeader)) {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r)
		})
	}, nil
}

func shouldCheckTrustedSubnet(path string, pathPrefixes []string) bool {
	if len(pathPrefixes) == 0 {
		return true
	}

	for _, prefix := range pathPrefixes {
		prefix = strings.TrimRight(strings.TrimSpace(prefix), "/")
		if prefix == "" {
			continue
		}
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			return true
		}
	}
	return false
}
