# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Np4Protocol is a Mixnet-based anonymous communication protocol with metadata protection. The Go implementation lives in the `go/` subdirectory with its own `go.mod` (module name: `Np4Protocol/go`).

## Development Commands

```bash
# All commands run from the go/ directory
cd go

# Run all tests
go test ./...

# Run tests for a specific package
go test ./pkg/crypto/ -v
go test ./pkg/transport/ -v -run TestTCPConnectAndListen

# Build CLI tools
go build -o bin/np4d ./cmd/np4d/
go build -o bin/np4cli ./cmd/np4cli/

# Regenerate protobuf code (requires protoc + protoc-gen-go)
protoc --go_out=. --go_opt=paths=source_relative ../proto/np4.proto
```

## Architecture

Four-layer protocol stack, each in its own package under `go/pkg/`:

```
Application  →  np4/node.go (wires everything together)
     ↓
Anonymous    →  mix/engine.go (batch shuffle with Fisher-Yates)
     ↓
Crypto       →  crypto/x25519.go + chacha20.go (X25519 key exchange, ChaCha20-Poly1305)
     ↓
Transport    →  transport/tcp.go (length-prefixed TCP framing)
```

Supporting packages:
- `router/` - Node discovery and random peer selection
- `message/` - Pub/sub message bus with async handler dispatch
- `proto/` - Generated protobuf types (not yet used in runtime; app uses JSON-serialized `message.Message`)

## Key Design Decisions

- **MixEngine** is generic (`MixEngine[T]`) and uses timer-based flush for partial batches
- **TCP framing**: 4-byte big-endian length header + payload, 1MB max message size
- **Thread safety**: `sync.RWMutex` in Router and MessageBus, `sync.Mutex` in MixEngine, `sync.Once` for Node.Stop()
- **Crypto**: Nonce (12 bytes) prepended to ciphertext in ChaCha20-Poly1305

## Protobuf

Source: `proto/np4.proto` (project root)
Generated: `go/pkg/proto/np4.pb.go`

Key types: `Envelope`, `Header`, `Payload`, `MixBatch`, `MessageType`, `ErrorCode`

## Current State

Prototype/MVP. The mix engine is wired into Node but `Send()` bypasses it (direct TCP). Protobuf types are generated but runtime uses JSON. No TLS on transport layer yet.
