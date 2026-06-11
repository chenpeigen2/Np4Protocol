package p2p

import (
	"context"
	"testing"
	"time"

	dht "github.com/libp2p/go-libp2p-kad-dht"
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

func TestDHTStartAndBootstrap(t *testing.T) {
	ctx := context.Background()

	// Create bootstrap node in server mode
	bootstrap, err := NewHost(0)
	if err != nil {
		t.Fatal(err)
	}
	defer bootstrap.Close()

	_, err = dht.New(ctx, bootstrap, dht.Mode(dht.ModeServer))
	if err != nil {
		t.Fatal(err)
	}

	// Create a client node
	h1, err := NewHost(0)
	if err != nil {
		t.Fatal(err)
	}
	defer h1.Close()

	bootstrapPeers := []peer.AddrInfo{{ID: bootstrap.ID(), Addrs: bootstrap.Addrs()}}
	dht1, err := StartDHT(ctx, h1, bootstrapPeers)
	if err != nil {
		t.Fatal(err)
	}

	// Connect client to bootstrap
	if err := h1.Connect(ctx, peer.AddrInfo{ID: bootstrap.ID(), Addrs: bootstrap.Addrs()}); err != nil {
		t.Fatal(err)
	}

	// Wait for routing table to populate
	deadline := time.After(10 * time.Second)
	for dht1.RoutingTable().Find(bootstrap.ID()) == "" {
		select {
		case <-deadline:
			t.Fatal("routing table did not populate with bootstrap peer")
		case <-time.After(100 * time.Millisecond):
		}
	}

	t.Logf("DHT routing table populated: found bootstrap peer %s", bootstrap.ID())
}

func TestDHTDiscovery(t *testing.T) {
	ctx := context.Background()

	// Create bootstrap node in server mode
	bootstrap, err := NewHost(0)
	if err != nil {
		t.Fatal(err)
	}
	defer bootstrap.Close()

	_, err = dht.New(ctx, bootstrap, dht.Mode(dht.ModeServer))
	if err != nil {
		t.Fatal(err)
	}

	// Create two client nodes
	h1, err := NewHost(0)
	if err != nil {
		t.Fatal(err)
	}
	defer h1.Close()
	h2, err := NewHost(0)
	if err != nil {
		t.Fatal(err)
	}
	defer h2.Close()

	bootstrapPeers := []peer.AddrInfo{{ID: bootstrap.ID(), Addrs: bootstrap.Addrs()}}

	dht1, err := StartDHT(ctx, h1, bootstrapPeers)
	if err != nil {
		t.Fatal(err)
	}
	dht2, err := StartDHT(ctx, h2, bootstrapPeers)
	if err != nil {
		t.Fatal(err)
	}

	// Connect both clients to bootstrap
	if err := h1.Connect(ctx, peer.AddrInfo{ID: bootstrap.ID(), Addrs: bootstrap.Addrs()}); err != nil {
		t.Fatal(err)
	}
	if err := h2.Connect(ctx, peer.AddrInfo{ID: bootstrap.ID(), Addrs: bootstrap.Addrs()}); err != nil {
		t.Fatal(err)
	}

	// Also connect h1 and h2 directly so the DHT routing tables populate
	if err := h1.Connect(ctx, peer.AddrInfo{ID: h2.ID(), Addrs: h2.Addrs()}); err != nil {
		t.Fatal(err)
	}

	// Wait for DHT to process connections and refresh routing tables
	time.Sleep(3 * time.Second)
	<-dht1.RefreshRoutingTable()
	<-dht2.RefreshRoutingTable()

	// Advertise both at same rendezvous
	if err := AdvertiseRendezvousSync(ctx, dht1, "np4-discovery"); err != nil {
		t.Logf("dht1 advertise: %v", err)
	}
	if err := AdvertiseRendezvousSync(ctx, dht2, "np4-discovery"); err != nil {
		t.Logf("dht2 advertise: %v", err)
	}

	// Give provider records time to propagate
	time.Sleep(2 * time.Second)

	// Find peers
	peerChan, err := FindPeers(ctx, dht1, "np4-discovery")
	if err != nil {
		t.Fatal(err)
	}

	found := false
	timeout := time.After(10 * time.Second)
	for {
		select {
		case pi, ok := <-peerChan:
			if !ok {
				goto done
			}
			if pi.ID == h2.ID() {
				found = true
				goto done
			}
		case <-timeout:
			goto done
		}
	}
done:
	if !found {
		t.Error("h1 should have found h2 via DHT")
	}
}
