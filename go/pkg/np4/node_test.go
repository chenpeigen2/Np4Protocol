package np4

import (
	"Np4Protocol/go/pkg/message"
	"sync"
	"testing"
	"time"
)

func TestNodeSendReceive(t *testing.T) {
	nodeA, err := NewNode(0)
	if err != nil {
		t.Fatal(err)
	}
	defer nodeA.Stop()

	nodeB, err := NewNode(0)
	if err != nil {
		t.Fatal(err)
	}
	defer nodeB.Stop()

	// Connect A -> B
	err = nodeA.Connect(nodeB.Host().Addrs(), nodeB.ID())
	if err != nil {
		t.Fatal(err)
	}

	var received []byte
	var mu sync.Mutex
	nodeB.OnMessage(func(msg *message.Message) {
		mu.Lock()
		received = msg.Content
		mu.Unlock()
	})

	err = nodeA.Send(nodeB.ID(), []byte("hello"))
	if err != nil {
		t.Fatal(err)
	}

	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	if string(received) != "hello" {
		t.Errorf("expected 'hello', got '%s'", string(received))
	}
	mu.Unlock()
}

func TestNodeBidirectional(t *testing.T) {
	nodeA, _ := NewNode(0)
	defer nodeA.Stop()
	nodeB, _ := NewNode(0)
	defer nodeB.Stop()

	nodeA.Connect(nodeB.Host().Addrs(), nodeB.ID())

	var receivedA, receivedB []byte
	var mu sync.Mutex

	nodeA.OnMessage(func(msg *message.Message) {
		mu.Lock()
		receivedA = msg.Content
		mu.Unlock()
	})
	nodeB.OnMessage(func(msg *message.Message) {
		mu.Lock()
		receivedB = msg.Content
		mu.Unlock()
	})

	nodeA.Send(nodeB.ID(), []byte("A->B"))
	nodeB.Send(nodeA.ID(), []byte("B->A"))

	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	if string(receivedB) != "A->B" {
		t.Errorf("B expected 'A->B', got '%s'", string(receivedB))
	}
	if string(receivedA) != "B->A" {
		t.Errorf("A expected 'B->A', got '%s'", string(receivedA))
	}
	mu.Unlock()
}
