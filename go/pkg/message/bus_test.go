package message

import (
	"testing"
	"time"
	"sync"
)

func TestMessageBusSendReceive(t *testing.T) {
	bus := NewMessageBus()

	var received *Message
	var mu sync.Mutex

	bus.OnMessage(func(msg *Message) {
		mu.Lock()
		received = msg
		mu.Unlock()
	})

	msg := &Message{
		Type:    TypeAsync,
		DestID:  "node1",
		Content: []byte("hello"),
	}

	err := bus.Send(msg)
	if err != nil {
		t.Fatal(err)
	}

	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	if received == nil {
		t.Error("message not received")
	}
	if string(received.Content) != "hello" {
		t.Errorf("expected 'hello', got '%s'", string(received.Content))
	}
	mu.Unlock()
}
