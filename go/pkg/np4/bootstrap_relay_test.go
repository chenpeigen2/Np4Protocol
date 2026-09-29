package np4

import (
	"path/filepath"
	"sync"
	"testing"
	"time"

	"Np4Protocol/go/pkg/message"

	"github.com/libp2p/go-libp2p/core/peer"
)

// TestBootstrapDoublesAsRelay covers the single-server deployment: the
// bootstrap node is ALSO the only mix relay, and both peers are ordinary
// clients that reach everything via outbound dials. The last hop
// (bootstrap -> receiver) must deliver over the receiver's pre-existing DHT
// connection — this is what makes NAT'd receivers reachable without hole
// punching. Clients pair with this deployment via --hops 1; longer paths
// would hard-fail (no silent downgrade), which is exactly the contract.
func TestBootstrapDoublesAsRelay(t *testing.T) {
	dir := t.TempDir()

	boot, err := NewNode(0,
		WithIdentity(filepath.Join(dir, "boot")),
		WithDHTServer(),
	)
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	defer boot.Close()
	bootAddr := peer.AddrInfo{ID: boot.ID(), Addrs: boot.Host().Addrs()}

	// Receiver joins first so the bootstrap's routing table gains its first
	// peer — records are stored ON peers, so relay advertisement is impossible
	// before any client exists.
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

	snd, err := NewNode(0,
		WithIdentity(filepath.Join(dir, "snd")),
		WithBootstrap([]peer.AddrInfo{bootAddr}),
		WithHops(1), // the bootstrap is the only relay
	)
	if err != nil {
		t.Fatalf("sender: %v", err)
	}
	defer snd.Close()

	// ServeRelay waits for a DHT peer itself; by now the two clients are that
	// peer, so this publishes the bootstrap's key and advertises np4-relay.
	if err := boot.ServeRelay(); err != nil {
		t.Fatalf("bootstrap ServeRelay: %v", err)
	}

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

	if err := snd.Send(recv.ID(), []byte("hello-single-relay")); err != nil {
		t.Fatalf("Send: %v", err)
	}

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		if string(gotContent) == "hello-single-relay" {
			mu.Unlock()
			return
		}
		mu.Unlock()
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("message never arrived; got %q", gotContent)
}
