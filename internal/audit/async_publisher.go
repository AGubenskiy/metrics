package audit

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"
)

var (
	// ErrPublisherClosed is returned when an audit event is published after shutdown started.
	ErrPublisherClosed = errors.New("audit publisher is closed")
	// ErrQueueFull is returned when the async audit queue is full.
	ErrQueueFull = errors.New("audit queue is full")
)

// AsyncPublisherConfig controls async audit delivery.
type AsyncPublisherConfig struct {
	QueueSize       int
	Workers         int
	DeliveryTimeout time.Duration
}

// AsyncPublisher decouples HTTP request handling from slow audit observers.
type AsyncPublisher struct {
	publisher       *Publisher
	queue           chan Event
	deliveryTimeout time.Duration

	mu      sync.RWMutex
	closed  bool
	closeCh chan struct{}
	wg      sync.WaitGroup
}

// NewAsyncPublisher wraps publisher with a bounded async delivery queue.
func NewAsyncPublisher(publisher *Publisher, cfg AsyncPublisherConfig) *AsyncPublisher {
	if publisher == nil {
		publisher = NewPublisher()
	}
	if cfg.QueueSize <= 0 {
		cfg.QueueSize = 256
	}
	if cfg.Workers <= 0 {
		cfg.Workers = 1
	}
	if cfg.DeliveryTimeout <= 0 {
		cfg.DeliveryTimeout = 3 * time.Second
	}

	p := &AsyncPublisher{
		publisher:       publisher,
		queue:           make(chan Event, cfg.QueueSize),
		deliveryTimeout: cfg.DeliveryTimeout,
		closeCh:         make(chan struct{}),
	}

	p.wg.Add(cfg.Workers)
	for i := 0; i < cfg.Workers; i++ {
		go p.runWorker()
	}

	return p
}

// Notify enqueues an audit event for background delivery.
func (p *AsyncPublisher) Notify(ctx context.Context, event Event) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.closed {
		return ErrPublisherClosed
	}

	select {
	case p.queue <- event:
		return nil
	default:
		return ErrQueueFull
	}
}

// Close stops accepting new events and waits until queued deliveries finish.
func (p *AsyncPublisher) Close(ctx context.Context) error {
	p.mu.Lock()
	if !p.closed {
		p.closed = true
		close(p.queue)
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

func (p *AsyncPublisher) runWorker() {
	defer p.wg.Done()

	for event := range p.queue {
		ctx, cancel := context.WithTimeout(context.Background(), p.deliveryTimeout)
		if err := p.publisher.Notify(ctx, event); err != nil {
			log.Printf("cannot deliver audit event: %v", err)
		}
		cancel()
	}
}
