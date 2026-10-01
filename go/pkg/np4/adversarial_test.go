package np4

import (
	"context"
	"crypto/rand"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"Np4Protocol/go/pkg/cell"
	"Np4Protocol/go/pkg/identity"
	"Np4Protocol/go/pkg/message"
	"Np4Protocol/go/pkg/onion"
	"Np4Protocol/go/pkg/p2p"
	"Np4Protocol/go/pkg/pathsel"

	"github.com/libp2p/go-libp2p/core/peer"
)

// adversarialNet is a minimal routed network: bootstrap + 2 relays + receiver
// (keys published). Tests craft wire packets manually to exercise hostile
// behavior that honest Send() cannot produce.
type adversarialNet struct {
	dir    string
	boot   *Node
	relay1 *Node
	relay2 *Node
	recv   *Node
	sender *Node

	// identities reloaded from the same files the nodes use, so tests can
	// build onions by hand.
	r1ID  *identity.Identity
	r2ID  *identity.Identity
	rcvID *identity.Identity
}

func newAdversarialNet(t *testing.T) *adversarialNet {
	t.Helper()
	dir := t.TempDir()

	boot, err := NewNode(0, WithIdentity(filepath.Join(dir, "boot")), WithDHTServer())
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	t.Cleanup(func() { boot.Close() })
	bootAddr := peer.AddrInfo{ID: boot.ID(), Addrs: boot.Host().Addrs()}

	mkRelay := func(name string) *Node {
		n, err := NewNode(0,
			WithIdentity(filepath.Join(dir, name)),
			WithBootstrap([]peer.AddrInfo{bootAddr}),
		)
		if err != nil {
			t.Fatalf("relay %s: %v", name, err)
		}
		t.Cleanup(func() { n.Close() })
		if err := n.ServeRelay(); err != nil {
			t.Fatalf("relay %s ServeRelay: %v", name, err)
		}
		return n
	}

	recv, err := NewNode(0,
		WithIdentity(filepath.Join(dir, "recv")),
		WithBootstrap([]peer.AddrInfo{bootAddr}),
	)
	if err != nil {
		t.Fatalf("receiver: %v", err)
	}
	t.Cleanup(func() { recv.Close() })
	if err := recv.PublishKeys(); err != nil {
		t.Fatalf("publish receiver keys: %v", err)
	}

	sender, err := NewNode(0,
		WithIdentity(filepath.Join(dir, "sender")),
		WithBootstrap([]peer.AddrInfo{bootAddr}),
	)
	if err != nil {
		t.Fatalf("sender: %v", err)
	}
	t.Cleanup(func() { sender.Close() })

	reload := func(name string) *identity.Identity {
		id, err := identity.LoadOrCreate(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("reload identity %s: %v", name, err)
		}
		return id
	}

	net := &adversarialNet{
		dir:    dir,
		boot:   boot,
		relay1: mkRelay("relay_a"),
		relay2: mkRelay("relay_b"),
		recv:   recv,
		sender: sender,
		r1ID:   reload("relay_a"),
		r2ID:   reload("relay_b"),
		rcvID:  reload("recv"),
	}

	// DHT records need time to propagate before hand-crafted onions can be
	// resolved — and before the adversarial traffic, so replayed/crafted
	// packets are indistinguishable from a healthy network state.
	time.Sleep(5 * time.Second)
	return net
}

// buildWire crafts a wire packet r1 → r2 → recv carrying content.
func (an *adversarialNet) buildWire(t *testing.T, content string, ttl uint8) []byte {
	t.Helper()
	msgID, err := cell.NewMsgID()
	if err != nil {
		t.Fatalf("cell msg id: %v", err)
	}
	c, err := cell.Seal(msgID, cell.TypeText, make([]byte, cell.TagSize), []byte(content))
	if err != nil {
		t.Fatalf("cell seal: %v", err)
	}
	hops := []onion.Hop{
		{PeerID: an.r1ID.PeerID(), ECDHPub: an.r1ID.ECDHPub()},
		{PeerID: an.r2ID.PeerID(), ECDHPub: an.r2ID.ECDHPub()},
		{PeerID: an.rcvID.PeerID(), ECDHPub: an.rcvID.ECDHPub()},
	}
	on, err := onion.Build(hops, c)
	if err != nil {
		t.Fatalf("build onion: %v", err)
	}
	wire, err := onion.Wrap(ttl, on.Bytes())
	if err != nil {
		t.Fatalf("wrap: %v", err)
	}
	return wire
}

// sendRaw delivers wire to dest through the libp2p onion protocol.
func (an *adversarialNet) sendRaw(t *testing.T, from, to *Node, wire []byte) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := from.Host().Connect(ctx, peer.AddrInfo{ID: to.ID(), Addrs: to.Host().Addrs()}); err != nil {
		t.Fatalf("connect: %v", err)
	}
	s, err := from.Host().NewStream(ctx, to.ID(), ProtocolOnion)
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	defer s.Close()
	if err := p2p.WriteMsg(s, wire); err != nil {
		t.Fatalf("write wire: %v", err)
	}
}

// recvCounter collects delivered contents on the receiver.
type recvCounter struct {
	mu  sync.Mutex
	got [][]byte
}

func (rc *recvCounter) attach(n *Node) {
	n.OnMessage(func(msg *message.Message) {
		rc.mu.Lock()
		rc.got = append(rc.got, append([]byte(nil), msg.Content...))
		rc.mu.Unlock()
	})
}

func (rc *recvCounter) count() int {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	return len(rc.got)
}

func (rc *recvCounter) has(want string) bool {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	for _, g := range rc.got {
		if string(g) == want {
			return true
		}
	}
	return false
}

// TestAdversarialReplayDropped: the same wire packet sent twice must deliver
// exactly once — the relay's replay cache drops the copy.
func TestAdversarialReplayDropped(t *testing.T) {
	an := newAdversarialNet(t)
	rc := &recvCounter{}
	rc.attach(an.recv)

	wire := an.buildWire(t, "replay-me", 2)
	an.sendRaw(t, an.sender, an.relay1, wire)
	an.sendRaw(t, an.sender, an.relay1, wire)

	waitFor(t, 15*time.Second, func() bool { return rc.count() == 1 })
	// Grace window: a second copy must not arrive late.
	time.Sleep(3 * time.Second)
	if rc.count() != 1 {
		t.Fatalf("replayed packet delivered %d times, want exactly 1", rc.count())
	}
}

// TestAdversarialTTLExhaustedDropped: a relay-layer packet arriving with
// ttl=0 must be dropped — otherwise crafted onions loop forever (each hop's
// decrypt is deterministic, so loops would not self-terminate).
func TestAdversarialTTLExhaustedDropped(t *testing.T) {
	an := newAdversarialNet(t)
	rc := &recvCounter{}
	rc.attach(an.recv)

	wire := an.buildWire(t, "ttl-zero", 0)
	an.sendRaw(t, an.sender, an.relay1, wire)

	time.Sleep(4 * time.Second)
	if rc.count() != 0 {
		t.Fatalf("ttl=0 relay packet was delivered %d times, want 0", rc.count())
	}
}

// TestAdversarialCorruptedLayerDies: a bit flip inside the AEAD ciphertext
// kills the chain (tag mismatch) — no delivery, no crash, node stays alive
// for subsequent valid traffic.
func TestAdversarialCorruptedLayerDies(t *testing.T) {
	an := newAdversarialNet(t)
	rc := &recvCounter{}
	rc.attach(an.recv)

	corrupt := an.buildWire(t, "corrupt-me", 2)
	// Offset inside layer 1's ciphertext (past ttl+clen+eph+nonce).
	corrupt[3+32+12+100] ^= 0xff
	an.sendRaw(t, an.sender, an.relay1, corrupt)

	time.Sleep(3 * time.Second)
	if rc.count() != 0 {
		t.Fatalf("corrupted packet delivered %d times, want 0", rc.count())
	}

	// Liveness: a valid packet through the same relay must still arrive.
	an.sendRaw(t, an.sender, an.relay1, an.buildWire(t, "after-corruption", 2))
	waitFor(t, 15*time.Second, func() bool { return rc.has("after-corruption") })
}

// TestAdversarialGarbageStreamNoPanic: random bytes on the onion protocol
// must be dropped without panic and without poisoning the node.
func TestAdversarialGarbageStreamNoPanic(t *testing.T) {
	an := newAdversarialNet(t)
	rc := &recvCounter{}
	rc.attach(an.recv)

	garbage := make([]byte, onion.WireSize)
	if _, err := rand.Read(garbage); err != nil {
		t.Fatal(err)
	}
	an.sendRaw(t, an.sender, an.relay1, garbage)

	time.Sleep(2 * time.Second)
	if rc.count() != 0 {
		t.Fatalf("garbage delivered %d times, want 0", rc.count())
	}

	an.sendRaw(t, an.sender, an.relay1, an.buildWire(t, "after-garbage", 2))
	waitFor(t, 15*time.Second, func() bool { return rc.has("after-garbage") })
}

// TestAdversarialTruncatedStreamNoPanic: a length prefix claiming more bytes
// than delivered must surface as a read error, not a panic or hang.
func TestAdversarialTruncatedStreamNoPanic(t *testing.T) {
	an := newAdversarialNet(t)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := an.sender.Host().Connect(ctx, peer.AddrInfo{ID: an.relay1.ID(), Addrs: an.relay1.Host().Addrs()}); err != nil {
		t.Fatalf("connect: %v", err)
	}
	s, err := an.sender.Host().NewStream(ctx, an.relay1.ID(), ProtocolOnion)
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	// Claim 5000 bytes, send 10, close.
	if _, err := s.Write([]byte{0x00, 0x00, 0x13, 0x88}); err != nil {
		t.Fatalf("write header: %v", err)
	}
	if _, err := s.Write([]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}); err != nil {
		t.Fatalf("write body: %v", err)
	}
	s.Close()

	time.Sleep(2 * time.Second) // handler must have hit EOF and returned.
}

// TestAdversarialDHTPoisoningBlocked: publishing a key record under a peer
// ID that is not the publisher's must fail at PutValue time — the master-key
// binding (IDFromPublicKey(master) == claimed ID) is the only
// signature-independent defense, and the attacker's own signature doesn't
// help them (their master key hashes to THEIR id, not the victim's).
func TestAdversarialDHTPoisoningBlocked(t *testing.T) {
	an := newAdversarialNet(t)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	pub, bucket := an.sender.identity.RotationPub()
	err := pathsel.PublishKey(ctx, an.sender.DHT(), an.recv.ID(), an.sender.identity.SigningPubKey(), pub, bucket, an.sender.identity.Sign)
	if err == nil {
		t.Fatal("poisoned PutValue accepted: attacker key stored under victim's peer ID")
	}

	// And the victim's real key must still resolve correctly.
	pub, err = pathsel.GetKey(ctx, an.sender.DHT(), an.recv.ID())
	if err != nil {
		t.Fatalf("legitimate key lookup failed after poisoning attempt: %v", err)
	}
	if len(pub) != 32 {
		t.Fatalf("resolved key length %d, want 32", len(pub))
	}
}

func waitFor(t *testing.T, d time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatal("condition not met before deadline")
}
