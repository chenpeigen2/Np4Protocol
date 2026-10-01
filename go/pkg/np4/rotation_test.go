package np4

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"

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
