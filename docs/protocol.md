# Np4Protocol Specification

## Overview

Np4Protocol is a Mixnet-based anonymous communication protocol designed for metadata protection. It provides sender/receiver anonymity by batching, shuffling, and delaying messages through relay nodes.

## Architecture

```
┌─────────────────────────────────────────────────────────┐
│  应用层    │  消息类型（异步/同步/广播/文件）              │
├─────────────────────────────────────────────────────────┤
│  匿名层    │  混洗引擎（Mixnet）- 批量混洗、重排序        │
├─────────────────────────────────────────────────────────┤
│  加密层    │  X25519 密钥交换 + ChaCha20-Poly1305 加密   │
├─────────────────────────────────────────────────────────┤
│  传输层    │  TCP 连接池 + Protobuf 序列化                │
└─────────────────────────────────────────────────────────┘
```

## Message Format

Messages are serialized using Protocol Buffers. See `proto/np4.proto` for the complete definition.

### Envelope

The network传输的基本单位:

- `payload`: Encrypted message content (bytes)
- `signature`: Optional Ed25519 signature (bytes)
- `header`: Plaintext routing information (Header)

### Header

- `type`: Message type (MessageType enum)
- `dest_id`: Destination node ID, may be pseudonym (bytes)
- `sender_id`: Sender's pseudonym ID (bytes)
- `timestamp`: Message creation time (uint64)
- `ttl`: Time-to-live in hops (uint32)
- `version`: Protocol version, currently 1 (uint32)

### Message Types

| Type | Value | Description |
|------|-------|-------------|
| ASYNC_MSG | 0 | Asynchronous message |
| SYNC_REQUEST | 1 | Synchronous request |
| SYNC_RESPONSE | 2 | Synchronous response |
| BROADCAST | 3 | Broadcast to all nodes |
| FILE_CHUNK | 4 | File transfer chunk |
| MIX_CTRL | 5 | Mix engine control |
| KEY_EXCHANGE | 6 | Key exchange |
| PEER_DISCOVERY | 7 | Peer discovery |

### Payload

The decrypted inner content:

- `content`: Actual message content (bytes)
- `session_key`: Session key for multi-hop (bytes)
- `hop_count`: Number of hops traversed (uint32)
- `next_hop`: Next hop address (bytes)

### MixBatch

Batch of shuffled messages:

- `batch_id`: Unique batch identifier (uint64)
- `messages`: Shuffled Envelope list (repeated Envelope)
- `proof`: HMAC-SHA256 batch integrity proof (bytes)

## Encryption

### Key Hierarchy

```
长期密钥（Identity Key）
    ↓ X25519 协商
会话密钥（Session Key）
    ↓ HKDF-SHA256 派生
消息密钥（Message Key）← 每条消息唯一
```

### Algorithms

| Operation | Algorithm | Notes |
|-----------|-----------|-------|
| Key Exchange | X25519 | ECDH, 32-byte public keys |
| Symmetric Encryption | ChaCha20-Poly1305 | AEAD, 12-byte nonce |
| Key Derivation | HKDF-SHA256 | Derive session keys |
| Signatures | Ed25519 | Node identity signing |

### Message Encryption

1. Generate random 12-byte nonce
2. Encrypt plaintext with ChaCha20-Poly1305 using session key
3. Prepend nonce to ciphertext
4. Result: `nonce || ciphertext || tag`

### Multi-Hop Encryption (Onion)

For messages traversing multiple MixNodes:

```
original → encrypt(key3) → encrypt(key2) → encrypt(key1)
```

Each MixNode decrypts one layer to reveal the next hop.

## Mix Engine

Messages are collected in batches and shuffled before forwarding to prevent traffic analysis.

### Parameters

| Parameter | Default | Description |
|-----------|---------|-------------|
| batch_size | 10 | Messages per batch |
| max_delay | 500ms | Maximum wait before flush |
| padding | 4KB | Fixed message length |

### Process

1. Messages arrive and are buffered
2. When buffer reaches batch_size OR max_delay expires:
   - Fisher-Yates shuffle the buffer
   - Pad all messages to 4KB
   - Forward entire batch
3. Dummy messages sent periodically to maintain constant traffic

### Anti-Traffic-Analysis

- **Padding**: All messages padded to 4KB to eliminate size fingerprinting
- **Dummy traffic**: MixNodes generate cover traffic every 5-10 seconds
- **Batch forwarding**: Messages in same window sent simultaneously
- **Constant rate**: MixNodes send at steady rate regardless of real traffic

## Node Types

- **Client**: End-user node that sends/receives messages
- **MixNode**: Relay node that shuffles and forwards messages
- **Bootstrap**: Entry point for new nodes joining the network

## Error Codes

| Code | Value | Description |
|------|-------|-------------|
| OK | 0 | Success |
| INVALID_FORMAT | 1 | Malformed message |
| DECRYPT_FAILED | 2 | Decryption failure |
| KEY_MISMATCH | 3 | Key mismatch |
| TTL_EXPIRED | 4 | Message expired |
| BATCH_FULL | 5 | Batch buffer full |
| UNKNOWN_NODE | 6 | Unknown destination |

## Transport

- Protocol: TCP with length-prefixed framing
- Frame format: 4-byte big-endian length + payload
- Connection management: Connection pooling with automatic reconnection
