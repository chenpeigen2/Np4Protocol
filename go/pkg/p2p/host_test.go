package p2p

import (
	"context"
	"testing"

	"Np4Protocol/go/pkg/identity"

	"github.com/libp2p/go-libp2p/core/peer"
)

func TestNewHost(t *testing.T) {
	h, err := NewHost(0) // random port
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()

	if h.ID() == "" {
		t.Error("expected non-empty peer ID")
	}
	if len(h.Addrs()) == 0 {
		t.Error("expected at least one address")
	}
}

func TestTwoHostsConnect(t *testing.T) {
	h1, err := NewHost(0)
	if err != nil {
		t.Fatal(err)
	}
	defer h1.Close()

	h2, err := NewHost(0)
	if err != nil {
		t.Fatal(err)
	}
	defer h2.Close()

	// h1 connects to h2
	info := peer.AddrInfo{
		ID:    h2.ID(),
		Addrs: h2.Addrs(),
	}
	err = h1.Connect(context.Background(), info)
	if err != nil {
		t.Fatal(err)
	}

	// Verify h2 sees h1 in its peerstore
	if h2.Peerstore().PeerInfo(h1.ID()).ID == "" {
		t.Error("h2 should know about h1")
	}
}

func TestNewHostWithIdentityStable(t *testing.T) {
	dir := t.TempDir()
	id1, _ := identity.LoadOrCreate(dir + "/a")
	id2, _ := identity.LoadOrCreate(dir + "/a") // same file

	h1, err := NewHostWithIdentity(id1, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer h1.Close()

	h2, err := NewHostWithIdentity(id2, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer h2.Close()

	if h1.ID() != h2.ID() {
		t.Errorf("identity-derived peer IDs differ: %s vs %s", h1.ID(), h2.ID())
	}
}
