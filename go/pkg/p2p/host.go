package p2p

import (
	"context"
	"fmt"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/security/noise"
	"github.com/libp2p/go-libp2p/p2p/transport/tcp"
	"github.com/multiformats/go-multiaddr"
)

// NewHost creates a new libp2p host listening on the given TCP port.
// Port 0 picks a random available port.
func NewHost(port int) (host.Host, error) {
	addr := fmt.Sprintf("/ip4/0.0.0.0/tcp/%d", port)

	h, err := libp2p.New(
		libp2p.ListenAddrStrings(addr),
		libp2p.Security(noise.ID, noise.New),
		libp2p.Transport(tcp.NewTCPTransport),
	)
	if err != nil {
		return nil, err
	}

	return h, nil
}

// ConnectPeer parses a multiaddr string and connects to the peer.
func ConnectPeer(h host.Host, addr string) error {
	maddr, err := multiaddr.NewMultiaddr(addr)
	if err != nil {
		return err
	}
	info, err := peer.AddrInfoFromP2pAddr(maddr)
	if err != nil {
		return err
	}
	return h.Connect(context.Background(), *info)
}
