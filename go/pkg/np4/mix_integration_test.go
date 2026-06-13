package np4

import (
	"path/filepath"
	"sync"
	"testing"
	"time"

	"Np4Protocol/go/pkg/message"

	"github.com/libp2p/go-libp2p/core/peer"
)

// TestEndToEndMix starts a bootstrap node, three relays, a sender, and a
// receiver, then verifies that a mix-routed message reaches the destination.
//
// This is the load-bearing integration test for the refactor: it exercises
// identity loading, DHT bootstrap, relay advertisement, ECDH pubkey discovery,
// onion path construction, mix batching/shuffle, and per-layer forwarding.
func TestEndToEndMix(t *testing.T) {
	dir := t.TempDir()

	// Bootstrap node — runs a standalone DHT in server mode (no bootstrap
	// peers of its own). Relays/clients publish their ECDH records to this
	// node's DHT; without WithDHTServer here, the bootstrap peer has no DHT
	// and PutValue on relays fails with "failed to find any peer in table".
	boot, err := NewNode(0,
		WithIdentity(filepath.Join(dir, "boot")),
		WithDHTServer(),
	)
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	defer boot.Close()
	bootAddr := peer.AddrInfo{ID: boot.ID(), Addrs: boot.Host().Addrs()}

	// Three relay nodes — these advertise as "np4-relay" and publish their
	// ECDH pubkeys so the sender can build onion layers through them.
	relays := make([]*Node, 3)
	for i := range relays {
		n, err := NewNode(0,
			WithIdentity(filepath.Join(dir, "relay"+string(rune('a'+i)))),
			WithBootstrap([]peer.AddrInfo{bootAddr}),
		)
		if err != nil {
			t.Fatalf("relay %d: %v", i, err)
		}
		defer n.Close()
		relays[i] = n
		if err := n.ServeRelay(); err != nil {
			t.Fatalf("relay %d ServeRelay: %v", i, err)
		}
	}

	// Receiver — publishes its own ECDH key so the sender's final onion layer
	// can be encrypted to it. Uses PublishKeys (waits for DHT peers first)
	// rather than ServeRelay so the receiver isn't advertised as a mix relay.
	recv, err := NewNode(0,
		WithIdentity(filepath.Join(dir, "recv")),
		WithBootstrap([]peer.AddrInfo{bootAddr}),
	)
	if err != nil {
		t.Fatalf("receiver: %v", err)
	}
	defer recv.Close()
	if err := recv.PublishKeys(); err != nil {
		t.Fatalf("publish receiver keys: %v", err)
	}

	// Sender — no ServeRelay, just sends.
	snd, err := NewNode(0,
		WithIdentity(filepath.Join(dir, "snd")),
		WithBootstrap([]peer.AddrInfo{bootAddr}),
	)
	if err != nil {
		t.Fatalf("sender: %v", err)
	}
	defer snd.Close()

	// DHT records need time to propagate across the network.
	time.Sleep(5 * time.Second)

	var (
		gotContent []byte
		mu         sync.Mutex
	)
	recv.OnMessage(func(msg *message.Message) {
		mu.Lock()
		gotContent = msg.Content
		mu.Unlock()
	})

	if err := snd.Send(recv.ID(), []byte("hello-mix")); err != nil {
		t.Fatalf("Send: %v", err)
	}

	// Poll for arrival. The mix introduces variable delay (batch + shuffle +
	// per-hop forwarding), so we can't assert instantaneously.
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		if string(gotContent) == "hello-mix" {
			mu.Unlock()
			return
		}
		mu.Unlock()
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("message never arrived; got %q", gotContent)
}
