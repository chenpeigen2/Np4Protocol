package mix

import (
    "testing"
    "time"
    "sync"
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

    engine.AddMessage(&MockEnvelope{ID: 1})
    engine.AddMessage(&MockEnvelope{ID: 2})
    engine.AddMessage(&MockEnvelope{ID: 3})

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

    engine.AddMessage(&MockEnvelope{ID: 1})
    engine.AddMessage(&MockEnvelope{ID: 2})

    time.Sleep(200 * time.Millisecond)

    mu.Lock()
    if len(received) != 2 {
        t.Errorf("expected 2 messages after timeout, got %d", len(received))
    }
    mu.Unlock()
}
