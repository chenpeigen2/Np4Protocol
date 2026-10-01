package np4

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/libp2p/go-libp2p/core/network"

	"Np4Protocol/go/pkg/pathsel"
)

// DirEntry is one address-book record: the complete set of facts needed to
// correspond with a node through the mix — who it is (peer_id), how to
// encrypt to it (ecdh_pub), where to reach it directly (addrs), whether it
// can serve as an intermediate hop (is_relay), and whether it is currently
// connected (connected).
type DirEntry struct {
	ID        string   `json:"peer_id"`
	Addrs     []string `json:"addrs"`
	ECDHPub   string   `json:"ecdh_pub"` // hex-encoded X25519 onion key
	Connected bool     `json:"connected"`
	IsRelay   bool     `json:"is_relay"`
}

// Directory builds the address book visible from this node: every
// key-publishing peer in the np4-peers rendezvous plus self. Entries pass the
// same key-verification bar as Send (ListPeers), so every listed peer is
// actually reachable through the mix. Relay membership comes from the
// np4-relay rendezvous; a failed relay lookup degrades to "no relays marked"
// rather than failing the whole directory.
func (n *Node) Directory(ctx context.Context) ([]DirEntry, error) {
	if n.dht == nil {
		return nil, errors.New("DHT not initialized; pass WithBootstrap when creating the node")
	}

	isRelay := make(map[string]bool)
	finder := &pathsel.DHTFinder{DHT: n.dht, Timeout: 5 * time.Second}
	if relays, err := finder.FindRelays(ctx); err == nil {
		for _, r := range relays {
			isRelay[r.ID.String()] = true
		}
	}

	peers, err := n.ListPeers(ctx)
	if err != nil {
		return nil, fmt.Errorf("list peers: %w", err)
	}
	out := make([]DirEntry, 0, len(peers)+1)
	for _, p := range peers {
		out = append(out, DirEntry{
			ID:        p.ID.String(),
			Addrs:     p.Addrs,
			ECDHPub:   hex.EncodeToString(p.ECDHPub),
			Connected: n.host.Network().Connectedness(p.ID) == network.Connected,
			IsRelay:   isRelay[p.ID.String()],
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })

	// Self leads the list: a node always knows its own key, and a relay (the
	// bootstrap included) should see itself the way clients see it.
	self := DirEntry{
		ID:        n.ID().String(),
		Addrs:     n.Addrs(),
		ECDHPub:   hex.EncodeToString(n.identity.ECDHPub()),
		Connected: true,
		IsRelay:   isRelay[n.ID().String()],
	}
	return append([]DirEntry{self}, out...), nil
}
