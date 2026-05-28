package handler_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/AGubenskiy/metrics/internal/handler"
	"github.com/AGubenskiy/metrics/internal/service"
	"github.com/AGubenskiy/metrics/internal/storage"
	"github.com/go-chi/chi/v5"
)

type examplePinger struct{}

func (examplePinger) PingContext(context.Context) error {
	return nil
}

func ExampleHandler_UpdateMetric() {
	server := httptest.NewServer(newExampleRouter(examplePinger{}))
	defer server.Close()

	resp, err := http.Post(server.URL+"/update/gauge/Alloc/42.5", "text/plain", nil)
	if err != nil {
		panic(err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		panic(err)
	}

	fmt.Println(resp.StatusCode)
	fmt.Println(strings.TrimSpace(string(body)))
	// Output:
	// 200
	// OK
}

func ExampleHandler_UpdateMetricJSON() {
	server := httptest.NewServer(newExampleRouter(examplePinger{}))
	defer server.Close()

	resp, err := http.Post(
		server.URL+"/update",
		"application/json",
		strings.NewReader(`{"id":"Alloc","type":"gauge","value":42.5}`),
	)
	if err != nil {
		panic(err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		panic(err)
	}

	fmt.Println(resp.StatusCode)
	fmt.Println(strings.TrimSpace(string(body)))
	// Output:
	// 200
	// {"id":"Alloc","type":"gauge","value":42.5}
}

func ExampleHandler_UpdateMetricsJSON() {
	server := httptest.NewServer(newExampleRouter(examplePinger{}))
	defer server.Close()

	resp, err := http.Post(
		server.URL+"/updates",
		"application/json",
		strings.NewReader(`[{"id":"Alloc","type":"gauge","value":42.5},{"id":"PollCount","type":"counter","delta":3}]`),
	)
	if err != nil {
		panic(err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	valueResp, err := http.Post(
		server.URL+"/value",
		"application/json",
		strings.NewReader(`{"id":"PollCount","type":"counter"}`),
	)
	if err != nil {
		panic(err)
	}
	defer func() {
		_ = valueResp.Body.Close()
	}()

	body, err := io.ReadAll(valueResp.Body)
	if err != nil {
		panic(err)
	}

	fmt.Println(resp.StatusCode)
	fmt.Println(strings.TrimSpace(string(body)))
	// Output:
	// 200
	// {"id":"PollCount","type":"counter","delta":3}
}

func ExampleHandler_GetMetricValueJSON() {
	server := httptest.NewServer(newExampleRouter(examplePinger{}))
	defer server.Close()

	updateResp, err := http.Post(
		server.URL+"/update",
		"application/json",
		strings.NewReader(`{"id":"Alloc","type":"gauge","value":42.5}`),
	)
	if err != nil {
		panic(err)
	}
	defer func() {
		_ = updateResp.Body.Close()
	}()

	resp, err := http.Post(
		server.URL+"/value",
		"application/json",
		strings.NewReader(`{"id":"Alloc","type":"gauge"}`),
	)
	if err != nil {
		panic(err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		panic(err)
	}

	fmt.Println(resp.StatusCode)
	fmt.Println(strings.TrimSpace(string(body)))
	// Output:
	// 200
	// {"id":"Alloc","type":"gauge","value":42.5}
}

func ExampleHandler_Ping() {
	server := httptest.NewServer(newExampleRouter(examplePinger{}))
	defer server.Close()

	resp, err := http.Get(server.URL + "/ping")
	if err != nil {
		panic(err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	fmt.Println(resp.StatusCode)
	// Output:
	// 200
}

func newExampleRouter(pinger handler.Pinger) http.Handler {
	store := storage.NewMemStorage()
	h := handler.NewHandlerWithPinger(service.NewMetrics(store), pinger)

	r := chi.NewRouter()
	r.Post("/update/{type}/{name}/{value}", h.UpdateMetric)
	r.Post("/update", h.UpdateMetricJSON)
	r.Post("/updates", h.UpdateMetricsJSON)
	r.Get("/value/{type}/{name}", h.GetMetricValue)
	r.Post("/value", h.GetMetricValueJSON)
	r.Get("/ping", h.Ping)

	return r
}
