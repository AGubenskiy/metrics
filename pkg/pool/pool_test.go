package pool

import "testing"

type stub struct {
	value int
}

func (s *stub) Reset() {
	if s == nil {
		return
	}

	s.value = 0
}

func TestGetUsesFactory(t *testing.T) {
	p := New(func() *stub {
		return &stub{value: 42}
	})

	got := p.Get()
	if got == nil {
		t.Fatal("Get() returned nil")
	}
	if got.value != 42 {
		t.Fatalf("Get() value = %d, want 42", got.value)
	}
}

func TestPutResetsValueBeforeReuse(t *testing.T) {
	p := New(func() *stub {
		return &stub{}
	})

	value := &stub{value: 99}
	p.Put(value)

	got := p.Get()
	if got != value {
		t.Fatalf("Get() returned unexpected pointer: got %p, want %p", got, value)
	}
	if got.value != 0 {
		t.Fatalf("Get() value = %d, want 0", got.value)
	}
}

func TestPutNilDoesNothing(t *testing.T) {
	p := New(func() *stub {
		return &stub{value: 7}
	})

	p.Put(nil)

	got := p.Get()
	if got == nil {
		t.Fatal("Get() returned nil")
	}
	if got.value != 7 {
		t.Fatalf("Get() value = %d, want 7", got.value)
	}
}
