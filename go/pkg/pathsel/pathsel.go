// Package pathsel selects random N-hop relay paths through the mix network.
//
// The Selector delegates relay discovery to a Finder, allowing the selection
// logic to be unit-tested without a live DHT. Production code plugs in a
// DHT-backed Finder (added in Task 4.2); tests use FakeFinder.
package pathsel

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"errors"
	"fmt"
	"math/big"
	"time"

	"Np4Protocol/go/pkg/onion"

	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/core/peer"
	drouting "github.com/libp2p/go-libp2p/p2p/discovery/routing"
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

const rendezvousString = "np4-relay"
const ecdhKeyPrefix = "/np4/ecdh/"

// DHTFinder finds relays via a libp2p Kademlia DHT. Implements Finder.
type DHTFinder struct {
	DHT     *dht.IpfsDHT
	Timeout time.Duration
}

// FindRelays queries the "np4-relay" rendezvous and resolves each candidate's
// ECDH pubkey via GetValue. Peers whose pubkey is missing or unreadable are skipped.
func (f *DHTFinder) FindRelays(ctx context.Context) ([]PeerInfo, error) {
	if f.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, f.Timeout)
		defer cancel()
	}
	rd := drouting.NewRoutingDiscovery(f.DHT)
	peerChan, err := rd.FindPeers(ctx, rendezvousString)
	if err != nil {
		return nil, err
	}

	var out []PeerInfo
	for pi := range peerChan {
		pub, err := f.lookupECDH(ctx, pi.ID)
		if err != nil || len(pub) == 0 {
			continue
		}
		out = append(out, PeerInfo{ID: pi.ID, ECDHPub: pub})
	}
	return out, nil
}

func (f *DHTFinder) lookupECDH(ctx context.Context, pid peer.ID) ([]byte, error) {
	key := ecdhKeyPrefix + base32.StdEncoding.EncodeToString([]byte(pid))
	return f.DHT.GetValue(ctx, key)
}

// PublishECDH stores a node's own ECDH pubkey in the DHT so other nodes can
// build onion paths through it. Called by nodes that opt in to relaying.
func PublishECDH(ctx context.Context, d *dht.IpfsDHT, pid peer.ID, ecdhPub []byte) error {
	key := ecdhKeyPrefix + base32.StdEncoding.EncodeToString([]byte(pid))
	return d.PutValue(ctx, key, ecdhPub)
}

// GetECDH reads a peer's published ECDH pubkey from the DHT. Returns the raw
// bytes (nil on any DHT error or missing key). Used when building an onion
// path whose final hop is a destination that has published its own key.
func GetECDH(ctx context.Context, d *dht.IpfsDHT, pid peer.ID) ([]byte, error) {
	key := ecdhKeyPrefix + base32.StdEncoding.EncodeToString([]byte(pid))
	return d.GetValue(ctx, key)
}
