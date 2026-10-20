package registry

import "sync"

// Registry stores live connections without exposing a map to concurrent callers.
type Registry[T any] struct {
	mu    sync.RWMutex
	items map[string]T
}

func New[T any]() *Registry[T] {
	return &Registry[T]{items: make(map[string]T)}
}

func (r *Registry[T]) Set(key string, value T) {
	r.mu.Lock()
	r.items[key] = value
	r.mu.Unlock()
}

func (r *Registry[T]) Get(key string) (T, bool) {
	r.mu.RLock()
	value, ok := r.items[key]
	r.mu.RUnlock()
	return value, ok
}

func (r *Registry[T]) Delete(key string) {
	r.mu.Lock()
	delete(r.items, key)
	r.mu.Unlock()
}

func (r *Registry[T]) Snapshot() []T {
	r.mu.RLock()
	values := make([]T, 0, len(r.items))
	for _, value := range r.items {
		values = append(values, value)
	}
	r.mu.RUnlock()
	return values
}

func (r *Registry[T]) Len() int {
	r.mu.RLock()
	length := len(r.items)
	r.mu.RUnlock()
	return length
}
