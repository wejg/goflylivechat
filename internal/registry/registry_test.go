package registry

import (
	"sync"
	"testing"
)

func TestRegistryConcurrentAccess(t *testing.T) {
	r := New[int]()
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r.Set("user", i)
			_, _ = r.Get("user")
			_ = r.Snapshot()
		}(i)
	}
	wg.Wait()

	if r.Len() != 1 {
		t.Fatalf("expected one user, got %d", r.Len())
	}
	if _, ok := r.Get("user"); !ok {
		t.Fatal("expected user to be present")
	}
	r.Delete("user")
	if r.Len() != 0 {
		t.Fatalf("expected empty registry, got %d", r.Len())
	}
}
