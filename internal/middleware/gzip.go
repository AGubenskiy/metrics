package middleware

import (
	"compress/gzip"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
)

type gzipResponseWriter struct {
	http.ResponseWriter
	acceptsGzip bool
	initialized bool
	gzipWriter  *gzip.Writer
	writer      io.Writer
}

func newGzipResponseWriter(w http.ResponseWriter, acceptsGzip bool) *gzipResponseWriter {
	return &gzipResponseWriter{
		ResponseWriter: w,
		acceptsGzip:    acceptsGzip,
	}
}

func (g *gzipResponseWriter) WriteHeader(statusCode int) {
	g.init(nil)
	g.ResponseWriter.WriteHeader(statusCode)
}

func (g *gzipResponseWriter) Write(data []byte) (int, error) {
	g.init(data)
	return g.writer.Write(data)
}

func (g *gzipResponseWriter) Close() error {
	if g.gzipWriter == nil {
		return nil
	}
	return g.gzipWriter.Close()
}

func (g *gzipResponseWriter) init(sample []byte) {
	if g.initialized {
		return
	}
	g.initialized = true
	g.writer = g.ResponseWriter

	if !g.acceptsGzip {
		return
	}

	contentType := g.Header().Get("Content-Type")
	if contentType == "" && len(sample) > 0 {
		contentType = http.DetectContentType(sample)
	}
	if !isCompressibleContentType(contentType) {
		return
	}

	g.Header().Set("Content-Encoding", "gzip")
	g.Header().Add("Vary", "Accept-Encoding")
	g.Header().Del("Content-Length")

	g.gzipWriter = gzip.NewWriter(g.ResponseWriter)
	g.writer = g.gzipWriter
}

type gzipReadCloser struct {
	reader *gzip.Reader
	body   io.Closer
}

func (g *gzipReadCloser) Read(p []byte) (int, error) {
	return g.reader.Read(p)
}

func (g *gzipReadCloser) Close() error {
	return errors.Join(g.reader.Close(), g.body.Close())
}

// Gzip transparently decompresses gzipped request bodies and compresses supported responses.
func Gzip(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hasGzipToken(r.Header.Get("Content-Encoding")) {
			reader, err := gzip.NewReader(r.Body)
			if err != nil {
				http.Error(w, "invalid gzip body", http.StatusBadRequest)
				return
			}
			r.Body = &gzipReadCloser{
				reader: reader,
				body:   r.Body,
			}
			r.Header.Del("Content-Encoding")
		}

		gzipWriter := newGzipResponseWriter(w, hasGzipToken(r.Header.Get("Accept-Encoding")))
		defer func() {
			_ = gzipWriter.Close()
		}()

		next.ServeHTTP(gzipWriter, r)
	})
}

func hasGzipToken(headerValue string) bool {
	if headerValue == "" {
		return false
	}

	for _, token := range strings.Split(headerValue, ",") {
		value := strings.TrimSpace(token)
		value = strings.SplitN(value, ";", 2)[0]
		if strings.EqualFold(value, "gzip") {
			return true
		}
	}

	return false
}

func isCompressibleContentType(contentType string) bool {
	if contentType == "" {
		return false
	}

	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		mediaType = strings.TrimSpace(strings.Split(contentType, ";")[0])
	}

	switch mediaType {
	case "application/json", "text/html":
		return true
	default:
		return false
	}
}
