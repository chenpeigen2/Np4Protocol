package np4

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"

	"Np4Protocol/go/pkg/cell"
	"Np4Protocol/go/pkg/message"
	"Np4Protocol/go/pkg/onion"
	"Np4Protocol/go/pkg/pathsel"
)

// rotationNet is the authNet topology with second-scale rotation buckets.
func rotationNet(t *testing.T) *authNet {
	t.Helper()
	dir := t.TempDir()
	boot, err := NewNode(0,
		WithIdentity(dir+"/boot"),
		WithDHTServer(),
		WithContactRefreshInterval(-1),
		WithTestBucketPeriod(time.Second),
	)
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	t.Cleanup(func() { boot.Close() })
	bootAddr := peer.AddrInfo{ID: boot.ID(), Addrs: boot.Host().Addrs()}

	newClient := func(name string) *Node {
		n, err := NewNode(0,
			WithIdentity(dir+"/"+name),
			WithBootstrap([]peer.AddrInfo{bootAddr}),
			WithContactRefreshInterval(-1),
			WithHops(1),
			WithTestBucketPeriod(time.Second),
		)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		t.Cleanup(func() { n.Close() })
		return n
	}
	snd, rcv := newClient("snd"), newClient("rcv")
	if err := boot.ServeRelay(); err != nil {
		t.Fatalf("ServeRelay: %v", err)
	}
	if err := snd.PublishKeys(); err != nil {
		t.Fatalf("snd PublishKeys: %v", err)
	}
	if err := rcv.PublishKeys(); err != nil {
		t.Fatalf("rcv PublishKeys: %v", err)
	}
	time.Sleep(3 * time.Second)
	return &authNet{boot: boot, snd: snd, rcv: rcv}
}

// TestMessageDeliveredAcrossRotation pins the forward-secrecy delivery
// contract: after several rotation buckets elapse, a message addressed to
// the receiver's STALE published record (the sender's key fetch predates the
// rotation) still decrypts via the retained subkey and still verifies — the
// retention window exists exactly so in-flight and offline traffic survives
// rotation.
func TestMessageDeliveredAcrossRotation(t *testing.T) {
	net := rotationNet(t)
	bucketAtStart := net.rcv.CurrentBucket()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := net.rcv.RefreshContacts(ctx); err != nil {
		t.Fatalf("RefreshContacts: %v", err)
	}

	// Let the nodes' rotation loops cross at least two buckets (1s each;
	// loops tick at half the period). The receiver's published record is now
	// stale — the DHT still holds the old subkey until republish.
	time.Sleep(3 * time.Second)
	if net.rcv.CurrentBucket() <= bucketAtStart+1 {
		t.Fatalf("expected ≥2 bucket advances, got %d → %d", bucketAtStart, net.rcv.CurrentBucket())
	}
	// Production contact caches refresh every 30s against 24h buckets; the
	// test's second-scale buckets need the same freshness here so the cached
	// sender key is the one the sender is actually signing with.
	if err := net.rcv.RefreshContacts(ctx); err != nil {
		t.Fatalf("RefreshContacts after rotation: %v", err)
	}

	go func() {
		if err := net.snd.Send(net.rcv.ID(), []byte("sent across a rotation")); err != nil {
			t.Errorf("Send: %v", err)
		}
	}()
	m := awaitMessage(t, net.rcv, 30*time.Second)
	if string(m.Content) != "sent across a rotation" {
		t.Fatalf("content: %q", m.Content)
	}
	if !m.Verified {
		t.Fatal("cross-rotation message must verify via the retained window")
	}
	if m.SenderID != net.snd.ID().String() {
		t.Fatalf("sender attributed to %s", m.SenderID)
	}
}

// TestRepublishedKeyResolves pins the rotation publish path: after
// publishKeyRecord, the DHT serves the NEW subkey (different from the
// pre-rotation one), messages addressed to it verify, and the record parses
// with the same machinery senders use.
func TestRepublishedKeyResolves(t *testing.T) {
	net := rotationNet(t)
	oldPub, _ := net.rcv.identity.RotationPub()

	time.Sleep(3 * time.Second) // cross buckets
	if err := net.rcv.PublishKeys(); err != nil {
		t.Fatalf("republish: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	fetched, err := pathsel.GetKey(ctx, net.boot.DHT(), net.rcv.ID())
	if err != nil {
		t.Fatalf("GetKey: %v", err)
	}
	if bytes.Equal(fetched, oldPub) {
		t.Fatal("record still serves the pre-rotation subkey after republish")
	}
	if curPub, _ := net.rcv.identity.RotationPub(); !bytes.Equal(fetched, curPub) {
		t.Fatal("served subkey does not match the receiver's current subkey")
	}

	if err := net.rcv.RefreshContacts(ctx); err != nil {
		t.Fatalf("RefreshContacts: %v", err)
	}
	go func() {
		if err := net.snd.Send(net.rcv.ID(), []byte("to the fresh subkey")); err != nil {
			t.Errorf("Send: %v", err)
		}
	}()
	m := awaitMessage(t, net.rcv, 30*time.Second)
	if string(m.Content) != "to the fresh subkey" || !m.Verified {
		t.Fatalf("fresh-subkey message broken: content=%q verified=%v", m.Content, m.Verified)
	}
}

// TestReplayDiesAfterRetentionExpiry pins the forward-secrecy end state: a
// wire packet addressed to a subkey whose bucket has fallen out of the
// retention window must be undeliverable — the private key is destroyed, so
// neither the original replay nor any re-wrapped variant can open it. The
// same packet was deliverable while the key was retained (covered by
// TestMessageDeliveredAcrossRotation).
func TestReplayDiesAfterRetentionExpiry(t *testing.T) {
	net := rotationNet(t) // 1s buckets, retained window = 7 buckets

	// Capture a packet addressed to the receiver's CURRENT subkey.
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := net.rcv.RefreshContacts(ctx); err != nil {
		t.Fatalf("RefreshContacts: %v", err)
	}
	destPub, err := net.snd.lookupDestPub(net.rcv.ID())
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	msgID, err := cell.NewMsgID()
	if err != nil {
		t.Fatalf("msg id: %v", err)
	}
	c, err := cell.Seal(msgID, cell.TypeText, make([]byte, cell.TagSize), []byte("captured today"))
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	hops := []onion.Hop{
		{PeerID: net.boot.ID(), ECDHPub: net.boot.identity.ECDHPub()},
		{PeerID: net.rcv.ID(), ECDHPub: destPub},
	}
	on, err := onion.Build(hops, c)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	wirePkt, err := onion.Wrap(1, on.Bytes())
	if err != nil {
		t.Fatalf("wrap: %v", err)
	}

	// Shrink the receiver's retention window to 3 buckets and advance well
	// past it (1s buckets): the rotation loop prunes the subkey this packet
	// was sealed to, so the captured wire packet becomes undecryptable —
	// forward secrecy's end state, pinned end to end.
	net.rcv.identity.SetTestRetention(3)
	time.Sleep(9 * time.Second)

	received := make(chan string, 4)
	net.rcv.OnMessage(func(m *message.Message) { received <- string(m.Content) })
	if err := sendWire(net.snd, net.boot.ID(), wirePkt); err != nil {
		t.Fatalf("replay: %v", err)
	}

	select {
	case got := <-received:
		t.Fatalf("expired-subkey packet delivered: %q — retention window is not enforcing forward secrecy", got)
	case <-time.After(4 * time.Second):
		// dropped, as forward secrecy requires
	}
}
