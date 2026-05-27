package pool

import (
	"reflect"
	"sync"
)

// Resetter describes values that can reset their internal state before reuse.
type Resetter interface {
	Reset()
}

// Pool stores reusable values of one concrete type that implements Resetter.
type Pool[T Resetter] struct {
	pool sync.Pool
}

// New creates a pool that lazily allocates values with factory when it is empty.
func New[T Resetter](factory func() T) *Pool[T] {
	p := &Pool[T]{}
	if factory != nil {
		p.pool.New = func() any {
			return factory()
		}
	}

	return p
}

// Get returns a value from the pool or creates a new one through the factory.
func (p *Pool[T]) Get() T {
	value := p.pool.Get()
	if typed, ok := value.(T); ok {
		return typed
	}

	var zero T
	return zero
}

// Put resets the value and returns it to the pool for reuse.
func (p *Pool[T]) Put(value T) {
	if isNil(value) {
		return
	}

	value.Reset()
	p.pool.Put(value)
}

func isNil[T any](value T) bool {
	v := reflect.ValueOf(value)
	if !v.IsValid() {
		return true
	}

	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	default:
		return false
	}
}
