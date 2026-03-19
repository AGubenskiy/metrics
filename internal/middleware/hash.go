package middleware

import (
	"bytes"
	"io"
	"net/http"

	"github.com/AGubenskiy/metrics/internal/signing"
)

type hashResponseWriter struct {
	http.ResponseWriter
	key         string
	statusCode  int
	wroteHeader bool
	body        bytes.Buffer
}

func newHashResponseWriter(w http.ResponseWriter, key string) *hashResponseWriter {
	return &hashResponseWriter{
		ResponseWriter: w,
		key:            key,
	}
}

func (w *hashResponseWriter) Header() http.Header {
	return w.ResponseWriter.Header()
}

func (w *hashResponseWriter) WriteHeader(statusCode int) {
	if w.wroteHeader {
		return
	}
	w.wroteHeader = true
	w.statusCode = statusCode
}

func (w *hashResponseWriter) Write(data []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	return w.body.Write(data)
}

func (w *hashResponseWriter) Flush() error {
	if !w.wroteHeader {
		w.statusCode = http.StatusOK
	}

	if w.key != "" && w.body.Len() > 0 {
		w.Header().Set(signing.HeaderName, signing.Hash(w.body.Bytes(), w.key))
	}

	w.ResponseWriter.WriteHeader(w.statusCode)
	_, err := w.ResponseWriter.Write(w.body.Bytes())
	return err
}

func Hash(key string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if key == "" {
			return next
		}

		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Body != nil {
				body, err := io.ReadAll(r.Body)
				if err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
				_ = r.Body.Close()

				expected := r.Header.Get(signing.HeaderName)
				if expected != "" && !signing.Valid(body, key, expected) {
					http.Error(w, "invalid request hash", http.StatusBadRequest)
					return
				}

				r.Body = io.NopCloser(bytes.NewReader(body))
			}

			hashWriter := newHashResponseWriter(w, key)
			next.ServeHTTP(hashWriter, r)
			if err := hashWriter.Flush(); err != nil {
				return
			}
		})
	}
}
