package audit

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	gojson "github.com/goccy/go-json"
)

// HTTPObserver delivers audit events to a remote HTTP endpoint with POST requests.
type HTTPObserver struct {
	client *http.Client
	url    string
}

// NewHTTPObserver creates an HTTP-based audit observer for targetURL.
func NewHTTPObserver(targetURL string, client *http.Client) (*HTTPObserver, error) {
	parsedURL, err := url.ParseRequestURI(targetURL)
	if err != nil {
		return nil, fmt.Errorf("invalid audit URL %q: %w", targetURL, err)
	}

	if client == nil {
		client = http.DefaultClient
	}

	return &HTTPObserver{
		client: client,
		url:    parsedURL.String(),
	}, nil
}

// Update sends event as a JSON payload to the configured HTTP endpoint.
func (o *HTTPObserver) Update(ctx context.Context, event Event) error {
	payload, err := gojson.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal audit event: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.url, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("build audit request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := o.client.Do(req)
	if err != nil {
		return fmt.Errorf("send audit request: %w", err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("audit server returned status %s", strings.TrimSpace(resp.Status))
	}

	return nil
}
