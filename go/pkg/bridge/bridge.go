// Package bridge exposes the np4 Node over a small, stable JSON API for
// embedding into GUI clients (the Flutter client today; other runtimes later).
//
// Design rules:
//
//   - One call surface: Create / Call / Stop. Every argument and result is
//     JSON; binary content travels as base64. No structs cross the FFI
//     boundary, so the ABI never changes when fields are added — callers only
//     need the four exported symbols in cmd/np4bridge.
//   - Events are POLLed, not pushed. A native callback into a GUI runtime
//     brings pointer-lifetime hazards (the GUI may consume the buffer after
//     the producer freed it). A bounded per-node queue drained synchronously
//     keeps every FFI call valid only for its duration, and the Dart side
//     still exposes an idiomatic Stream on top.
//   - Anonymity semantics are inherited unchanged: Send is mix-only and
//     hard-fails; direct sends are an explicit method. There is no fallback
//     here either.
package bridge

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"Np4Protocol/go/pkg/message"
	"Np4Protocol/go/pkg/np4"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
)

// Config is the JSON body for Create.
type Config struct {
	Port         int    `json:"port"`          // 0 = random
	IdentityPath string `json:"identity_path"` // empty = ephemeral in-memory identity
	Bootstrap    string `json:"bootstrap"`     // bootstrap node multiaddr; empty = direct-only mode
	Hops         int    `json:"hops"`          // onion path length; 0 = protocol default (3)
	Rendezvous   string `json:"rendezvous"`    // empty = "np4-network"
}

// Request is the JSON body for Call.
type Request struct {
	Method string          `json:"method"`
	Args   json.RawMessage `json:"args"`
}

var errUnknownHandle = errors.New("bridge: unknown handle")

// Manager owns every live node handle. Handles are process-global: Dart
// isolates may come and go, the Go objects outlive them.
type Manager struct {
	mu      sync.Mutex
	next    int64
	handles map[int64]*handle
}

// Default is the manager used by the exported FFI symbols.
var Default = NewManager()

func NewManager() *Manager {
	return &Manager{handles: map[int64]*handle{}}
}

type handle struct {
	node   *np4.Node
	events *eventQueue
}

// Create builds a node from a JSON Config and registers a handle for it.
func (m *Manager) Create(configJSON []byte) (map[string]any, error) {
	var cfg Config
	if err := json.Unmarshal(configJSON, &cfg); err != nil {
		return nil, fmt.Errorf("bridge: config: %w", err)
	}

	var opts []np4.Option
	if cfg.IdentityPath != "" {
		opts = append(opts, np4.WithIdentity(cfg.IdentityPath))
	}
	if cfg.Hops > 0 {
		opts = append(opts, np4.WithHops(cfg.Hops))
	}
	if cfg.Rendezvous != "" {
		opts = append(opts, np4.WithRendezvous(cfg.Rendezvous))
	}
	if cfg.Bootstrap != "" {
		ma, err := multiaddr.NewMultiaddr(cfg.Bootstrap)
		if err != nil {
			return nil, fmt.Errorf("bridge: bootstrap multiaddr: %w", err)
		}
		ai, err := peer.AddrInfoFromP2pAddr(ma)
		if err != nil {
			return nil, fmt.Errorf("bridge: bootstrap multiaddr: %w", err)
		}
		opts = append(opts, np4.WithBootstrap([]peer.AddrInfo{*ai}))
	}

	node, err := np4.NewNode(cfg.Port, opts...)
	if err != nil {
		return nil, fmt.Errorf("bridge: node: %w", err)
	}

	h := &handle{node: node, events: newEventQueue(eventQueueCapacity)}
	node.OnMessage(func(msg *message.Message) {
		h.events.push(map[string]any{
			"type":        "message",
			"sender":      msg.SenderID,
			"content_b64": base64.StdEncoding.EncodeToString(msg.Content),
		})
	})

	m.mu.Lock()
	defer m.mu.Unlock()
	m.next++
	id := m.next
	m.handles[id] = h
	return map[string]any{
		"handle":  id,
		"peer_id": node.ID().String(),
		"addrs":   node.Addrs(),
	}, nil
}

// Call executes one method on a handle and returns its JSON result.
func (m *Manager) Call(id int64, req []byte) (map[string]any, error) {
	var r Request
	if err := json.Unmarshal(req, &r); err != nil {
		return nil, fmt.Errorf("bridge: request: %w", err)
	}

	m.mu.Lock()
	h, ok := m.handles[id]
	m.mu.Unlock()
	if !ok {
		return nil, errUnknownHandle
	}

	switch r.Method {
	case "info":
		return map[string]any{
			"peer_id": h.node.ID().String(),
			"addrs":   h.node.Addrs(),
		}, nil

	case "publish_keys":
		if err := h.node.PublishKeys(); err != nil {
			return nil, err
		}
		return map[string]any{}, nil

	case "serve_relay":
		if err := h.node.ServeRelay(); err != nil {
			return nil, err
		}
		return map[string]any{}, nil

	case "send", "send_direct":
		var args struct {
			Dest       string `json:"dest"`
			ContentB64 string `json:"content_b64"`
		}
		if err := json.Unmarshal(r.Args, &args); err != nil {
			return nil, fmt.Errorf("bridge: %s args: %w", r.Method, err)
		}
		dest, err := peer.Decode(args.Dest)
		if err != nil {
			return nil, fmt.Errorf("bridge: dest: %w", err)
		}
		content, err := base64.StdEncoding.DecodeString(args.ContentB64)
		if err != nil {
			return nil, fmt.Errorf("bridge: content_b64: %w", err)
		}
		if r.Method == "send" {
			err = h.node.Send(dest, content)
		} else {
			err = h.node.SendDirect(dest, content)
		}
		if err != nil {
			return nil, err
		}
		return map[string]any{}, nil

	case "wait_flushed":
		var args struct {
			TimeoutMs int `json:"timeout_ms"`
		}
		if err := json.Unmarshal(r.Args, &args); err != nil {
			return nil, fmt.Errorf("bridge: wait_flushed args: %w", err)
		}
		timeout := time.Duration(args.TimeoutMs) * time.Millisecond
		if timeout <= 0 {
			timeout = 30 * time.Second
		}
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		if err := h.node.WaitFlushed(ctx); err != nil {
			return nil, err
		}
		return map[string]any{}, nil

	case "poll":
		events, dropped := h.events.drain()
		return map[string]any{"events": events, "dropped": dropped}, nil

	case "list_peers":
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		peers, err := h.node.ListPeers(ctx)
		if err != nil {
			return nil, err
		}
		list := make([]map[string]any, 0, len(peers))
		for _, p := range peers {
			list = append(list, map[string]any{
				"peer_id": p.ID.String(),
				"addrs":   p.Addrs,
			})
		}
		return map[string]any{"peers": list}, nil

	default:
		return nil, fmt.Errorf("bridge: unknown method %q", r.Method)
	}
}

// Stop closes a node and retires its handle.
func (m *Manager) Stop(id int64) error {
	m.mu.Lock()
	h, ok := m.handles[id]
	delete(m.handles, id)
	m.mu.Unlock()
	if !ok {
		return errUnknownHandle
	}
	return h.node.Close()
}

// Envelope wraps a result or error into the wire JSON every FFI call returns:
// {"ok":true,"result":...} or {"ok":false,"error":"..."}.
func Envelope(result map[string]any, err error) []byte {
	if err != nil {
		b, _ := json.Marshal(map[string]any{"ok": false, "error": err.Error()})
		return b
	}
	b, _ := json.Marshal(map[string]any{"ok": true, "result": result})
	return b
}
