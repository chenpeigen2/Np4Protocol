package np4

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"

	"Np4Protocol/go/pkg/message"
)

// authNet is the minimal topology for receive-path tests: a bootstrap relay,
// a sender, and a receiver with the contact-refresh loop disabled so tests
// control exactly what the verification cache holds.
type authNet struct {
	boot, snd, rcv *Node
}

func newAuthNet(t *testing.T, dir string) *authNet {
	t.Helper()
	boot, err := NewNode(0,
		WithIdentity(filepath.Join(dir, "boot")),
		WithDHTServer(),
		WithContactRefreshInterval(-1),
	)
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	t.Cleanup(func() { boot.Close() })
	bootAddr := peer.AddrInfo{ID: boot.ID(), Addrs: boot.Host().Addrs()}

	newClient := func(name string, opts ...Option) *Node {
		n, err := NewNode(0,
			append([]Option{
				WithIdentity(filepath.Join(dir, name)),
				WithBootstrap([]peer.AddrInfo{bootAddr}),
				WithContactRefreshInterval(-1),
			}, opts...)...,
		)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		t.Cleanup(func() { n.Close() })
		return n
	}
	// Only one relay (the bootstrap) exists in this topology.
	snd, rcv := newClient("snd", WithHops(1)), newClient("rcv", WithHops(1))
	if err := boot.ServeRelay(); err != nil {
		t.Fatalf("boot ServeRelay: %v", err)
	}
	if err := snd.PublishKeys(); err != nil {
		t.Fatalf("snd PublishKeys: %v", err)
	}
	if err := rcv.PublishKeys(); err != nil {
		t.Fatalf("rcv PublishKeys: %v", err)
	}
	// Provider records need a moment to propagate.
	time.Sleep(5 * time.Second)
	return &authNet{boot: boot, snd: snd, rcv: rcv}
}

// awaitMessage collects the next message the bus delivers.
func awaitMessage(t *testing.T, n *Node, timeout time.Duration) *message.Message {
	t.Helper()
	ch := make(chan *message.Message, 8)
	n.OnMessage(func(m *message.Message) { ch <- m })
	select {
	case m := <-ch:
		return m
	case <-time.After(timeout):
		t.Fatalf("no message delivered within %v", timeout)
		return nil
	}
}

// TestSendMessageVerifiesAgainstContactCache pins the authenticated-receive
// contract: a mix message from a published, cached contact arrives with
// Verified=true and SenderID equal to that contact's peer ID — the receiver
// learns who sent it without any relay seeing the tag.
func TestSendMessageVerifiesAgainstContactCache(t *testing.T) {
	net := newAuthNet(t, t.TempDir())

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := net.rcv.RefreshContacts(ctx); err != nil {
		t.Fatalf("RefreshContacts: %v", err)
	}

	go func() {
		if err := net.snd.Send(net.rcv.ID(), []byte("hi from a verified friend")); err != nil {
			t.Errorf("Send: %v", err)
		}
	}()
	m := awaitMessage(t, net.rcv, 30*time.Second)
	if string(m.Content) != "hi from a verified friend" {
		t.Fatalf("content: %q", m.Content)
	}
	if !m.Verified {
		t.Fatalf("authenticated message arrived as unverified")
	}
	if m.SenderID != net.snd.ID().String() {
		t.Fatalf("sender attributed to %s, want %s", m.SenderID, net.snd.ID())
	}
}

// TestUnCachedSenderArrivesUnverified pins the degrade-not-drop policy: a
// sender absent from the receiver's contact cache is delivered as
// Verified=false (badge in the UI), never silently discarded.
func TestUnCachedSenderArrivesUnverified(t *testing.T) {
	net := newAuthNet(t, t.TempDir())

	// Receiver cache deliberately left cold (refresh loop disabled).
	go func() {
		if err := net.snd.Send(net.rcv.ID(), []byte("who goes there")); err != nil {
			t.Errorf("Send: %v", err)
		}
	}()
	m := awaitMessage(t, net.rcv, 30*time.Second)
	if string(m.Content) != "who goes there" {
		t.Fatalf("content: %q", m.Content)
	}
	if m.Verified {
		t.Fatalf("cold-cache sender must not verify")
	}
	if m.SenderID != "anonymous" {
		t.Fatalf("unverified sender must be anonymous, got %s", m.SenderID)
	}
}

// TestAllowlistBlocksPublication pins single-server admission: a node absent
// from the allowlist cannot even handshake with the gated bootstrap (so it
// never joins the DHT — connection gating is the airtight choke point;
// record validation alone cannot stop a node from storing its own records
// locally), therefore PublishKeys fails and it is invisible to other nodes'
// discovery. Admitted peers work normally, and the bootstrap's own ID is
// exempt — it must serve.
func TestAllowlistBlocksPublication(t *testing.T) {
	dir := t.TempDir()
	// The admission closure runs on libp2p handler goroutines while this
	// test mutates the set — guard exactly the way production's
	// allowlistFile does (atomic swap), or the race detector rightly fires.
	var admMu sync.RWMutex
	admitted := map[peer.ID]struct{}{}
	boot, err := NewNode(0,
		WithIdentity(filepath.Join(dir, "boot")),
		WithDHTServer(),
		WithContactRefreshInterval(-1),
		WithAdmission(func(id peer.ID) bool {
			admMu.RLock()
			defer admMu.RUnlock()
			_, ok := admitted[id]
			return ok
		}),
	)
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	defer boot.Close()
	admMu.Lock()
	admitted[boot.ID()] = struct{}{} // the bootstrap itself must be able to serve
	admMu.Unlock()
	bootAddr := peer.AddrInfo{ID: boot.ID(), Addrs: boot.Host().Addrs()}

	recv, err := NewNode(0,
		WithIdentity(filepath.Join(dir, "recv")),
		WithBootstrap([]peer.AddrInfo{bootAddr}),
		WithContactRefreshInterval(-1),
	)
	if err != nil {
		t.Fatalf("recv: %v", err)
	}
	defer recv.Close()
	admMu.Lock()
	admitted[recv.ID()] = struct{}{}
	admMu.Unlock()

	stranger, err := NewNode(0,
		WithIdentity(filepath.Join(dir, "stranger")),
		WithBootstrap([]peer.AddrInfo{bootAddr}),
		WithContactRefreshInterval(-1),
	)
	if err != nil {
		t.Fatalf("stranger: %v", err)
	}
	defer stranger.Close()

	_ = boot.ServeRelay()
	if err := recv.PublishKeys(); err != nil {
		t.Fatalf("admitted peer must publish, got %v", err)
	}
	// The stranger cannot reach the gated bootstrap; publishing waits for
	// DHT peers that will never appear and times out.
	if err := stranger.PublishKeys(); err == nil {
		t.Fatal("non-admitted peer published keys: allowlist has no teeth")
	}

	time.Sleep(3 * time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	peers, err := recv.ListPeers(ctx)
	if err != nil {
		t.Fatalf("ListPeers: %v", err)
	}
	for _, p := range peers {
		if p.ID == stranger.ID() {
			t.Fatal("non-admitted peer visible in discovery")
		}
	}
}
