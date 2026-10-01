package np4

import (
	"context"
	"fmt"
	"math/rand/v2"
	"os"
	"sync"
	"time"

	"Np4Protocol/go/pkg/cell"
	"Np4Protocol/go/pkg/onion"
	"Np4Protocol/go/pkg/pathsel"

	"github.com/libp2p/go-libp2p/core/peer"
)

// dummyLoop injects cover traffic on a Poisson schedule. Real message
// arrivals are bursty and user-driven; without dummies, the send-rate
// itself is an activity fingerprint an observer can read straight off any
// relay. Dummies are protocol-identical to real packets — real path
// selection, real onion structure, constant wire size — and differ only in
// the innermost cell's type byte, which no relay ever sees. Receivers drop
// them before the dedup cache.
//
// Every failure here is silent by design: cover traffic must never produce
// a user-visible error or displace real traffic (the mix yields — a full
// mix queue rejects dummies and keeps real sends).
func (n *Node) dummyLoop() {
	if n.dht == nil || n.dummyRate <= 0 {
		return
	}
	var loggedOnce sync.Once
	for {
		interval := time.Duration(rand.ExpFloat64() / n.dummyRate * float64(time.Second))
		select {
		case <-n.ctx.Done():
			return
		case <-time.After(interval):
		}
		pkt, err := n.buildDummyPacket()
		if err != nil || pkt == nil {
			continue // cold contact cache or no path yet: retry next tick
		}
		if err := n.mix.Add(pkt); err != nil {
			continue // mix full: real traffic wins
		}
		loggedOnce.Do(func() {
			fmt.Fprintf(os.Stderr, "[np4] cover traffic active (%.2f cells/s mean)\n", n.dummyRate)
		})
	}
}

// buildDummyPacket assembles one cover packet addressed to a random eligible
// contact. Returns (nil, nil) when injection is currently impossible (no
// eligible destination, no relay path) — the loop simply waits for the next
// tick.
func (n *Node) buildDummyPacket() (*pendingPacket, error) {
	dest, destPub, ok := n.pickDummyDest()
	if !ok {
		return nil, nil
	}
	relays, err := n.dummyPath(dest)
	if err != nil || len(relays) == 0 {
		return nil, nil
	}

	msgID, err := cell.NewMsgID()
	if err != nil {
		return nil, err
	}
	// Tag is all-zero: only the receiver reads it, and it drops dummies
	// before verification. Empty content keeps the padding zone all-zero.
	c, err := cell.Seal(msgID, cell.TypeDummy, make([]byte, cell.TagSize), nil)
	if err != nil {
		return nil, err
	}
	hops := append(relays, onion.Hop{PeerID: dest, ECDHPub: destPub})
	if len(hops)-1 > onion.MaxInitialTTL {
		return nil, fmt.Errorf("dummy path too long: %d hops", len(hops)-1)
	}
	on, err := onion.Build(hops, c)
	if err != nil {
		return nil, err
	}
	return &pendingPacket{firstHop: hops[0].PeerID, ttl: uint8(len(hops) - 1), onion: on}, nil
}

// pickDummyDest chooses the destination for cover traffic: a random contact
// from the verified cache, excluding self (a node cannot onion-address
// itself usefully) and the bootstrap relay — a packet terminating AT the
// bootstrap would be trivially classifiable as dummy by the one node that
// sees all timing in the single-relay deployment. Real chat contacts are
// exactly the remaining set, which is what makes the cover credible.
func (n *Node) pickDummyDest() (peer.ID, []byte, bool) {
	n.contactMu.RLock()
	candidates := make([]peer.ID, 0, len(n.contacts))
	for pid := range n.contacts {
		if pid == n.ID() {
			continue
		}
		if bootID, ok := n.BootstrapID(); ok && pid == bootID {
			continue
		}
		candidates = append(candidates, pid)
	}
	if len(candidates) == 0 {
		n.contactMu.RUnlock()
		return "", nil, false
	}
	dest := candidates[rand.IntN(len(candidates))]
	pub := n.contacts[dest]
	n.contactMu.RUnlock()
	return dest, pub, true
}

// dummyPath builds the relay leg toward dest. Routed nodes reuse the normal
// path selector (same distribution as real sends — reusing anything else
// would make dummies distinguishable by path shape). The bootstrap has no
// selector: it picks a random other relay directly, and in a single-relay
// deployment there is none — cover traffic then comes from clients only.
func (n *Node) dummyPath(dest peer.ID) ([]onion.Hop, error) {
	if n.pathSel != nil {
		ctx, cancel := context.WithTimeout(n.ctx, 10*time.Second)
		defer cancel()
		return n.pathSel.Pick(ctx, n.ID(), dest)
	}
	finder := &pathsel.DHTFinder{DHT: n.dht, Timeout: 5 * time.Second}
	relays, err := finder.FindRelays(n.ctx)
	if err != nil {
		return nil, err
	}
	eligible := make([]onion.Hop, 0, len(relays))
	for _, r := range relays {
		if r.ID == n.ID() || r.ID == dest || len(r.ECDHPub) == 0 {
			continue
		}
		eligible = append(eligible, onion.Hop{PeerID: r.ID, ECDHPub: r.ECDHPub})
	}
	if len(eligible) == 0 {
		return nil, nil
	}
	return []onion.Hop{eligible[rand.IntN(len(eligible))]}, nil
}
