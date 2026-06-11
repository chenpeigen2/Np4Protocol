package p2p

import (
	"context"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
)

const TestProtocol = protocol.ID("/np4/test/1.0.0")

func connectHosts(t *testing.T, h1, h2 host.Host) {
	t.Helper()
	info := peer.AddrInfo{ID: h2.ID(), Addrs: h2.Addrs()}
	if err := h1.Connect(context.Background(), info); err != nil {
		t.Fatal(err)
	}
}

func TestStreamReadWrite(t *testing.T) {
	h1, _ := NewHost(0)
	defer h1.Close()
	h2, _ := NewHost(0)
	defer h2.Close()

	// h2 registers handler
	received := make(chan []byte, 1)
	h2.SetStreamHandler(TestProtocol, func(s network.Stream) {
		defer s.Close()
		data, err := ReadMsg(s)
		if err != nil {
			return
		}
		received <- data
	})

	// Connect h1 -> h2
	connectHosts(t, h1, h2)

	// h1 opens stream and writes
	s, err := h1.NewStream(context.Background(), h2.ID(), TestProtocol)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	err = WriteMsg(s, []byte("hello libp2p"))
	if err != nil {
		t.Fatal(err)
	}
	s.CloseWrite()

	select {
	case data := <-received:
		if string(data) != "hello libp2p" {
			t.Errorf("expected 'hello libp2p', got '%s'", string(data))
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for message")
	}
}

func TestStreamRequestResponse(t *testing.T) {
	h1, _ := NewHost(0)
	defer h1.Close()
	h2, _ := NewHost(0)
	defer h2.Close()

	// h2 echoes back with prefix
	h2.SetStreamHandler(TestProtocol, func(s network.Stream) {
		defer s.Close()
		data, err := ReadMsg(s)
		if err != nil {
			return
		}
		WriteMsg(s, append([]byte("echo: "), data...))
	})

	connectHosts(t, h1, h2)

	s, err := h1.NewStream(context.Background(), h2.ID(), TestProtocol)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	WriteMsg(s, []byte("ping"))
	s.CloseWrite()

	resp, err := ReadMsg(s)
	if err != nil {
		t.Fatal(err)
	}
	if string(resp) != "echo: ping" {
		t.Errorf("expected 'echo: ping', got '%s'", string(resp))
	}
}
