package p2p

import (
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
)

func TestMDNSDiscovery(t *testing.T) {
	h1, _ := NewHost(0)
	defer h1.Close()
	h2, _ := NewHost(0)
	defer h2.Close()

	found := make(chan peer.ID, 1)

	notifee1 := &discoveryNotifee{h: h1, found: found}
	if err := StartMDNS(h1, "np4-test", notifee1); err != nil {
		t.Fatal(err)
	}

	notifee2 := &discoveryNotifee{h: h2, found: make(chan peer.ID, 1)}
	if err := StartMDNS(h2, "np4-test", notifee2); err != nil {
		t.Fatal(err)
	}

	select {
	case pid := <-found:
		if pid != h2.ID() {
			t.Errorf("expected to find h2, got %s", pid)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("mDNS discovery timeout")
	}
}
