package np4

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
)

// TestListPeersExposesKeyPublishers pins the peer-discovery contract: every
// node that published its key (relays via ServeRelay, receivers via
// PublishKeys) is discoverable through ListPeers, never the caller itself,
// and non-published nodes are absent.
func TestListPeersExposesKeyPublishers(t *testing.T) {
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
	)
	if err != nil {
		t.Fatalf("sender: %v", err)
	}
	defer snd.Close()

	// The relay role advertises BOTH rendezvous (np4-relay and np4-peers).
	if err := boot.ServeRelay(); err != nil {
		t.Fatalf("boot ServeRelay: %v", err)
	}

	// Provider records need a moment to propagate.
	time.Sleep(5 * time.Second)

	// Mute the unused-variable lint for the receiver handle; recv matters
	// only through its published record.
	_ = recv

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	peers, err := snd.ListPeers(ctx)
	if err != nil {
		t.Fatalf("ListPeers: %v", err)
	}

	seen := map[peer.ID]bool{}
	for _, p := range peers {
		seen[p.ID] = true
		if p.ID == snd.ID() {
			t.Fatalf("ListPeers must not include the caller itself")
		}
	}
	if !seen[boot.ID()] {
		t.Errorf("relay (key published via ServeRelay) missing from peer list")
	}
	if !seen[recv.ID()] {
		t.Errorf("receiver (key published via PublishKeys) missing from peer list")
	}
	if len(peers) != 2 {
		t.Errorf("expected exactly 2 peers (relay + receiver), got %d", len(peers))
	}
}
