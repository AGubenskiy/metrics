package pool

import "testing"

type stub struct {
	value      int
	resetCalls int
}

func (s *stub) Reset() {
	if s == nil {
		return
	}

	s.resetCalls++
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

func TestPutResetsValueBeforeReturningToPool(t *testing.T) {
	p := New(func() *stub {
		return &stub{}
	})

	value := &stub{value: 99}
	p.Put(value)

	if value.resetCalls == 0 {
		t.Fatal("Put() did not call Reset()")
	}
	if value.value != 0 {
		t.Fatalf("Put() did not reset value: got %d, want 0", value.value)
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
