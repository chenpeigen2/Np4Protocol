package np4

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"

	"Np4Protocol/go/pkg/cell"
	"Np4Protocol/go/pkg/message"
	"Np4Protocol/go/pkg/onion"
)

// TestDummyPacketShape dissects a cover packet: the first hop sees an ordinary
// relay layer pointing at the destination; the destination sees a final layer
// whose cell is TypeDummy with a zero tag and empty content. Everything else
// — path selection, onion structure, cell size — is identical to real traffic
// by construction; this test pins the parts that could silently drift.
func TestDummyPacketShape(t *testing.T) {
	// newAuthNet gives the exact minimum topology where dummies exist at all:
	// two clients + relay (a client's only non-bootstrap contact is the
	// other client — cover traffic to the bootstrap would be classifiable).
	net := newAuthNet(t, t.TempDir())
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := net.snd.RefreshContacts(ctx); err != nil {
		t.Fatalf("RefreshContacts: %v", err)
	}

	pkt, err := net.snd.buildDummyPacket()
	if err != nil {
		t.Fatalf("buildDummyPacket: %v", err)
	}
	if pkt == nil {
		t.Fatal("no dummy packet built despite an eligible contact")
	}

	// First hop (the bootstrap relay) peels a relay layer.
	bootLayer, err := onion.Decode(pkt.onion.Bytes(), net.boot.identity)
	if err != nil {
		t.Fatalf("first-hop peel: %v", err)
	}
	if bootLayer.IsFinal {
		t.Fatal("dummy terminates at the first hop")
	}
	if bootLayer.NextHop != net.rcv.ID() {
		t.Fatalf("next hop %s, want the destination client", bootLayer.NextHop)
	}

	// The destination peels a final layer holding a dummy cell.
	finalLayer, err := onion.Decode(bootLayer.Inner, net.rcv.identity)
	if err != nil {
		t.Fatalf("final peel: %v", err)
	}
	if !finalLayer.IsFinal {
		t.Fatal("innermost layer is not final")
	}
	msgID, typ, tag, content, err := cell.Open(finalLayer.Inner)
	if err != nil {
		t.Fatalf("cell open: %v", err)
	}
	if typ != cell.TypeDummy {
		t.Fatalf("cell type %#x, want dummy", typ)
	}
	if !bytes.Equal(tag, make([]byte, cell.TagSize)) {
		t.Fatal("dummy tag is not zeroed")
	}
	if len(content) != 0 {
		t.Fatal("dummy content is not empty")
	}
	if len(msgID) != 16 {
		t.Fatalf("msg_id len %d", len(msgID))
	}
}

// TestDummyNeverDelivered pins the receiver contract end to end: under a
// heavy cover-traffic rate, the application bus sees exactly the real
// message — dummies die at the receiver, never reach the dedup cache, and
// never emit events.
func TestDummyNeverDelivered(t *testing.T) {
	net := newAuthNet(t, t.TempDir())
	// A fresh sender with cover traffic at a rate high enough that several
	// dummy cycles elapse during the observation window.
	bootAddr := peer.AddrInfo{ID: net.boot.ID(), Addrs: net.boot.Host().Addrs()}
	snd2, err := NewNode(0,
		WithIdentity(filepath.Join(t.TempDir(), "snd2")),
		WithBootstrap([]peer.AddrInfo{bootAddr}),
		WithContactRefreshInterval(-1),
		WithHops(1),
		WithDummyRate(10),
	)
	if err != nil {
		t.Fatalf("snd2: %v", err)
	}
	defer snd2.Close()
	if err := snd2.PublishKeys(); err != nil {
		t.Fatalf("PublishKeys: %v", err)
	}
	time.Sleep(3 * time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := net.rcv.RefreshContacts(ctx); err != nil {
		t.Fatalf("RefreshContacts: %v", err)
	}

	received := make(chan string, 64)
	net.rcv.OnMessage(func(m *message.Message) { received <- string(m.Content) })

	const real = "the one real message"
	if err := snd2.Send(net.rcv.ID(), []byte(real)); err != nil {
		t.Fatalf("Send: %v", err)
	}
	// ~40 mean dummies at 10 cells/s should have been attempted by now.
	time.Sleep(5 * time.Second)

	close(received)
	count := 0
	for content := range received {
		count++
		if content != real {
			t.Fatalf("non-real content delivered: %q", content)
		}
	}
	if count != 1 {
		t.Fatalf("delivered %d messages, want exactly the 1 real one (dummy leak)", count)
	}
}
