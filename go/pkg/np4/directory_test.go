package np4

import (
	"context"
	"encoding/hex"
	"path/filepath"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
)

// TestDirectoryListsVerifiedContacts pins the address-book contract: self
// leads with its own X25519 key and relay flag, published peers appear with
// their verified keys and addresses, and a connected-but-never-published node
// is absent — the directory only lists nodes that are actually reachable
// through the mix.
func TestDirectoryListsVerifiedContacts(t *testing.T) {
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

	// Joins the DHT but publishes nothing: infrastructure visible to libp2p,
	// invisible to the address book.
	stranger, err := NewNode(0,
		WithIdentity(filepath.Join(dir, "stranger")),
		WithBootstrap([]peer.AddrInfo{bootAddr}),
	)
	if err != nil {
		t.Fatalf("stranger: %v", err)
	}
	defer stranger.Close()

	if err := boot.ServeRelay(); err != nil {
		t.Fatalf("boot ServeRelay: %v", err)
	}

	// Provider records need a moment to propagate.
	time.Sleep(5 * time.Second)
	_ = stranger

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	entries, err := boot.Directory(ctx)
	if err != nil {
		t.Fatalf("Directory: %v", err)
	}

	if len(entries) != 2 {
		t.Fatalf("expected 2 entries (self + receiver), got %d: %+v", len(entries), entries)
	}

	self := entries[0]
	if self.ID != boot.ID().String() {
		t.Errorf("self must lead the directory, got %s", self.ID)
	}
	if !self.IsRelay {
		t.Errorf("self advertised via ServeRelay but not marked as relay")
	}
	if self.ECDHPub != hex.EncodeToString(boot.identity.ECDHPub()) {
		t.Errorf("self ecdh_pub mismatch with own identity key")
	}

	byID := map[string]DirEntry{}
	for _, e := range entries[1:] {
		byID[e.ID] = e
	}
	recvEntry, ok := byID[recv.ID().String()]
	if !ok {
		t.Fatalf("receiver missing from directory: %+v", entries)
	}
	if recvEntry.ECDHPub == "" {
		t.Errorf("receiver listed without its published X25519 key")
	}
	if len(recvEntry.Addrs) == 0 {
		t.Errorf("receiver listed without addresses")
	}
	if recvEntry.IsRelay {
		t.Errorf("receiver published keys only, must not be marked as relay")
	}
	if !recvEntry.Connected {
		t.Errorf("receiver is directly connected to the bootstrap; connected must be true")
	}
	if _, ok := byID[stranger.ID().String()]; ok {
		t.Errorf("stranger (never published) must not appear in the directory")
	}
}
