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
	"testing"

	gojson "github.com/goccy/go-json"
)

type stubObserver struct {
	err    error
	events []Event
}

func (o *stubObserver) Update(_ context.Context, event Event) error {
	o.events = append(o.events, event)
	return o.err
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
