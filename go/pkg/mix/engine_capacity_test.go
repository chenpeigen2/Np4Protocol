package mix

import (
	"errors"
	"sync"
	"testing"
	"testing/synctest"
	"time"
)

// TDD: the entry/relay mix buffers were unbounded — a sender (or a hostile
// relay flood) outrunning flushes grows memory without limit, and the
// protocol's BATCH_FULL error had no producer. MixEngine grows a capacity
// option; Add must fail with ErrMixFull once the buffer is at capacity.
func TestMixEngineCapacityRejectsWhenFull(t *testing.T) {
	var flushed int
	engine := NewMixEngine[int](10, time.Hour, func(batch []*int) { flushed += len(batch) },
		WithCapacity[int](3))

	for i := 0; i < 3; i++ {
		v := i
		if err := engine.Add(&v); err != nil {
			t.Fatalf("Add %d into fresh capacity-3 engine: %v", i, err)
		}
	}
	if engine.Pending() != 3 {
		t.Fatalf("pending = %d, want 3", engine.Pending())
	}
	extra := 99
	if err := engine.Add(&extra); !errors.Is(err, ErrMixFull) {
		t.Fatalf("4th Add into capacity-3 engine: got %v, want ErrMixFull", err)
	}
	if flushed != 0 {
		t.Fatalf("nothing should have flushed yet, flushed %d", flushed)
	}
}

// TestMixEngineCapacityFreesSpaceOnFlush: a batch flush empties the buffer,
// so previously-at-capacity engines accept new messages again.
func TestMixEngineCapacityFreesSpaceOnFlush(t *testing.T) {
	engine := NewMixEngine[int](2, time.Hour, func([]*int) {}, WithCapacity[int](2))

	v1, v2, v3 := 1, 2, 3
	if err := engine.Add(&v1); err != nil {
		t.Fatalf("Add 1: %v", err)
	}
	// Add 2 reaches batchSize → immediate flush → buffer empty again.
	if err := engine.Add(&v2); err != nil {
		t.Fatalf("Add 2 (triggers batch flush): %v", err)
	}
	if engine.Pending() != 0 {
		t.Fatalf("pending after batch flush = %d, want 0", engine.Pending())
	}
	if err := engine.Add(&v3); err != nil {
		t.Fatalf("Add after flush must succeed: %v", err)
	}
	if engine.Pending() != 1 {
		t.Fatalf("pending = %d, want 1", engine.Pending())
	}
}

// TestMixEngineAddAfterCloseFails pins the closed-engine contract.
func TestMixEngineAddAfterCloseFails(t *testing.T) {
	engine := NewMixEngine[int](4, time.Hour, func([]*int) {})
	if err := engine.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	v := 1
	if err := engine.Add(&v); err == nil {
		t.Fatal("Add after Close must fail")
	}
	if engine.Pending() != 0 {
		t.Fatalf("pending after Close+Add = %d, want 0", engine.Pending())
	}
}

// TestMixEngineConcurrentAddsNoneLost hammers Add from many goroutines with
// the race detector and asserts every message is flushed exactly once.
func TestMixEngineConcurrentAddsNoneLost(t *testing.T) {
	const goroutines, perG = 16, 50
	var mu sync.Mutex
	seen := make(map[*int]int)

	engine := NewMixEngine[int](7, 5*time.Millisecond, func(batch []*int) {
		mu.Lock()
		defer mu.Unlock()
		for _, m := range batch {
			seen[m]++
		}
	}, WithCapacity[int](goroutines*perG))

	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < perG; i++ {
				v := g*perG + i
				if err := engine.Add(&v); err != nil {
					t.Errorf("Add lost message: %v", err)
					return
				}
			}
		}(g)
	}
	wg.Wait()

	deadline := time.Now().Add(5 * time.Second)
	for engine.Pending() > 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if err := engine.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(seen) != goroutines*perG {
		t.Fatalf("flushed %d unique messages, want %d", len(seen), goroutines*perG)
	}
	for m, n := range seen {
		if n != 1 {
			t.Fatalf("message %p flushed %d times, want exactly 1", m, n)
		}
	}
}

// TestMixEngineNoFlushAfterClose: a flush timer racing Close must not
// double-deliver. The synctest bubble freezes time so the race window is
// deterministic (sleeping 3s past the 1s timer fires instantly).
func TestMixEngineNoFlushAfterClose(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		flushes := 0
		engine := NewMixEngine[int](100, 1*time.Second, func(batch []*int) { flushes += len(batch) })
		v := 1
		if err := engine.Add(&v); err != nil {
			t.Fatalf("Add: %v", err)
		}
		if err := engine.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
		time.Sleep(3 * time.Second) // would fire the pending timer if not stopped
		if flushes != 1 {
			t.Fatalf("flushed %d messages across Close+timer, want exactly 1", flushes)
		}
	})
}
