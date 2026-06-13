package mix

import (
	"sync"
	"testing"
	"time"
)

type MockEnvelope struct {
	ID int
}

func TestMixEngineBatchFlush(t *testing.T) {
	var received []*MockEnvelope
	var mu sync.Mutex

	engine := NewMixEngine(3, 1*time.Second, func(batch []*MockEnvelope) {
		mu.Lock()
		received = append(received, batch...)
		mu.Unlock()
	})

	_ = engine.Add(&MockEnvelope{ID: 1})
	_ = engine.Add(&MockEnvelope{ID: 2})
	_ = engine.Add(&MockEnvelope{ID: 3})

	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	if len(received) != 3 {
		t.Errorf("expected 3 messages, got %d", len(received))
	}
	mu.Unlock()
}

func TestMixEngineTimeout(t *testing.T) {
	var received []*MockEnvelope
	var mu sync.Mutex

	engine := NewMixEngine(10, 100*time.Millisecond, func(batch []*MockEnvelope) {
		mu.Lock()
		received = append(received, batch...)
		mu.Unlock()
	})

	_ = engine.Add(&MockEnvelope{ID: 1})
	_ = engine.Add(&MockEnvelope{ID: 2})

	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	if len(received) != 2 {
		t.Errorf("expected 2 messages after timeout, got %d", len(received))
	}
	mu.Unlock()
}

func TestMixEngineClose(t *testing.T) {
	engine := NewMixEngine(10, 1*time.Second, func(batch []*MockEnvelope) {})
	if err := engine.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := engine.Add(&MockEnvelope{ID: 1}); err == nil {
		t.Fatal("Add after Close should error")
	}
}

func TestMixEngineCloseFlushesPending(t *testing.T) {
	var mu sync.Mutex
	var got []*MockEnvelope
	engine := NewMixEngine(10, 1*time.Hour, func(b []*MockEnvelope) {
		mu.Lock()
		got = b
		mu.Unlock()
	})
	_ = engine.Add(&MockEnvelope{ID: 1})
	_ = engine.Add(&MockEnvelope{ID: 2})
	_ = engine.Close()

	mu.Lock()
	defer mu.Unlock()
	if len(got) != 2 {
		t.Errorf("expected 2 flushed on Close, got %d", len(got))
	}
}
