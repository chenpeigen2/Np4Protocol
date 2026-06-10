# P2P Key Exchange via Bootstrap Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement a BootstrapServer for X25519 key relay and encrypted P2P communication between nodes

**Architecture:** BootstrapServer acts as a key relay (does not hold secrets). Nodes register with Bootstrap, exchange X25519 public keys through it, then communicate directly with ChaCha20-Poly1305 encryption.

**Tech Stack:** Go 1.26, existing transport/crypto/router/message packages

---

## File Structure

```
go/pkg/
├── bootstrap/
│   ├── server.go          # BootstrapServer implementation
│   ├── server_test.go     # BootstrapServer tests
│   └── message.go         # BootstrapMessage types
├── np4/
│   ├── node.go            # Modify: add peerKeys, Register, ExchangeKeys, SendEncrypted
│   └── node_test.go       # Modify: add P2P encryption tests
```

---

## Task 1: Bootstrap Message Types

**Files:**
- Create: `go/pkg/bootstrap/message.go`
- Create: `go/pkg/bootstrap/message_test.go`

- [ ] **Step 1: Write failing test for message serialization**

```go
package bootstrap

import (
    "testing"
)

func TestBootstrapMessageSerialize(t *testing.T) {
    msg := BootstrapMessage{
        Type:     "register",
        NodeID:   "node1",
        Addr:     "192.168.1.1:8080",
        PublicKey: []byte{1, 2, 3, 4},
    }

    data, err := Serialize(msg)
    if err != nil {
        t.Fatal(err)
    }

    var decoded BootstrapMessage
    err = Deserialize(data, &decoded)
    if err != nil {
        t.Fatal(err)
    }

    if decoded.Type != msg.Type {
        t.Errorf("Type: got %s, want %s", decoded.Type, msg.Type)
    }
    if decoded.NodeID != msg.NodeID {
        t.Errorf("NodeID: got %s, want %s", decoded.NodeID, msg.NodeID)
    }
    if decoded.Addr != msg.Addr {
        t.Errorf("Addr: got %s, want %s", decoded.Addr, msg.Addr)
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /Users/chenpeigen/GolandProjects/Np4Protocol/go
go test ./pkg/bootstrap/ -v
```

Expected: FAIL with "package bootstrap does not exist"

- [ ] **Step 3: Implement message types**

```go
package bootstrap

import "encoding/json"

type BootstrapMessage struct {
    Type      string `json:"type"`
    NodeID    string `json:"node_id"`
    Addr      string `json:"addr"`
    PublicKey  []byte `json:"public_key"`
    TargetID  string `json:"target_id,omitempty"`
    Nonce     []byte `json:"nonce,omitempty"`
    Success   bool   `json:"success"`
    Error     string `json:"error,omitempty"`
}

func Serialize(msg BootstrapMessage) ([]byte, error) {
    return json.Marshal(msg)
}

func Deserialize(data []byte, msg *BootstrapMessage) error {
    return json.Unmarshal(data, msg)
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./pkg/bootstrap/ -v -run TestBootstrapMessageSerialize
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add go/pkg/bootstrap/
git commit -m "feat: add bootstrap message types"
```

---

## Task 2: BootstrapServer - Peer Registration

**Files:**
- Create: `go/pkg/bootstrap/server.go`
- Modify: `go/pkg/bootstrap/server_test.go`

- [ ] **Step 1: Write failing test for registration**

```go
package bootstrap

import (
    "Np4Protocol/go/pkg/transport"
    "testing"
    "time"
)

func TestBootstrapServerRegister(t *testing.T) {
    server, err := NewBootstrapServer()
    if err != nil {
        t.Fatal(err)
    }
    defer server.Stop()

    err = server.Start("127.0.0.1:0")
    if err != nil {
        t.Fatal(err)
    }

    tcp := transport.NewTCPTransport()
    conn, err := tcp.Connect(server.Addr())
    if err != nil {
        t.Fatal(err)
    }
    defer conn.Close()

    regMsg := BootstrapMessage{
        Type:     "register",
        NodeID:   "node1",
        Addr:     "192.168.1.1:8080",
        PublicKey: []byte{1, 2, 3},
    }

    data, _ := Serialize(regMsg)
    conn.Write(data)

    time.Sleep(50 * time.Millisecond)

    if server.PeerCount() != 1 {
        t.Errorf("expected 1 peer, got %d", server.PeerCount())
    }

    peer, ok := server.GetPeer("node1")
    if !ok {
        t.Fatal("peer not found")
    }
    if peer.Addr != "192.168.1.1:8080" {
        t.Errorf("addr: got %s, want 192.168.1.1:8080", peer.Addr)
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./pkg/bootstrap/ -v -run TestBootstrapServerRegister
```

Expected: FAIL with "undefined: NewBootstrapServer"

- [ ] **Step 3: Implement BootstrapServer with registration**

```go
package bootstrap

import (
    "Np4Protocol/go/pkg/transport"
    "crypto/rand"
    "encoding/hex"
    "sync"
)

type PeerInfo struct {
    ID        string
    Addr      string
    PublicKey  []byte
    Nonce     []byte
}

type BootstrapServer struct {
    transport transport.Transport
    listener  transport.Listener
    peers     map[string]*PeerInfo
    mu        sync.RWMutex
}

func NewBootstrapServer() (*BootstrapServer, error) {
    tcp := transport.NewTCPTransport()
    return &BootstrapServer{
        transport: tcp,
        peers:     make(map[string]*PeerInfo),
    }, nil
}

func (s *BootstrapServer) Start(addr string) error {
    listener, err := s.transport.Listen(addr)
    if err != nil {
        return err
    }
    s.listener = listener
    go s.acceptLoop()
    return nil
}

func (s *BootstrapServer) Stop() {
    if s.listener != nil {
        s.listener.Close()
    }
}

func (s *BootstrapServer) Addr() string {
    return s.listener.Addr().String()
}

func (s *BootstrapServer) PeerCount() int {
    s.mu.RLock()
    defer s.mu.RUnlock()
    return len(s.peers)
}

func (s *BootstrapServer) GetPeer(id string) (*PeerInfo, bool) {
    s.mu.RLock()
    defer s.mu.RUnlock()
    peer, ok := s.peers[id]
    return peer, ok
}

func (s *BootstrapServer) acceptLoop() {
    for {
        conn, err := s.listener.Accept()
        if err != nil {
            return
        }
        go s.handleConn(conn)
    }
}

func (s *BootstrapServer) handleConn(conn transport.Conn) {
    defer conn.Close()

    data, err := conn.Read()
    if err != nil {
        return
    }

    var msg BootstrapMessage
    if err := Deserialize(data, &msg); err != nil {
        return
    }

    switch msg.Type {
    case "register":
        s.handleRegister(conn, msg)
    }
}

func (s *BootstrapServer) handleRegister(conn transport.Conn, msg BootstrapMessage) {
    nonce := make([]byte, 16)
    rand.Read(nonce)

    s.mu.Lock()
    s.peers[msg.NodeID] = &PeerInfo{
        ID:       msg.NodeID,
        Addr:     msg.Addr,
        PublicKey: msg.PublicKey,
        Nonce:    nonce,
    }
    s.mu.Unlock()

    resp := BootstrapMessage{
        Type:    "register",
        Success: true,
        Nonce:   nonce,
    }
    data, _ := Serialize(resp)
    conn.Write(data)
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./pkg/bootstrap/ -v -run TestBootstrapServerRegister
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add go/pkg/bootstrap/
git commit -m "feat: implement bootstrap server with peer registration"
```

---

## Task 3: BootstrapServer - Key Exchange Relay

**Files:**
- Modify: `go/pkg/bootstrap/server.go`
- Modify: `go/pkg/bootstrap/server_test.go`

- [ ] **Step 1: Write failing test for key exchange relay**

```go
func TestBootstrapServerKeyExchange(t *testing.T) {
    server, err := NewBootstrapServer()
    if err != nil {
        t.Fatal(err)
    }
    defer server.Stop()

    err = server.Start("127.0.0.1:0")
    if err != nil {
        t.Fatal(err)
    }

    tcp := transport.NewTCPTransport()

    // Start a mock Node B that listens for key exchange requests
    nodeBListener, _ := tcp.Listen("127.0.0.1:0")
    defer nodeBListener.Close()
    nodeBAddr := nodeBListener.Addr().String()

    go func() {
        conn, err := nodeBListener.Accept()
        if err != nil {
            return
        }
        defer conn.Close()

        data, _ := conn.Read()
        var reqMsg BootstrapMessage
        Deserialize(data, &reqMsg)

        // Respond with B's public key
        resp := BootstrapMessage{
            Type: "key_exchange_response", PublicKey: []byte{0xBB},
        }
        respData, _ := Serialize(resp)
        conn.Write(respData)
    }()

    // Node A registers
    connA, _ := tcp.Connect(server.Addr())
    defer connA.Close()
    regA := BootstrapMessage{
        Type: "register", NodeID: "nodeA", Addr: "10.0.0.1:8080",
        PublicKey: []byte{0xAA},
    }
    dataA, _ := Serialize(regA)
    connA.Write(dataA)
    time.Sleep(50 * time.Millisecond)

    // Node B registers (using the listener address)
    connB, _ := tcp.Connect(server.Addr())
    defer connB.Close()
    regB := BootstrapMessage{
        Type: "register", NodeID: "nodeB", Addr: nodeBAddr,
        PublicKey: []byte{0xBB},
    }
    dataB, _ := Serialize(regB)
    connB.Write(dataB)
    time.Sleep(50 * time.Millisecond)

    // Node A requests key exchange with B
    reqMsg := BootstrapMessage{
        Type: "key_exchange_request", NodeID: "nodeA", TargetID: "nodeB",
    }
    reqData, _ := Serialize(reqMsg)
    connA.Write(reqData)

    time.Sleep(200 * time.Millisecond)

    // Node A should receive B's public key from bootstrap
    respData, err := connA.Read()
    if err != nil {
        t.Fatal("node A did not receive key exchange response")
    }

    var respMsg BootstrapMessage
    Deserialize(respData, &respMsg)

    if respMsg.Type != "key_exchange_response" {
        t.Errorf("expected key_exchange_response, got %s", respMsg.Type)
    }
    if !respMsg.Success {
        t.Errorf("expected success, got error: %s", respMsg.Error)
    }
    if string(respMsg.PublicKey) != string([]byte{0xBB}) {
        t.Error("public key mismatch")
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./pkg/bootstrap/ -v -run TestBootstrapServerKeyExchange
```

Expected: FAIL (key_exchange_request not handled)

- [ ] **Step 3: Implement key exchange relay**

Add to `server.go`:

```go
func (s *BootstrapServer) handleKeyExchangeRequest(conn transport.Conn, msg BootstrapMessage) {
    s.mu.RLock()
    targetPeer, ok := s.peers[msg.TargetID]
    senderPeer, senderOk := s.peers[msg.NodeID]
    s.mu.RUnlock()

    if !ok || !senderOk {
        resp := BootstrapMessage{Type: "key_exchange_response", Success: false, Error: "peer not found"}
        data, _ := Serialize(resp)
        conn.Write(data)
        return
    }

    // Connect to target node and forward the request
    targetConn, err := s.transport.Connect(targetPeer.Addr)
    if err != nil {
        resp := BootstrapMessage{Type: "key_exchange_response", Success: false, Error: "target unreachable"}
        data, _ := Serialize(resp)
        conn.Write(data)
        return
    }
    defer targetConn.Close()

    // Forward request to target with sender's public key
    forwardMsg := BootstrapMessage{
        Type:      "key_exchange_request",
        NodeID:    msg.NodeID,
        Addr:      senderPeer.Addr,
        PublicKey:  senderPeer.PublicKey,
        TargetID:  msg.TargetID,
    }
    forwardData, _ := Serialize(forwardMsg)
    targetConn.Write(forwardData)

    // Read target's response (its public key)
    respData, err := targetConn.Read()
    if err != nil {
        resp := BootstrapMessage{Type: "key_exchange_response", Success: false, Error: "target timeout"}
        data, _ := Serialize(resp)
        conn.Write(data)
        return
    }

    var targetResp BootstrapMessage
    Deserialize(respData, &targetResp)

    // Forward target's public key back to requester
    replyMsg := BootstrapMessage{
        Type:     "key_exchange_response",
        NodeID:   msg.TargetID,
        Addr:     targetPeer.Addr,
        PublicKey: targetResp.PublicKey,
        Success:  true,
    }
    replyData, _ := Serialize(replyMsg)
    conn.Write(replyData)
}
```

Update `handleConn` to route "key_exchange_request":

```go
func (s *BootstrapServer) handleConn(conn transport.Conn) {
    defer conn.Close()

    data, err := conn.Read()
    if err != nil {
        return
    }

    var msg BootstrapMessage
    if err := Deserialize(data, &msg); err != nil {
        return
    }

    switch msg.Type {
    case "register":
        s.handleRegister(conn, msg)
    case "key_exchange_request":
        s.handleKeyExchangeRequest(conn, msg)
    }
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./pkg/bootstrap/ -v -run TestBootstrapServerKeyExchange
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add go/pkg/bootstrap/
git commit -m "feat: implement key exchange relay in bootstrap server"
```

---

## Task 4: Node - Registration with Bootstrap

**Files:**
- Modify: `go/pkg/np4/node.go`
- Modify: `go/pkg/np4/node_test.go`

- [ ] **Step 1: Write failing test for registration**

```go
func TestNodeRegister(t *testing.T) {
    bootstrap, err := bootstrap.NewBootstrapServer()
    if err != nil {
        t.Fatal(err)
    }
    defer bootstrap.Stop()
    bootstrap.Start("127.0.0.1:0")

    node, err := NewNode("127.0.0.1:0")
    if err != nil {
        t.Fatal(err)
    }
    defer node.Stop()

    err = node.Register(bootstrap.Addr())
    if err != nil {
        t.Fatal(err)
    }

    if bootstrap.PeerCount() != 1 {
        t.Errorf("expected 1 peer, got %d", bootstrap.PeerCount())
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./pkg/np4/ -v -run TestNodeRegister
```

Expected: FAIL with "undefined: Register"

- [ ] **Step 3: Implement Register method**

Add to `node.go`:

```go
func (n *Node) Register(bootstrapAddr string) error {
    conn, err := n.transport.Connect(bootstrapAddr)
    if err != nil {
        return err
    }

    pubKey, privKey, err := n.keyExch.GenerateKeyPair()
    if err != nil {
        conn.Close()
        return err
    }

    n.mu.Lock()
    n.publicKey = pubKey
    n.privateKey = privKey
    n.mu.Unlock()

    msg := bootstrap.BootstrapMessage{
        Type:     "register",
        NodeID:   n.id,
        Addr:     n.listener.Addr().String(),
        PublicKey: pubKey,
    }

    data, err := bootstrap.Serialize(msg)
    if err != nil {
        conn.Close()
        return err
    }

    conn.Write(data)
    conn.Close()
    return nil
}
```

Also add `publicKey` and `privateKey` fields to Node struct:

```go
type Node struct {
    // ... existing fields ...
    publicKey  []byte
    privateKey []byte
    peerKeys   map[string]*PeerSession
}

type PeerSession struct {
    PeerID    string
    PeerAddr  string
    SharedKey []byte
    CreatedAt time.Time
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./pkg/np4/ -v -run TestNodeRegister
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add go/pkg/np4/
git commit -m "feat: add node registration with bootstrap"
```

---

## Task 5: Node - Key Exchange via Bootstrap

**Files:**
- Modify: `go/pkg/np4/node.go`
- Modify: `go/pkg/np4/node_test.go`

- [ ] **Step 1: Write failing test for key exchange**

```go
func TestNodeExchangeKeys(t *testing.T) {
    bootstrap, err := bootstrap.NewBootstrapServer()
    if err != nil {
        t.Fatal(err)
    }
    defer bootstrap.Stop()
    bootstrap.Start("127.0.0.1:0")

    nodeA, _ := NewNode("127.0.0.1:0")
    defer nodeA.Stop()
    nodeA.Register(bootstrap.Addr())

    nodeB, _ := NewNode("127.0.0.1:0")
    defer nodeB.Stop()
    nodeB.Register(bootstrap.Addr())

    // A exchanges keys with B
    err = nodeA.ExchangeKeys(bootstrap.Addr(), nodeB.ID())
    if err != nil {
        t.Fatal(err)
    }

    // Both should have shared keys
    sessionA, okA := nodeA.GetPeerSession(nodeB.ID())
    sessionB, okB := nodeB.GetPeerSession(nodeA.ID())

    if !okA || !okB {
        t.Fatal("peer sessions not established")
    }

    if !bytes.Equal(sessionA.SharedKey, sessionB.SharedKey) {
        t.Error("shared keys do not match")
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./pkg/np4/ -v -run TestNodeExchangeKeys
```

Expected: FAIL with "undefined: ExchangeKeys"

- [ ] **Step 3: Implement ExchangeKeys**

```go
func (n *Node) ExchangeKeys(bootstrapAddr, peerID string) error {
    conn, err := n.transport.Connect(bootstrapAddr)
    if err != nil {
        return err
    }
    defer conn.Close()

    n.mu.RLock()
    pubKey := n.publicKey
    n.mu.RUnlock()

    reqMsg := bootstrap.BootstrapMessage{
        Type:     "key_exchange_request",
        NodeID:   n.id,
        TargetID: peerID,
        PublicKey: pubKey,
    }

    data, _ := bootstrap.Serialize(reqMsg)
    conn.Write(data)

    // Read response with peer's public key
    respData, err := conn.Read()
    if err != nil {
        return err
    }

    var respMsg bootstrap.BootstrapMessage
    bootstrap.Deserialize(respData, &respMsg)

    if !respMsg.Success {
        return errors.New("key exchange failed: " + respMsg.Error)
    }

    // Compute shared secret
    n.mu.RLock()
    privKey := n.privateKey
    n.mu.RUnlock()

    sharedKey, err := n.keyExch.ComputeSharedSecret(privKey, respMsg.PublicKey)
    if err != nil {
        return err
    }

    n.mu.Lock()
    n.peerKeys[peerID] = &PeerSession{
        PeerID:    peerID,
        PeerAddr:  respMsg.Addr,
        SharedKey: sharedKey,
        CreatedAt: time.Now(),
    }
    n.mu.Unlock()

    return nil
}

func (n *Node) GetPeerSession(peerID string) (*PeerSession, bool) {
    n.mu.RLock()
    defer n.mu.RUnlock()
    session, ok := n.peerKeys[peerID]
    return session, ok
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./pkg/np4/ -v -run TestNodeExchangeKeys
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add go/pkg/np4/
git commit -m "feat: implement key exchange via bootstrap"
```

---

## Task 6: Node - Encrypted P2P Send

**Files:**
- Modify: `go/pkg/np4/node.go`
- Modify: `go/pkg/np4/node_test.go`

- [ ] **Step 1: Write failing test for encrypted send**

```go
func TestNodeSendEncrypted(t *testing.T) {
    bootstrap, _ := bootstrap.NewBootstrapServer()
    defer bootstrap.Stop()
    bootstrap.Start("127.0.0.1:0")

    nodeA, _ := NewNode("127.0.0.1:0")
    defer nodeA.Stop()
    nodeA.Register(bootstrap.Addr())

    nodeB, _ := NewNode("127.0.0.1:0")
    defer nodeB.Stop()
    nodeB.Register(bootstrap.Addr())

    nodeA.ExchangeKeys(bootstrap.Addr(), nodeB.ID())
    nodeB.ExchangeKeys(bootstrap.Addr(), nodeA.ID())

    var received []byte
    var mu sync.Mutex
    nodeB.OnMessage(func(msg *message.Message) {
        mu.Lock()
        received = msg.Content
        mu.Unlock()
    })

    err := nodeA.SendEncrypted(nodeB.ID(), []byte("secret message"))
    if err != nil {
        t.Fatal(err)
    }

    time.Sleep(100 * time.Millisecond)

    mu.Lock()
    if string(received) != "secret message" {
        t.Errorf("expected 'secret message', got '%s'", string(received))
    }
    mu.Unlock()
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./pkg/np4/ -v -run TestNodeSendEncrypted
```

Expected: FAIL with "undefined: SendEncrypted"

- [ ] **Step 3: Implement SendEncrypted**

```go
type EncryptedMessage struct {
    SenderID   string `json:"sender_id"`
    Nonce      []byte `json:"nonce"`
    Ciphertext []byte `json:"ciphertext"`
}

func (n *Node) SendEncrypted(destID string, content []byte) error {
    n.mu.RLock()
    session, ok := n.peerKeys[destID]
    n.mu.RUnlock()

    if !ok {
        return errors.New("no shared key with peer")
    }

    ciphertext, err := n.crypto.Encrypt(content, session.SharedKey)
    if err != nil {
        return err
    }

    encMsg := EncryptedMessage{
        SenderID:   n.id,
        Ciphertext: ciphertext,
    }

    data, _ := json.Marshal(encMsg)

    conn, err := n.transport.Connect(session.PeerAddr)
    if err != nil {
        return err
    }
    defer conn.Close()

    return conn.Write(data)
}
```

Also update `handleConn` to detect encrypted messages and key exchange requests:

```go
func (n *Node) handleConn(conn transport.Conn) {
    defer conn.Close()

    data, err := conn.Read()
    if err != nil {
        return
    }

    // Try bootstrap message (key exchange request from bootstrap)
    var bsMsg bootstrap.BootstrapMessage
    if json.Unmarshal(data, &bsMsg) == nil && bsMsg.Type == "key_exchange_request" {
        n.handleKeyExchangeRequest(conn, bsMsg)
        return
    }

    // Try encrypted message
    var encMsg EncryptedMessage
    if json.Unmarshal(data, &encMsg) == nil && encMsg.Ciphertext != nil {
        n.handleEncryptedMessage(encMsg)
        return
    }

    // Fall back to plain message
    var msg message.Message
    if json.Unmarshal(data, &msg) != nil {
        return
    }
    n.bus.Send(&msg)
}

func (n *Node) handleKeyExchangeRequest(conn transport.Conn, bsMsg bootstrap.BootstrapMessage) {
    n.mu.RLock()
    pubKey := n.publicKey
    n.mu.RUnlock()

    // Respond with our public key
    resp := bootstrap.BootstrapMessage{
        Type:     "key_exchange_response",
        PublicKey: pubKey,
        Success:  true,
    }
    data, _ := bootstrap.Serialize(resp)
    conn.Write(data)
}

func (n *Node) handleEncryptedMessage(encMsg EncryptedMessage) {
    n.mu.RLock()
    session, ok := n.peerKeys[encMsg.SenderID]
    n.mu.RUnlock()

    if !ok {
        return // unknown sender, drop
    }

    plaintext, err := n.crypto.Decrypt(encMsg.Ciphertext, session.SharedKey)
    if err != nil {
        return // decryption failed, drop
    }

    msg := &message.Message{
        Type:     message.TypeAsync,
        SenderID: encMsg.SenderID,
        Content:  plaintext,
    }
    n.bus.Send(msg)
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./pkg/np4/ -v -run TestNodeSendEncrypted
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add go/pkg/np4/
git commit -m "feat: implement encrypted P2P send"
```

---

## Task 7: Integration Test

**Files:**
- Modify: `go/pkg/np4/node_test.go`

- [ ] **Step 1: Write full integration test**

```go
func TestFullP2PKeyExchangeFlow(t *testing.T) {
    // Start bootstrap
    bootstrap, _ := bootstrap.NewBootstrapServer()
    defer bootstrap.Stop()
    bootstrap.Start("127.0.0.1:0")

    // Create nodes
    nodeA, _ := NewNode("127.0.0.1:0")
    defer nodeA.Stop()
    nodeB, _ := NewNode("127.0.0.1:0")
    defer nodeB.Stop()

    // Register
    nodeA.Register(bootstrap.Addr())
    nodeB.Register(bootstrap.Addr())

    // Exchange keys
    nodeA.ExchangeKeys(bootstrap.Addr(), nodeB.ID())
    nodeB.ExchangeKeys(bootstrap.Addr(), nodeA.ID())

    // Set up receivers
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

    // A sends to B
    nodeA.SendEncrypted(nodeB.ID(), []byte("hello from A"))
    time.Sleep(100 * time.Millisecond)

    // B sends to A
    nodeB.SendEncrypted(nodeA.ID(), []byte("hello from B"))
    time.Sleep(100 * time.Millisecond)

    mu.Lock()
    if string(receivedB) != "hello from A" {
        t.Errorf("B got: %s", string(receivedB))
    }
    if string(receivedA) != "hello from B" {
        t.Errorf("A got: %s", string(receivedA))
    }
    mu.Unlock()
}
```

- [ ] **Step 2: Run test to verify it passes**

```bash
go test ./pkg/np4/ -v -run TestFullP2PKeyExchangeFlow
```

Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add go/pkg/np4/
git commit -m "test: add full P2P key exchange integration test"
```

---

## Task 8: Bootstrap CLI Tool

**Files:**
- Create: `go/cmd/bootstrap/main.go`

- [ ] **Step 1: Create bootstrap CLI**

```go
package main

import (
    "Np4Protocol/go/pkg/bootstrap"
    "flag"
    "fmt"
    "log"
    "os"
    "os/signal"
    "syscall"
)

func main() {
    addr := flag.String("addr", "0.0.0.0:9090", "Listen address")
    flag.Parse()

    server, err := bootstrap.NewBootstrapServer()
    if err != nil {
        log.Fatal(err)
    }

    err = server.Start(*addr)
    if err != nil {
        log.Fatal(err)
    }

    fmt.Printf("Bootstrap server started on %s\n", *addr)

    sigCh := make(chan os.Signal, 1)
    signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
    <-sigCh

    fmt.Println("\nShutting down...")
    server.Stop()
}
```

- [ ] **Step 2: Build and verify**

```bash
cd /Users/chenpeigen/GolandProjects/Np4Protocol/go
go build -o bin/bootstrap ./cmd/bootstrap/
```

- [ ] **Step 3: Commit**

```bash
git add go/cmd/bootstrap/
git commit -m "feat: add bootstrap CLI tool"
```

---

## Self-Review Checklist

- [ ] BootstrapMessage types defined and serializable
- [ ] BootstrapServer handles register and key_exchange_request
- [ ] Node.Register() connects to bootstrap and registers
- [ ] Node.ExchangeKeys() exchanges keys via bootstrap relay
- [ ] Node.SendEncrypted() sends ChaCha20-Poly1305 encrypted messages
- [ ] Node.handleConn() detects and decrypts encrypted messages
- [ ] Full integration test (A↔B bidirectional encrypted chat)
- [ ] Bootstrap CLI tool builds
- [ ] All tests pass
