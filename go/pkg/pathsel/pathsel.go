// Package pathsel selects random N-hop relay paths through the mix network.
//
// The Selector delegates relay discovery to a Finder, allowing the selection
// logic to be unit-tested without a live DHT. Production code plugs in a
// DHT-backed Finder (added in Task 4.2); tests use FakeFinder.
package pathsel

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"

	"Np4Protocol/go/pkg/onion"

	"github.com/libp2p/go-libp2p/core/peer"
)

// ErrNotEnoughRelays is returned when the Finder returns fewer eligible relays
// than the requested hop count.
var ErrNotEnoughRelays = errors.New("not enough relays available")

// PeerInfo is the minimal info needed to build an onion Hop.
type PeerInfo struct {
	ID      peer.ID
	ECDHPub []byte
}

// Finder abstracts relay discovery so the Selector can be tested without a
// real DHT. Production code uses DHTFinder (added in Task 4.2).
type Finder interface {
	FindRelays(ctx context.Context) ([]PeerInfo, error)
}

// Selector picks a random N-hop path of relays via its Finder.
type Selector struct {
	Hops   int
	Finder Finder
}

// Pick returns Hops distinct relay onion.Hops, excluding self and any peers
// passed in exclude (typically the destination, to avoid trivial loops).
func (s *Selector) Pick(ctx context.Context, self peer.ID, exclude ...peer.ID) ([]onion.Hop, error) {
	if s.Hops <= 0 {
		return nil, errors.New("Hops must be > 0")
	}
	candidates, err := s.Finder.FindRelays(ctx)
	if err != nil {
		return nil, fmt.Errorf("find relays: %w", err)
	}

	excluded := make(map[peer.ID]struct{})
	excluded[self] = struct{}{}
	for _, p := range exclude {
		excluded[p] = struct{}{}
	}

	eligible := make([]PeerInfo, 0, len(candidates))
	for _, c := range candidates {
		if _, skip := excluded[c.ID]; skip {
			continue
		}
		if len(c.ECDHPub) == 0 {
			continue
		}
		eligible = append(eligible, c)
	}
	if len(eligible) < s.Hops {
		return nil, fmt.Errorf("%w: have %d, want %d", ErrNotEnoughRelays, len(eligible), s.Hops)
	}

	// Random subset without replacement.
	chosen := make([]onion.Hop, 0, s.Hops)
	used := make(map[int]struct{})
	for len(chosen) < s.Hops {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(eligible))))
		if err != nil {
			return nil, err
		}
		idx := int(n.Int64())
		if _, dup := used[idx]; dup {
			continue
		}
		used[idx] = struct{}{}
		c := eligible[idx]
		chosen = append(chosen, onion.Hop{PeerID: c.ID, ECDHPub: c.ECDHPub})
	}
	return chosen, nil
}
