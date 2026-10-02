package p2p

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
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

// fakeStream replays a scripted byte sequence to ReadMsgCap.
type fakeStream struct {
	network.Stream
	data []byte
	off  int
}

func (f *fakeStream) Read(p []byte) (int, error) {
	if f.off >= len(f.data) {
		return 0, io.EOF
	}
	n := copy(p, f.data[f.off:])
	f.off += n
	return n, nil
}

func prefixedFrame(t *testing.T, length uint32, body []byte) []byte {
	t.Helper()
	out := make([]byte, 4, 4+len(body)+int(length))
	binary.BigEndian.PutUint32(out[:4], length)
	return append(out, body...)
}

// TestReadMsgCapRejectsOverlongPrefix: a sender naming a huge length must be
// rejected on the prefix alone — the payload never gets allocated. This is
// the anti-amplification contract the onion ingress depends on.
func TestReadMsgCapRejectsOverlongPrefix(t *testing.T) {
	s := &fakeStream{data: prefixedFrame(t, 1<<20, nil)} // claims 1MB
	out, err := ReadMsgCap(s, 2*8192)
	if err == nil {
		t.Fatal("overlong prefix accepted")
	}
	if out != nil {
		t.Fatal("payload allocated for rejected frame")
	}
}

// TestReadMsgCapAcceptsBoundary: a frame exactly at the cap passes, one over
// fails, and a zero-length frame is valid.
func TestReadMsgCapAcceptsBoundary(t *testing.T) {
	const cap = 16
	body := bytes.Repeat([]byte{0x5A}, cap)
	s := &fakeStream{data: prefixedFrame(t, cap, body)}
	out, err := ReadMsgCap(s, cap)
	if err != nil || !bytes.Equal(out, body) {
		t.Fatalf("exact-cap frame rejected: %v", err)
	}

	s = &fakeStream{data: prefixedFrame(t, cap+1, append(body, 0))}
	if _, err := ReadMsgCap(s, cap); err == nil {
		t.Fatal("frame one over cap accepted")
	}

	s = &fakeStream{data: prefixedFrame(t, 0, nil)}
	if out, err := ReadMsgCap(s, cap); err != nil || len(out) != 0 {
		t.Fatalf("zero-length frame: out=%d err=%v", len(out), err)
	}

	s = &fakeStream{data: prefixedFrame(t, cap, body)}
	if _, err := ReadMsgCap(s, MaxMessageSize+1); err == nil {
		t.Fatal("cap above MaxMessageSize accepted")
	}
}
