package pathsel

import (
	"context"

	"github.com/libp2p/go-libp2p/core/peer"
)

// TestPeer is a test-only relay descriptor used by FakeFinder.
type TestPeer struct {
	ID      peer.ID
	ECDHPub []byte
}

// FakeFinder returns a fixed peer list for unit tests.
type FakeFinder struct {
	Peers []TestPeer
	Err   error
}

func (f *FakeFinder) FindRelays(ctx context.Context) ([]PeerInfo, error) {
	if f.Err != nil {
		return nil, f.Err
	}
	out := make([]PeerInfo, len(f.Peers))
	for i, p := range f.Peers {
		out[i] = PeerInfo{ID: p.ID, ECDHPub: p.ECDHPub}
	}
	return out, nil
}
