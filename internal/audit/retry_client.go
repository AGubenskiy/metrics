package audit

import (
	"io"
	"net/http"
	"time"
)

const (
	defaultHTTPRetryCount = 2
	defaultHTTPRetryWait  = 100 * time.Millisecond
)

// NewRetryHTTPClient returns a client that transparently retries transient audit delivery failures.
func NewRetryHTTPClient(client *http.Client) *http.Client {
	if client == nil {
		client = http.DefaultClient
	}

	cloned := *client
	transport := cloned.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}

	cloned.Transport = &retryTransport{
		base:       transport,
		retryCount: defaultHTTPRetryCount,
		retryWait:  defaultHTTPRetryWait,
	}

	return &cloned
}

type retryTransport struct {
	base       http.RoundTripper
	retryCount int
	retryWait  time.Duration
}

func (t *retryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	currentReq := req

	for attempt := 0; ; attempt++ {
		if attempt > 0 {
			retryReq, err := cloneRequest(req)
			if err != nil {
				return nil, err
			}
			currentReq = retryReq
		}

		resp, err := t.base.RoundTrip(currentReq)
		if attempt >= t.retryCount || !shouldRetryRequest(req, resp, err) {
			return resp, err
		}

		if resp != nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
		}

		timer := time.NewTimer(t.retryWait)
		select {
		case <-req.Context().Done():
			timer.Stop()
			return nil, req.Context().Err()
		case <-timer.C:
		}
	}
}

func cloneRequest(req *http.Request) (*http.Request, error) {
	cloned := req.Clone(req.Context())
	if req.Body == nil {
		return cloned, nil
	}

	if req.GetBody == nil {
		return nil, http.ErrNotSupported
	}

	body, err := req.GetBody()
	if err != nil {
		return nil, err
	}
	cloned.Body = body

	return cloned, nil
}

func shouldRetryRequest(req *http.Request, resp *http.Response, err error) bool {
	if req.Context().Err() != nil {
		return false
	}
	if err != nil {
		return true
	}
	if resp == nil {
		return false
	}

	return resp.StatusCode == http.StatusRequestTimeout ||
		resp.StatusCode == http.StatusTooManyRequests ||
		resp.StatusCode >= http.StatusInternalServerError
}
