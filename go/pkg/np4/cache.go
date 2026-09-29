package np4

import (
	"container/list"
	"sync"
)

// seenCache is a fixed-capacity FIFO set used for replay detection (ephemeral
// onion keys) and end-to-end message-ID dedup. When full, the oldest entry is
// evicted — a bounded replay window, not a growing leak.
type seenCache struct {
	mu  sync.Mutex
	cap int
	ll  *list.List // values are string keys; front = oldest
	m   map[string]*list.Element
}

func newSeenCache(capacity int) *seenCache {
	if capacity < 1 {
		capacity = 1
	}
	return &seenCache{
		cap: capacity,
		ll:  list.New(),
		m:   make(map[string]*list.Element, capacity),
	}
}

// Mark records key and reports whether it was already present. Safe for
// concurrent use; exactly one caller per key observes false.
func (c *seenCache) Mark(key string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.m[key]; ok {
		return true
	}
	c.m[key] = c.ll.PushBack(key)
	if c.ll.Len() > c.cap {
		oldest := c.ll.Front()
		c.ll.Remove(oldest)
		delete(c.m, oldest.Value.(string))
	}
	return false
}

// Len reports the number of cached entries (diagnostics/tests only).
func (c *seenCache) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ll.Len()
}
