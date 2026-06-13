// Package np4 wires together the mix engine, onion layer, identity, and p2p
// host into a single Node. A Node runs in one of three modes:
//
//   - Direct-only: created without WithBootstrap/WithDHTServer. No DHT, no mix
//     routing; Send falls back to a single-hop direct stream.
//   - DHT server: created with WithDHTServer. Runs a standalone Kademlia DHT
//     in server mode (a seed/bootstrap node). Records published by other nodes
//     are stored here. Send falls back to direct.
//   - Routed: created with WithBootstrap. Joins the DHT, routes Send through
//     an onion path of relays selected via the path selector.
package np4

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"Np4Protocol/go/pkg/identity"
	"Np4Protocol/go/pkg/message"
	"Np4Protocol/go/pkg/mix"
	"Np4Protocol/go/pkg/onion"
	"Np4Protocol/go/pkg/p2p"
	"Np4Protocol/go/pkg/pathsel"

	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
)

const (
	ProtocolOnion  = protocol.ID("/np4/onion/1.0.0")
	ProtocolDirect = protocol.ID("/np4/direct/1.0.0")
)

const (
	defaultHops        = 3
	defaultMixBatch    = 10
	defaultMixDelay    = 500 * time.Millisecond
	defaultSendTimeout = 30 * time.Second
)

// Node is the local Np4Protocol peer. It owns a libp2p host, an identity, a
// message bus, and (optionally) a mix engine + DHT-backed path selector.
type Node struct {
	host     host.Host
	identity *identity.Identity
	bus      *message.MessageBus
	mix      *mix.MixEngine[pendingPacket]
	dht      *dht.IpfsDHT
	pathSel  *pathsel.Selector

	ctx      context.Context
	cancel   context.CancelFunc
	stopOnce sync.Once
}

// pendingPacket is what the MixEngine flushes: a ready-to-send onion packet
// and the peer ID of the first relay to send it to.
type pendingPacket struct {
	firstHop peer.ID
	onion    *onion.Onion
}

// Option configures a Node at construction time.
type Option func(*config)

type config struct {
	identityPath string
	bootstrap    []peer.AddrInfo
	rendezvous   string
	hops         int
	dhtServer    bool
}

// WithIdentity loads (or creates) the node's identity from path.
func WithIdentity(path string) Option { return func(c *config) { c.identityPath = path } }

// WithBootstrap enables DHT mode and bootstraps off the given peers.
func WithBootstrap(p []peer.AddrInfo) Option { return func(c *config) { c.bootstrap = p } }

// WithDHTServer runs the node as a standalone DHT server (no bootstrap peers).
// Use this for bootstrap/seed nodes that other nodes connect to — they need a
// running DHT in server mode to store records published by relays/clients.
// Mutually exclusive with WithBootstrap: WithDHTServer takes precedence.
func WithDHTServer() Option { return func(c *config) { c.dhtServer = true } }

// WithRendezvous overrides the default "np4-network" rendezvous string.
func WithRendezvous(r string) Option { return func(c *config) { c.rendezvous = r } }

// WithHops overrides the default onion path length.
func WithHops(h int) Option { return func(c *config) { c.hops = h } }

// NewNode creates a Node. Without WithBootstrap, the node runs in direct-only
// mode (no mix routing, no DHT). With WithBootstrap, it joins the DHT and
// routes Send calls through the mix.
func NewNode(port int, opts ...Option) (*Node, error) {
	cfg := config{identityPath: "", rendezvous: "np4-network", hops: defaultHops}
	for _, opt := range opts {
		opt(&cfg)
	}

	id, err := identity.LoadOrCreate(cfg.identityPath)
	if err != nil {
		return nil, fmt.Errorf("identity: %w", err)
	}
	h, err := p2p.NewHostWithIdentity(id, port)
	if err != nil {
		return nil, fmt.Errorf("host: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	n := &Node{
		host:     h,
		identity: id,
		bus:      message.NewMessageBus(),
		ctx:      ctx,
		cancel:   cancel,
	}
	n.bus.Start()
	n.mix = mix.NewMixEngine[pendingPacket](defaultMixBatch, defaultMixDelay, n.flushBatch)

	// DHT is enabled in two cases:
	//   - WithDHTServer: standalone seed/bootstrap node (no bootstrap peers,
	//     stores records for other nodes).
	//   - WithBootstrap: joins an existing DHT via bootstrap peers, also routes
	//     Send through the mix.
	if cfg.dhtServer || len(cfg.bootstrap) > 0 {
		kdht, err := p2p.StartDHT(ctx, h, cfg.bootstrap)
		if err != nil {
			cancel()
			h.Close()
			return nil, fmt.Errorf("dht: %w", err)
		}
		n.dht = kdht
		p2p.AdvertiseRendezvous(ctx, kdht, cfg.rendezvous)

		// A standalone DHT server (seed node) has no onion-path consumers; it
		// only serves records. Skip the path selector so Send falls back to
		// direct.
		if len(cfg.bootstrap) > 0 {
			n.pathSel = &pathsel.Selector{
				Hops:   cfg.hops,
				Finder: &pathsel.DHTFinder{DHT: kdht, Timeout: 15 * time.Second},
			}
		}
	}

	h.SetStreamHandler(ProtocolOnion, n.handleOnionStream)
	h.SetStreamHandler(ProtocolDirect, n.handleDirectStream)

	return n, nil
}

// ID returns the node's libp2p peer ID.
func (n *Node) ID() peer.ID { return n.host.ID() }

// Host returns the underlying libp2p host (for tests and low-level access).
func (n *Node) Host() host.Host { return n.host }

// DHT returns the node's Kademlia DHT, or nil in direct-only mode.
func (n *Node) DHT() *dht.IpfsDHT { return n.dht }

// Addrs returns this node's p2p multiaddrs as strings (for advertising).
func (n *Node) Addrs() []string {
	addrs := n.host.Addrs()
	out := make([]string, len(addrs))
	for i, a := range addrs {
		out[i] = fmt.Sprintf("%s/p2p/%s", a.String(), n.host.ID().String())
	}
	return out
}

// OnMessage registers a handler invoked for every delivered (final-hop) message.
func (n *Node) OnMessage(handler func(*message.Message)) {
	n.bus.OnMessage(handler)
}

// Connect establishes a direct libp2p connection to info (used for --direct
// sends and for chat's pre-connect step).
func (n *Node) Connect(info peer.AddrInfo) error {
	ctx, cancel := context.WithTimeout(n.ctx, 15*time.Second)
	defer cancel()
	return n.host.Connect(ctx, info)
}

// Send routes content through the mix (default). Falls back to SendDirect if
// path selection fails (e.g., not enough relays discovered) or if the node
// was created without WithBootstrap (direct-only mode).
func (n *Node) Send(dest peer.ID, content []byte) error {
	if n.pathSel == nil {
		return n.SendDirect(dest, content)
	}
	path, err := n.pathSel.Pick(n.ctx, n.ID(), dest)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[np4] mix path selection failed (%v); falling back to direct\n", err)
		return n.SendDirect(dest, content)
	}
	// Append destination as the final hop. lookupDestPub may return nil if
	// the destination hasn't published its ECDH key — onion.Build will then
	// fail and the caller gets a clear error.
	hops := append(path, onion.Hop{PeerID: dest, ECDHPub: n.lookupDestPub(dest)})
	on, err := onion.Build(hops, content)
	if err != nil {
		return fmt.Errorf("build onion: %w", err)
	}
	pkt := &pendingPacket{firstHop: hops[0].PeerID, onion: on}
	return n.mix.Add(pkt)
}

// SendDirect sends a single-hop message bypassing the mix (debug fallback).
func (n *Node) SendDirect(dest peer.ID, content []byte) error {
	ctx, cancel := context.WithTimeout(n.ctx, defaultSendTimeout)
	defer cancel()
	s, err := n.host.NewStream(ctx, dest, ProtocolDirect)
	if err != nil {
		return fmt.Errorf("open stream: %w", err)
	}
	defer s.Close()

	msg := &message.Message{
		Type:     message.TypeAsync,
		DestID:   dest.String(),
		SenderID: n.ID().String(),
		Content:  content,
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	return p2p.WriteMsg(s, data)
}

// PickPath returns the peer IDs of N relays that would be used to route to dest.
// Useful for debugging path selection without actually sending.
func (n *Node) PickPath(ctx context.Context, dest peer.ID) ([]peer.ID, error) {
	if n.pathSel == nil {
		return nil, errors.New("DHT not initialized")
	}
	hops, err := n.pathSel.Pick(ctx, n.ID(), dest)
	if err != nil {
		return nil, err
	}
	out := make([]peer.ID, len(hops))
	for i, h := range hops {
		out[i] = h.PeerID
	}
	return out, nil
}

// lookupDestPub fetches the destination's ECDH pubkey from the DHT. Returns
// nil on any error (caller's onion.Build will then fail with a clear message).
func (n *Node) lookupDestPub(dest peer.ID) []byte {
	if n.dht == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(n.ctx, 5*time.Second)
	defer cancel()
	pub, err := pathsel.GetECDH(ctx, n.dht, dest)
	if err != nil {
		return nil
	}
	return pub
}

// flushBatch is the MixEngine's onFlush callback: dispatches each pending
// packet to its first relay. Concurrent to amortize multiple sends.
func (n *Node) flushBatch(batch []*pendingPacket) {
	for _, pkt := range batch {
		go n.sendToRelay(pkt)
	}
}

func (n *Node) sendToRelay(pkt *pendingPacket) {
	ctx, cancel := context.WithTimeout(n.ctx, defaultSendTimeout)
	defer cancel()
	s, err := n.host.NewStream(ctx, pkt.firstHop, ProtocolOnion)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[np4] open relay stream: %v\n", err)
		return
	}
	defer s.Close()
	if err := p2p.WriteMsg(s, pkt.onion.Bytes()); err != nil {
		fmt.Fprintf(os.Stderr, "[np4] write to relay: %v\n", err)
	}
}

// handleOnionStream peels one layer using the local identity. If the layer is
// final, dispatches the payload to the message bus. Otherwise forwards the
// remaining ciphertext to the next hop.
func (n *Node) handleOnionStream(s network.Stream) {
	defer s.Close()
	data, err := p2p.ReadMsg(s)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[np4] onion read: %v\n", err)
		return
	}
	dec, err := onion.Decode(data, n.identity)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[np4] onion decode: %v\n", err)
		return
	}
	if dec.IsFinal {
		n.bus.Send(&message.Message{
			Type:     message.TypeAsync,
			SenderID: "anonymous",
			Content:  dec.Inner,
		})
		return
	}
	// Relay layer: forward remaining ciphertext to next hop.
	ctx, cancel := context.WithTimeout(n.ctx, defaultSendTimeout)
	defer cancel()
	next, err := n.host.NewStream(ctx, dec.NextHop, ProtocolOnion)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[np4] forward stream: %v\n", err)
		return
	}
	defer next.Close()
	if err := p2p.WriteMsg(next, dec.Inner); err != nil {
		fmt.Fprintf(os.Stderr, "[np4] forward write: %v\n", err)
	}
}

// handleDirectStream handles single-hop --direct messages.
func (n *Node) handleDirectStream(s network.Stream) {
	defer s.Close()
	data, err := p2p.ReadMsg(s)
	if err != nil {
		return
	}
	var msg message.Message
	if err := json.Unmarshal(data, &msg); err != nil {
		return
	}
	n.bus.Send(&msg)
}

// waitForDHTPeers blocks until the DHT routing table has at least min peers or
// the context expires. Returns nil if min peers are present, an error otherwise.
// This is critical for ServeRelay — PutValue needs at least one peer in the
// routing table to store the record.
func (n *Node) waitForDHTPeers(ctx context.Context, min int) error {
	if n.dht == nil {
		return errors.New("DHT not initialized")
	}
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		if n.dht.RoutingTable().Size() >= min {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("timed out waiting for %d DHT peers (have %d): %w",
				min, n.dht.RoutingTable().Size(), ctx.Err())
		case <-ticker.C:
		}
	}
}

// ServeRelay advertises this node as a mix relay in the DHT. Other nodes will
// be able to include it in their onion paths. Requires WithBootstrap.
func (n *Node) ServeRelay() error {
	if n.dht == nil {
		return errors.New("DHT not initialized; pass WithBootstrap when creating the node")
	}
	// Wait for the routing table to have at least one peer before publishing.
	// Without this, PutValue fails with "failed to find any peer in table".
	waitCtx, cancel := context.WithTimeout(n.ctx, 15*time.Second)
	defer cancel()
	if err := n.waitForDHTPeers(waitCtx, 1); err != nil {
		return fmt.Errorf("wait for DHT peers: %w", err)
	}
	p2p.AdvertiseRendezvous(n.ctx, n.dht, "np4-relay")
	if err := pathsel.PublishECDH(n.ctx, n.dht, n.ID(), n.identity.ECDHPub()); err != nil {
		return fmt.Errorf("publish ECDH: %w", err)
	}
	return nil
}

// PublishKeys publishes this node's ECDH pubkey to the DHT so other peers can
// build onion layers addressed to it. Like ServeRelay, it waits for the DHT
// routing table to have at least one peer first — but unlike ServeRelay it does
// NOT advertise as a mix relay. Use this for receiver/client nodes that need
// to be reachable (final onion hop) but should not be selected as intermediate
// relays. Requires WithBootstrap.
func (n *Node) PublishKeys() error {
	if n.dht == nil {
		return errors.New("DHT not initialized; pass WithBootstrap when creating the node")
	}
	waitCtx, cancel := context.WithTimeout(n.ctx, 15*time.Second)
	defer cancel()
	if err := n.waitForDHTPeers(waitCtx, 1); err != nil {
		return fmt.Errorf("wait for DHT peers: %w", err)
	}
	if err := pathsel.PublishECDH(n.ctx, n.dht, n.ID(), n.identity.ECDHPub()); err != nil {
		return fmt.Errorf("publish ECDH: %w", err)
	}
	return nil
}

// Close stops the mix engine, DHT, and host. Idempotent.
func (n *Node) Close() error {
	var firstErr error
	n.stopOnce.Do(func() {
		n.bus.Stop()
		n.cancel()
		if err := n.mix.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
		if n.dht != nil {
			if err := n.dht.Close(); err != nil && firstErr == nil {
				firstErr = err
			}
		}
		if err := n.host.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	})
	return firstErr
}

// Stop aliases Close for backward compatibility with existing callers.
func (n *Node) Stop() { _ = n.Close() }

// FindPeers wraps p2p.FindPeers for the CLI's `peers` command.
func (n *Node) FindPeers(ctx context.Context, rendezvous string) (<-chan peer.AddrInfo, error) {
	if n.dht == nil {
		return nil, errors.New("DHT not initialized")
	}
	return p2p.FindPeers(ctx, n.dht, rendezvous)
}
