package mix

import (
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"os"
	"sync"
	"time"
)

// ErrMixFull is returned by Add when the engine is at its configured
// capacity — the protocol's BATCH_FULL backpressure signal.
var ErrMixFull = errors.New("mix buffer full")

// Option configures a MixEngine at construction time.
type Option[T any] func(*MixEngine[T])

// WithCapacity bounds the buffer at n messages. When full, Add returns
// ErrMixFull (backpressure) instead of growing memory without limit. Zero or
// negative means unbounded (the historical behavior; tests only).
func WithCapacity[T any](n int) Option[T] {
	return func(m *MixEngine[T]) {
		if n > 0 {
			m.capacity = n
		}
	}
}

// MixEngine batches messages, shuffles them, and flushes either when batchSize
// is reached or maxDelay elapses.
//
// The shuffle draws fresh randomness from crypto/rand per swap — STATELESS by
// design. A seeded PRNG (the historical approach) carries a brute-forceable
// seed: batch permutations are observable output (~22 bits of constraint per
// 10-item batch), so a global observer recording long enough could recover a
// 62-bit offline seed and replay every historical shuffle mapping.
type MixEngine[T any] struct {
	buffer    []*T
	batchSize int
	maxDelay  time.Duration
	capacity  int
	onFlush   func([]*T)
	mu        sync.Mutex
	timer     *time.Timer
	closed    bool
}

// cryptoShuffle is a Fisher-Yates shuffle drawing each swap index from
// crypto/rand — no PRNG state to recover. On the (exceptional) failure of
// rand.Reader it returns early, flushing in near-original order: degraded
// mixing for one batch beats losing the process or stalling the queue.
func cryptoShuffle(n int, swap func(i, j int)) {
	for i := n - 1; i > 0; i-- {
		j, err := rand.Int(rand.Reader, big.NewInt(int64(i+1)))
		if err != nil {
			fmt.Fprintf(os.Stderr, "[mix] crypto/rand failed, flushing partially unshuffled: %v\n", err)
			return
		}
		swap(i, int(j.Int64()))
	}
}

func NewMixEngine[T any](batchSize int, maxDelay time.Duration, onFlush func([]*T), opts ...Option[T]) *MixEngine[T] {
	m := &MixEngine[T]{
		batchSize: batchSize,
		maxDelay:  maxDelay,
		onFlush:   onFlush,
	}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

// Add enqueues msg. Returns ErrMixFull when the engine is at capacity and
// an error if the engine is closed.
func (m *MixEngine[T]) Add(msg *T) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return errors.New("mix engine closed")
	}
	if m.capacity > 0 && len(m.buffer) >= m.capacity {
		return ErrMixFull
	}
	m.buffer = append(m.buffer, msg)
	if len(m.buffer) >= m.batchSize {
		m.flushLocked()
		return nil
	}
	if m.timer == nil {
		m.timer = time.AfterFunc(m.maxDelay, func() {
			m.mu.Lock()
			m.flushLocked()
			m.mu.Unlock()
		})
	}
	return nil
}

// Close stops the timer, flushes any pending messages synchronously, and rejects
// future Add calls. Safe to call multiple times.
func (m *MixEngine[T]) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil
	}
	m.closed = true
	if m.timer != nil {
		m.timer.Stop()
		m.timer = nil
	}
	m.flushLocked()
	return nil
}

// Pending reports the number of buffered messages not yet flushed.
func (m *MixEngine[T]) Pending() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.buffer)
}

// flushLocked shuffles and dispatches the current buffer. Caller must hold m.mu.
// onFlush is called synchronously so callers can coordinate shutdown.
//
// The callback runs in the timer goroutine (or inside Add's caller); a panic
// there would take the whole node down. The batch is already detached from
// the buffer, so the panic is contained, logged, and the engine keeps
// serving — losing one batch beats losing the process.
func (m *MixEngine[T]) flushLocked() {
	if len(m.buffer) == 0 {
		return
	}
	if m.timer != nil {
		m.timer.Stop()
		m.timer = nil
	}
	cryptoShuffle(len(m.buffer), func(i, j int) {
		m.buffer[i], m.buffer[j] = m.buffer[j], m.buffer[i]
	})
	batch := m.buffer
	m.buffer = nil
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "[mix] flush callback panicked: %v\n", r)
		}
	}()
	m.onFlush(batch)
}
