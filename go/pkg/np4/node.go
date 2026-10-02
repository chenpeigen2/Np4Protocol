// Package np4 wires together the mix engine, onion layer, identity, and p2p
// host into a single Node. A Node runs in one of three modes:
//
//   - Direct-only: created without WithBootstrap/WithDHTServer. No DHT, no mix
//     routing; Send returns an error and callers must opt into SendDirect.
//   - DHT server: created with WithDHTServer. Runs a standalone Kademlia DHT
//     in server mode (a seed/bootstrap node). Records published by other nodes
//     are stored here. Send returns an error (no path selection).
//   - Routed: created with WithBootstrap. Joins the DHT, routes Send through
//     an onion path of relays selected via the path selector, wraps payloads
//     in fixed-size cells, and — if the node advertised as a relay — batches
//     and shuffles forwarded packets through its own relay-side MixEngine.
//
// Anonymity downgrade is never silent: Send hard-fails instead of falling
// back to a direct stream. Direct sends exist only as the explicit SendDirect
// API (CLI: --insecure).
package np4

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"Np4Protocol/go/pkg/auth"
	"Np4Protocol/go/pkg/cell"
	"Np4Protocol/go/pkg/identity"
	"Np4Protocol/go/pkg/message"
	"Np4Protocol/go/pkg/mix"
	"Np4Protocol/go/pkg/onion"
	"Np4Protocol/go/pkg/p2p"
	"Np4Protocol/go/pkg/pathsel"

	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/core/connmgr"
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

	// Relay-side mixing is weaker than entry mixing (its timing-correlation
	// value decays with depth) so it gets a tighter delay budget. Worst case
	// end-to-end latency ≈ 500ms + 3 × 200ms ≈ 1.1s.
	defaultRelayMixBatch = 10
	defaultRelayMixDelay = 200 * time.Millisecond

	// maxFlushConcurrency bounds the goroutines spawned by mix flushes.
	// Acquire blocks (backpressure) instead of spawning without limit.
	maxFlushConcurrency = 64

	// Replay window (~3.2 MB of 32-byte keys) and end-to-end message dedup.
	replayCacheCapacity = 100_000
	seenMsgCapacity     = 100_000

	// Discovery record TTLs. Short on purpose: a killed node's record must
	// expire quickly so peer/relay lists reflect live nodes instead of
	// accumulating corpses (the DHT default is hours). Republish happens at
	// half-TTL while the process is alive.
	peerAdvertiseTTL  = 2 * time.Minute
	relayAdvertiseTTL = 5 * time.Minute

	// maxDiscoveryLookups bounds how many rendezvous provider records one
	// discovery call resolves (each costs a DHT roundtrip). Provider spam
	// must not turn every Send / contact refresh into unbounded DHT work.
	maxDiscoveryLookups = 64

	// mixCapacity bounds the entry and relay mix buffers (~25 full batches):
	// the entry mix backpressures Send with a hard error, the relay mix
	// drops excess packets under flood instead of growing memory.
	mixCapacity = 256

	// defaultRelayBurst is the per-peer ingress bucket capacity: enough to
	// absorb a full mix batch flushed back-to-back without drops.
	defaultRelayBurst = 50

	// defaultContactRefresh is how often the receiver-side contact cache
	// (peer ID → published ECDH key, used for sender verification) rebuilds
	// from the DHT. Verification uses the snapshot; a brand-new contact is
	// verifiable within one interval.
	defaultContactRefresh = 30 * time.Second

	// productionDummyRate is the cover-traffic mean wired into the
	// production entrypoints (CLI, bridge, bootstrap). The library default
	// stays 0 so tests and embeddings get deterministic traffic unless they
	// opt in — the anonymity baseline measurement depends on that.
	productionDummyRate = 0.5 // cells/s per node ≈ 1 cell per 2s

	// defaultDirectBudget guards the unauthenticated direct protocol: far
	// above any human chat pace, tight enough that a flood is throttled.
	defaultDirectRate  = 5.0
	defaultDirectBurst = 20
)

// ProductionDummyRate is the cover-traffic mean used by production
// entrypoints; embedders may mirror it or pick their own via WithDummyRate.
const ProductionDummyRate = productionDummyRate

// ProductionIngressRate/Burst is the per-peer onion ingress budget that
// production entrypoints (CLI, bridge) apply when not explicitly configured:
// far above legitimate flow (a relay flushes at most 50 cells/s per peer),
// tight enough that a flood is throttled. Every node faces untrusted peers
// in open-admission mode — not just relays.
const (
	ProductionIngressRate  = 100.0
	ProductionIngressBurst = 200
)

// Node is the local Np4Protocol peer. It owns a libp2p host, an identity, a
// message bus, an entry mix engine, and — when serving as a relay — a relay
// mix engine, plus replay/dedup caches.
type Node struct {
	host      host.Host
	identity  *identity.Identity
	bus       *message.MessageBus
	mix       *mix.MixEngine[pendingPacket]
	relayMix  *mix.MixEngine[relayPacket]
	dht       *dht.IpfsDHT
	pathSel   *pathsel.Selector
	bootstrap peer.AddrInfo // empty in direct-only and DHT-server modes

	replay      *seenCache    // ephemeral onion keys seen by this node (anti-replay)
	seenMsg     *seenCache    // end-to-end msg_id dedup
	dispatchSem chan struct{} // semaphore bounding flush goroutines

	limiter       *relayLimiter // per-peer onion ingress rate limit; nil = unlimited
	directLimiter *relayLimiter // per-peer direct ingress budget (always on unless disabled)

	contactMu   sync.RWMutex
	contacts    map[peer.ID][]byte // verified-binding published ECDH keys, for sender verification
	contactTick time.Duration      // negative = manual refresh only
	dummyRate   float64            // cover-traffic mean cells/s; <=0 = off

	evMu      sync.RWMutex
	evHandler func(LinkEvent)

	ctx      context.Context
	cancel   context.CancelFunc
	stopOnce sync.Once
}

// LinkEvent is a passive observation of a packet crossing this node on the
// onion protocol. Everything here is visible to an honest-but-curious relay
// WITHOUT decrypting anything: arrival/departure time, immediate neighbor,
// and the ephemeral key of the observed layer (first 32 wire bytes).
//
// This hook exists for anonymity experiments: a test positions an adversary
// relay between two honest relays and measures how well timing correlation
// links ingress to egress.
type LinkEvent struct {
	T    time.Time
	In   bool    // true: received; false: forwarded out
	Peer peer.ID // immediate neighbor on this link
	Eph  [32]byte
}

// OnLinkEvent registers a passive observer for packets crossing this node.
// Must be called before traffic starts (no synchronization with in-flight
// handlers is provided).
func (n *Node) OnLinkEvent(h func(LinkEvent)) {
	n.evMu.Lock()
	n.evHandler = h
	n.evMu.Unlock()
}

func (n *Node) emitLinkEvent(ev LinkEvent) {
	n.evMu.RLock()
	h := n.evHandler
	n.evMu.RUnlock()
	if h != nil {
		h(ev)
	}
}

// pendingPacket is what the entry MixEngine flushes: a ready-to-send onion
// packet (already wrapped for its first hop) and the peer ID of that hop.
type pendingPacket struct {
	firstHop peer.ID
	ttl      uint8
	onion    *onion.Onion
}

// relayPacket is what the relay MixEngine flushes: a peeled layer's remaining
// ciphertext and where to send it next.
type relayPacket struct {
	nextHop peer.ID
	ttl     uint8
	data    []byte // layer ciphertext for the next hop (no TTL byte)
}

// Option configures a Node at construction time.
type Option func(*config)

type config struct {
	identityPath string
	bootstrap    []peer.AddrInfo
	rendezvous   string
	hops         int
	dhtServer    bool

	// immediateRelay disables relay-side batching (flush per packet, in
	// arrival order). TEST-ONLY control knob for anonymity experiments: it
	// lets a test run the same traffic with and without mixing to verify the
	// measurement harness actually detects correlation when mixing is off.
	immediateRelay bool

	// admission, when non-nil, gates key publication on this node's DHT:
	// records for peer IDs it rejects are refused at the validator. This is
	// the single-server allowlist choke point — a node that cannot publish
	// its key is invisible to the directory and unusable as relay/destination.
	admission func(peer.ID) bool

	// relay ingress rate limit; rate <= 0 disables the limiter.
	relayRate  float64
	relayBurst int

	// direct ingress budget (unauthenticated protocol, listens on every
	// node); rate <= 0 disables it. Defaults to defaultDirectRate/burst.
	directRate  float64
	directBurst int

	// contactRefresh controls the background sender-verification cache
	// rebuild. 0 disables the loop (RefreshContacts must be called manually).
	contactRefresh time.Duration

	// dummyRate is the cover-traffic mean in cells/s (Poisson). The library
	// default is 0 (deterministic tests); production entrypoints set 0.5.
	dummyRate float64

	// testBucketPeriod shortens the identity key-rotation period. TEST-ONLY
	// control knob (0 = production 24h): rotation e2e tests use second-scale
	// buckets instead of waiting a day.
	testBucketPeriod time.Duration
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

// WithImmediateRelayForward disables relay-side batching/shuffle (forward
// each packet immediately, in arrival order). This is a TEST-ONLY control
// knob for anonymity experiments — never enable in production: it removes
// the mixing this protocol exists to provide.
func WithImmediateRelayForward() Option { return func(c *config) { c.immediateRelay = true } }

// WithAdmission gates who may talk to this node at the connection level and,
// on DHT-server nodes, who may publish records: peer IDs the function rejects
// cannot complete a handshake (a gated bootstrap keeps outsiders out of the
// DHT entirely) and their np4 records are refused at the validator. Use it
// for single-server allowlist admission. nil (default) allows all.
func WithAdmission(allow func(peer.ID) bool) Option { return func(c *config) { c.admission = allow } }

// WithRelayRateLimit enables the per-peer ingress token bucket on the onion
// protocol: rate cells per second, burst bucket capacity. rate <= 0 disables
// limiting. Production entrypoints default it to 100/s with burst 200; the
// bootstrap's stricter --relay-rate default (10/s) reflects its dedicated
// relay role facing all clients at once.
func WithRelayRateLimit(rate float64, burst int) Option {
	return func(c *config) { c.relayRate, c.relayBurst = rate, burst }
}

// WithDirectRateLimit overrides the per-peer budget on the direct protocol
// (default 5 msg/s, burst 20). rate <= 0 disables it — only sensible in
// closed test setups, since the direct protocol is unauthenticated.
func WithDirectRateLimit(rate float64, burst int) Option {
	return func(c *config) { c.directRate, c.directBurst = rate, burst }
}

// WithContactRefreshInterval sets how often the sender-verification contact
// cache rebuilds from the DHT (default 30s). Pass a negative duration to
// disable the background loop entirely — callers then drive RefreshContacts
// manually; tests use that for determinism.
func WithContactRefreshInterval(d time.Duration) Option {
	return func(c *config) { c.contactRefresh = d }
}

// WithDummyRate sets the cover-traffic mean in cells/s (Poisson process).
// Zero disables dummy injection — the library default, so tests and
// embeddings stay deterministic; production entrypoints (CLI, bridge,
// bootstrap) enable 0.5. Dummies are protocol-identical to real packets
// (same path selection, same wire size, TypeDummy inside the innermost
// cell) and receivers drop them before the dedup cache.
func WithDummyRate(rate float64) Option { return func(c *config) { c.dummyRate = rate } }

// WithTestBucketPeriod shortens the identity key-rotation period. TEST-ONLY
// control knob (0 = production 24h): rotation e2e tests use second-scale
// buckets instead of waiting a day. Never enable in production.
func WithTestBucketPeriod(d time.Duration) Option {
	return func(c *config) { c.testBucketPeriod = d }
}

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
	if cfg.testBucketPeriod > 0 {
		id.SetTestRotation(nil, cfg.testBucketPeriod)
		if _, err := id.EnsureCurrentBucket(); err != nil {
			return nil, fmt.Errorf("test rotation: %w", err)
		}
	}
	// Connection-level admission: an unadmitted peer cannot even complete a
	// handshake with this node, so a gated bootstrap keeps outsiders out of
	// the DHT entirely — record-level validation alone cannot do that (a
	// node always stores its own records locally). nil gater = open network.
	var gater connmgr.ConnectionGater
	if cfg.admission != nil {
		gater = p2p.AdmissionGater(cfg.admission)
	}
	h, err := p2p.NewHostWithIdentity(id, port, gater)
	if err != nil {
		return nil, fmt.Errorf("host: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	n := &Node{
		host:        h,
		identity:    id,
		bus:         message.NewMessageBus(),
		replay:      newSeenCache(replayCacheCapacity),
		seenMsg:     newSeenCache(seenMsgCapacity),
		dispatchSem: make(chan struct{}, maxFlushConcurrency),
		contacts:    make(map[peer.ID][]byte),
		ctx:         ctx,
		cancel:      cancel,
	}
	if cfg.contactRefresh == 0 {
		cfg.contactRefresh = defaultContactRefresh
	}
	n.contactTick = cfg.contactRefresh // negative = manual refresh only
	n.dummyRate = cfg.dummyRate
	if cfg.relayRate > 0 {
		n.limiter = newRelayLimiter(cfg.relayRate, cfg.relayBurst)
	}
	if cfg.directRate == 0 {
		cfg.directRate, cfg.directBurst = defaultDirectRate, defaultDirectBurst
	}
	if cfg.directRate > 0 {
		n.directLimiter = newRelayLimiter(cfg.directRate, cfg.directBurst)
	}
	n.bus.Start()
	// Capacity bounds memory under flood: the entry mix backpressures Send
	// (hard error), the relay mix drops excess packets — both instead of
	// buffering without limit.
	n.mix = mix.NewMixEngine[pendingPacket](defaultMixBatch, defaultMixDelay, n.flushBatch,
		mix.WithCapacity[pendingPacket](mixCapacity))
	relayBatch, relayDelay := defaultRelayMixBatch, defaultRelayMixDelay
	if cfg.immediateRelay {
		relayBatch, relayDelay = 1, 0
	}
	n.relayMix = mix.NewMixEngine[relayPacket](relayBatch, relayDelay, n.flushRelayBatch,
		mix.WithCapacity[relayPacket](mixCapacity))

	// DHT is enabled in two cases:
	//   - WithDHTServer: standalone seed/bootstrap node (no bootstrap peers,
	//     stores records for other nodes).
	//   - WithBootstrap: joins an existing DHT via bootstrap peers, also routes
	//     Send through the mix.
	if cfg.dhtServer || len(cfg.bootstrap) > 0 {
		kdht, err := p2p.StartDHT(ctx, h, cfg.bootstrap, cfg.admission)
		if err != nil {
			cancel()
			h.Close()
			return nil, fmt.Errorf("dht: %w", err)
		}
		n.dht = kdht
		if len(cfg.bootstrap) > 0 {
			n.bootstrap = cfg.bootstrap[0]
		}
		p2p.AdvertiseRendezvousTTL(ctx, kdht, cfg.rendezvous, relayAdvertiseTTL)

		// Sender verification needs the published keys of every contact.
		// Rebuild the snapshot in the background; verification degrades to
		// "unverified" (never to a dropped message) while the cache is cold.
		go n.contactRefreshLoop()
		// Cover traffic on the Poisson schedule (rate 0 = off, the library
		// default; production entrypoints enable it).
		go n.dummyLoop()
		// Key rotation + record republish (forward secrecy schedule).
		go n.rotationLoop()

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

// BootstrapID returns the peer ID of this node's bootstrap node, if any.
// Clients use it to label infrastructure peers (the bootstrap is a relay in
// the single-server deployment, never a chat contact).
func (n *Node) BootstrapID() (peer.ID, bool) {
	if n.bootstrap.ID == "" {
		return "", false
	}
	return n.bootstrap.ID, true
}

// ID returns the node's libp2p peer ID.
func (n *Node) ID() peer.ID { return n.host.ID() }

// CurrentBucket exposes the identity's active rotation bucket (observability
// and tests).
func (n *Node) CurrentBucket() int64 { return n.identity.CurrentBucket() }

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

// pathSelectionTimeout bounds how long Send/PickPath wait for the DHT to
// become usable (routing table population + provider record fetch). A fresh
// node's routing table is empty for the first seconds; querying immediately
// silently returns nothing, which must not surface as an instant hard failure.
const pathSelectionTimeout = 25 * time.Second

// pickPath selects a relay path, retrying until the DHT is warm enough. Path
// selection fails fast on an empty routing table, so a single attempt at node
// startup is indistinguishable from "no relays exist" — retrying is the only
// way to tell them apart.
func (n *Node) pickPath(ctx context.Context, dest peer.ID) ([]onion.Hop, error) {
	deadline := time.Now().Add(pathSelectionTimeout)
	var lastErr error
	for {
		path, err := n.pathSel.Pick(ctx, n.ID(), dest)
		if err == nil {
			return path, nil
		}
		lastErr = err
		if time.Now().After(deadline) {
			return nil, lastErr
		}
		// A dropped bootstrap connection empties the routing table, and
		// FindPeers against an empty table returns instantly-empty — no
		// amount of plain retrying fixes that. Kick the DHT back to life.
		if n.dht.RoutingTable().Size() == 0 {
			_ = n.dht.Bootstrap(n.ctx)
		}
		select {
		case <-n.ctx.Done():
			return nil, n.ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

// Send routes content through the mix. It wraps content in a fixed-size cell
// (padding against size fingerprinting), picks a random relay path, builds
// the onion, and enqueues it in the entry mix engine.
//
// A nil error means "accepted into the mix" — it is NOT an end-to-end
// delivery guarantee (that requires the [v2] return-path ACK). Every failure
// — unrouted mode, path selection, missing destination keys, oversized
// content, full queue — is a hard error. There is NO silent direct fallback;
// callers who accept an unprotected send must say so explicitly via
// SendDirect.
func (n *Node) Send(dest peer.ID, content []byte) error {
	if n.pathSel == nil {
		return errors.New("mix unavailable: node not in routed mode (missing WithBootstrap); use SendDirect explicitly if an unprotected send is acceptable")
	}
	path, err := n.pickPath(n.ctx, dest)
	if err != nil {
		if errors.Is(err, pathsel.ErrNotEnoughRelays) {
			return fmt.Errorf("mix path selection failed: %w (reduce --hops or run more relays)", err)
		}
		return fmt.Errorf("mix path selection failed: %w", err)
	}
	destPub, err := n.lookupDestPub(dest)
	if err != nil {
		return fmt.Errorf("destination keys: %w", err)
	}
	// Sender authentication: pairwise HMAC over msg_id‖content, verifiable
	// only by the receiver (see package auth). Failure here is a hard error
	// before the mix accepts anything — no silent unauthenticated fallback.
	msgID, err := cell.NewMsgID()
	if err != nil {
		return fmt.Errorf("msg id: %w", err)
	}
	tag, err := auth.Tag(n.identity, destPub, dest, msgID, content)
	if err != nil {
		return fmt.Errorf("sender auth: %w", err)
	}
	c, err := cell.Seal(msgID, cell.TypeText, tag, content)
	if err != nil {
		return err
	}
	hops := append(path, onion.Hop{PeerID: dest, ECDHPub: destPub})
	if len(hops)-1 > onion.MaxInitialTTL {
		return fmt.Errorf("path too long: %d hops exceeds MaxInitialTTL %d", len(hops)-1, onion.MaxInitialTTL)
	}
	on, err := onion.Build(hops, c)
	if err != nil {
		return fmt.Errorf("build onion: %w", err)
	}
	pkt := &pendingPacket{firstHop: hops[0].PeerID, ttl: uint8(len(hops) - 1), onion: on}
	if err := n.mix.Add(pkt); err != nil {
		return fmt.Errorf("mix enqueue: %w", err)
	}
	return nil
}

// SendDirect sends a single-hop message bypassing the mix. This is an
// EXPLICIT anonymity downgrade — every CLI surface must label it insecure.
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

// WaitFlushed blocks until the entry mix queue is empty (everything accepted
// by Send has been dispatched to the first relay) or ctx expires. It does NOT
// guarantee end-to-end delivery — relays forward asynchronously after that.
//
// CLI one-shot commands need this: without it the process exits before the
// 500ms flush timer fires and the queued packet dies with the process.
func (n *Node) WaitFlushed(ctx context.Context) error {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		if n.mix.Pending() == 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// PickPath returns the peer IDs of N relays that would be used to route to dest.
// Useful for debugging path selection without actually sending.
func (n *Node) PickPath(ctx context.Context, dest peer.ID) ([]peer.ID, error) {
	if n.pathSel == nil {
		return nil, errors.New("DHT not initialized")
	}
	hops, err := n.pickPath(ctx, dest)
	if err != nil {
		return nil, err
	}
	out := make([]peer.ID, len(hops))
	for i, h := range hops {
		out[i] = h.PeerID
	}
	return out, nil
}

// lookupDestPub fetches the destination's published ed25519 key from the DHT
// and converts it to the X25519 pubkey used for the final onion layer.
func (n *Node) lookupDestPub(dest peer.ID) ([]byte, error) {
	if n.dht == nil {
		return nil, errors.New("DHT not initialized")
	}
	ctx, cancel := context.WithTimeout(n.ctx, 5*time.Second)
	defer cancel()
	return pathsel.GetKey(ctx, n.dht, dest)
}

// flushBatch is the entry MixEngine's onFlush callback: dispatches each
// pending packet to its first relay with bounded concurrency.
func (n *Node) flushBatch(batch []*pendingPacket) {
	for _, pkt := range batch {
		n.dispatch(func() { n.sendToRelay(pkt) })
	}
}

// flushRelayBatch is the relay MixEngine's onFlush callback: forwards each
// peeled packet to its next hop.
func (n *Node) flushRelayBatch(batch []*relayPacket) {
	for _, pkt := range batch {
		n.dispatch(func() { n.forwardToNextHop(pkt) })
	}
}

// dispatch runs fn on a new goroutine, bounding concurrency with a
// semaphore. When saturated it blocks — deliberate backpressure into the mix
// engine rather than unbounded goroutine/stream growth.
func (n *Node) dispatch(fn func()) {
	n.dispatchSem <- struct{}{}
	go func() {
		defer func() { <-n.dispatchSem }()
		fn()
	}()
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
	wire, err := onion.Wrap(pkt.ttl, pkt.onion.Bytes())
	if err != nil {
		fmt.Fprintf(os.Stderr, "[np4] wrap relay packet: %v\n", err)
		return
	}
	if err := p2p.WriteMsg(s, wire); err != nil {
		fmt.Fprintf(os.Stderr, "[np4] write to relay: %v\n", err)
	}
}

func (n *Node) forwardToNextHop(pkt *relayPacket) {
	ctx, cancel := context.WithTimeout(n.ctx, defaultSendTimeout)
	defer cancel()
	s, err := n.host.NewStream(ctx, pkt.nextHop, ProtocolOnion)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[np4] forward stream: %v\n", err)
		return
	}
	defer s.Close()
	wire, err := onion.Wrap(pkt.ttl, pkt.data)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[np4] wrap forward packet: %v\n", err)
		return
	}
	if len(pkt.data) >= 32 {
		var eph [32]byte
		copy(eph[:], pkt.data[:32])
		n.emitLinkEvent(LinkEvent{T: time.Now(), In: false, Peer: pkt.nextHop, Eph: eph})
	}
	if err := p2p.WriteMsg(s, wire); err != nil {
		fmt.Fprintf(os.Stderr, "[np4] forward write: %v\n", err)
	}
}

// handleOnionStream peels one layer using the local identity. If the layer is
// final, unwraps the cell, dedups by msg_id, and dispatches to the message
// bus. Otherwise it enqueues the remaining ciphertext in the relay mix
// engine (batch + shuffle + delay) instead of forwarding immediately.
func (n *Node) handleOnionStream(s network.Stream) {
	defer s.Close()
	// Per-peer ingress budget (relay nodes under client flood). Checked
	// before any crypto work so garbage costs the attacker nothing but a
	// closed stream.
	if n.limiter != nil && !n.limiter.allow(s.Conn().RemotePeer()) {
		fmt.Fprintf(os.Stderr, "[np4] ingress rate limited: %s\n", s.Conn().RemotePeer())
		return
	}
	// Wire packets are exactly WireSize by protocol; the tight read cap
	// keeps an overlong length prefix from allocating 1MB per stream before
	// the size check can reject it (allocation-amplification guard — matters
	// most on clients, which run no ingress limiter).
	data, err := p2p.ReadMsgCap(s, 2*onion.WireSize)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[np4] onion read: %v\n", err)
		return
	}
	ttl, layer, err := onion.Unwrap(data)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[np4] onion unwrap: %v\n", err)
		return
	}
	// Inbound TTL ceiling: honest senders cap the initial TTL at
	// MaxInitialTTL and it only decrements, so anything larger is crafted.
	// Without this ceiling a single 8KB packet with ttl=255 forces up to 255
	// decrypt rounds and 255 mix round-trips across the looped path — a
	// cheap CPU/queue-occupancy amplifier against relays.
	if ttl > onion.MaxInitialTTL {
		fmt.Fprintf(os.Stderr, "[np4] crafted ttl %d exceeds MaxInitialTTL, dropping\n", ttl)
		return
	}
	var eph [32]byte
	copy(eph[:], layer[:32])
	n.emitLinkEvent(LinkEvent{T: time.Now(), In: true, Peer: s.Conn().RemotePeer(), Eph: eph})
	dec, err := onion.Decode(layer, n.identity)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[np4] onion decode: %v\n", err)
		return
	}
	// Anti-replay: mark only after a successful decrypt so garbage floods
	// cannot evict legitimate keys from the cache. Exactly one concurrent
	// copy wins the mark.
	if key, ok := onion.ReplayKey(layer); ok && n.replay.Mark(string(key)) {
		fmt.Fprintf(os.Stderr, "[np4] replayed layer dropped\n")
		return
	}
	if dec.IsFinal {
		msgID, typ, tag, content, err := cell.Open(dec.Inner)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[np4] cell open: %v\n", err)
			return
		}
		// Dummy cells are cover traffic: drop before dedup so they cannot
		// evict real msg_ids from the seen cache.
		if typ == cell.TypeDummy {
			return
		}
		// End-to-end dedup backstop: a replay that slips past per-hop caches
		// (e.g. re-sent after cache eviction) must not double-deliver.
		if n.seenMsg.Mark(string(msgID)) {
			return
		}
		// Sender verification: the first contact whose pairwise key
		// reproduces the tag is attributed; otherwise the message is
		// delivered as unverified (badge), never dropped — a friend not yet
		// in the contact cache must still be able to reach us.
		senderID, verified := "anonymous", false
		n.contactMu.RLock()
		for pid, pub := range n.contacts {
			ok, err := auth.Verify(n.identity, pub, pid, msgID, content, tag)
			if err == nil && ok {
				senderID, verified = pid.String(), true
				break
			}
		}
		n.contactMu.RUnlock()
		n.bus.Send(&message.Message{
			Type:     message.TypeAsync,
			SenderID: senderID,
			Verified: verified,
			Content:  content,
		})
		return
	}
	// Relay layer: TTL 0 means this packet has looped or was crafted with an
	// exhausted budget — drop it. Looping onions otherwise live forever (each
	// hop's decrypt is deterministic).
	if ttl == 0 {
		fmt.Fprintf(os.Stderr, "[np4] ttl exhausted, dropping\n")
		return
	}
	if err := n.relayMix.Add(&relayPacket{nextHop: dec.NextHop, ttl: ttl - 1, data: dec.Inner}); err != nil {
		fmt.Fprintf(os.Stderr, "[np4] relay mix enqueue: %v\n", err)
	}
}

// handleDirectStream handles single-hop --direct messages.
func (n *Node) handleDirectStream(s network.Stream) {
	defer s.Close()
	// The direct protocol carries no authentication (it IS the anonymity
	// downgrade) and listens on every node, not just --insecure senders —
	// without a budget it is an unthrottled UI-spam injection channel that
	// bypasses the mix and the relay limiter entirely.
	if n.directLimiter != nil && !n.directLimiter.allow(s.Conn().RemotePeer()) {
		return
	}
	data, err := p2p.ReadMsg(s)
	if err != nil {
		return
	}
	var msg message.Message
	if err := json.Unmarshal(data, &msg); err != nil {
		return
	}
	// Only accept direct messages actually addressed to us.
	if msg.DestID != "" && msg.DestID != n.ID().String() {
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
	p2p.AdvertiseRendezvousTTL(n.ctx, n.dht, RendezvousRelay, relayAdvertiseTTL)
	if err := n.publishKeyRecord(); err != nil {
		return fmt.Errorf("publish key: %w", err)
	}
	p2p.AdvertiseRendezvousTTL(n.ctx, n.dht, RendezvousPeers, peerAdvertiseTTL)
	return nil
}

// publishKeyRecord publishes the current rotation record (master-bound,
// signed subkey) to the DHT.
func (n *Node) publishKeyRecord() error {
	pub, bucket := n.identity.RotationPub()
	return pathsel.PublishKey(n.ctx, n.dht, n.ID(), n.identity.SigningPubKey(), pub, bucket, n.identity.Sign)
}

// rotationLoop keeps the published record aligned with the key schedule:
// on each tick it rotates the subkey if the bucket rolled over (republishing
// the fresh key) and republishes regardless — DHT records carry a finite
// EOL, so a node that never republishes goes unreachable after a day. All
// failures are silent: the next tick retries once the DHT is reachable.
func (n *Node) rotationLoop() {
	if n.dht == nil {
		return
	}
	tick := 15 * time.Minute
	// Short test buckets need proportionally faster rotation checks.
	if p := n.identity.BucketPeriod(); p/2 < tick {
		tick = p / 2
	}
	ticker := time.NewTicker(tick)
	defer ticker.Stop()
	for {
		if rotated, err := n.identity.EnsureCurrentBucket(); err == nil && rotated {
			fmt.Fprintf(os.Stderr, "[np4] key rotated: subkey for bucket %d published\n", n.identity.CurrentBucket())
		}
		_ = n.publishKeyRecord()
		select {
		case <-n.ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// PublishKeys publishes this node's ed25519 key to the DHT so other peers can
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
	if err := n.publishKeyRecord(); err != nil {
		return fmt.Errorf("publish key: %w", err)
	}
	// Advertise under the peers rendezvous so ListPeers (client peer pickers)
	// discovers us. Short TTL + republish: dead nodes vanish within ~2 min.
	p2p.AdvertiseRendezvousTTL(n.ctx, n.dht, RendezvousPeers, peerAdvertiseTTL)
	return nil
}

// Close stops the mix engines, DHT, and host. Idempotent.
func (n *Node) Close() error {
	var firstErr error
	n.stopOnce.Do(func() {
		n.bus.Stop()
		n.cancel()
		if err := n.mix.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
		if err := n.relayMix.Close(); err != nil && firstErr == nil {
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

// RendezvousPeers is the rendezvous under which every key-publishing node
// advertises itself, making the set of addressable peers discoverable via
// ListPeers. Relays additionally advertise RendezvousRelay for path
// selection.
const RendezvousPeers = "np4-peers"

// RendezvousRelay is the relay-discovery rendezvous used by path selection.
const RendezvousRelay = "np4-relay"

// ListPeers returns the addressable peers discovered via the np4-peers
// rendezvous, excluding self. A peer is only listed when its published key
// verifies against the DHT's peer-ID binding — the same check Send relies on
// — so every entry is reachable through the mix. The verified X25519 onion
// key and addresses ride along for callers that need them (address book,
// direct-mode dialing).
func (n *Node) ListPeers(ctx context.Context) ([]pathsel.PeerInfo, error) {
	if n.dht == nil {
		return nil, errors.New("DHT not initialized")
	}
	peerChan, err := p2p.FindPeers(ctx, n.dht, RendezvousPeers)
	if err != nil {
		return nil, fmt.Errorf("find peers: %w", err)
	}
	var out []pathsel.PeerInfo
	examined := 0
	for pi := range peerChan {
		// Provider-spam bound: an attacker can flood the rendezvous with
		// provider records, and each candidate costs a DHT GetKey roundtrip.
		// Examining a bounded slice keeps discovery O(1) per call; a friends
		// network never has this many simultaneous contacts.
		examined++
		if examined > maxDiscoveryLookups {
			// Keep draining in the background until the query closes the
			// channel — dropping the producer mid-send would leak its
			// goroutine until the node-level context expires.
			go func() {
				for range peerChan {
				}
			}()
			break
		}
		if pi.ID == n.ID() {
			continue
		}
		ecdhPub, err := pathsel.GetKey(ctx, n.dht, pi.ID)
		if err != nil {
			continue // no key yet, stale record, or invalid binding
		}
		addrs := make([]string, 0, len(pi.Addrs))
		for _, a := range pi.Addrs {
			addrs = append(addrs, a.String())
		}
		out = append(out, pathsel.PeerInfo{ID: pi.ID, ECDHPub: ecdhPub, Addrs: addrs})
	}
	return out, nil
}

// FindPeers wraps p2p.FindPeers for the CLI's `peers` command.
func (n *Node) FindPeers(ctx context.Context, rendezvous string) (<-chan peer.AddrInfo, error) {
	if n.dht == nil {
		return nil, errors.New("DHT not initialized")
	}
	return p2p.FindPeers(ctx, n.dht, rendezvous)
}

// RefreshContacts rebuilds the sender-verification cache: every key-verified
// peer from ListPeers with its published ECDH key. The background loop calls
// this every contactTick; tests and embedding UIs may call it manually.
func (n *Node) RefreshContacts(ctx context.Context) error {
	peers, err := n.ListPeers(ctx)
	if err != nil {
		return err
	}
	m := make(map[peer.ID][]byte, len(peers))
	for _, p := range peers {
		m[p.ID] = p.ECDHPub
	}
	n.contactMu.Lock()
	n.contacts = m
	n.contactMu.Unlock()
	return nil
}

// contactRefreshLoop keeps the verification cache warm until the node stops.
// A cold cache degrades messages to "unverified" instead of blocking them,
// so the loop is best-effort by design.
func (n *Node) contactRefreshLoop() {
	if n.contactTick < 0 || n.dht == nil {
		return
	}
	for {
		ctx, cancel := context.WithTimeout(n.ctx, 10*time.Second)
		if err := n.RefreshContacts(ctx); err != nil {
			// Transient DHT state (e.g. empty routing table on startup).
			_ = err
		}
		cancel()
		select {
		case <-n.ctx.Done():
			return
		case <-time.After(n.contactTick):
		}
	}
}
