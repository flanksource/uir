package query

import (
	"container/list"
	"sync"
)

// lru is a concurrency-safe map that keeps at most capacity entries, evicting the least recently used.
// A capacity of zero keeps nothing.
type lru[K comparable, V any] struct {
	mu       sync.Mutex
	capacity int
	order    *list.List
	entries  map[K]*list.Element
}

type lruEntry[K comparable, V any] struct {
	key   K
	value V
}

func newLRU[K comparable, V any](capacity int) *lru[K, V] {
	return &lru[K, V]{capacity: capacity, order: list.New(), entries: map[K]*list.Element{}}
}

// get returns the value under key and marks it most recently used.
func (cache *lru[K, V]) get(key K) (V, bool) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	element, found := cache.entries[key]
	if !found {
		var zero V
		return zero, false
	}
	cache.order.MoveToFront(element)
	return element.Value.(*lruEntry[K, V]).value, true
}

// put stores value under key as the most recently used entry, evicting the least recently used ones
// beyond capacity.
func (cache *lru[K, V]) put(key K, value V) {
	if cache.capacity == 0 {
		return
	}
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if element, found := cache.entries[key]; found {
		element.Value.(*lruEntry[K, V]).value = value
		cache.order.MoveToFront(element)
		return
	}
	cache.entries[key] = cache.order.PushFront(&lruEntry[K, V]{key: key, value: value})
	for cache.order.Len() > cache.capacity {
		oldest := cache.order.Back()
		cache.order.Remove(oldest)
		delete(cache.entries, oldest.Value.(*lruEntry[K, V]).key)
	}
}
