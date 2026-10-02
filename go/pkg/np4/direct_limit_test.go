package np4

import (
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"

	"Np4Protocol/go/pkg/message"
)

// TestDirectFloodThrottled pins the unauthenticated-channel budget: the
// direct protocol listens on every node with NO authentication, so its
// per-peer token bucket is the only thing between a flood and the message
// bus. Under a tight budget a back-to-back burst is partially dropped —
// never amplified into the application.
func TestDirectFloodThrottled(t *testing.T) {
	dir := t.TempDir()
	snd, err := NewNode(0, WithIdentity(filepath.Join(dir, "snd")))
	if err != nil {
		t.Fatalf("snd: %v", err)
	}
	defer snd.Close()
	rcv, err := NewNode(0,
		WithIdentity(filepath.Join(dir, "rcv")),
		WithDirectRateLimit(1, 2), // 1 msg/s, burst 2: deterministic throttle
	)
	if err != nil {
		t.Fatalf("rcv: %v", err)
	}
	defer rcv.Close()

	if err := snd.Connect(peer.AddrInfo{ID: rcv.ID(), Addrs: rcv.Host().Addrs()}); err != nil {
		t.Fatalf("connect: %v", err)
	}

	// Delivery is asynchronous and may straddle the observation window —
	// collect under a mutex instead of closing a channel (a late delivery
	// must never panic the test).
	var mu sync.Mutex
	count := 0
	rcv.OnMessage(func(m *message.Message) {
		mu.Lock()
		count++
		mu.Unlock()
	})

	const burst = 8
	for i := 0; i < burst; i++ {
		if err := snd.SendDirect(rcv.ID(), []byte{byte('a' + i)}); err != nil {
			t.Fatalf("SendDirect %d: %v", i, err)
		}
	}
	time.Sleep(1500 * time.Millisecond) // delivery settle; refill adds ≤2 tokens

	mu.Lock()
	defer mu.Unlock()
	// Burst of 2 plus at most two refills within the settle window.
	if count > 4 {
		t.Fatalf("direct flood delivered %d/%d messages: throttle has no teeth", count, burst)
	}
	if count == 0 {
		t.Fatal("throttle dropped the entire burst including legitimate budget")
	}
}
