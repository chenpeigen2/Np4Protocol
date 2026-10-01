package p2p

import (
	"context"
	"fmt"

	"Np4Protocol/go/pkg/identity"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/connmgr"
	"github.com/libp2p/go-libp2p/core/control"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/security/noise"
	"github.com/libp2p/go-libp2p/p2p/transport/tcp"
	ma "github.com/multiformats/go-multiaddr"
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

// NewHostWithIdentity creates a libp2p host whose Peer ID is derived from the
// provided identity. Two hosts built from the same identity (e.g. loaded from
// the same key file) produce the same stable Peer ID, fixing the random-Peer-ID
// bug where every restart changed the node's address.
//
// gater, when non-nil, is the network admission control: connections from
// peer IDs it rejects are refused after the identity handshake. This is the
// only airtight choke point for single-server allowlists — record-level
// validation cannot stop a node from storing its own records locally, but a
// gated bootstrap keeps the uninvited node from ever joining the DHT at all.
func NewHostWithIdentity(id *identity.Identity, port int, gater connmgr.ConnectionGater) (host.Host, error) {
	addr := fmt.Sprintf("/ip4/0.0.0.0/tcp/%d", port)

	opts := []libp2p.Option{
		libp2p.Identity(id.PrivKey()),
		libp2p.ListenAddrStrings(addr),
		libp2p.Security(noise.ID, noise.New),
		libp2p.Transport(tcp.NewTCPTransport),
	}
	if gater != nil {
		opts = append(opts, libp2p.ConnectionGater(gater))
	}
	h, err := libp2p.New(opts...)
	if err != nil {
		return nil, err
	}

	return h, nil
}

// AdmissionGater wraps an admission function as a libp2p ConnectionGater.
func AdmissionGater(allow func(peer.ID) bool) connmgr.ConnectionGater {
	return admissionGater{allow: allow}
}

// admissionGater enforces admission in InterceptSecured — the only hook where
// the remote peer ID is authenticated. Earlier hooks must pass: the peer ID
// is simply not known before the security handshake.
type admissionGater struct {
	allow func(peer.ID) bool
}

func (g admissionGater) InterceptPeerDial(peer.ID) bool { return true }

func (g admissionGater) InterceptAddrDial(peer.ID, ma.Multiaddr) bool { return true }

func (g admissionGater) InterceptAccept(network.ConnMultiaddrs) bool { return true }

func (g admissionGater) InterceptSecured(_ network.Direction, p peer.ID, _ network.ConnMultiaddrs) bool {
	return g.allow(p)
}

func (g admissionGater) InterceptUpgraded(network.Conn) (bool, control.DisconnectReason) {
	return true, 0
}

// ConnectPeer parses a multiaddr string and connects to the peer.
func ConnectPeer(h host.Host, addr string) error {
	maddr, err := ma.NewMultiaddr(addr)
	if err != nil {
		return err
	}
	info, err := peer.AddrInfoFromP2pAddr(maddr)
	if err != nil {
		return err
	}
	return h.Connect(context.Background(), *info)
}
