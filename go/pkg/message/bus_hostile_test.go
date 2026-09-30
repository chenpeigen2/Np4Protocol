package message

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TDD: handlers run in worker goroutines with no recovery — one panicking
// handler kills the entire node process. The bus must contain handler panics
// (log-and-continue) and keep dispatching afterwards.
func TestBusSurvivesPanickingHandler(t *testing.T) {
	bus := NewMessageBus()

	var good int32
	panicOnce := func(*Message) { panic("boom from handler") }
	counting := func(*Message) { atomic.AddInt32(&good, 1) }

	bus.OnMessage(panicOnce)
	bus.OnMessage(counting)
	bus.Start()

	for i := 0; i < 50; i++ {
		if err := bus.Send(&Message{Type: TypeAsync, Content: []byte("x")}); err != nil {
			t.Fatalf("Send %d: %v", i, err)
		}
	}
	deadline := time.Now().Add(5 * time.Second)
	for atomic.LoadInt32(&good) < 50 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := atomic.LoadInt32(&good); got != 50 {
		t.Fatalf("healthy handler saw %d/50 messages — dispatch died with the panics", got)
	}
}

// TDD: after Stop, Send must ALWAYS return ErrBusClosed. The current select
// races the closed-quit case against the buffer case, so sends to a dead bus
// sometimes report success.
func TestSendAfterStopAlwaysClosed(t *testing.T) {
	bus := NewMessageBus()
	bus.Start()
	bus.Stop()

	var successes int
	for i := 0; i < 200; i++ {
		if err := bus.Send(&Message{Type: TypeAsync}); err == nil {
			successes++
		} else if !errors.Is(err, ErrBusClosed) {
			t.Fatalf("Send after Stop returned %v, want ErrBusClosed", err)
		}
	}
	if successes != 0 {
		t.Fatalf("%d/200 sends to a stopped bus reported success", successes)
	}
}

// TDD: Start is documented as idempotent; calling it twice must not double
// the worker pool (observable: total handler invocations stay exactly N).
func TestStartIsIdempotent(t *testing.T) {
	bus := NewMessageBus()
	var count int32
	bus.OnMessage(func(*Message) { atomic.AddInt32(&count, 1) })
	bus.Start()
	bus.Start() // second call must be a no-op

	const n = 10
	for i := 0; i < n; i++ {
		if err := bus.Send(&Message{Type: TypeAsync}); err != nil {
			t.Fatalf("Send: %v", err)
		}
	}
	deadline := time.Now().Add(5 * time.Second)
	for atomic.LoadInt32(&count) < n && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(100 * time.Millisecond) // let any duplicate-worker double-dispatch land
	if got := atomic.LoadInt32(&count); got != n {
		t.Fatalf("handler invoked %d times for %d messages — Start is not idempotent", got, n)
	}
}

// TestConcurrentSendAndStop: hammering Send while Stop fires must never
// panic, block indefinitely, or return anything but nil/ErrBusFull/ErrBusClosed.
func TestConcurrentSendAndStop(t *testing.T) {
	bus := NewMessageBus()
	bus.Start()

	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				err := bus.Send(&Message{Type: TypeAsync})
				if err != nil && !errors.Is(err, ErrBusFull) && !errors.Is(err, ErrBusClosed) {
					t.Errorf("unexpected error: %v", err)
					return
				}
			}
		}()
	}
	time.Sleep(20 * time.Millisecond)
	bus.Stop()
	wg.Wait()
}
