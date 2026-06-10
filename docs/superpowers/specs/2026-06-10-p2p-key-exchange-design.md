# P2P Key Exchange via Bootstrap Server - Design Spec

## Overview

Add a BootstrapServer that facilitates X25519 key exchange between nodes. After key exchange, nodes communicate directly P2P with ChaCha20-Poly1305 encryption.

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                      BootstrapServer                        │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐        │
│  │ PeerRegistry│  │ KeyRelay    │  │ Transport   │        │
│  └─────────────┘  └─────────────┘  └─────────────┘        │
└─────────────────────────────────────────────────────────────┘
        ▲  注册/请求                    ▲  注册/请求
        │                               │
   ┌────┴────┐                    ┌────┴────┐
   │ Node A  │                    │ Node B  │
   │         │ ──── P2P 加密直连 ────→ │         │
   └─────────┘                    └─────────┘
```

**Flow:**
1. Node A, B connect to Bootstrap, register public keys and addresses
2. Node A requests key exchange with Node B
3. Bootstrap relays public keys between A and B
4. A, B compute shared secret independently
5. A, B disconnect from Bootstrap, communicate directly with encryption

## BootstrapServer

**Package:** `go/pkg/bootstrap/`

```go
type BootstrapServer struct {
    transport transport.Transport
    listener  transport.Listener
    peers     map[string]*PeerInfo
    mu        sync.RWMutex
}

type PeerInfo struct {
    ID        string
    Addr      string
    PublicKey  []byte
    Conn      transport.Conn  // non-nil when online
}
```

### Methods

| Method | Description |
|--------|-------------|
| `NewBootstrapServer()` | Constructor |
| `Start(addr string) error` | Start listening |
| `Stop()` | Stop server |
| `handleRegister(conn, msg)` | Register node (ID + addr + pubkey) |
| `handleRequestKeyExchange(conn, msg)` | Relay key exchange between nodes |
| `handleLookup(conn, msg)` | Query peer info |

### Message Protocol (JSON over TCP)

```go
type BootstrapMessage struct {
    Type      string  // "register", "key_exchange_request", "key_exchange_response", "lookup"
    NodeID    string
    Addr      string
    PublicKey  []byte
    TargetID  string  // for key_exchange_request
    Nonce     []byte  // 16 random bytes, replay protection
}

// P2P encrypted message format (sent directly between nodes)
type EncryptedMessage struct {
    SenderID  string
    Nonce     []byte   // 12 bytes for ChaCha20-Poly1305
    Ciphertext []byte
}
```

## Node Changes

### New Fields

```go
type Node struct {
    // ... existing fields ...
    peerKeys  map[string]*PeerSession
    mu        sync.RWMutex
}

type PeerSession struct {
    PeerID    string
    PeerAddr  string
    SharedKey []byte
    CreatedAt time.Time
}
```

### New Methods

| Method | Description |
|--------|-------------|
| `Register(bootstrapAddr string) error` | Connect to Bootstrap and register |
| `ExchangeKeys(bootstrapAddr, peerID string) error` | Exchange keys via Bootstrap |
| `SendEncrypted(destID string, content []byte) error` | Send encrypted message |
| `handleEncryptedConn(conn)` | Receive and decrypt message |

**Encryption/Decryption:** Uses `EncryptedMessage.SenderID` to look up `PeerSession.SharedKey` from `peerKeys` map. If sender unknown, message is dropped.

### Key Exchange Flow (Detailed)

```
Node A                    Bootstrap                    Node B
  │                          │                           │
  │ 1. register(id, addr, pubkey)                        │
  │ ───────────────────────→ │                           │
  │                          │ ←─────────────────────── │ 2. register(id, addr, pubkey)
  │                          │                           │
  │ 3. key_exchange_request(target=B)                    │
  │ ───────────────────────→ │                           │
  │                          │ 4. key_exchange_request(from=A, pubkey=A)
  │                          │ ────────────────────────→ │
  │                          │                           │
  │                          │ 5. key_exchange_response(from=B, pubkey=B)
  │                          │ ←──────────────────────── │
  │ 6. key_exchange_response(pubkey=B)                   │
  │ ←─────────────────────── │                           │
  │                          │                           │
  │ 7. compute shared key    │                           │ compute shared key
  │ 8. direct P2P encrypted ───────────────────────────→ │
```

## Error Handling

| Scenario | Handling |
|----------|----------|
| Bootstrap unreachable | Return connection error, no retry |
| Target node offline | Bootstrap returns error, Node returns `ErrPeerOffline` |
| Key exchange timeout | 10s timeout, return `ErrKeyExchangeTimeout` |
| Decryption failure | Return `ErrDecryptionFailed`, disconnect |
| Replay attack | Nonce check, reject duplicate requests |

## Testing

| Test | Content |
|------|---------|
| Unit | Bootstrap register/query, message serialization |
| Integration | A exchanges keys with B via Bootstrap, then P2P encrypted chat |
| Edge cases | Node offline, timeout, duplicate registration |
