package np4

import (
	"testing"
	"time"

	ic "github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
)

func testPeerID(t *testing.T, name string) peer.ID {
	t.Helper()
	_, pub, err := ic.GenerateEd25519Key(nil)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := peer.IDFromPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	return pid
}

// TestRelayLimiterBurstThenDrain pins the bucket semantics: a fresh peer can
// spend its full burst instantly, is throttled afterwards, regains tokens at
// the refill rate, and an idle bucket refills up to the burst ceiling.
func TestRelayLimiterBurstThenDrain(t *testing.T) {
	l := newRelayLimiter(10, 5) // 10 cells/s, burst 5
	pid := testPeerID(t, "flood")

	for i := 0; i < 5; i++ {
		if !l.allow(pid) {
			t.Fatalf("burst packet %d rejected within burst budget", i)
		}
	}
	if l.allow(pid) {
		t.Fatal("burst exceeded: extra packet allowed")
	}
	// 100ms at 10 cells/s = 1 token.
	time.Sleep(100 * time.Millisecond)
	if !l.allow(pid) {
		t.Fatal("refill did not restore a token after 100ms")
	}
	if l.allow(pid) {
		t.Fatal("more than one token restored after 100ms")
	}
	// Idle refill must cap at burst, not accumulate unbounded.
	time.Sleep(600 * time.Millisecond) // 6 tokens worth; ceiling is 5
	for i := 0; i < 5; i++ {
		if !l.allow(pid) {
			t.Fatalf("ceiling refill packet %d rejected", i)
		}
	}
	if l.allow(pid) {
		t.Fatal("bucket filled beyond burst")
	}
}

// TestRelayLimiterPerPeerIndependence: one peer exhausting its own bucket
// must not touch anyone else's — that independence is the point of the
// per-peer design.
func TestRelayLimiterPerPeerIndependence(t *testing.T) {
	l := newRelayLimiter(1, 2)
	a, b := testPeerID(t, "a"), testPeerID(t, "b")

	for i := 0; i < 2; i++ {
		if !l.allow(a) {
			t.Fatalf("peer A burst packet %d rejected", i)
		}
	}
	if l.allow(a) {
		t.Fatal("peer A over burst")
	}
	for i := 0; i < 2; i++ {
		if !l.allow(b) {
			t.Fatalf("peer B packet %d rejected while A is drained", i)
		}
	}
}

// TestRelayLimiterSweep: once the map grows past the cap, the sweep evicts
// the least-recently-used buckets — a churn of short-lived peers cannot leak
// memory.
func TestRelayLimiterSweep(t *testing.T) {
	l := newRelayLimiter(1, 1)
	for i := 0; i < maxLimiterEntries+10; i++ {
		l.allow(testPeerID(t, "churn"))
	}
	l.mu.Lock()
	size := len(l.buckets)
	l.mu.Unlock()
	if size >= maxLimiterEntries+10 {
		t.Fatalf("sweep left %d buckets (cap %d)", size, maxLimiterEntries)
	}
}
