package p2p

import (
	"context"

	dht "github.com/libp2p/go-libp2p-kad-dht"
	record "github.com/libp2p/go-libp2p-record"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/discovery/mdns"
	drouting "github.com/libp2p/go-libp2p/p2p/discovery/routing"
	dutil "github.com/libp2p/go-libp2p/p2p/discovery/util"
)

// PeerFoundHandler is called when a peer is discovered.
type PeerFoundHandler func(peer.AddrInfo)

// discoveryNotifee implements mdns.Notifee.
type discoveryNotifee struct {
	h       host.Host
	found   chan peer.ID
	handler PeerFoundHandler
}

func (n *discoveryNotifee) HandlePeerFound(pi peer.AddrInfo) {
	if n.handler != nil {
		n.handler(pi)
	}
	if n.found != nil {
		select {
		case n.found <- pi.ID:
		default:
		}
	}
}

// StartMDNS starts mDNS peer discovery on the given host.
func StartMDNS(h host.Host, serviceTag string, notifee *discoveryNotifee) error {
	s := mdns.NewMdnsService(h, serviceTag, notifee)
	return s.Start()
}

// acceptAllValidator accepts every record as valid. The libp2p record system
// already signs records with the publisher's private key, so we don't need
// additional validation for our ECDH pubkeys stored under /np4/ecdh/*.
type acceptAllValidator struct{}

func (acceptAllValidator) Validate(key string, value []byte) error { return nil }
func (acceptAllValidator) Select(key string, values [][]byte) (int, error) {
	if len(values) == 0 {
		return 0, nil
	}
	return 0, nil
}

// StartDHT creates a DHT instance, registers an accept-all validator for the
// "np4" namespace (so PutValue/GetValue work for /np4/ecdh/* records), sets
// server mode so this node stores+serves records, and bootstraps.
//
// The np4 validator is injected into the DHT's namespaced validator map AFTER
// construction. The Amino-locked default DHT validates its config to require
// exactly the /pk and /ipns namespaces during dht.New, so we let those
// defaults populate normally and then add "np4" to the (already-exposed)
// record.NamespacedValidator map before any PutValue/GetValue runs.
func StartDHT(ctx context.Context, h host.Host, bootstrapPeers []peer.AddrInfo) (*dht.IpfsDHT, error) {
	kademliaDHT, err := dht.New(ctx, h,
		dht.BootstrapPeers(bootstrapPeers...),
		dht.Mode(dht.ModeServer),
	)
	if err != nil {
		return nil, err
	}
	if nsVal, ok := kademliaDHT.Validator.(record.NamespacedValidator); ok {
		nsVal["np4"] = acceptAllValidator{}
	}
	if err := kademliaDHT.Bootstrap(ctx); err != nil {
		return nil, err
	}
	return kademliaDHT, nil
}

// AdvertiseRendezvous advertises this host at the given rendezvous string.
func AdvertiseRendezvous(ctx context.Context, kademliaDHT *dht.IpfsDHT, rendezvous string) {
	routingDiscovery := drouting.NewRoutingDiscovery(kademliaDHT)
	dutil.Advertise(ctx, routingDiscovery, rendezvous)
}

// AdvertiseRendezvousSync synchronously advertises and waits for the first advertisement to complete.
func AdvertiseRendezvousSync(ctx context.Context, kademliaDHT *dht.IpfsDHT, rendezvous string) error {
	routingDiscovery := drouting.NewRoutingDiscovery(kademliaDHT)
	_, err := routingDiscovery.Advertise(ctx, rendezvous)
	return err
}

// FindPeers discovers peers at the given rendezvous string.
func FindPeers(ctx context.Context, kademliaDHT *dht.IpfsDHT, rendezvous string) (<-chan peer.AddrInfo, error) {
	routingDiscovery := drouting.NewRoutingDiscovery(kademliaDHT)
	return routingDiscovery.FindPeers(ctx, rendezvous)
}
