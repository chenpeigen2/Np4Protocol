# Np4Protocol Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement a Mixnet-based anonymous communication protocol with metadata protection in Go

**Architecture:** Four-layer protocol stack (Transport → Crypto → Mix → Application) using Protobuf for message serialization and X25519+ChaCha20-Poly1305 for encryption

**Tech Stack:** Go 1.26, Protobuf, golang.org/x/crypto (X25519, ChaCha20-Poly1305, Ed25519)

---

## File Structure

```
Np4Protocol/
├── proto/
│   └── np4.proto                    # Protocol buffer definitions
├── go/
│   ├── go.mod                       # Go module definition
│   ├── pkg/
│   │   ├── transport/
│   │   │   ├── transport.go         # Transport interface
│   │   │   ├── tcp.go               # TCP implementation
│   │   │   └── tcp_test.go          # TCP tests
│   │   ├── crypto/
│   │   │   ├── crypto.go            # Crypto interface
│   │   │   ├── x25519.go            # X25519 key exchange
│   │   │   ├── chacha20.go          # ChaCha20-Poly1305 encryption
│   │   │   └── crypto_test.go       # Crypto tests
│   │   ├── mix/
│   │   │   ├── engine.go            # MixEngine implementation
│   │   │   └── engine_test.go       # MixEngine tests
│   │   ├── router/
│   │   │   ├── router.go            # Router implementation
│   │   │   └── router_test.go       # Router tests
│   │   └── message/
│   │       ├── bus.go               # MessageBus implementation
│   │       └── bus_test.go          # MessageBus tests
│   └── cmd/
│       ├── np4d/
│       │   └── main.go              # MixNode daemon
│       └── np4cli/
│           └── main.go              # Client CLI tool
└── docs/
    └── protocol.md                  # Protocol specification
```

---

## Task 1: Project Setup and Protobuf Definition

**Files:**
- Create: `proto/np4.proto`
- Create: `go/go.mod`

- [ ] **Step 1: Create Protobuf message definitions**

```protobuf
syntax = "proto3";
package np4;

option go_package = "Np4Protocol/go/pkg/proto";

message Envelope {
  bytes  payload    = 1;
  bytes  signature  = 2;
  Header header     = 3;
}

message Header {
  MessageType type       = 1;
  bytes       dest_id    = 2;
  bytes       sender_id  = 3;
  uint64      timestamp  = 4;
  uint32      ttl        = 5;
  uint32      version    = 6;
}

enum MessageType {
  ASYNC_MSG      = 0;
  SYNC_REQUEST   = 1;
  SYNC_RESPONSE  = 2;
  BROADCAST      = 3;
  FILE_CHUNK     = 4;
  MIX_CTRL       = 5;
  KEY_EXCHANGE   = 6;
  PEER_DISCOVERY = 7;
}

message Payload {
  bytes  content      = 1;
  bytes  session_key  = 2;
  uint32 hop_count    = 3;
  bytes  next_hop     = 4;
}

message MixBatch {
  uint64            batch_id   = 1;
  repeated Envelope messages   = 2;
  bytes             proof      = 3;
}

enum ErrorCode {
  OK                = 0;
  INVALID_FORMAT    = 1;
  DECRYPT_FAILED    = 2;
  KEY_MISMATCH      = 3;
  TTL_EXPIRED       = 4;
  BATCH_FULL        = 5;
  UNKNOWN_NODE      = 6;
}
```

- [ ] **Step 2: Initialize Go module**

```bash
cd /Users/chenpeigen/GolandProjects/Np4Protocol/go
go mod init Np4Protocol/go
go get google.golang.org/protobuf
```

- [ ] **Step 3: Generate Go code from Protobuf**

```bash
protoc --go_out=. --go_opt=paths=source_relative proto/np4.proto
```

- [ ] **Step 4: Verify generated code**

```bash
ls -la go/pkg/proto/
```

Expected: `np4.pb.go` file exists

- [ ] **Step 5: Commit**

```bash
git add proto/np4.proto go/
git commit -m "feat: add protobuf definitions and go module setup"
```

---

## Task 2: Transport Layer - Interface and TCP Implementation

**Files:**
- Create: `go/pkg/transport/transport.go`
- Create: `go/pkg/transport/tcp.go`
- Create: `go/pkg/transport/tcp_test.go`

- [ ] **Step 1: Write Transport interface**

```go
package transport

import "net"

type Conn interface {
    Read() ([]byte, error)
    Write(data []byte) error
    Close() error
    RemoteAddr() net.Addr
}

type Listener interface {
    Accept() (Conn, error)
    Close() error
    Addr() net.Addr
}

type Transport interface {
    Connect(addr string) (Conn, error)
    Listen(addr string) (Listener, error)
}
```

- [ ] **Step 2: Write failing test for TCP connection**

```go
package transport

import (
    "testing"
    "sync"
)

func TestTCPConnectAndListen(t *testing.T) {
    tcp := NewTCPTransport()

    listener, err := tcp.Listen("127.0.0.1:0")
    if err != nil {
        t.Fatal(err)
    }
    defer listener.Close()

    addr := listener.Addr().String()

    var wg sync.WaitGroup
    wg.Add(1)

    go func() {
        defer wg.Done()
        conn, err := listener.Accept()
        if err != nil {
            t.Error(err)
            return
        }
        defer conn.Close()

        data, err := conn.Read()
        if err != nil {
            t.Error(err)
            return
        }

        if string(data) != "hello" {
            t.Errorf("expected 'hello', got '%s'", string(data))
        }
    }()

    client, err := tcp.Connect(addr)
    if err != nil {
        t.Fatal(err)
    }
    defer client.Close()

    err = client.Write([]byte("hello"))
    if err != nil {
        t.Fatal(err)
    }

    wg.Wait()
}
```

- [ ] **Step 3: Run test to verify it fails**

```bash
cd /Users/chenpeigen/GolandProjects/Np4Protocol/go
go test ./pkg/transport/ -v -run TestTCPConnectAndListen
```

Expected: FAIL with "undefined: NewTCPTransport"

- [ ] **Step 4: Implement TCP transport**

```go
package transport

import (
    "encoding/binary"
    "io"
    "net"
)

type TCPConn struct {
    conn net.Conn
}

func (c *TCPConn) Read() ([]byte, error) {
    var len uint32
    err := binary.Read(c.conn, binary.BigEndian, &len)
    if err != nil {
        return nil, err
    }

    data := make([]byte, len)
    _, err = io.ReadFull(c.conn, data)
    return data, err
}

func (c *TCPConn) Write(data []byte) error {
    err := binary.Write(c.conn, binary.BigEndian, uint32(len(data)))
    if err != nil {
        return err
    }
    _, err = c.conn.Write(data)
    return err
}

func (c *TCPConn) Close() error {
    return c.conn.Close()
}

func (c *TCPConn) RemoteAddr() net.Addr {
    return c.conn.RemoteAddr()
}

type TCPListener struct {
    listener net.Listener
}

func (l *TCPListener) Accept() (Conn, error) {
    conn, err := l.listener.Accept()
    if err != nil {
        return nil, err
    }
    return &TCPConn{conn: conn}, nil
}

func (l *TCPListener) Close() error {
    return l.listener.Close()
}

func (l *TCPListener) Addr() net.Addr {
    return l.listener.Addr()
}

type TCPTransport struct{}

func NewTCPTransport() *TCPTransport {
    return &TCPTransport{}
}

func (t *TCPTransport) Connect(addr string) (Conn, error) {
    conn, err := net.Dial("tcp", addr)
    if err != nil {
        return nil, err
    }
    return &TCPConn{conn: conn}, nil
}

func (t *TCPTransport) Listen(addr string) (Listener, error) {
    listener, err := net.Listen("tcp", addr)
    if err != nil {
        return nil, err
    }
    return &TCPListener{listener: listener}, nil
}
```

- [ ] **Step 5: Run test to verify it passes**

```bash
go test ./pkg/transport/ -v -run TestTCPConnectAndListen
```

Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add go/pkg/transport/
git commit -m "feat: implement TCP transport layer"
```

---

## Task 3: Crypto Layer - X25519 Key Exchange

**Files:**
- Create: `go/pkg/crypto/crypto.go`
- Create: `go/pkg/crypto/x25519.go`
- Create: `go/pkg/crypto/crypto_test.go`

- [ ] **Step 1: Write Crypto interface**

```go
package crypto

type KeyExchange interface {
    GenerateKeyPair() (public, private []byte, err error)
    ComputeSharedSecret(localPrivate, remotePublic []byte) ([]byte, error)
}

type Encryptor interface {
    Encrypt(plaintext, key []byte) ([]byte, error)
    Decrypt(ciphertext, key []byte) ([]byte, error)
}
```

- [ ] **Step 2: Write failing test for X25519**

```go
package crypto

import (
    "bytes"
    "testing"
)

func TestX25519KeyExchange(t *testing.T) {
    x25519 := NewX25519KeyExchange()

    // Generate Alice's key pair
    alicePub, alicePriv, err := x25519.GenerateKeyPair()
    if err != nil {
        t.Fatal(err)
    }

    // Generate Bob's key pair
    bobPub, bobPriv, err := x25519.GenerateKeyPair()
    if err != nil {
        t.Fatal(err)
    }

    // Alice computes shared secret
    aliceShared, err := x25519.ComputeSharedSecret(alicePriv, bobPub)
    if err != nil {
        t.Fatal(err)
    }

    // Bob computes shared secret
    bobShared, err := x25519.ComputeSharedSecret(bobPriv, alicePub)
    if err != nil {
        t.Fatal(err)
    }

    // Shared secrets should match
    if !bytes.Equal(aliceShared, bobShared) {
        t.Error("shared secrets do not match")
    }
}
```

- [ ] **Step 3: Run test to verify it fails**

```bash
go test ./pkg/crypto/ -v -run TestX25519KeyExchange
```

Expected: FAIL with "undefined: NewX25519KeyExchange"

- [ ] **Step 4: Implement X25519 key exchange**

```go
package crypto

import (
    "crypto/rand"
    "errors"

    "golang.org/x/crypto/curve25519"
)

type X25519KeyExchange struct{}

func NewX25519KeyExchange() *X25519KeyExchange {
    return &X25519KeyExchange{}
}

func (x *X25519KeyExchange) GenerateKeyPair() (public, private []byte, err error) {
    private = make([]byte, 32)
    _, err = rand.Read(private)
    if err != nil {
        return nil, nil, err
    }

    public, err = curve25519.X25519(private, curve25519.Basepoint)
    if err != nil {
        return nil, nil, err
    }

    return public, private, nil
}

func (x *X25519KeyExchange) ComputeSharedSecret(localPrivate, remotePublic []byte) ([]byte, error) {
    if len(localPrivate) != 32 || len(remotePublic) != 32 {
        return nil, errors.New("invalid key length")
    }

    shared, err := curve25519.X25519(localPrivate, remotePublic)
    if err != nil {
        return nil, err
    }

    return shared, nil
}
```

- [ ] **Step 5: Run test to verify it passes**

```bash
go test ./pkg/crypto/ -v -run TestX25519KeyExchange
```

Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add go/pkg/crypto/
git commit -m "feat: implement X25519 key exchange"
```

---

## Task 4: Crypto Layer - ChaCha20-Poly1305 Encryption

**Files:**
- Modify: `go/pkg/crypto/chacha20.go`
- Modify: `go/pkg/crypto/crypto_test.go`

- [ ] **Step 1: Write failing test for ChaCha20-Poly1305**

Add to `crypto_test.go`:

```go
func TestChaCha20Poly1305EncryptDecrypt(t *testing.T) {
    encryptor := NewChaCha20Encryptor()

    key := make([]byte, 32)
    rand.Read(key)

    plaintext := []byte("Hello, Np4Protocol!")

    ciphertext, err := encryptor.Encrypt(plaintext, key)
    if err != nil {
        t.Fatal(err)
    }

    // Ciphertext should be different from plaintext
    if bytes.Equal(ciphertext, plaintext) {
        t.Error("ciphertext should differ from plaintext")
    }

    decrypted, err := encryptor.Decrypt(ciphertext, key)
    if err != nil {
        t.Fatal(err)
    }

    if !bytes.Equal(decrypted, plaintext) {
        t.Error("decrypted text should match original")
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./pkg/crypto/ -v -run TestChaCha20Poly1305EncryptDecrypt
```

Expected: FAIL with "undefined: NewChaCha20Encryptor"

- [ ] **Step 3: Implement ChaCha20-Poly1305**

```go
package crypto

import (
    "crypto/cipher"
    "crypto/rand"
    "errors"

    "golang.org/x/crypto/chacha20poly1305"
)

type ChaCha20Encryptor struct{}

func NewChaCha20Encryptor() *ChaCha20Encryptor {
    return &ChaCha20Encryptor{}
}

func (c *ChaCha20Encryptor) Encrypt(plaintext, key []byte) ([]byte, error) {
    if len(key) != 32 {
        return nil, errors.New("key must be 32 bytes")
    }

    aead, err := chacha20poly1305.New(key)
    if err != nil {
        return nil, err
    }

    nonce := make([]byte, aead.NonceSize())
    _, err = rand.Read(nonce)
    if err != nil {
        return nil, err
    }

    ciphertext := aead.Seal(nonce, nonce, plaintext, nil)
    return ciphertext, nil
}

func (c *ChaCha20Encryptor) Decrypt(ciphertext, key []byte) ([]byte, error) {
    if len(key) != 32 {
        return nil, errors.New("key must be 32 bytes")
    }

    aead, err := chacha20poly1305.New(key)
    if err != nil {
        return nil, err
    }

    nonceSize := aead.NonceSize()
    if len(ciphertext) < nonceSize {
        return nil, errors.New("ciphertext too short")
    }

    nonce, ct := ciphertext[:nonceSize], ciphertext[nonceSize:]
    plaintext, err := aead.Open(nil, nonce, ct, nil)
    if err != nil {
        return nil, err
    }

    return plaintext, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./pkg/crypto/ -v -run TestChaCha20Poly1305EncryptDecrypt
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add go/pkg/crypto/
git commit -m "feat: implement ChaCha20-Poly1305 encryption"
```

---

## Task 5: Mix Engine - Core Implementation

**Files:**
- Create: `go/pkg/mix/engine.go`
- Create: `go/pkg/mix/engine_test.go`

- [ ] **Step 1: Write failing test for MixEngine**

```go
package mix

import (
    "testing"
    "time"
    "sync"
)

type MockEnvelope struct {
    ID int
}

func TestMixEngineBatchFlush(t *testing.T) {
    var received []*MockEnvelope
    var mu sync.Mutex

    engine := NewMixEngine(3, 1*time.Second, func(batch []*MockEnvelope) {
        mu.Lock()
        received = append(received, batch...)
        mu.Unlock()
    })

    // Add 3 messages to trigger batch flush
    engine.AddMessage(&MockEnvelope{ID: 1})
    engine.AddMessage(&MockEnvelope{ID: 2})
    engine.AddMessage(&MockEnvelope{ID: 3})

    // Wait for processing
    time.Sleep(100 * time.Millisecond)

    mu.Lock()
    if len(received) != 3 {
        t.Errorf("expected 3 messages, got %d", len(received))
    }
    mu.Unlock()
}

func TestMixEngineTimeout(t *testing.T) {
    var received []*MockEnvelope
    var mu sync.Mutex

    engine := NewMixEngine(10, 100*time.Millisecond, func(batch []*MockEnvelope) {
        mu.Lock()
        received = append(received, batch...)
        mu.Unlock()
    })

    // Add only 2 messages (less than batch size)
    engine.AddMessage(&MockEnvelope{ID: 1})
    engine.AddMessage(&MockEnvelope{ID: 2})

    // Wait for timeout
    time.Sleep(200 * time.Millisecond)

    mu.Lock()
    if len(received) != 2 {
        t.Errorf("expected 2 messages after timeout, got %d", len(received))
    }
    mu.Unlock()
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./pkg/mix/ -v
```

Expected: FAIL with "undefined: NewMixEngine"

- [ ] **Step 3: Implement MixEngine**

```go
package mix

import (
    "math/rand"
    "sync"
    "time"
)

type MixEngine[T any] struct {
    buffer    []*T
    batchSize int
    maxDelay  time.Duration
    onFlush   func([]*T)
    mu        sync.Mutex
    timer     *time.Timer
}

func NewMixEngine[T any](batchSize int, maxDelay time.Duration, onFlush func([]*T)) *MixEngine[T] {
    return &MixEngine[T]{
        batchSize: batchSize,
        maxDelay:  maxDelay,
        onFlush:   onFlush,
    }
}

func (m *MixEngine[T]) AddMessage(msg *T) {
    m.mu.Lock()
    defer m.mu.Unlock()

    m.buffer = append(m.buffer, msg)

    if len(m.buffer) >= m.batchSize {
        m.flush()
        return
    }

    if m.timer == nil {
        m.timer = time.AfterFunc(m.maxDelay, func() {
            m.mu.Lock()
            m.flush()
            m.mu.Unlock()
        })
    }
}

func (m *MixEngine[T]) flush() {
    if len(m.buffer) == 0 {
        return
    }

    if m.timer != nil {
        m.timer.Stop()
        m.timer = nil
    }

    // Fisher-Yates shuffle
    rand.Shuffle(len(m.buffer), func(i, j int) {
        m.buffer[i], m.buffer[j] = m.buffer[j], m.buffer[i]
    })

    batch := m.buffer
    m.buffer = nil

    go m.onFlush(batch)
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./pkg/mix/ -v
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add go/pkg/mix/
git commit -m "feat: implement mix engine with batch processing"
```

---

## Task 6: Router - Node Management

**Files:**
- Create: `go/pkg/router/router.go`
- Create: `go/pkg/router/router_test.go`

- [ ] **Step 1: Write failing test for Router**

```go
package router

import (
    "testing"
)

import (
    "fmt"
    "testing"
)

func TestRouterAddRemoveNode(t *testing.T) {
    router := NewRouter()

    node1 := &Node{ID: "node1", Addr: "192.168.1.1:8080"}
    node2 := &Node{ID: "node2", Addr: "192.168.1.2:8080"}

    router.AddNode(node1)
    router.AddNode(node2)

    if router.NodeCount() != 2 {
        t.Errorf("expected 2 nodes, got %d", router.NodeCount())
    }

    router.RemoveNode("node1")

    if router.NodeCount() != 1 {
        t.Errorf("expected 1 node after removal, got %d", router.NodeCount())
    }
}

func TestRouterSelectRandomNodes(t *testing.T) {
    router := NewRouter()

    for i := 0; i < 10; i++ {
        router.AddNode(&Node{
            ID:   fmt.Sprintf("node%d", i),
            Addr: fmt.Sprintf("192.168.1.%d:8080", i),
        })
    }

    selected := router.SelectRandomNodes(3)
    if len(selected) != 3 {
        t.Errorf("expected 3 nodes, got %d", len(selected))
    }

    // Check for duplicates
    seen := make(map[string]bool)
    for _, node := range selected {
        if seen[node.ID] {
            t.Error("duplicate node selected")
        }
        seen[node.ID] = true
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./pkg/router/ -v
```

Expected: FAIL with "undefined: NewRouter"

- [ ] **Step 3: Implement Router**

```go
package router

import (
    "math/rand"
    "sync"
)

type Node struct {
    ID   string
    Addr string
}

type Router struct {
    nodes map[string]*Node
    mu    sync.RWMutex
}

func NewRouter() *Router {
    return &Router{
        nodes: make(map[string]*Node),
    }
}

func (r *Router) AddNode(node *Node) {
    r.mu.Lock()
    r.nodes[node.ID] = node
    r.mu.Unlock()
}

func (r *Router) RemoveNode(id string) {
    r.mu.Lock()
    delete(r.nodes, id)
    r.mu.Unlock()
}

func (r *Router) GetNode(id string) (*Node, bool) {
    r.mu.RLock()
    node, ok := r.nodes[id]
    r.mu.RUnlock()
    return node, ok
}

func (r *Router) NodeCount() int {
    r.mu.RLock()
    count := len(r.nodes)
    r.mu.RUnlock()
    return count
}

func (r *Router) SelectRandomNodes(count int) []*Node {
    r.mu.RLock()
    defer r.mu.RUnlock()

    all := make([]*Node, 0, len(r.nodes))
    for _, node := range r.nodes {
        all = append(all, node)
    }

    if count > len(all) {
        count = len(all)
    }

    rand.Shuffle(len(all), func(i, j int) {
        all[i], all[j] = all[j], all[i]
    })

    return all[:count]
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./pkg/router/ -v
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add go/pkg/router/
git commit -m "feat: implement router with node management"
```

---

## Task 7: Message Bus - Application Interface

**Files:**
- Create: `go/pkg/message/bus.go`
- Create: `go/pkg/message/bus_test.go`

- [ ] **Step 1: Write failing test for MessageBus**

```go
package message

import (
    "testing"
    "sync"
)

import (
    "testing"
    "time"
    "sync"
)

func TestMessageBusSendReceive(t *testing.T) {
    bus := NewMessageBus()

    var received *Message
    var mu sync.Mutex

    bus.OnMessage(func(msg *Message) {
        mu.Lock()
        received = msg
        mu.Unlock()
    })

    msg := &Message{
        Type:    TypeAsync,
        DestID:  "node1",
        Content: []byte("hello"),
    }

    err := bus.Send(msg)
    if err != nil {
        t.Fatal(err)
    }

    // Wait for processing
    time.Sleep(50 * time.Millisecond)

    mu.Lock()
    if received == nil {
        t.Error("message not received")
    }
    if string(received.Content) != "hello" {
        t.Errorf("expected 'hello', got '%s'", string(received.Content))
    }
    mu.Unlock()
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./pkg/message/ -v
```

Expected: FAIL with "undefined: NewMessageBus"

- [ ] **Step 3: Implement MessageBus**

```go
package message

import (
    "errors"
    "sync"
)

type MessageType int

const (
    TypeAsync MessageType = iota
    TypeSyncRequest
    TypeSyncResponse
    TypeBroadcast
    TypeFileChunk
)

type Message struct {
    Type      MessageType
    DestID    string
    SenderID  string
    Content   []byte
    SessionKey []byte
}

type MessageHandler func(*Message)

type MessageBus struct {
    handlers []MessageHandler
    mu       sync.RWMutex
}

func NewMessageBus() *MessageBus {
    return &MessageBus{}
}

func (b *MessageBus) OnMessage(handler MessageHandler) {
    b.mu.Lock()
    b.handlers = append(b.handlers, handler)
    b.mu.Unlock()
}

func (b *MessageBus) Send(msg *Message) error {
    if msg == nil {
        return errors.New("message is nil")
    }

    b.mu.RLock()
    handlers := make([]MessageHandler, len(b.handlers))
    copy(handlers, b.handlers)
    b.mu.RUnlock()

    for _, handler := range handlers {
        go handler(msg)
    }

    return nil
}

func (b *MessageBus) Broadcast(msg *Message) error {
    msg.Type = TypeBroadcast
    msg.DestID = ""
    return b.Send(msg)
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./pkg/message/ -v
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add go/pkg/message/
git commit -m "feat: implement message bus"
```

---

## Task 8: Integration - Wire Components Together

**Files:**
- Create: `go/pkg/np4/node.go`
- Create: `go/pkg/np4/node_test.go`

- [ ] **Step 1: Write failing test for Node integration**

```go
package np4

import (
    "testing"
    "time"
    "sync"
)

func TestNodeCommunication(t *testing.T) {
    // Create two nodes
    node1, err := NewNode("127.0.0.1:0")
    if err != nil {
        t.Fatal(err)
    }
    defer node1.Stop()

    node2, err := NewNode("127.0.0.1:0")
    if err != nil {
        t.Fatal(err)
    }
    defer node2.Stop()

    // Register node2 with node1
    node1.AddPeer(node2.ID(), node2.Addr())

    var received []byte
    var mu sync.Mutex

    node2.OnMessage(func(msg *Message) {
        mu.Lock()
        received = msg.Content
        mu.Unlock()
    })

    // Send message from node1 to node2
    err = node1.Send(node2.ID(), []byte("hello from node1"))
    if err != nil {
        t.Fatal(err)
    }

    time.Sleep(100 * time.Millisecond)

    mu.Lock()
    if string(received) != "hello from node1" {
        t.Errorf("expected 'hello from node1', got '%s'", string(received))
    }
    mu.Unlock()
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./pkg/np4/ -v
```

Expected: FAIL with "undefined: NewNode"

- [ ] **Step 3: Implement Node**

```go
package np4

import (
    "Np4Protocol/go/pkg/crypto"
    "Np4Protocol/go/pkg/message"
    "Np4Protocol/go/pkg/mix"
    "Np4Protocol/go/pkg/router"
    "Np4Protocol/go/pkg/transport"
    "encoding/hex"
    "encoding/json"
    "errors"
    "math/rand"
    "sync"
    "time"
)

type Node struct {
    id        string
    transport transport.Transport
    crypto    *crypto.ChaCha20Encryptor
    keyExch   *crypto.X25519KeyExchange
    router    *router.Router
    mixEngine *mix.MixEngine[message.Message]
    bus       *message.MessageBus
    listener  transport.Listener
    stopCh    chan struct{}
    mu        sync.RWMutex
}

func NewNode(listenAddr string) (*Node, error) {
    idBytes := make([]byte, 16)
    rand.Read(idBytes)
    id := hex.EncodeToString(idBytes)

    tcp := transport.NewTCPTransport()
    listener, err := tcp.Listen(listenAddr)
    if err != nil {
        return nil, err
    }

    n := &Node{
        id:        id,
        transport: tcp,
        crypto:    crypto.NewChaCha20Encryptor(),
        keyExch:   crypto.NewX25519KeyExchange(),
        router:    router.NewRouter(),
        bus:       message.NewMessageBus(),
        listener:  listener,
        stopCh:    make(chan struct{}),
    }

    n.mixEngine = mix.NewMixEngine[message.Message](10, 500*time.Millisecond, n.sendBatch)

    go n.acceptLoop()

    return n, nil
}

func (n *Node) ID() string {
    return n.id
}

func (n *Node) Addr() string {
    return n.listener.Addr().String()
}

func (n *Node) AddPeer(id, addr string) {
    n.router.AddNode(&router.Node{ID: id, Addr: addr})
}

func (n *Node) OnMessage(handler func(*message.Message)) {
    n.bus.OnMessage(handler)
}

func (n *Node) Send(destID string, content []byte) error {
    node, ok := n.router.GetNode(destID)
    if !ok {
        return errors.New("unknown destination")
    }

    conn, err := n.transport.Connect(node.Addr)
    if err != nil {
        return err
    }
    defer conn.Close()

    msg := &message.Message{
        Type:     message.TypeAsync,
        DestID:   destID,
        SenderID: n.id,
        Content:  content,
    }

    data, err := json.Marshal(msg)
    if err != nil {
        return err
    }

    return conn.Write(data)
}

func (n *Node) Stop() {
    close(n.stopCh)
    n.listener.Close()
}

func (n *Node) acceptLoop() {
    for {
        select {
        case <-n.stopCh:
            return
        default:
        }

        conn, err := n.listener.Accept()
        if err != nil {
            continue
        }

        go n.handleConn(conn)
    }
}

func (n *Node) handleConn(conn transport.Conn) {
    defer conn.Close()

    data, err := conn.Read()
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
go test ./pkg/np4/ -v
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add go/pkg/np4/
git commit -m "feat: implement node with integrated components"
```

---

## Task 9: CLI Tools

**Files:**
- Create: `go/cmd/np4d/main.go`
- Create: `go/cmd/np4cli/main.go`

- [ ] **Step 1: Create MixNode daemon**

```go
package main

import (
    "Np4Protocol/go/pkg/np4"
    "flag"
    "fmt"
    "log"
    "os"
    "os/signal"
    "syscall"
)

func main() {
    addr := flag.String("addr", "0.0.0.0:8080", "Listen address")
    flag.Parse()

    node, err := np4.NewNode(*addr)
    if err != nil {
        log.Fatal(err)
    }

    fmt.Printf("Np4Protocol MixNode started on %s\n", node.Addr())
    fmt.Printf("Node ID: %s\n", node.ID())

    // Wait for interrupt
    sigCh := make(chan os.Signal, 1)
    signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
    <-sigCh

    fmt.Println("\nShutting down...")
    node.Stop()
}
```

- [ ] **Step 2: Create CLI client**

```go
package main

import (
    "Np4Protocol/go/pkg/message"
    "Np4Protocol/go/pkg/np4"
    "bufio"
    "flag"
    "fmt"
    "log"
    "os"
    "strings"
)

func main() {
    addr := flag.String("addr", "127.0.0.1:8080", "MixNode address")
    flag.Parse()

    node, err := np4.NewNode("127.0.0.1:0")
    if err != nil {
        log.Fatal(err)
    }
    defer node.Stop()

    fmt.Printf("Np4Protocol CLI started\n")
    fmt.Printf("Your Node ID: %s\n", node.ID())

    node.OnMessage(func(msg *message.Message) {
        fmt.Printf("\nReceived from %s: %s\n", msg.SenderID, string(msg.Content))
    })

    scanner := bufio.NewScanner(os.Stdin)
    for {
        fmt.Print("> ")
        if !scanner.Scan() {
            break
        }

        input := strings.TrimSpace(scanner.Text())
        if input == "" {
            continue
        }

        parts := strings.SplitN(input, " ", 2)
        if len(parts) < 2 {
            fmt.Println("Usage: <dest_id> <message>")
            continue
        }

        destID := parts[0]
        content := []byte(parts[1])

        err := node.Send(destID, content)
        if err != nil {
            fmt.Printf("Error: %v\n", err)
        }
    }
}
```

- [ ] **Step 3: Build CLI tools**

```bash
cd /Users/chenpeigen/GolandProjects/Np4Protocol/go
go build -o bin/np4d ./cmd/np4d/
go build -o bin/np4cli ./cmd/np4cli/
```

- [ ] **Step 4: Test CLI**

```bash
# Terminal 1: Start MixNode
./bin/np4d -addr 127.0.0.1:8080

# Terminal 2: Connect client
./bin/np4cli -addr 127.0.0.1:8080
```

- [ ] **Step 5: Commit**

```bash
git add go/cmd/
git commit -m "feat: add CLI tools for MixNode and client"
```

---

## Task 10: Documentation

**Files:**
- Create: `docs/protocol.md`

- [ ] **Step 1: Write protocol specification**

```markdown
# Np4Protocol Specification

## Overview

Np4Protocol is a Mixnet-based anonymous communication protocol designed for metadata protection.

## Message Format

Messages are serialized using Protocol Buffers. See `proto/np4.proto` for the complete definition.

### Envelope Structure

- `payload`: Encrypted message content
- `signature`: Optional signature for authentication
- `header`: Plaintext routing information

### Header Fields

- `type`: Message type (async, sync, broadcast, etc.)
- `dest_id`: Destination node ID (may be pseudonym)
- `sender_id`: Sender's pseudonym ID
- `timestamp`: Message creation time
- `ttl`: Time-to-live in hops
- `version`: Protocol version (currently 1)

## Encryption

- Key Exchange: X25519
- Symmetric Encryption: ChaCha20-Poly1305
- Key Derivation: HKDF-SHA256
- Signatures: Ed25519

## Mix Engine

Messages are collected in batches and shuffled before forwarding to prevent traffic analysis.

### Parameters

- `batch_size`: 10 messages per batch
- `max_delay`: 500ms maximum wait time
- `padding`: All messages padded to 4KB

## Node Types

- **Client**: End-user node
- **MixNode**: Relay node that shuffles messages
- **Bootstrap**: Entry point for new nodes
```

- [ ] **Step 2: Commit**

```bash
git add docs/protocol.md
git commit -m "docs: add protocol specification"
```

---

## Self-Review Checklist

- [ ] All protobuf messages from spec are defined
- [ ] All message types are implemented
- [ ] X25519 key exchange works correctly
- [ ] ChaCha20-Poly1305 encryption/decryption works
- [ ] MixEngine batches and shuffles messages
- [ ] Router manages nodes correctly
- [ ] MessageBus delivers messages to handlers
- [ ] Node integrates all components
- [ ] CLI tools build and run
- [ ] Protocol documentation is complete
