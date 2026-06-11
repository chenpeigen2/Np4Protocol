package p2p

import (
	"context"

	dht "github.com/libp2p/go-libp2p-kad-dht"
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
		n.found <- pi.ID
	}
}

// StartMDNS starts mDNS peer discovery on the given host.
func StartMDNS(h host.Host, serviceTag string, notifee *discoveryNotifee) error {
	s := mdns.NewMdnsService(h, serviceTag, notifee)
	return s.Start()
}

// StartDHT creates a DHT instance and bootstraps it with the given peers.
func StartDHT(ctx context.Context, h host.Host, bootstrapPeers []peer.AddrInfo) (*dht.IpfsDHT, error) {
	kademliaDHT, err := dht.New(ctx, h, dht.BootstrapPeers(bootstrapPeers...))
	if err != nil {
		return nil, err
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
