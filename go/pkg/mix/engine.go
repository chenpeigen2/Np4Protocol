package mix

import (
    "math/rand"
    "sync"
    "time"
)

type MixEngine[T any] struct {
    buffer    []*T
    batchSize int
    maxDelay  time.Duration
    onFlush   func([]*T)
    mu        sync.Mutex
    timer     *time.Timer
}

func NewMixEngine[T any](batchSize int, maxDelay time.Duration, onFlush func([]*T)) *MixEngine[T] {
    return &MixEngine[T]{
        batchSize: batchSize,
        maxDelay:  maxDelay,
        onFlush:   onFlush,
    }
}

func (m *MixEngine[T]) AddMessage(msg *T) {
    m.mu.Lock()
    defer m.mu.Unlock()

    m.buffer = append(m.buffer, msg)

    if len(m.buffer) >= m.batchSize {
        m.flush()
        return
    }

    if m.timer == nil {
        m.timer = time.AfterFunc(m.maxDelay, func() {
            m.mu.Lock()
            m.flush()
            m.mu.Unlock()
        })
    }
}

func (m *MixEngine[T]) flush() {
    if len(m.buffer) == 0 {
        return
    }

    if m.timer != nil {
        m.timer.Stop()
        m.timer = nil
    }

    // Fisher-Yates shuffle
    rand.Shuffle(len(m.buffer), func(i, j int) {
        m.buffer[i], m.buffer[j] = m.buffer[j], m.buffer[i]
    })

    batch := m.buffer
    m.buffer = nil

    go m.onFlush(batch)
}
