# Peer Discovery and Approval Flow Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add peer discovery via Bootstrap and a connection approval flow where the target node must approve before key exchange

**Architecture:** Bootstrap acts as a peer registry and message relay. Nodes query Bootstrap for online peers, request connections through Bootstrap, and the target node's callback decides whether to approve.

**Tech Stack:** Go 1.26, existing bootstrap/transport/crypto packages

---

## File Structure

```
go/pkg/bootstrap/
├── server.go          # Modify: add handleListPeers, handleConnectRequest
└── server_test.go     # Modify: add discovery and approval tests

go/pkg/np4/
├── node.go            # Modify: add ListPeers, RequestConnect, OnApprovalRequest
└── node_test.go       # Modify: add discovery and approval tests
```

---

## Task 1: Bootstrap - List Peers

**Files:**
- Modify: `go/pkg/bootstrap/server.go`
- Modify: `go/pkg/bootstrap/server_test.go`

- [ ] **Step 1: Write failing test**

Add to `go/pkg/bootstrap/server_test.go`:

```go
func TestBootstrapListPeers(t *testing.T) {
    server, _ := NewBootstrapServer()
    defer server.Stop()
    server.Start("127.0.0.1:0")

    tcp := transport.NewTCPTransport()

    // Register node A
    connA, _ := tcp.Connect(server.Addr())
    defer connA.Close()
    regA := BootstrapMessage{Type: "register", NodeID: "nodeA", Addr: "10.0.0.1:8080", PublicKey: []byte{0xAA}}
    dataA, _ := Serialize(regA)
    connA.Write(dataA)
    time.Sleep(50 * time.Millisecond)

    // Register node B
    connB, _ := tcp.Connect(server.Addr())
    defer connB.Close()
    regB := BootstrapMessage{Type: "register", NodeID: "nodeB", Addr: "10.0.0.2:8080", PublicKey: []byte{0xBB}}
    dataB, _ := Serialize(regB)
    connB.Write(dataB)
    time.Sleep(50 * time.Millisecond)

    // Node A lists peers
    listMsg := BootstrapMessage{Type: "list_peers", NodeID: "nodeA"}
    listData, _ := Serialize(listMsg)
    connA.Write(listData)
    time.Sleep(50 * time.Millisecond)

    // Read response (skip register response first)
    connA.Read() // drain register response
    respData, err := connA.Read()
    if err != nil {
        t.Fatal("no response")
    }

    var respMsg BootstrapMessage
    Deserialize(respData, &respMsg)

    if respMsg.Type != "list_peers" {
        t.Errorf("expected list_peers, got %s", respMsg.Type)
    }
    if !respMsg.Success {
        t.Error("expected success")
    }
    // Should have 1 peer (B), not including self (A)
    if len(respMsg.Peers) != 1 {
        t.Errorf("expected 1 peer, got %d", len(respMsg.Peers))
    }
    if respMsg.Peers[0].ID != "nodeB" {
        t.Errorf("expected nodeB, got %s", respMsg.Peers[0].ID)
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /Users/chenpeigen/GolandProjects/Np4Protocol/go
go test ./pkg/bootstrap/ -v -run TestBootstrapListPeers
```

- [ ] **Step 3: Implement list peers**

Add `Peers` field to `BootstrapMessage`:

```go
type BootstrapMessage struct {
    Type      string     `json:"type"`
    NodeID    string     `json:"node_id"`
    Addr      string     `json:"addr"`
    PublicKey  []byte     `json:"public_key"`
    TargetID  string     `json:"target_id,omitempty"`
    Nonce     []byte     `json:"nonce,omitempty"`
    Success   bool       `json:"success"`
    Error     string     `json:"error,omitempty"`
    Peers     []PeerInfo `json:"peers,omitempty"`
}
```

Add `handleListPeers` to `server.go`:

```go
func (s *BootstrapServer) handleListPeers(conn transport.Conn, msg BootstrapMessage) {
    s.mu.RLock()
    peers := make([]PeerInfo, 0)
    for id, peer := range s.peers {
        if id != msg.NodeID {
            peers = append(peers, *peer)
        }
    }
    s.mu.RUnlock()

    resp := BootstrapMessage{
        Type:    "list_peers",
        Success: true,
        Peers:   peers,
    }
    data, _ := Serialize(resp)
    conn.Write(data)
}
```

Update `handleConn` switch:

```go
case "list_peers":
    s.handleListPeers(conn, msg)
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./pkg/bootstrap/ -v -run TestBootstrapListPeers
```

- [ ] **Step 5: Commit**

```bash
git add go/pkg/bootstrap/
git commit -m "feat: add peer listing to bootstrap server"
```

---

## Task 2: Bootstrap - Connect Request Relay

**Files:**
- Modify: `go/pkg/bootstrap/server.go`
- Modify: `go/pkg/bootstrap/server_test.go`

- [ ] **Step 1: Write failing test**

Add to `go/pkg/bootstrap/server_test.go`:

```go
func TestBootstrapConnectRequest(t *testing.T) {
    server, _ := NewBootstrapServer()
    defer server.Stop()
    server.Start("127.0.0.1:0")

    tcp := transport.NewTCPTransport()

    // Start mock Node B that handles connect_request
    nodeBListener, _ := tcp.Listen("127.0.0.1:0")
    defer nodeBListener.Close()
    nodeBAddr := nodeBListener.Addr().String()

    go func() {
        conn, _ := nodeBListener.Accept()
        defer conn.Close()
        data, _ := conn.Read()
        var reqMsg BootstrapMessage
        Deserialize(data, &reqMsg)
        // Approve the request
        resp := BootstrapMessage{Type: "connect_response", Success: true, Approved: true}
        respData, _ := Serialize(resp)
        conn.Write(respData)
    }()

    // Register A
    connA, _ := tcp.Connect(server.Addr())
    defer connA.Close()
    regA := BootstrapMessage{Type: "register", NodeID: "nodeA", Addr: "10.0.0.1:8080", PublicKey: []byte{0xAA}}
    dataA, _ := Serialize(regA)
    connA.Write(dataA)
    time.Sleep(50 * time.Millisecond)

    // Register B
    connB, _ := tcp.Connect(server.Addr())
    defer connB.Close()
    regB := BootstrapMessage{Type: "register", NodeID: "nodeB", Addr: nodeBAddr, PublicKey: []byte{0xBB}}
    dataB, _ := Serialize(regB)
    connB.Write(dataB)
    time.Sleep(50 * time.Millisecond)

    // A requests connection to B
    reqMsg := BootstrapMessage{Type: "connect_request", NodeID: "nodeA", TargetID: "nodeB"}
    reqData, _ := Serialize(reqMsg)
    connA.Write(reqData)
    time.Sleep(200 * time.Millisecond)

    // A should receive approval
    connA.Read() // drain register response
    respData, err := connA.Read()
    if err != nil {
        t.Fatal("no response")
    }

    var respMsg BootstrapMessage
    Deserialize(respData, &respMsg)

    if respMsg.Type != "connect_response" {
        t.Errorf("expected connect_response, got %s", respMsg.Type)
    }
    if !respMsg.Approved {
        t.Error("expected approved")
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./pkg/bootstrap/ -v -run TestBootstrapConnectRequest
```

- [ ] **Step 3: Implement connect request relay**

Add `Approved` field to `BootstrapMessage`:

```go
type BootstrapMessage struct {
    Type      string     `json:"type"`
    NodeID    string     `json:"node_id"`
    Addr      string     `json:"addr"`
    PublicKey  []byte     `json:"public_key"`
    TargetID  string     `json:"target_id,omitempty"`
    Nonce     []byte     `json:"nonce,omitempty"`
    Success   bool       `json:"success"`
    Approved  bool       `json:"approved,omitempty"`
    Error     string     `json:"error,omitempty"`
    Peers     []PeerInfo `json:"peers,omitempty"`
}
```

Add `handleConnectRequest` to `server.go`:

```go
func (s *BootstrapServer) handleConnectRequest(conn transport.Conn, msg BootstrapMessage) {
    s.mu.RLock()
    targetPeer, ok := s.peers[msg.TargetID]
    senderPeer, senderOk := s.peers[msg.NodeID]
    s.mu.RUnlock()

    if !ok || !senderOk {
        resp := BootstrapMessage{Type: "connect_response", Success: false, Error: "peer not found"}
        data, _ := Serialize(resp)
        conn.Write(data)
        return
    }

    // Connect to target and forward request
    targetConn, err := s.transport.Connect(targetPeer.Addr)
    if err != nil {
        resp := BootstrapMessage{Type: "connect_response", Success: false, Error: "target unreachable"}
        data, _ := Serialize(resp)
        conn.Write(data)
        return
    }
    defer targetConn.Close()

    // Forward request with sender info
    forwardMsg := BootstrapMessage{
        Type:     "connect_request",
        NodeID:   msg.NodeID,
        Addr:     senderPeer.Addr,
        PublicKey: senderPeer.PublicKey,
    }
    forwardData, _ := Serialize(forwardMsg)
    targetConn.Write(forwardData)

    // Read approval response
    respData, err := targetConn.Read()
    if err != nil {
        resp := BootstrapMessage{Type: "connect_response", Success: false, Error: "target timeout"}
        data, _ := Serialize(resp)
        conn.Write(data)
        return
    }

    var targetResp BootstrapMessage
    if err := Deserialize(respData, &targetResp); err != nil {
        resp := BootstrapMessage{Type: "connect_response", Success: false, Error: "invalid response"}
        data, _ := Serialize(resp)
        conn.Write(data)
        return
    }

    // Forward approval to requester
    replyMsg := BootstrapMessage{
        Type:     "connect_response",
        NodeID:   msg.TargetID,
        Approved: targetResp.Approved,
        Success:  true,
    }
    replyData, _ := Serialize(replyMsg)
    conn.Write(replyData)
}
```

Update `handleConn` switch:

```go
case "connect_request":
    s.handleConnectRequest(conn, msg)
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./pkg/bootstrap/ -v -run TestBootstrapConnectRequest
```

- [ ] **Step 5: Commit**

```bash
git add go/pkg/bootstrap/
git commit -m "feat: add connect request relay to bootstrap server"
```

---

## Task 3: Node - ListPeers and OnApprovalRequest

**Files:**
- Modify: `go/pkg/np4/node.go`
- Modify: `go/pkg/np4/node_test.go`

- [ ] **Step 1: Write failing test for ListPeers**

Add to `go/pkg/np4/node_test.go`:

```go
func TestNodeListPeers(t *testing.T) {
    bs, _ := bootstrap.NewBootstrapServer()
    defer bs.Stop()
    bs.Start("127.0.0.1:0")

    nodeA, _ := NewNode("127.0.0.1:0")
    defer nodeA.Stop()
    nodeA.Register(bs.Addr())

    nodeB, _ := NewNode("127.0.0.1:0")
    defer nodeB.Stop()
    nodeB.Register(bs.Addr())

    peers, err := nodeA.ListPeers(bs.Addr())
    if err != nil {
        t.Fatal(err)
    }

    if len(peers) != 1 {
        t.Errorf("expected 1 peer, got %d", len(peers))
    }
    if peers[0].ID != nodeB.ID() {
        t.Errorf("expected %s, got %s", nodeB.ID(), peers[0].ID)
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./pkg/np4/ -v -run TestNodeListPeers
```

- [ ] **Step 3: Implement ListPeers**

Add to `node.go`:

```go
func (n *Node) ListPeers(bootstrapAddr string) ([]bootstrap.PeerInfo, error) {
    conn, err := n.transport.Connect(bootstrapAddr)
    if err != nil {
        return nil, err
    }
    defer conn.Close()

    msg := bootstrap.BootstrapMessage{
        Type:   "list_peers",
        NodeID: n.id,
    }
    data, _ := bootstrap.Serialize(msg)
    conn.Write(data)

    respData, err := conn.Read()
    if err != nil {
        return nil, err
    }

    var resp bootstrap.BootstrapMessage
    if err := bootstrap.Deserialize(respData, &resp); err != nil {
        return nil, err
    }

    if !resp.Success {
        return nil, errors.New("list peers failed: " + resp.Error)
    }

    return resp.Peers, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./pkg/np4/ -v -run TestNodeListPeers
```

- [ ] **Step 5: Commit**

```bash
git add go/pkg/np4/
git commit -m "feat: add ListPeers to node"
```

---

## Task 4: Node - RequestConnect with Approval

**Files:**
- Modify: `go/pkg/np4/node.go`
- Modify: `go/pkg/np4/node_test.go`

- [ ] **Step 1: Write failing test**

Add to `go/pkg/np4/node_test.go`:

```go
func TestNodeRequestConnect(t *testing.T) {
    bs, _ := bootstrap.NewBootstrapServer()
    defer bs.Stop()
    bs.Start("127.0.0.1:0")

    nodeA, _ := NewNode("127.0.0.1:0")
    defer nodeA.Stop()
    nodeA.Register(bs.Addr())

    nodeB, _ := NewNode("127.0.0.1:0")
    defer nodeB.Stop()
    nodeB.Register(bs.Addr())

    // B approves all requests
    nodeB.OnApprovalRequest(func(info bootstrap.PeerInfo) bool {
        return true
    })

    approved, err := nodeA.RequestConnect(bs.Addr(), nodeB.ID())
    if err != nil {
        t.Fatal(err)
    }

    if !approved {
        t.Error("expected approved")
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./pkg/np4/ -v -run TestNodeRequestConnect
```

- [ ] **Step 3: Implement RequestConnect and OnApprovalRequest**

Add `approvalHandler` field to Node:

```go
type Node struct {
    // ... existing fields ...
    approvalHandler func(bootstrap.PeerInfo) bool
}
```

Add methods:

```go
func (n *Node) OnApprovalRequest(handler func(bootstrap.PeerInfo) bool) {
    n.mu.Lock()
    n.approvalHandler = handler
    n.mu.Unlock()
}

func (n *Node) RequestConnect(bootstrapAddr, peerID string) (bool, error) {
    conn, err := n.transport.Connect(bootstrapAddr)
    if err != nil {
        return false, err
    }
    defer conn.Close()

    reqMsg := bootstrap.BootstrapMessage{
        Type:     "connect_request",
        NodeID:   n.id,
        TargetID: peerID,
    }
    data, _ := bootstrap.Serialize(reqMsg)
    conn.Write(data)

    respData, err := conn.Read()
    if err != nil {
        return false, err
    }

    var resp bootstrap.BootstrapMessage
    if err := bootstrap.Deserialize(respData, &resp); err != nil {
        return false, err
    }

    if !resp.Success {
        return false, errors.New("connect request failed: " + resp.Error)
    }

    return resp.Approved, nil
}
```

Update `handleConn` to handle incoming connect_request:

```go
// In handleConn, add to the probe section:
if probe.Type == "connect_request" {
    n.handleConnectRequest(data)
    return
}
```

Add `handleConnectRequest`:

```go
func (n *Node) handleConnectRequest(data []byte) {
    var reqMsg bootstrap.BootstrapMessage
    if bootstrap.Deserialize(data, &reqMsg) != nil {
        return
    }

    approved := false
    n.mu.RLock()
    handler := n.approvalHandler
    n.mu.RUnlock()

    if handler != nil {
        approved = handler(bootstrap.PeerInfo{
            ID:       reqMsg.NodeID,
            Addr:     reqMsg.Addr,
            PublicKey: reqMsg.PublicKey,
        })
    }

    resp := bootstrap.BootstrapMessage{
        Type:     "connect_response",
        Approved: approved,
        Success:  true,
    }
    respData, _ := bootstrap.Serialize(resp)
    // Need to send response back - but we need the connection
    // This requires storing the connection or using a different approach
    // For now, we'll handle this through the bootstrap relay
}
```

Wait - there's an issue. The Node receives the connect_request from Bootstrap, but needs to send the response back through the same connection. Let me fix the design.

Actually, looking at the Bootstrap implementation, Bootstrap connects to the target node, sends the request, and waits for a response on that connection. So the Node's handleConn receives the request and sends the response on the same connection. Let me update handleConn properly.

Update `handleConn` to handle connect_request responses:

```go
func (n *Node) handleConn(conn transport.Conn) {
    defer conn.Close()

    data, err := conn.Read()
    if err != nil {
        return
    }

    // Probe for message type
    var probe struct {
        Type string `json:"type"`
    }
    json.Unmarshal(data, &probe)

    switch probe.Type {
    case "key_exchange_request":
        n.handleKeyExchangeRequest(conn, data)
    case "connect_request":
        n.handleConnectRequest(conn, data)
    default:
        // Try encrypted message
        var encMsg EncryptedMessage
        if json.Unmarshal(data, &encMsg) == nil && encMsg.Ciphertext != nil {
            n.handleEncryptedMessage(encMsg)
            return
        }
        // Fall back to plain message
        var msg message.Message
        if json.Unmarshal(data, &msg) == nil {
            n.bus.Send(&msg)
        }
    }
}

func (n *Node) handleConnectRequest(conn transport.Conn, data []byte) {
    var reqMsg bootstrap.BootstrapMessage
    if bootstrap.Deserialize(data, &reqMsg) != nil {
        return
    }

    approved := false
    n.mu.RLock()
    handler := n.approvalHandler
    n.mu.RUnlock()

    if handler != nil {
        approved = handler(bootstrap.PeerInfo{
            ID:       reqMsg.NodeID,
            Addr:     reqMsg.Addr,
            PublicKey: reqMsg.PublicKey,
        })
    }

    resp := bootstrap.BootstrapMessage{
        Type:     "connect_response",
        Approved: approved,
        Success:  true,
    }
    respData, _ := bootstrap.Serialize(resp)
    conn.Write(respData)
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./pkg/np4/ -v -run TestNodeRequestConnect
```

- [ ] **Step 5: Commit**

```bash
git add go/pkg/np4/
git commit -m "feat: add RequestConnect and OnApprovalRequest to node"
```

---

## Task 5: Integration Test - Full Discovery and Approval Flow

**Files:**
- Modify: `go/pkg/np4/node_test.go`

- [ ] **Step 1: Write full integration test**

```go
func TestFullDiscoveryApprovalFlow(t *testing.T) {
    bs, _ := bootstrap.NewBootstrapServer()
    defer bs.Stop()
    bs.Start("127.0.0.1:0")

    nodeA, _ := NewNode("127.0.0.1:0")
    defer nodeA.Stop()
    nodeA.Register(bs.Addr())

    nodeB, _ := NewNode("127.0.0.1:0")
    defer nodeB.Stop()
    nodeB.Register(bs.Addr())

    // A discovers peers
    peers, err := nodeA.ListPeers(bs.Addr())
    if err != nil {
        t.Fatal(err)
    }
    if len(peers) != 1 || peers[0].ID != nodeB.ID() {
        t.Fatal("peer discovery failed")
    }

    // B approves connections
    nodeB.OnApprovalRequest(func(info bootstrap.PeerInfo) bool {
        return true
    })

    // A requests connection to B
    approved, err := nodeA.RequestConnect(bs.Addr(), nodeB.ID())
    if err != nil {
        t.Fatal(err)
    }
    if !approved {
        t.Fatal("connection not approved")
    }

    // Exchange keys and send encrypted message
    nodeA.ExchangeKeys(bs.Addr(), nodeB.ID())
    nodeB.ExchangeKeys(bs.Addr(), nodeA.ID())

    var received []byte
    var mu sync.Mutex
    nodeB.OnMessage(func(msg *message.Message) {
        mu.Lock()
        received = msg.Content
        mu.Unlock()
    })

    nodeA.SendEncrypted(nodeB.ID(), []byte("approved message"))
    time.Sleep(100 * time.Millisecond)

    mu.Lock()
    if string(received) != "approved message" {
        t.Errorf("expected 'approved message', got '%s'", string(received))
    }
    mu.Unlock()
}
```

- [ ] **Step 2: Run test to verify it passes**

```bash
go test ./pkg/np4/ -v -run TestFullDiscoveryApprovalFlow
```

- [ ] **Step 3: Commit**

```bash
git add go/pkg/np4/
git commit -m "test: add full discovery and approval flow integration test"
```

---

## Self-Review Checklist

- [ ] Bootstrap: handleListPeers returns online peers (excluding self)
- [ ] Bootstrap: handleConnectRequest relays request and approval
- [ ] Node: ListPeers queries Bootstrap for online peers
- [ ] Node: OnApprovalRequest registers callback
- [ ] Node: RequestConnect sends request and waits for approval
- [ ] Node: handleConn handles connect_request from Bootstrap
- [ ] Integration test covers discovery → approval → key exchange → encrypted chat
- [ ] All tests pass
