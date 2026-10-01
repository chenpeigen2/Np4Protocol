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

	"Np4Protocol/go/pkg/identity"
	"Np4Protocol/go/pkg/onion"

	dht "github.com/libp2p/go-libp2p-kad-dht"
	ic "github.com/libp2p/go-libp2p/core/crypto"
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

// PublishKey stores a node's ed25519 public key in the DHT under
// /np4/ecdh/<peerID>. The DHT never verifies record signatures, so the
// validator's IDFromPublicKey-vs-key binding is the ONLY thing preventing
// poisoning — the stored key must be the publisher's own. Callers pass
// id.SigningPubKey().
func PublishKey(ctx context.Context, d *dht.IpfsDHT, pid peer.ID, pubKey ic.PubKey) error {
	if pubKey == nil {
		return errors.New("nil public key")
	}
	value, err := ic.MarshalPublicKey(pubKey)
	if err != nil {
		return err
	}
	key := ecdhKeyPrefix + base32.StdEncoding.EncodeToString([]byte(pid))
	return d.PutValue(ctx, key, value)
}

// GetKey reads a peer's published ed25519 key from the DHT, verifies the
// peer-ID binding client-side (defense in depth on top of the validator),
// and converts it to the X25519 pubkey used for onion layers.
func GetKey(ctx context.Context, d *dht.IpfsDHT, pid peer.ID) ([]byte, error) {
	key := ecdhKeyPrefix + base32.StdEncoding.EncodeToString([]byte(pid))
	value, err := d.GetValue(ctx, key)
	if err != nil {
		return nil, err
	}
	pubKey, err := ic.UnmarshalPublicKey(value)
	if err != nil {
		return nil, fmt.Errorf("unmarshal published key: %w", err)
	}
	bound, err := peer.IDFromPublicKey(pubKey)
	if err != nil {
		return nil, err
	}
	if bound != pid {
		return nil, fmt.Errorf("published key binds to %s, wanted %s", bound, pid)
	}
	raw, err := pubKey.Raw()
	if err != nil || len(raw) != 32 {
		return nil, fmt.Errorf("unexpected published key material (len=%d, err=%v)", len(raw), err)
	}
	return identity.Ed25519PubToX25519(raw)
}
