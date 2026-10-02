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

// PeerInfo is the minimal info needed to build an onion Hop. Addrs is
// optional metadata (used by peer-list features; path selection ignores it).
type PeerInfo struct {
	ID      peer.ID
	ECDHPub []byte
	Addrs   []string
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

// ErrDestIsOnlyRelay is returned when the destination itself is the only
// eligible relay: the path cannot use the destination as its own intermediate
// hop, so the request is impossible until more relays join.
var ErrDestIsOnlyRelay = errors.New("destination is the only relay — it cannot relay for itself; add more relays or pick another destination")

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
	// The destination is the last exclude entry by convention (node.Send).
	// With no exclude list there is no destination to special-case.
	dest := peer.ID("")
	if len(exclude) > 0 {
		dest = exclude[len(exclude)-1]
	}
	destWasCandidate := false

	eligible := make([]PeerInfo, 0, len(candidates))
	for _, c := range candidates {
		if _, skip := excluded[c.ID]; skip {
			if len(exclude) > 0 && c.ID == dest {
				destWasCandidate = true
			}
			continue
		}
		if len(c.ECDHPub) == 0 {
			continue
		}
		eligible = append(eligible, c)
	}
	if len(eligible) < s.Hops {
		if destWasCandidate && len(eligible) == 0 && s.Hops == 1 {
			return nil, fmt.Errorf("%w", ErrDestIsOnlyRelay)
		}
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

// maxRelayLookups bounds how many relay candidates one FindRelays call
// resolves — each candidate costs a DHT GetKey roundtrip, and an attacker
// can flood the relay rendezvous with provider records in open-admission
// mode. A bounded slice keeps path selection O(1) per call. (Review-verified
// bound; a dedicated test would need a 65-provider topology.)
const maxRelayLookups = 64

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

	candidates := make([]peer.AddrInfo, 0, maxRelayLookups)
examined:
	for pi := range peerChan {
		candidates = append(candidates, pi)
		if len(candidates) >= maxRelayLookups {
			// Keep draining in the background until the query closes the
			// channel — dropping the producer mid-send would leak its
			// goroutine for as long as the caller's context lives.
			go func() {
				for range peerChan {
				}
			}()
			break examined
		}
	}

	var out []PeerInfo
	for _, pi := range candidates {
		pub, err := f.lookupKey(ctx, pi.ID)
		if err != nil || len(pub) == 0 {
			continue
		}
		out = append(out, PeerInfo{ID: pi.ID, ECDHPub: pub})
	}
	return out, nil
}

func (f *DHTFinder) lookupKey(ctx context.Context, pid peer.ID) ([]byte, error) {
	return GetKey(ctx, f.DHT, pid)
}

// GetKey reads a peer's rotation record from the DHT and returns the
// verified current X25519 subkey: framing, master-key peer-ID binding (the
// anti-poisoning defense, unchanged since v1) and the master's signature
// over the subkey all verify before the key is trusted. A sender uses this
// key for the final onion layer AND the sender-auth tag.
func GetKey(ctx context.Context, d *dht.IpfsDHT, pid peer.ID) ([]byte, error) {
	key := ecdhKeyPrefix + base32.StdEncoding.EncodeToString([]byte(pid))
	value, err := d.GetValue(ctx, key)
	if err != nil {
		return nil, err
	}
	return ParseRotationRecord(pid, value)
}
