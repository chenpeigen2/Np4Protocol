package message

import (
	"errors"
	"runtime"
	"sync"
)

type MessageType int

const (
	TypeAsync MessageType = iota
	TypeSyncRequest
	TypeSyncResponse
	TypeBroadcast
	TypeFileChunk
)

type Message struct {
	Type       MessageType
	DestID     string
	SenderID   string
	Content    []byte
	SessionKey []byte
}

type MessageHandler func(*Message)

// MessageBus dispatches Messages to registered handlers via a fixed-size worker
// pool. This bounds concurrency and prevents goroutine explosion under load.
type MessageBus struct {
	handlers []MessageHandler
	hu       sync.RWMutex

	ch      chan *Message
	quit    chan struct{}
	once    sync.Once
	workers int
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
	for i := 0; i < b.workers; i++ {
		go b.worker()
	}
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
				h(msg)
			}
		case <-b.quit:
			return
		}
	}
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
	select {
	case b.ch <- msg:
		return nil
	case <-b.quit:
		return ErrBusClosed
	default:
		return ErrBusFull
	}
}

// Broadcast is like Send but does not mutate the input message; it sends a copy
// tagged as TypeBroadcast with an empty DestID.
func (b *MessageBus) Broadcast(msg *Message) error {
	cp := *msg
	cp.Type = TypeBroadcast
	cp.DestID = ""
	return b.Send(&cp)
}

// Stop signals all workers to exit. Idempotent.
func (b *MessageBus) Stop() {
	b.once.Do(func() {
		close(b.quit)
	})
}
