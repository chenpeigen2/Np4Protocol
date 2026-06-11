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
go test ./pkg/p2p/ -v
go test ./pkg/np4/ -v -run TestNodeSendReceive

# Build CLI tools
go build -o bin/np4d ./cmd/np4d/
go build -o bin/np4cli ./cmd/np4cli/

# Regenerate protobuf code (requires protoc + protoc-gen-go)
protoc --go_out=. --go_opt=paths=source_relative ../proto/np4.proto
```

## Architecture

Three-layer protocol stack using libp2p for P2P networking:

```
Application  →  np4/node.go (wires everything together)
     ↓
Anonymous    →  mix/engine.go (batch shuffle with Fisher-Yates)
     ↓
P2P Network  →  p2p/host.go + stream.go + discovery.go (go-libp2p)
```

libp2p provides transport (TCP), security (Noise: X25519 + ChaCha20-Poly1305), stream multiplexing (yamux), and peer discovery (mDNS) out of the box.

Supporting packages:
- `p2p/` - libp2p Host wrapper, stream helpers (length-prefixed framing), mDNS discovery
- `message/` - Pub/sub message bus with async handler dispatch
- `proto/` - Generated protobuf types (not yet used in runtime; app uses JSON-serialized `message.Message`)

## Key Design Decisions

- **libp2p** handles all transport, encryption, and peer discovery (Noise security = X25519 + ChaCha20-Poly1305)
- **MixEngine** is generic (`MixEngine[T]`) and uses timer-based flush for partial batches
- **Stream framing**: 4-byte big-endian length header + payload, 1MB max message size
- **Thread safety**: `sync.Mutex` in MixEngine, `sync.Once` for Node.Stop()
- **Protocol ID**: `/np4/message/1.0.0` for message streams

## Protobuf

Source: `proto/np4.proto` (project root)
Generated: `go/pkg/proto/np4.pb.go`

Key types: `Envelope`, `Header`, `Payload`, `MixBatch`, `MessageType`, `ErrorCode`

## Current State

Prototype/MVP. The mix engine is wired into Node but `Send()` bypasses it (direct stream). Protobuf types are generated but runtime uses JSON. libp2p Noise provides encrypted transport.
