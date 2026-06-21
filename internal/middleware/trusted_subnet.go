package middleware

import (
	"fmt"
	"net"
	"net/http"
	"strings"
)

const realIPHeader = "X-Real-IP"

// TrustedSubnet allows requests whose X-Real-IP belongs to cidr.
// If cidr is empty, the middleware is disabled. Optional path prefixes limit
// the check to selected endpoints.
func TrustedSubnet(cidr string, pathPrefixes ...string) (func(http.Handler) http.Handler, error) {
	cidr = strings.TrimSpace(cidr)
	if cidr == "" {
		return func(next http.Handler) http.Handler {
			return next
		}, nil
	}

	_, subnet, err := net.ParseCIDR(cidr)
	if err != nil {
		return nil, fmt.Errorf("invalid trusted_subnet %q: %w", cidr, err)
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !shouldCheckTrustedSubnet(r.URL.Path, pathPrefixes) {
				next.ServeHTTP(w, r)
				return
			}

			ip := net.ParseIP(strings.TrimSpace(r.Header.Get(realIPHeader)))
			if ip == nil || !subnet.Contains(ip) {
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
