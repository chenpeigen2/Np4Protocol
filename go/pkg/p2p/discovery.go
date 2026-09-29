package p2p

import (
	"bytes"
	"context"
	"encoding/base32"
	"errors"
	"fmt"
	"strings"

	dht "github.com/libp2p/go-libp2p-kad-dht"
	record "github.com/libp2p/go-libp2p-record"
	"github.com/libp2p/go-libp2p/core/crypto"
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

// np4Validator enforces the record↔peer binding for /np4/ecdh/* records.
//
// The DHT never verifies record signatures (see routing.go/handlers.go in
// go-libp2p-kad-dht — Validate is the only integrity checkpoint), so this
// binding is the ONLY defense against key poisoning: an attacker cannot
// publish their own key under a victim's peer ID without finding a public
// key that hashes to the victim's ID.
//
// The stored value is the node's ed25519 public key (libp2p-marshaled), same
// convention as /pk records. Senders convert it to X25519 locally.
//
// Note: NamespacedValidator passes the FULL key through to inner validators
// (it only uses the first segment for dispatch), so `key` here is
// "/np4/ecdh/<base32(peer-multihash)>". We take everything after the last
// '/' — base32's alphabet never contains '/'.
type np4Validator struct{}

func (np4Validator) Validate(key string, value []byte) error {
	raw, err := base32.StdEncoding.DecodeString(key[strings.LastIndexByte(key, '/')+1:])
	if err != nil {
		return fmt.Errorf("np4: key is not valid base32: %w", err)
	}
	pk, err := crypto.UnmarshalPublicKey(value)
	if err != nil {
		return fmt.Errorf("np4: value is not a libp2p public key: %w", err)
	}
	id, err := peer.IDFromPublicKey(pk)
	if err != nil {
		return err
	}
	if !bytes.Equal(raw, []byte(id)) {
		return fmt.Errorf("np4: public key binds to peer %s, wanted %s", id, peer.ID(raw))
	}
	return nil
}

func (np4Validator) Select(key string, values [][]byte) (int, error) {
	if len(values) == 0 {
		return 0, errors.New("np4: no values to select")
	}
	return 0, nil
}

// StartDHT creates a DHT instance, registers the np4 record validator (which
// enforces the record↔peer binding — the DHT does not verify signatures, so
// this check is the only defense against key poisoning), sets server mode so
// this node stores+serves records, and bootstraps.
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
		nsVal["np4"] = np4Validator{}
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
