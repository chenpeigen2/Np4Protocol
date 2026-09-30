package mix

import (
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	mathrand "math/rand"
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
// is reached or maxDelay elapses. The shuffle order is seeded from crypto/rand
// so it varies across process restarts.
type MixEngine[T any] struct {
	buffer    []*T
	batchSize int
	maxDelay  time.Duration
	capacity  int
	onFlush   func([]*T)
	mu        sync.Mutex
	timer     *time.Timer
	closed    bool
	rnd       *lockedRand
}

// lockedRand wraps math/rand.Rand with a mutex for concurrent Shuffle use.
type lockedRand struct {
	mu sync.Mutex
	r  *mathrand.Rand
}

func (lr *lockedRand) Shuffle(n int, swap func(i, j int)) {
	lr.mu.Lock()
	defer lr.mu.Unlock()
	lr.r.Shuffle(n, swap)
}

func NewMixEngine[T any](batchSize int, maxDelay time.Duration, onFlush func([]*T), opts ...Option[T]) *MixEngine[T] {
	seedInt, err := rand.Int(rand.Reader, big.NewInt(1<<62))
	if err != nil {
		// crypto/rand failure is exceptional; fall back to time-based seed.
		seedInt = big.NewInt(time.Now().UnixNano())
	}
	m := &MixEngine[T]{
		batchSize: batchSize,
		maxDelay:  maxDelay,
		onFlush:   onFlush,
		rnd:       &lockedRand{r: mathrand.New(mathrand.NewSource(seedInt.Int64()))},
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
	m.rnd.Shuffle(len(m.buffer), func(i, j int) {
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
