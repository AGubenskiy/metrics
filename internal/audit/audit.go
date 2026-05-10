package audit

import (
	"context"
	"errors"
	"sync"
)

// Event describes a single audit record produced after a successful metrics update.
type Event struct {
	// TS is the Unix timestamp of the audited event.
	TS int64 `json:"ts"`
	// Metrics contains names of metrics received in the request.
	Metrics []string `json:"metrics"`
	// IPAddress is the remote IP address of the incoming request.
	IPAddress string `json:"ip_address"`
}

// Observer consumes audit events published by a Publisher.
type Observer interface {
	Update(ctx context.Context, event Event) error
}

// Publisher fan-outs audit events to registered observers.
type Publisher struct {
	mu        sync.RWMutex
	observers []Observer
}

// NewPublisher creates a Publisher with an optional initial observer list.
func NewPublisher(observers ...Observer) *Publisher {
	publisher := &Publisher{}
	for _, observer := range observers {
		publisher.Register(observer)
	}
	return publisher
}

// Register attaches an observer to the publisher.
func (p *Publisher) Register(observer Observer) {
	if observer == nil {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	p.observers = append(p.observers, observer)
}

// Notify sends event to all currently registered observers.
func (p *Publisher) Notify(ctx context.Context, event Event) error {
	p.mu.RLock()
	observers := append([]Observer(nil), p.observers...)
	p.mu.RUnlock()

	var errs []error
	for _, observer := range observers {
		if err := observer.Update(ctx, event); err != nil {
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}
