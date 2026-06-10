package np4

import (
	"Np4Protocol/go/pkg/bootstrap"
	"Np4Protocol/go/pkg/message"
	"sync"
	"testing"
	"time"
)

func TestNodeCommunication(t *testing.T) {
	node1, err := NewNode("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer node1.Stop()

	node2, err := NewNode("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer node2.Stop()

	node1.AddPeer(node2.ID(), node2.Addr())

	var received []byte
	var mu sync.Mutex

	node2.OnMessage(func(msg *message.Message) {
		mu.Lock()
		received = msg.Content
		mu.Unlock()
	})

	err = node1.Send(node2.ID(), []byte("hello from node1"))
	if err != nil {
		t.Fatal(err)
	}

	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	if string(received) != "hello from node1" {
		t.Errorf("expected 'hello from node1', got '%s'", string(received))
	}
	mu.Unlock()
}

func TestNodeRegister(t *testing.T) {
	bs, err := bootstrap.NewBootstrapServer()
	if err != nil {
		t.Fatal(err)
	}
	defer bs.Stop()
	bs.Start("127.0.0.1:0")

	node, err := NewNode("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer node.Stop()

	err = node.Register(bs.Addr())
	if err != nil {
		t.Fatal(err)
	}

	time.Sleep(50 * time.Millisecond)

	if bs.PeerCount() != 1 {
		t.Errorf("expected 1 peer, got %d", bs.PeerCount())
	}
}
