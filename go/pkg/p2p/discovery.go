package p2p

import (
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/discovery/mdns"
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
