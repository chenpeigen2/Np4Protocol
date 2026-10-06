package message

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"sync"
)

// MessageType discriminates wire message kinds. Only TypeAsync is live
// today (mix delivery); the rest are protocol placeholders for [v2].
type MessageType int

const (
	TypeAsync MessageType = iota
)

type Message struct {
	Type     MessageType
	DestID   string
	SenderID string
	// Verified reports that SenderID was attributed by sender authentication
	// (pairwise tag). False means the sender is anonymous — either the tag
	// matched no known contact or the transport cannot authenticate (direct).
	// UIs badge unverified messages instead of dropping them.
	Verified bool
	Content  []byte
}

type MessageHandler func(*Message)

// MessageBus dispatches Messages to registered handlers via a fixed-size worker
// pool. This bounds concurrency and prevents goroutine explosion under load.
type MessageBus struct {
	handlers  []MessageHandler
	hu        sync.RWMutex
	ch        chan *Message
	quit      chan struct{}
	once      sync.Once
	startOnce sync.Once
	workers   int
}

// ErrBusFull is returned by Send when the internal queue is full.
var ErrBusFull = errors.New("message bus queue full")

// ErrBusClosed is returned by Send after Stop has been called.
var ErrBusClosed = errors.New("message bus closed")

// NewMessageBus creates a bus with a default of GOMAXPROCS*2 workers and a
// 1024-message buffer. Call Start before sending; call Stop to release workers.
func NewMessageBus() *MessageBus {
	workers := runtime.GOMAXPROCS(0) * 2
	if workers < 1 {
		workers = 1
	}
	return &MessageBus{
		ch:      make(chan *Message, 1024),
		quit:    make(chan struct{}),
		workers: workers,
	}
}

// Start launches the worker goroutines. Idempotent: subsequent calls are no-ops.
func (b *MessageBus) Start() {
	b.startOnce.Do(func() {
		for i := 0; i < b.workers; i++ {
			go b.worker()
		}
	})
}

func (b *MessageBus) worker() {
	for {
		select {
		case msg := <-b.ch:
			if msg == nil {
				return
			}
			b.hu.RLock()
			handlers := make([]MessageHandler, len(b.handlers))
			copy(handlers, b.handlers)
			b.hu.RUnlock()
			for _, h := range handlers {
				dispatch(h, msg)
			}
		case <-b.quit:
			return
		}
	}
}

// dispatch contains handler panics: handlers run in plain worker goroutines,
// so one panic would take the whole node down. Log and keep dispatching —
// a misbehaving application handler must not become a remote crash vector.
func dispatch(h MessageHandler, msg *Message) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "[message] handler panicked: %v\n", r)
		}
	}()
	h(msg)
}

// OnMessage registers a handler. Handlers must be safe to call from any worker
// goroutine.
func (b *MessageBus) OnMessage(handler MessageHandler) {
	b.hu.Lock()
	b.handlers = append(b.handlers, handler)
	b.hu.Unlock()
}

// Send enqueues msg for dispatch. Returns ErrBusFull if the buffer is full
// (non-blocking) or ErrBusClosed after Stop.
func (b *MessageBus) Send(msg *Message) error {
	if msg == nil {
		return errors.New("message is nil")
	}
	// Check shutdown FIRST and deterministically: a plain select would race
	// the closed-quit case against the buffered-channel case, randomly
	// reporting success on a dead bus.
	select {
	case <-b.quit:
		return ErrBusClosed
	default:
	}
	select {
	case b.ch <- msg:
		return nil
	default:
		return ErrBusFull
	}
}

// Stop signals all workers to exit. Idempotent.
func (b *MessageBus) Stop() {
	b.once.Do(func() {
		close(b.quit)
	})
}
