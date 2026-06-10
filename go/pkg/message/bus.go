package message

import (
	"errors"
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

type MessageBus struct {
	handlers []MessageHandler
	mu       sync.RWMutex
}

func NewMessageBus() *MessageBus {
	return &MessageBus{}
}

func (b *MessageBus) OnMessage(handler MessageHandler) {
	b.mu.Lock()
	b.handlers = append(b.handlers, handler)
	b.mu.Unlock()
}

func (b *MessageBus) Send(msg *Message) error {
	if msg == nil {
		return errors.New("message is nil")
	}

	b.mu.RLock()
	handlers := make([]MessageHandler, len(b.handlers))
	copy(handlers, b.handlers)
	b.mu.RUnlock()

	for _, handler := range handlers {
		go handler(msg)
	}

	return nil
}

func (b *MessageBus) Broadcast(msg *Message) error {
	msg.Type = TypeBroadcast
	msg.DestID = ""
	return b.Send(msg)
}
