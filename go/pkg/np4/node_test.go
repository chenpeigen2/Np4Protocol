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

func TestNodeExchangeKeys(t *testing.T) {
	bs, err := bootstrap.NewBootstrapServer()
	if err != nil {
		t.Fatal(err)
	}
	defer bs.Stop()
	bs.Start("127.0.0.1:0")

	nodeA, err := NewNode("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer nodeA.Stop()
	nodeA.Register(bs.Addr())

	nodeB, err := NewNode("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer nodeB.Stop()
	nodeB.Register(bs.Addr())

	// A exchanges keys with B
	err = nodeA.ExchangeKeys(bs.Addr(), nodeB.ID())
	if err != nil {
		t.Fatal(err)
	}

	// Allow time for B to process the key exchange request
	time.Sleep(100 * time.Millisecond)

	// A should have shared key
	sessionA, okA := nodeA.GetPeerSession(nodeB.ID())
	if !okA {
		t.Fatal("peer session not established on A")
	}

	if len(sessionA.SharedKey) == 0 {
		t.Error("shared key is empty on A")
	}

	// B should also have shared key
	sessionB, okB := nodeB.GetPeerSession(nodeA.ID())
	if !okB {
		t.Fatal("peer session not established on B")
	}

	if len(sessionB.SharedKey) == 0 {
		t.Error("shared key is empty on B")
	}

	// Shared keys must match (X25519 property)
	if string(sessionA.SharedKey) != string(sessionB.SharedKey) {
		t.Error("shared keys do not match between A and B")
	}
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
