package middleware

import (
	"bytes"
	"crypto/rsa"
	"io"
	"net/http"

	"github.com/AGubenskiy/metrics/internal/cryptoutil"
)

// Decrypt decrypts request bodies encrypted by the metrics agent.
func Decrypt(privateKey *rsa.PrivateKey) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if privateKey == nil {
			return next
		}

		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			encryptedHeader := r.Header.Get(cryptoutil.HeaderName)
			if encryptedHeader == "" {
				next.ServeHTTP(w, r)
				return
			}
			if encryptedHeader != cryptoutil.HeaderValue {
				http.Error(w, "unsupported encrypted body", http.StatusBadRequest)
				return
			}
			if r.Body == nil {
				http.Error(w, "encrypted body is empty", http.StatusBadRequest)
				return
			}

			encryptedBody, err := io.ReadAll(r.Body)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			_ = r.Body.Close()

			body, err := cryptoutil.Decrypt(encryptedBody, privateKey)
			if err != nil {
				http.Error(w, "cannot decrypt request body", http.StatusBadRequest)
				return
			}

			r.Body = io.NopCloser(bytes.NewReader(body))
			r.ContentLength = int64(len(body))
			r.Header.Del(cryptoutil.HeaderName)
			next.ServeHTTP(w, r)
		})
	}
}
