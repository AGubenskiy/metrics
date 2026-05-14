package audit

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"
)

var (
	// ErrPublisherClosed is returned when an audit event is published after shutdown started.
	ErrPublisherClosed = errors.New("audit publisher is closed")
	// ErrQueueFull is returned when at least one async observer queue is full.
	ErrQueueFull = errors.New("audit queue is full")
	// ErrNilPublisher is returned when async publisher is created without a base publisher.
	ErrNilPublisher = errors.New("audit publisher is nil")
	// ErrInvalidQueueSize is returned when async publisher queue size is not positive.
	ErrInvalidQueueSize = errors.New("audit queue size must be positive")
	// ErrInvalidDeliveryTimeout is returned when async publisher delivery timeout is not positive.
	ErrInvalidDeliveryTimeout = errors.New("audit delivery timeout must be positive")
)

// AsyncPublisherConfig controls async audit delivery.
type AsyncPublisherConfig struct {
	QueueSize       int
	DeliveryTimeout time.Duration
}

type observerQueue struct {
	observer Observer
	queue    chan Event
}

// AsyncPublisher decouples HTTP request handling from slow audit observers.
type AsyncPublisher struct {
	observers       []observerQueue
	deliveryTimeout time.Duration

	mu      sync.RWMutex
	closed  bool
	closeCh chan struct{}
	wg      sync.WaitGroup
}

// NewAsyncPublisher wraps publisher with a bounded async delivery queue.
func NewAsyncPublisher(publisher *Publisher, cfg AsyncPublisherConfig) (*AsyncPublisher, error) {
	if publisher == nil {
		return nil, ErrNilPublisher
	}
	if cfg.QueueSize <= 0 {
		return nil, ErrInvalidQueueSize
	}
	if cfg.DeliveryTimeout <= 0 {
		return nil, ErrInvalidDeliveryTimeout
	}

	publisher.mu.RLock()
	observers := append([]Observer(nil), publisher.observers...)
	publisher.mu.RUnlock()

	p := &AsyncPublisher{
		observers:       make([]observerQueue, 0, len(observers)),
		deliveryTimeout: cfg.DeliveryTimeout,
		closeCh:         make(chan struct{}),
	}

	for _, observer := range observers {
		observerQueue := observerQueue{
			observer: observer,
			queue:    make(chan Event, cfg.QueueSize),
		}
		p.observers = append(p.observers, observerQueue)
		p.wg.Add(1)
		go p.runWorker(observerQueue.observer, observerQueue.queue)
	}

	return p, nil
}

// Notify enqueues an audit event for background delivery to every configured observer.
func (p *AsyncPublisher) Notify(ctx context.Context, event Event) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.closed {
		return ErrPublisherClosed
	}

	var errs []error
	for _, observerQueue := range p.observers {
		select {
		case observerQueue.queue <- event:
		default:
			errs = append(errs, fmt.Errorf("%w: observer %T", ErrQueueFull, observerQueue.observer))
		}
	}

	return errors.Join(errs...)
}

// Close stops accepting new events and waits until queued deliveries finish.
func (p *AsyncPublisher) Close(ctx context.Context) error {
	p.mu.Lock()
	if !p.closed {
		p.closed = true
		for _, observerQueue := range p.observers {
			close(observerQueue.queue)
		}
		go func() {
			p.wg.Wait()
			close(p.closeCh)
		}()
	}
	p.mu.Unlock()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-p.closeCh:
		return nil
	}
}

func (p *AsyncPublisher) runWorker(observer Observer, queue <-chan Event) {
	defer p.wg.Done()

	for event := range queue {
		ctx, cancel := context.WithTimeout(context.Background(), p.deliveryTimeout)
		if err := observer.Update(ctx, event); err != nil {
			log.Printf("cannot deliver audit event to %T: %v", observer, err)
		}
		cancel()
	}
}
