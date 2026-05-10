package audit

import (
	"context"
	"errors"
	"sync"
)

type Event struct {
	TS        int64    `json:"ts"`
	Metrics   []string `json:"metrics"`
	IPAddress string   `json:"ip_address"`
}

type Observer interface {
	Update(ctx context.Context, event Event) error
}

type Publisher struct {
	mu        sync.RWMutex
	observers []Observer
}

func NewPublisher(observers ...Observer) *Publisher {
	publisher := &Publisher{}
	for _, observer := range observers {
		publisher.Register(observer)
	}
	return publisher
}

func (p *Publisher) Register(observer Observer) {
	if observer == nil {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	p.observers = append(p.observers, observer)
}

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
