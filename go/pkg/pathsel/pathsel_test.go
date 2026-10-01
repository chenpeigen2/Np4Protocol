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

// TestPickDestAsOnlyRelayGetsClearError: messaging the sole relay as the
// destination is impossible (it cannot relay for itself) — the error must be
// the explicit, actionable one, not a generic "not enough relays".
func TestPickDestAsOnlyRelayGetsClearError(t *testing.T) {
	dir := t.TempDir()
	relay, err := identity.LoadOrCreate(filepath.Join(dir, "relay"))
	if err != nil {
		t.Fatalf("identity: %v", err)
	}
	other, err := identity.LoadOrCreate(filepath.Join(dir, "other"))
	if err != nil {
		t.Fatalf("identity: %v", err)
	}

	// Sole relay IS the destination → explicit error.
	sel := Selector{Hops: 1, Finder: &FakeFinder{Peers: []TestPeer{
		{ID: relay.PeerID(), ECDHPub: relay.ECDHPub()},
	}}}
	_, err = sel.Pick(context.Background(), peer.ID("self"), relay.PeerID())
	if !errors.Is(err, ErrDestIsOnlyRelay) {
		t.Fatalf("got %v, want ErrDestIsOnlyRelay", err)
	}

	// With a second relay available the same destination routes fine, and the
	// destination itself is never chosen as its own hop.
	sel2 := Selector{Hops: 1, Finder: &FakeFinder{Peers: []TestPeer{
		{ID: relay.PeerID(), ECDHPub: relay.ECDHPub()},
		{ID: other.PeerID(), ECDHPub: other.ECDHPub()},
	}}}
	path, err := sel2.Pick(context.Background(), peer.ID("self"), relay.PeerID())
	if err != nil {
		t.Fatalf("Pick with second relay: %v", err)
	}
	if path[0].PeerID != other.PeerID() {
		t.Fatalf("hop = %s, want the other relay (destination must be excluded)", path[0].PeerID)
	}

	// Genuinely insufficient relays still get the generic error.
	sel3 := Selector{Hops: 2, Finder: &FakeFinder{Peers: []TestPeer{
		{ID: relay.PeerID(), ECDHPub: relay.ECDHPub()},
	}}}
	_, err = sel3.Pick(context.Background(), peer.ID("self"))
	if !errors.Is(err, ErrNotEnoughRelays) {
		t.Fatalf("got %v, want ErrNotEnoughRelays", err)
	}
}
