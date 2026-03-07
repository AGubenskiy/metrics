package logger

import (
	"net/http"
	"time"

	"go.uber.org/zap"
)

type loggingResponseWriter struct {
	http.ResponseWriter
	statusCode int
	size       int
}

func (lrw *loggingResponseWriter) WriteHeader(code int) {
	lrw.statusCode = code
	lrw.ResponseWriter.WriteHeader(code)
}

func (lrw *loggingResponseWriter) Write(b []byte) (int, error) {
	if lrw.statusCode == 0 {
		lrw.statusCode = http.StatusOK
	}

	n, err := lrw.ResponseWriter.Write(b)
	lrw.size += n
	return n, err
}

func WithLogging(logger *zap.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			lrw := &loggingResponseWriter{ResponseWriter: w}

			next.ServeHTTP(lrw, r)

			duration := time.Since(start)
			if lrw.statusCode == 0 {
				lrw.statusCode = http.StatusOK
			}

			logger.Info("request processed",
				zap.String("uri", r.RequestURI),
				zap.String("method", r.Method),
				zap.Duration("duration", duration),
				zap.Int("status", lrw.statusCode),
				zap.Int("size", lrw.size),
			)
		})
	}
}
