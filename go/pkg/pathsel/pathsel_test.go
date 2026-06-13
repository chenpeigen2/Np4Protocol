package pathsel

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"Np4Protocol/go/pkg/identity"
	"github.com/libp2p/go-libp2p/core/peer"
)

func TestPickReturnsRequestedHops(t *testing.T) {
	dir := t.TempDir()
	candidates := make([]TestPeer, 5)
	for i := range candidates {
		id, _ := identity.LoadOrCreate(filepath.Join(dir, "p"+string(rune('a'+i))))
		candidates[i] = TestPeer{ID: id.PeerID(), ECDHPub: id.ECDHPub()}
	}

	finder := &FakeFinder{Peers: candidates}
	sel := Selector{Hops: 3, Finder: finder}
	path, err := sel.Pick(context.Background(), peer.ID("self"))
	if err != nil {
		t.Fatalf("Pick: %v", err)
	}
	if len(path) != 3 {
		t.Fatalf("expected 3 hops, got %d", len(path))
	}
	seen := map[peer.ID]bool{}
	for _, h := range path {
		if h.PeerID == peer.ID("self") {
			t.Error("self in path")
		}
		if seen[h.PeerID] {
			t.Errorf("duplicate %s in path", h.PeerID)
		}
		seen[h.PeerID] = true
	}
}

func TestPickExcludesListedPeers(t *testing.T) {
	dir := t.TempDir()
	candidates := make([]TestPeer, 4)
	for i := range candidates {
		id, _ := identity.LoadOrCreate(filepath.Join(dir, "p"+string(rune('a'+i))))
		candidates[i] = TestPeer{ID: id.PeerID(), ECDHPub: id.ECDHPub()}
	}
	excluded := candidates[0].ID

	finder := &FakeFinder{Peers: candidates}
	sel := Selector{Hops: 3, Finder: finder}
	path, err := sel.Pick(context.Background(), peer.ID("self"), excluded)
	if err != nil {
		t.Fatalf("Pick: %v", err)
	}
	for _, h := range path {
		if h.PeerID == excluded {
			t.Errorf("excluded peer %s appeared in path", excluded)
		}
	}
}

func TestPickErrorsWhenNotEnough(t *testing.T) {
	finder := &FakeFinder{Peers: []TestPeer{}}
	sel := Selector{Hops: 3, Finder: finder}
	_, err := sel.Pick(context.Background(), peer.ID("self"))
	if !errors.Is(err, ErrNotEnoughRelays) {
		t.Errorf("expected ErrNotEnoughRelays, got %v", err)
	}
}
