package np4

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
)

// TestProbeLiveBootstrap is a MANUAL diagnostic (only runs when
// NP4_LIVE_BOOTSTRAP is set): joins the running network like a fresh client
// and reports exactly what relay discovery sees — what the user's Send does
// before failing with "not enough relays".
func TestProbeLiveBootstrap(t *testing.T) {
	addrStr := os.Getenv("NP4_LIVE_BOOTSTRAP")
	if addrStr == "" {
		t.Skip("set NP4_LIVE_BOOTSTRAP=/ip4/../tcp/../p2p/.. to probe a live network")
	}

	node, err := NewNode(0, WithIdentity(""), WithBootstrap([]peer.AddrInfo{mustAddrInfo(t, addrStr)}))
	if err != nil {
		t.Fatalf("node: %v", err)
	}
	defer node.Close()

	time.Sleep(8 * time.Second) // DHT warmup, same as production Send retries

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	// Relay discovery — the exact code path in pathsel.DHTFinder.FindRelays.
	relays, err := node.FindPeers(ctx, "np4-relay")
	if err != nil {
		t.Fatalf("FindPeers(np4-relay): %v", err)
	}
	n := 0
	for pi := range relays {
		n++
		t.Logf("np4-relay provider: %s addrs=%d", pi.ID, len(pi.Addrs))
	}
	t.Logf("np4-relay providers: %d", n)

	// Peer discovery — the peer list the UI shows (works per user logs).
	peers, err := node.ListPeers(ctx)
	if err != nil {
		t.Fatalf("ListPeers: %v", err)
	}
	for _, p := range peers {
		t.Logf("np4-peers: %s addrs=%v", p.ID, p.Addrs)
	}
	t.Logf("np4-peers count: %d", len(peers))

	if n == 0 {
		t.Fatalf("relay discovery sees 0 providers while the bootstrap is up — rendezvous record problem confirmed")
	}
}

func mustAddrInfo(t *testing.T, addr string) peer.AddrInfo {
	t.Helper()
	ma, err := multiaddr.NewMultiaddr(addr)
	if err != nil {
		t.Fatalf("parse multiaddr: %v", err)
	}
	ai, err := peer.AddrInfoFromP2pAddr(ma)
	if err != nil {
		t.Fatalf("parse bootstrap addr: %v", err)
	}
	return *ai
}
