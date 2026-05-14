package audit

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	gojson "github.com/goccy/go-json"
)

type stubObserver struct {
	err    error
	events []Event
}

type blockingObserver struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

type notifyingObserver struct {
	received chan Event
}

type stepObserver struct {
	started chan Event
	release chan struct{}
}

func (o *stubObserver) Update(_ context.Context, event Event) error {
	o.events = append(o.events, event)
	return o.err
}

func (o *blockingObserver) Update(_ context.Context, _ Event) error {
	o.once.Do(func() {
		close(o.started)
	})
	<-o.release
	return nil
}

func (o *notifyingObserver) Update(_ context.Context, event Event) error {
	o.received <- event
	return nil
}

func (o *stepObserver) Update(_ context.Context, event Event) error {
	o.started <- event
	<-o.release
	return nil
}

func TestPublisherNotifyToAllObservers(t *testing.T) {
	t.Parallel()

	first := &stubObserver{}
	second := &stubObserver{}
	publisher := NewPublisher(first, second)
	event := Event{
		TS:        1,
		Metrics:   []string{"Alloc"},
		IPAddress: "192.168.0.42",
	}

	if err := publisher.Notify(context.Background(), event); err != nil {
		t.Fatalf("Notify returned error: %v", err)
	}

	if len(first.events) != 1 {
		t.Fatalf("expected first observer to receive 1 event, got %d", len(first.events))
	}
	if len(second.events) != 1 {
		t.Fatalf("expected second observer to receive 1 event, got %d", len(second.events))
	}
	if !reflect.DeepEqual(first.events[0], event) {
		t.Fatalf("unexpected event for first observer: %+v", first.events[0])
	}
	if !reflect.DeepEqual(second.events[0], event) {
		t.Fatalf("unexpected event for second observer: %+v", second.events[0])
	}
}

func TestPublisherNotifyJoinsErrors(t *testing.T) {
	t.Parallel()

	firstErr := errors.New("first failed")
	secondErr := errors.New("second failed")
	publisher := NewPublisher(
		&stubObserver{err: firstErr},
		&stubObserver{err: secondErr},
	)

	err := publisher.Notify(context.Background(), Event{})
	if err == nil {
		t.Fatal("expected joined error, got nil")
	}
	if !errors.Is(err, firstErr) {
		t.Fatalf("expected first error to be joined, got %v", err)
	}
	if !errors.Is(err, secondErr) {
		t.Fatalf("expected second error to be joined, got %v", err)
	}
}

func TestAsyncPublisherNotifyDoesNotWaitForObserver(t *testing.T) {
	t.Parallel()

	observer := &blockingObserver{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	publisher, err := NewAsyncPublisher(NewPublisher(observer), AsyncPublisherConfig{
		QueueSize:       1,
		DeliveryTimeout: time.Second,
	})
	if err != nil {
		t.Fatalf("NewAsyncPublisher returned error: %v", err)
	}
	defer func() {
		close(observer.release)

		closeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := publisher.Close(closeCtx); err != nil {
			t.Fatalf("Close returned error: %v", err)
		}
	}()

	notifyDone := make(chan error, 1)
	go func() {
		notifyDone <- publisher.Notify(context.Background(), Event{TS: 1})
	}()

	select {
	case err := <-notifyDone:
		if err != nil {
			t.Fatalf("Notify returned error: %v", err)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Notify blocked on slow observer")
	}

	select {
	case <-observer.started:
	case <-time.After(time.Second):
		t.Fatal("observer was not invoked")
	}
}

func TestAsyncPublisherCloseFlushesQueuedEvents(t *testing.T) {
	t.Parallel()

	observer := &stubObserver{}
	publisher, err := NewAsyncPublisher(NewPublisher(observer), AsyncPublisherConfig{
		QueueSize:       8,
		DeliveryTimeout: time.Second,
	})
	if err != nil {
		t.Fatalf("NewAsyncPublisher returned error: %v", err)
	}

	events := []Event{
		{TS: 1, Metrics: []string{"Alloc"}},
		{TS: 2, Metrics: []string{"HeapAlloc"}},
		{TS: 3, Metrics: []string{"PollCount"}},
	}
	for _, event := range events {
		if err := publisher.Notify(context.Background(), event); err != nil {
			t.Fatalf("Notify returned error: %v", err)
		}
	}

	closeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := publisher.Close(closeCtx); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}

	if !reflect.DeepEqual(observer.events, events) {
		t.Fatalf("unexpected delivered events: %+v", observer.events)
	}
}

func TestAsyncPublisherSlowObserverDoesNotBlockOtherObservers(t *testing.T) {
	t.Parallel()

	slowObserver := &stepObserver{
		started: make(chan Event, 2),
		release: make(chan struct{}, 2),
	}
	fastObserver := &notifyingObserver{received: make(chan Event, 3)}

	publisher, err := NewAsyncPublisher(NewPublisher(slowObserver, fastObserver), AsyncPublisherConfig{
		QueueSize:       1,
		DeliveryTimeout: time.Second,
	})
	if err != nil {
		t.Fatalf("NewAsyncPublisher returned error: %v", err)
	}
	defer func() {
		slowObserver.release <- struct{}{}
		slowObserver.release <- struct{}{}

		closeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := publisher.Close(closeCtx); err != nil {
			t.Fatalf("Close returned error: %v", err)
		}
	}()

	firstEvent := Event{TS: 1}
	secondEvent := Event{TS: 2}
	thirdEvent := Event{TS: 3}

	if err := publisher.Notify(context.Background(), firstEvent); err != nil {
		t.Fatalf("Notify returned error: %v", err)
	}

	select {
	case <-slowObserver.started:
	case <-time.After(time.Second):
		t.Fatal("slow observer was not invoked")
	}

	if err := publisher.Notify(context.Background(), secondEvent); err != nil {
		t.Fatalf("Notify returned error: %v", err)
	}

	for _, wantEvent := range []Event{firstEvent, secondEvent} {
		select {
		case gotEvent := <-fastObserver.received:
			if !reflect.DeepEqual(gotEvent, wantEvent) {
				t.Fatalf("unexpected event for fast observer: %+v", gotEvent)
			}
		case <-time.After(time.Second):
			t.Fatalf("fast observer did not receive event %+v", wantEvent)
		}
	}

	err = publisher.Notify(context.Background(), thirdEvent)
	if !errors.Is(err, ErrQueueFull) {
		t.Fatalf("expected ErrQueueFull, got %v", err)
	}

	select {
	case gotEvent := <-fastObserver.received:
		if !reflect.DeepEqual(gotEvent, thirdEvent) {
			t.Fatalf("unexpected event for fast observer: %+v", gotEvent)
		}
	case <-time.After(time.Second):
		t.Fatalf("fast observer did not receive event %+v", thirdEvent)
	}
}

func TestAsyncPublisherProcessesEventsSequentiallyPerObserver(t *testing.T) {
	t.Parallel()

	observer := &stepObserver{
		started: make(chan Event, 2),
		release: make(chan struct{}, 2),
	}
	publisher, err := NewAsyncPublisher(NewPublisher(observer), AsyncPublisherConfig{
		QueueSize:       2,
		DeliveryTimeout: time.Second,
	})
	if err != nil {
		t.Fatalf("NewAsyncPublisher returned error: %v", err)
	}
	defer func() {
		observer.release <- struct{}{}
		observer.release <- struct{}{}

		closeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := publisher.Close(closeCtx); err != nil {
			t.Fatalf("Close returned error: %v", err)
		}
	}()

	firstEvent := Event{TS: 1}
	secondEvent := Event{TS: 2}

	if err := publisher.Notify(context.Background(), firstEvent); err != nil {
		t.Fatalf("Notify returned error: %v", err)
	}

	select {
	case gotEvent := <-observer.started:
		if !reflect.DeepEqual(gotEvent, firstEvent) {
			t.Fatalf("unexpected first event: %+v", gotEvent)
		}
	case <-time.After(time.Second):
		t.Fatal("observer did not start first event")
	}

	if err := publisher.Notify(context.Background(), secondEvent); err != nil {
		t.Fatalf("Notify returned error: %v", err)
	}

	select {
	case gotEvent := <-observer.started:
		t.Fatalf("observer started second event before finishing first: %+v", gotEvent)
	case <-time.After(100 * time.Millisecond):
	}

	observer.release <- struct{}{}

	select {
	case gotEvent := <-observer.started:
		if !reflect.DeepEqual(gotEvent, secondEvent) {
			t.Fatalf("unexpected second event: %+v", gotEvent)
		}
	case <-time.After(time.Second):
		t.Fatal("observer did not start second event")
	}
}

func TestNewAsyncPublisherReturnsErrorForInvalidConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		publisher *Publisher
		cfg       AsyncPublisherConfig
		wantErr   error
	}{
		{
			name:      "nil publisher",
			publisher: nil,
			cfg: AsyncPublisherConfig{
				QueueSize:       1,
				DeliveryTimeout: time.Second,
			},
			wantErr: ErrNilPublisher,
		},
		{
			name:      "queue size",
			publisher: NewPublisher(),
			cfg: AsyncPublisherConfig{
				QueueSize:       0,
				DeliveryTimeout: time.Second,
			},
			wantErr: ErrInvalidQueueSize,
		},
		{
			name:      "delivery timeout",
			publisher: NewPublisher(),
			cfg: AsyncPublisherConfig{
				QueueSize:       1,
				DeliveryTimeout: 0,
			},
			wantErr: ErrInvalidDeliveryTimeout,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			publisher, err := NewAsyncPublisher(tt.publisher, tt.cfg)
			if publisher != nil {
				t.Fatal("expected nil publisher on invalid config")
			}
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("expected error %v, got %v", tt.wantErr, err)
			}
		})
	}
}

func TestFileObserverUpdateAppendsJSONLines(t *testing.T) {
	t.Parallel()

	filePath := filepath.Join(t.TempDir(), "audit.log")
	observer := NewFileObserver(filePath)

	events := []Event{
		{TS: 1, Metrics: []string{"Alloc"}, IPAddress: "192.168.0.1"},
		{TS: 2, Metrics: []string{"Frees", "HeapAlloc"}, IPAddress: "192.168.0.2"},
	}

	for _, event := range events {
		if err := observer.Update(context.Background(), event); err != nil {
			t.Fatalf("Update returned error: %v", err)
		}
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("failed to read audit file: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(content)), "\n")
	if len(lines) != len(events) {
		t.Fatalf("expected %d lines, got %d", len(events), len(lines))
	}

	for i, line := range lines {
		var got Event
		if err := gojson.Unmarshal([]byte(line), &got); err != nil {
			t.Fatalf("failed to unmarshal line %d: %v", i, err)
		}
		if !reflect.DeepEqual(got, events[i]) {
			t.Fatalf("unexpected event on line %d: %+v", i, got)
		}
	}
}

func TestHTTPObserverUpdatePostsJSON(t *testing.T) {
	t.Parallel()

	var (
		gotMethod      string
		gotContentType string
		gotEvent       Event
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			_ = r.Body.Close()
		}()

		gotMethod = r.Method
		gotContentType = r.Header.Get("Content-Type")
		if err := gojson.NewDecoder(r.Body).Decode(&gotEvent); err != nil {
			t.Fatalf("failed to decode request body: %v", err)
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	observer, err := NewHTTPObserver(server.URL, server.Client())
	if err != nil {
		t.Fatalf("failed to create observer: %v", err)
	}

	wantEvent := Event{
		TS:        12345678,
		Metrics:   []string{"Alloc", "Frees"},
		IPAddress: "192.168.0.42",
	}

	if err := observer.Update(context.Background(), wantEvent); err != nil {
		t.Fatalf("Update returned error: %v", err)
	}

	if gotMethod != http.MethodPost {
		t.Fatalf("expected method %q, got %q", http.MethodPost, gotMethod)
	}
	if gotContentType != "application/json" {
		t.Fatalf("expected Content-Type application/json, got %q", gotContentType)
	}
	if !reflect.DeepEqual(gotEvent, wantEvent) {
		t.Fatalf("unexpected posted event: %+v", gotEvent)
	}
}

func TestHTTPObserverUpdateRetriesTransientFailures(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			_ = r.Body.Close()
		}()

		attempt := attempts.Add(1)
		if attempt < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	observer, err := NewHTTPObserver(server.URL, NewRetryHTTPClient(server.Client()))
	if err != nil {
		t.Fatalf("failed to create observer: %v", err)
	}

	if err := observer.Update(context.Background(), Event{TS: 1}); err != nil {
		t.Fatalf("Update returned error: %v", err)
	}

	if got := attempts.Load(); got != 3 {
		t.Fatalf("expected 3 attempts, got %d", got)
	}
}

func TestHTTPObserverUpdateDoesNotRetryClientFailures(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			_ = r.Body.Close()
		}()

		attempts.Add(1)
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()

	observer, err := NewHTTPObserver(server.URL, NewRetryHTTPClient(server.Client()))
	if err != nil {
		t.Fatalf("failed to create observer: %v", err)
	}

	if err := observer.Update(context.Background(), Event{TS: 1}); err == nil {
		t.Fatal("expected Update to fail on client error status")
	}

	if got := attempts.Load(); got != 1 {
		t.Fatalf("expected 1 attempt, got %d", got)
	}
}
