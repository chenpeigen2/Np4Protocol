package mix

import (
	"sync"
	"testing"
	"time"
)

// TDD: flushLocked runs inside the timer goroutine (and inside Add's caller).
// A panicking onFlush there kills the whole process — fatal for a long-lived
// relay. The engine must isolate callback panics and stay usable.
func TestMixEngineSurvivesPanickingFlush(t *testing.T) {
	var panickedOnce bool
	engine := NewMixEngine[int](2, time.Hour, func(batch []*int) {
		if !panickedOnce {
			panickedOnce = true
			panic("boom from onFlush")
		}
	})

	a, b := 1, 2
	if err := engine.Add(&a); err != nil {
		t.Fatalf("Add a: %v", err)
	}
	if err := engine.Add(&b); err != nil {
		t.Fatalf("Add b (triggers panicking flush): %v", err)
	}
	// The panic was contained: the engine is still alive and drained.
	if engine.Pending() != 0 {
		t.Fatalf("pending = %d, want 0 (batch detached before callback)", engine.Pending())
	}
	if err := engine.Close(); err != nil {
		t.Fatalf("Close after panicking flush: %v", err)
	}
}

// TestMixEngineTimerFlushPanicContained: the same containment on the timer
// path — a panic in a time.AfterFunc callback would otherwise kill the
// process with no recovery opportunity.
func TestMixEngineTimerFlushPanicContained(t *testing.T) {
	done := make(chan struct{})
	var once sync.Once
	engine := NewMixEngine[int](1000, 20*time.Millisecond, func([]*int) {
		once.Do(func() {
			panic("boom from timer flush")
		})
	})
	v := 1
	if err := engine.Add(&v); err != nil {
		t.Fatalf("Add: %v", err)
	}
	// If the panic escaped the timer goroutine, the test binary dies before
	// this receive — the pass itself is the assertion.
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
	}
	if err := engine.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if engine.Pending() != 0 {
		t.Fatalf("pending = %d, want 0", engine.Pending())
	}
}
