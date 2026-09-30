package np4

import (
	"fmt"
	"sync"
	"testing"
)

// TestSeenCacheMarkDedup: exactly one caller per key observes "not seen".
func TestSeenCacheMarkDedup(t *testing.T) {
	c := newSeenCache(4)
	if c.Mark("a") {
		t.Fatal("first Mark of 'a' reported already-seen")
	}
	if !c.Mark("a") {
		t.Fatal("second Mark of 'a' must report already-seen")
	}
	if c.Len() != 1 {
		t.Fatalf("len = %d, want 1", c.Len())
	}
}

// TestSeenCacheEvictionOrder: capacity-N cache evicts oldest first. Assert
// presence BEFORE probing an evicted key — Mark re-inserts, so probing "a"
// would evict "b" and corrupt the later assertion.
func TestSeenCacheEvictionOrder(t *testing.T) {
	c := newSeenCache(2)
	c.Mark("a")
	c.Mark("b")
	// "a" is oldest: filling with "c" evicts it, "b" survives.
	c.Mark("c")
	if !c.Mark("b") {
		t.Fatal("'b' should still be cached")
	}
	if c.Mark("a") {
		t.Fatal("'a' should have been evicted (oldest)")
	}
	if c.Len() != 2 {
		t.Fatalf("len = %d, want 2", c.Len())
	}
}

// TestSeenCacheFifoNotLru: Marking an existing key must not extend its life
// — FIFO, not LRU. Sequence a, b, re-mark a, c: FIFO evicts "a" (oldest
// insertion), LRU would evict "b" (least recently *used*).
func TestSeenCacheFifoNotLru(t *testing.T) {
	c := newSeenCache(2)
	c.Mark("a")
	c.Mark("b")
	c.Mark("a") // re-Mark must NOT move "a" to the back of the eviction order
	c.Mark("c") // capacity 2 → one eviction
	if !c.Mark("b") {
		t.Fatal("'b' must still be present — 'a' (oldest insertion) was the victim (FIFO, not LRU)")
	}
	if c.Mark("a") {
		t.Fatal("'a' must have been evicted first (FIFO, not LRU)")
	}
}

// TestSeenCacheConcurrentMarkExactlyOnce: under concurrency, exactly one
// Mark per key reports false — the anti-replay contract.
func TestSeenCacheConcurrentMarkExactlyOnce(t *testing.T) {
	const goroutines, keys = 8, 200
	c := newSeenCache(1000)

	var wg sync.WaitGroup
	fresh := make([]int, keys)
	var mu sync.Mutex
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for k := 0; k < keys; k++ {
				key := fmt.Sprintf("key-%d", k)
				if !c.Mark(key) {
					mu.Lock()
					fresh[k]++
					mu.Unlock()
				}
			}
		}()
	}
	wg.Wait()

	for k, n := range fresh {
		if n != 1 {
			t.Fatalf("key-%d observed fresh %d times, want exactly 1", k, n)
		}
	}
}

// TestSeenCacheCapacityOne: degenerate capacity still works (evicts every
// time).
func TestSeenCacheCapacityOne(t *testing.T) {
	c := newSeenCache(1)
	c.Mark("x")
	if c.Mark("x") != true {
		t.Fatal("duplicate within capacity 1 must be seen")
	}
	c.Mark("y")
	if c.Mark("x") != false {
		t.Fatal("'x' must have been evicted when 'y' arrived (capacity 1)")
	}
}
