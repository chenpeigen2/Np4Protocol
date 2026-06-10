# Peer Discovery and Approval Flow - Design Spec

## Overview

Add peer discovery via Bootstrap and a connection approval flow where the target node must approve before key exchange happens.

## Complete Flow

```
Node A                Bootstrap                Node B
  │                      │                       │
  │ 1. list_peers        │                       │
  │ ────────────────────→ │                       │
  │ ←──────────────────── │ 返回在线节点列表       │
  │                      │                       │
  │ 2. connect_request(target=B)                 │
  │ ────────────────────→ │                       │
  │                      │ 3. connect_request(from=A)
  │                      │ ─────────────────────→ │
  │                      │                       │
  │                      │ 4. B 的 OnApprovalRequest 回调
  │                      │    用户代码决定 approve/reject
  │                      │                       │
  │                      │ 5. connect_response(approved=true/false)
  │                      │ ←───────────────────── │
  │ 6. connect_response  │                       │
  │ ←──────────────────── │                       │
  │                      │                       │
  │ 7. 如果 approved:                             │
  │    ExchangeKeys → SendEncrypted               │
```

## Bootstrap Changes

### New Message Types

- `"list_peers"` - Query online peer list
- `"connect_request"` - Connection request (A→Bootstrap→B)
- `"connect_response"` - Connection response (B→Bootstrap→A)

### New Methods

| Method | Description |
|--------|-------------|
| `handleListPeers(conn, msg)` | Return online peers (excluding requester) |
| `handleConnectRequest(conn, msg)` | Forward request to target, wait for approval, relay response |

### Updated PeerInfo

```go
type PeerInfo struct {
    ID        string
    Addr      string
    PublicKey  []byte
    Nonce     []byte
    Conn      transport.Conn  // persistent connection for push messages
}
```

## Node Changes

### New Methods

| Method | Description |
|--------|-------------|
| `ListPeers(bootstrapAddr) ([]PeerInfo, error)` | Query online peers |
| `RequestConnect(bootstrapAddr, peerID) (bool, error)` | Request connection, wait for approval |
| `OnApprovalRequest(handler func(PeerInfo) bool)` | Register approval callback |

### New Callback

```go
type ApprovalHandler func(requester PeerInfo) bool
```

### Usage Example

```go
// Node B: register approval callback
nodeB.OnApprovalRequest(func(requester PeerInfo) bool {
    fmt.Printf("Node %s wants to connect. Approve?", requester.ID)
    return true // or false
})

// Node A: discover peers
peers, _ := nodeA.ListPeers(bootstrapAddr)
for _, p := range peers {
    fmt.Printf("Peer: %s\n", p.ID)
}

// Node A: request connection
approved, _ := nodeA.RequestConnect(bootstrapAddr, nodeB.ID())
if approved {
    nodeA.ExchangeKeys(bootstrapAddr, nodeB.ID())
    nodeA.SendEncrypted(nodeB.ID(), []byte("hello"))
}
```

## Error Handling

| Scenario | Handling |
|----------|----------|
| Target node offline | Bootstrap returns error immediately |
| Approval timeout | 30 seconds, returns rejected |
| Target rejects | Returns approved=false |
| Bootstrap unreachable | Return connection error |
