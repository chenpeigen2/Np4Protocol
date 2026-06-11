# libp2p Integration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace custom TCP transport, X25519 key exchange, ChaCha20 encryption, and bootstrap server with go-libp2p's built-in equivalents, while keeping MixEngine and MessageBus as application-layer components.

**Architecture:** libp2p Host replaces the custom transport/crypto/router/bootstrap stack. The Node struct wraps a libp2p Host and uses streams for communication. Noise security provides X25519+ChaCha20 transparently. Peer discovery uses mDNS (local) or DHT (global). MixEngine and MessageBus remain unchanged as application logic.

**Tech Stack:** Go 1.26, go-libp2p v0.40+, go-libp2p-kad-dht

---

## File Structure

```
go/pkg/p2p/
├── host.go            # Create: libp2p Host wrapper with Node identity
├── host_test.go       # Create: host creation and connection tests
├── discovery.go       # Create: mDNS and DHT peer discovery
├── discovery_test.go  # Create: discovery tests
├── stream.go          # Create: stream read/write helpers with length-prefix framing
└── stream_test.go     # Create: stream helper tests

go/pkg/np4/
├── node.go            # Modify: refactor to use libp2p Host
└── node_test.go       # Modify: update tests for libp2p

go/cmd/np4d/main.go    # Modify: use libp2p host
go/cmd/np4cli/main.go  # Modify: use libp2p host
go/cmd/bootstrap/main.go # Delete: no longer needed (libp2p has built-in discovery)

go/go.mod              # Modify: add libp2p dependencies
```

---

## Task 1: Add go-libp2p Dependency

**Files:**
- Modify: `go/go.mod`

- [ ] **Step 1: Add libp2p dependency**

```bash
cd /Users/chenpeigen/GolandProjects/Np4Protocol/go
go get github.com/libp2p/go-libp2p@latest
go get github.com/libp2p/go-libp2p-kad-dht@latest
go get github.com/multiformats/go-multiaddr@latest
```

- [ ] **Step 2: Verify dependency resolution**

```bash
go mod tidy
go build ./...
```

Expected: Build succeeds (may have unused import warnings, that's OK).

- [ ] **Step 3: Commit**

```bash
git add go.mod go.sum
git commit -m "deps: add go-libp2p and go-libp2p-kad-dht"
```

---

## Task 2: Create libp2p Host Wrapper

**Files:**
- Create: `go/pkg/p2p/host.go`
- Create: `go/pkg/p2p/host_test.go`

- [ ] **Step 1: Write failing test**

Create `go/pkg/p2p/host_test.go`:

```go
package p2p

import (
	"context"
	"testing"

	"github.com/libp2p/go-libp2p/core/peer"
)

func TestNewHost(t *testing.T) {
	h, err := NewHost(0) // random port
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()

	if h.ID() == "" {
		t.Error("expected non-empty peer ID")
	}
	if len(h.Addrs()) == 0 {
		t.Error("expected at least one address")
	}
}

func TestTwoHostsConnect(t *testing.T) {
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

	// h1 connects to h2
	info := peer.AddrInfo{
		ID:    h2.ID(),
		Addrs: h2.Addrs(),
	}
	err = h1.Connect(context.Background(), info)
	if err != nil {
		t.Fatal(err)
	}

	// Verify h2 sees h1 in its peerstore
	if h2.Peerstore().PeerInfo(h1.ID()).ID == "" {
		t.Error("h2 should know about h1")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /Users/chenpeigen/GolandProjects/Np4Protocol/go
go test ./pkg/p2p/ -v -run TestNewHost
```

Expected: FAIL - package does not exist.

- [ ] **Step 3: Implement host wrapper**

Create `go/pkg/p2p/host.go`:

```go
package p2p

import (
	"fmt"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/security/noise"
	"github.com/libp2p/go-libp2p/p2p/transport/tcp"
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
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./pkg/p2p/ -v -run TestNewHost
go test ./pkg/p2p/ -v -run TestTwoHostsConnect
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add go/pkg/p2p/
git commit -m "feat: add libp2p host wrapper with Noise security"
```

---

## Task 3: Create Stream Helpers with Length-Prefix Framing

**Files:**
- Create: `go/pkg/p2p/stream.go`
- Create: `go/pkg/p2p/stream_test.go`

- [ ] **Step 1: Write failing test**

Create `go/pkg/p2p/stream_test.go`:

```go
package p2p

import (
	"context"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/protocol"
)

const TestProtocol = protocol.ID("/np4/test/1.0.0")

func TestStreamReadWrite(t *testing.T) {
	h1, _ := NewHost(0)
	defer h1.Close()
	h2, _ := NewHost(0)
	defer h2.Close()

	// h2 registers handler
	received := make(chan []byte, 1)
	h2.SetStreamHandler(TestProtocol, func(s network.Stream) {
		defer s.Close()
		data, err := ReadMsg(s)
		if err != nil {
			return
		}
		received <- data
	})

	// Connect h1 -> h2
	connectHosts(t, h1, h2)

	// h1 opens stream and writes
	s, err := h1.NewStream(context.Background(), h2.ID(), TestProtocol)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	err = WriteMsg(s, []byte("hello libp2p"))
	if err != nil {
		t.Fatal(err)
	}
	s.CloseWrite()

	select {
	case data := <-received:
		if string(data) != "hello libp2p" {
			t.Errorf("expected 'hello libp2p', got '%s'", string(data))
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for message")
	}
}

func TestStreamRequestResponse(t *testing.T) {
	h1, _ := NewHost(0)
	defer h1.Close()
	h2, _ := NewHost(0)
	defer h2.Close()

	// h2 echoes back with prefix
	h2.SetStreamHandler(TestProtocol, func(s network.Stream) {
		defer s.Close()
		data, err := ReadMsg(s)
		if err != nil {
			return
		}
		WriteMsg(s, append([]byte("echo: "), data...))
	})

	connectHosts(t, h1, h2)

	s, err := h1.NewStream(context.Background(), h2.ID(), TestProtocol)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	WriteMsg(s, []byte("ping"))
	s.CloseWrite()

	resp, err := ReadMsg(s)
	if err != nil {
		t.Fatal(err)
	}
	if string(resp) != "echo: ping" {
		t.Errorf("expected 'echo: ping', got '%s'", string(resp))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./pkg/p2p/ -v -run TestStreamReadWrite
```

Expected: FAIL - ReadMsg/WriteMsg not defined.

- [ ] **Step 3: Implement stream helpers**

Create `go/pkg/p2p/stream.go`:

```go
package p2p

import (
	"encoding/binary"
	"errors"
	"io"

	"github.com/libp2p/go-libp2p/core/network"
)

const MaxMessageSize = 1 * 1024 * 1024 // 1 MB

// WriteMsg writes a length-prefixed message to a stream.
func WriteMsg(s network.Stream, data []byte) error {
	if len(data) > MaxMessageSize {
		return errors.New("message too large")
	}
	var buf [4]byte
	binary.BigEndian.PutUint32(buf[:], uint32(len(data)))
	if _, err := s.Write(buf[:]); err != nil {
		return err
	}
	_, err := s.Write(data)
	return err
}

// ReadMsg reads a length-prefixed message from a stream.
func ReadMsg(s network.Stream) ([]byte, error) {
	var buf [4]byte
	if _, err := io.ReadFull(s, buf[:]); err != nil {
		return nil, err
	}
	length := binary.BigEndian.Uint32(buf[:])
	if length > MaxMessageSize {
		return nil, errors.New("message too large")
	}
	data := make([]byte, length)
	_, err := io.ReadFull(s, data)
	return data, err
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./pkg/p2p/ -v -run TestStreamReadWrite
go test ./pkg/p2p/ -v -run TestStreamRequestResponse
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add go/pkg/p2p/
git commit -m "feat: add length-prefixed stream read/write helpers"
```

---

## Task 4: Create Peer Discovery (mDNS + DHT)

**Files:**
- Create: `go/pkg/p2p/discovery.go`
- Create: `go/pkg/p2p/discovery_test.go`

- [ ] **Step 1: Write failing test**

Create `go/pkg/p2p/discovery_test.go`:

```go
package p2p

import (
	"context"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
)

func TestMDNSDiscovery(t *testing.T) {
	h1, _ := NewHost(0)
	defer h1.Close()
	h2, _ := NewHost(0)
	defer h2.Close()

	found := make(chan peer.ID, 1)

	notifee1 := &discoveryNotifee{h: h1, found: found}
	StartMDNS(h1, "np4-test", notifee1)

	notifee2 := &discoveryNotifee{h: h2, found: make(chan peer.ID, 1)}
	StartMDNS(h2, "np4-test", notifee2)

	select {
	case pid := <-found:
		if pid != h2.ID() {
			t.Errorf("expected to find h2, got %s", pid)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("mDNS discovery timeout")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./pkg/p2p/ -v -run TestMDNSDiscovery
```

Expected: FAIL - StartMDNS not defined.

- [ ] **Step 3: Implement mDNS discovery**

Create `go/pkg/p2p/discovery.go`:

```go
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
	h     host.Host
	found chan peer.ID
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
func StartMDNS(h host.Host, serviceTag string, notifee *discoveryNotifee) {
	_ = mdns.NewMdnsService(h, serviceTag, notifee)
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./pkg/p2p/ -v -run TestMDNSDiscovery
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add go/pkg/p2p/
git commit -m "feat: add mDNS peer discovery"
```

---

## Task 5: Refactor Node to Use libp2p Host

**Files:**
- Modify: `go/pkg/np4/node.go`
- Modify: `go/pkg/np4/node_test.go`

This is the core task. The Node struct gets refactored to wrap a libp2p Host instead of using custom TCP transport. The key changes:

- `Node.transport` -> `Node.host` (libp2p Host)
- `Node.publicKey/privateKey` -> removed (libp2p handles identity)
- `Node.peerKeys` -> removed (libp2p Noise handles key exchange)
- `Node.approvalHandler` -> kept for connect approval
- `acceptLoop` -> replaced by `SetStreamHandler`
- `handleConn` -> replaced by protocol-specific stream handlers

- [ ] **Step 1: Write failing test**

Add to `go/pkg/np4/node_test.go`:

```go
func TestNodeLibp2pSend(t *testing.T) {
	nodeA, err := NewNode(0)
	if err != nil {
		t.Fatal(err)
	}
	defer nodeA.Stop()

	nodeB, err := NewNode(0)
	if err != nil {
		t.Fatal(err)
	}
	defer nodeB.Stop()

	// Connect A -> B
	err = nodeA.Connect(nodeB.Host().Addrs(), nodeB.Host().ID())
	if err != nil {
		t.Fatal(err)
	}

	// Register message handler on B
	received := make(chan string, 1)
	nodeB.OnMessage(func(msg *message.Message) {
		received <- string(msg.Content)
	})

	// A sends to B
	err = nodeA.Send(nodeB.Host().ID(), []byte("hello via libp2p"))
	if err != nil {
		t.Fatal(err)
	}

	select {
	case msg := <-received:
		if msg != "hello via libp2p" {
			t.Errorf("expected 'hello via libp2p', got '%s'", msg)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./pkg/np4/ -v -run TestNodeLibp2pSend
```

Expected: FAIL - NewNode signature changed, Connect/Host methods don't exist.

- [ ] **Step 3: Refactor Node struct**

Replace `go/pkg/np4/node.go` with:

```go
package np4

import (
	"Np4Protocol/go/pkg/message"
	"Np4Protocol/go/pkg/mix"
	"Np4Protocol/go/pkg/p2p"
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
)

const Np4MessageProtocol = protocol.ID("/np4/message/1.0.0")

type Node struct {
	host       host.Host
	bus        *message.MessageBus
	mixEngine  *mix.MixEngine[message.Message]
	stopCh     chan struct{}
	stopOnce   sync.Once
}

func NewNode(port int) (*Node, error) {
	h, err := p2p.NewHost(port)
	if err != nil {
		return nil, err
	}

	n := &Node{
		host:   h,
		bus:    message.NewMessageBus(),
		stopCh: make(chan struct{}),
	}

	n.mixEngine = mix.NewMixEngine[message.Message](10, 500*time.Millisecond, n.sendBatch)

	// Register stream handler for incoming messages
	h.SetStreamHandler(Np4MessageProtocol, n.handleStream)

	return n, nil
}

func (n *Node) ID() peer.ID {
	return n.host.ID()
}

func (n *Node) Host() host.Host {
	return n.host
}

func (n *Node) Addrs() []string {
	addrs := n.host.Addrs()
	result := make([]string, len(addrs))
	for i, a := range addrs {
		result[i] = a.String() + "/p2p/" + n.host.ID().String()
	}
	return result
}

func (n *Node) OnMessage(handler func(*message.Message)) {
	n.bus.OnMessage(handler)
}

func (n *Node) Connect(addrs []multiaddr.Multiaddr, pid peer.ID) error {
	info := peer.AddrInfo{ID: pid, Addrs: addrs}
	return n.host.Connect(context.Background(), info)
}

func (n *Node) Send(destID peer.ID, content []byte) error {
	s, err := n.host.NewStream(context.Background(), destID, Np4MessageProtocol)
	if err != nil {
		return err
	}
	defer s.Close()

	msg := &message.Message{
		Type:     message.TypeAsync,
		SenderID: n.host.ID().String(),
		Content:  content,
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	return p2p.WriteMsg(s, data)
}

func (n *Node) Stop() {
	n.stopOnce.Do(func() {
		close(n.stopCh)
		n.host.Close()
	})
}

func (n *Node) handleStream(s network.Stream) {
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

func (n *Node) sendBatch(batch []*message.Message) {
	for _, msg := range batch {
		n.bus.Send(msg)
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./pkg/np4/ -v -run TestNodeLibp2pSend
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add go/pkg/np4/
git commit -m "refactor: Node uses libp2p Host instead of custom TCP"
```

---

## Task 6: Update Node Tests

**Files:**
- Modify: `go/pkg/np4/node_test.go`

- [ ] **Step 1: Update existing tests**

Replace all tests in `go/pkg/np4/node_test.go` to use the new Node API:

```go
package np4

import (
	"Np4Protocol/go/pkg/message"
	"sync"
	"testing"
	"time"
)

func TestNodeSendReceive(t *testing.T) {
	nodeA, err := NewNode(0)
	if err != nil {
		t.Fatal(err)
	}
	defer nodeA.Stop()

	nodeB, err := NewNode(0)
	if err != nil {
		t.Fatal(err)
	}
	defer nodeB.Stop()

	// Connect A -> B
	err = nodeA.Connect(nodeB.Host().Addrs(), nodeB.ID())
	if err != nil {
		t.Fatal(err)
	}

	var received []byte
	var mu sync.Mutex
	nodeB.OnMessage(func(msg *message.Message) {
		mu.Lock()
		received = msg.Content
		mu.Unlock()
	})

	err = nodeA.Send(nodeB.ID(), []byte("hello"))
	if err != nil {
		t.Fatal(err)
	}

	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	if string(received) != "hello" {
		t.Errorf("expected 'hello', got '%s'", string(received))
	}
	mu.Unlock()
}

func TestNodeBidirectional(t *testing.T) {
	nodeA, _ := NewNode(0)
	defer nodeA.Stop()
	nodeB, _ := NewNode(0)
	defer nodeB.Stop()

	nodeA.Connect(nodeB.Host().Addrs(), nodeB.ID())

	var receivedA, receivedB []byte
	var mu sync.Mutex

	nodeA.OnMessage(func(msg *message.Message) {
		mu.Lock()
		receivedA = msg.Content
		mu.Unlock()
	})
	nodeB.OnMessage(func(msg *message.Message) {
		mu.Lock()
		receivedB = msg.Content
		mu.Unlock()
	})

	nodeA.Send(nodeB.ID(), []byte("A->B"))
	nodeB.Send(nodeA.ID(), []byte("B->A"))

	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	if string(receivedB) != "A->B" {
		t.Errorf("B expected 'A->B', got '%s'", string(receivedB))
	}
	if string(receivedA) != "B->A" {
		t.Errorf("A expected 'B->A', got '%s'", string(receivedA))
	}
	mu.Unlock()
}
```

- [ ] **Step 2: Run tests**

```bash
go test ./pkg/np4/ -v
```

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add go/pkg/np4/
git commit -m "test: update node tests for libp2p"
```

---

## Task 7: Update CLI Tools

**Files:**
- Modify: `go/cmd/np4d/main.go`
- Modify: `go/cmd/np4cli/main.go`
- Delete: `go/cmd/bootstrap/main.go` (no longer needed)

- [ ] **Step 1: Update np4d**

Replace `go/cmd/np4d/main.go`:

```go
package main

import (
	"Np4Protocol/go/pkg/np4"
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	port := 4001
	if len(os.Args) > 1 {
		fmt.Sscanf(os.Args[1], "%d", &port)
	}

	node, err := NewNode(port)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create node: %v\n", err)
		os.Exit(1)
	}
	defer node.Stop()

	fmt.Printf("Node ID: %s\n", node.ID())
	fmt.Printf("Listening on: %v\n", node.Addrs())

	node.OnMessage(func(msg *message.Message) {
		fmt.Printf("Received from %s: %s\n", msg.SenderID, string(msg.Content))
	})

	// Wait for interrupt
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	<-ch
	fmt.Println("\nShutting down...")
}
```

- [ ] **Step 2: Update np4cli**

Replace `go/cmd/np4cli/main.go`:

```go
package main

import (
	"Np4Protocol/go/pkg/np4"
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: np4cli <listen-port>")
		os.Exit(1)
	}

	port := 0
	fmt.Sscanf(os.Args[1], "%d", &port)

	node, err := NewNode(port)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create node: %v\n", err)
		os.Exit(1)
	}
	defer node.Stop()

	fmt.Printf("Node ID: %s\n", node.ID())
	fmt.Printf("Addresses: %v\n", node.Addrs())

	node.OnMessage(func(msg *message.Message) {
		fmt.Printf("\n[%s]: %s\n> ", msg.SenderID, string(msg.Content))
	})

	scanner := bufio.NewScanner(os.Stdin)
	fmt.Print("> ")
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			fmt.Print("> ")
			continue
		}

		if strings.HasPrefix(line, "connect ") {
			addr := strings.TrimPrefix(line, "connect ")
			maddr, err := multiaddr.NewMultiaddr(addr)
			if err != nil {
				fmt.Printf("invalid address: %v\n", err)
				fmt.Print("> ")
				continue
			}
			info, err := peer.AddrInfoFromP2pAddr(maddr)
			if err != nil {
				fmt.Printf("invalid peer info: %v\n", err)
				fmt.Print("> ")
				continue
			}
			if err := node.Connect(info.Addrs, info.ID); err != nil {
				fmt.Printf("connect failed: %v\n", err)
			} else {
				fmt.Printf("connected to %s\n", info.ID)
			}
		} else if strings.HasPrefix(line, "send ") {
			parts := strings.SplitN(strings.TrimPrefix(line, "send "), " ", 2)
			if len(parts) != 2 {
				fmt.Println("Usage: send <peer-id> <message>")
				fmt.Print("> ")
				continue
			}
			pid, err := peer.Decode(parts[0])
			if err != nil {
				fmt.Printf("invalid peer ID: %v\n", err)
				fmt.Print("> ")
				continue
			}
			if err := node.Send(pid, []byte(parts[1])); err != nil {
				fmt.Printf("send failed: %v\n", err)
			}
		} else {
			fmt.Println("Commands: connect <multiaddr>, send <peer-id> <message>")
		}
		fmt.Print("> ")
	}
}
```

- [ ] **Step 3: Remove bootstrap CLI**

```bash
rm -rf go/cmd/bootstrap/
```

- [ ] **Step 4: Build CLI tools**

```bash
cd /Users/chenpeigen/GolandProjects/Np4Protocol/go
go build -o bin/np4d ./cmd/np4d/
go build -o bin/np4cli ./cmd/np4cli/
```

Expected: Both build successfully.

- [ ] **Step 5: Commit**

```bash
git add go/cmd/
git commit -m "refactor: update CLI tools for libp2p, remove bootstrap CLI"
```

---

## Task 8: Clean Up Unused Packages

**Files:**
- Delete: `go/pkg/transport/` (replaced by libp2p)
- Delete: `go/pkg/crypto/` (replaced by libp2p Noise)
- Delete: `go/pkg/router/` (replaced by libp2p PeerStore)
- Delete: `go/pkg/bootstrap/` (replaced by libp2p discovery)

- [ ] **Step 1: Remove unused packages**

```bash
rm -rf go/pkg/transport/ go/pkg/crypto/ go/pkg/router/ go/pkg/bootstrap/
```

- [ ] **Step 2: Update go.mod**

```bash
cd /Users/chenpeigen/GolandProjects/Np4Protocol/go
go mod tidy
```

- [ ] **Step 3: Verify full build and tests**

```bash
go build ./...
go test ./...
```

Expected: All packages build, all tests pass.

- [ ] **Step 4: Update CLAUDE.md**

Update the architecture section in `CLAUDE.md` to reflect the new libp2p-based architecture.

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "chore: remove custom transport/crypto/router/bootstrap, use libp2p"
```

---

## Task 9: Integration Test - Full P2P Flow

**Files:**
- Modify: `go/pkg/np4/node_test.go`

- [ ] **Step 1: Write integration test**

Add to `go/pkg/np4/node_test.go`:

```go
func TestFullP2PFlow(t *testing.T) {
	// Create 3 nodes
	nodeA, _ := NewNode(0)
	defer nodeA.Stop()
	nodeB, _ := NewNode(0)
	defer nodeB.Stop()
	nodeC, _ := NewNode(0)
	defer nodeC.Stop()

	// A connects to B and C
	nodeA.Connect(nodeB.Host().Addrs(), nodeB.ID())
	nodeA.Connect(nodeC.Host().Addrs(), nodeC.ID())

	// B connects to C
	nodeB.Connect(nodeC.Host().Addrs(), nodeC.ID())

	var receivedB, receivedC []byte
	var mu sync.Mutex

	nodeB.OnMessage(func(msg *message.Message) {
		mu.Lock()
		receivedB = msg.Content
		mu.Unlock()
	})
	nodeC.OnMessage(func(msg *message.Message) {
		mu.Lock()
		receivedC = msg.Content
		mu.Unlock()
	})

	// A broadcasts to B and C
	nodeA.Send(nodeB.ID(), []byte("hello B"))
	nodeA.Send(nodeC.ID(), []byte("hello C"))

	time.Sleep(300 * time.Millisecond)

	mu.Lock()
	if string(receivedB) != "hello B" {
		t.Errorf("B expected 'hello B', got '%s'", string(receivedB))
	}
	if string(receivedC) != "hello C" {
		t.Errorf("C expected 'hello C', got '%s'", string(receivedC))
	}
	mu.Unlock()
}
```

- [ ] **Step 2: Run integration test**

```bash
go test ./pkg/np4/ -v -run TestFullP2PFlow
```

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add go/pkg/np4/
git commit -m "test: add full P2P integration test with 3 nodes"
```

---

## Self-Review Checklist

- [ ] libp2p Host created with Noise security (X25519 + ChaCha20-Poly1305)
- [ ] Length-prefixed stream framing works (4-byte header + payload)
- [ ] mDNS discovery finds peers on local network
- [ ] Node wraps libp2p Host, uses streams for messaging
- [ ] Message dispatch uses SetStreamHandler instead of acceptLoop
- [ ] MixEngine and MessageBus unchanged
- [ ] CLI tools updated for libp2p
- [ ] Custom transport/crypto/router/bootstrap packages removed
- [ ] All tests pass
- [ ] CLAUDE.md updated
