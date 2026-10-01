# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

**MANDATORY:** read and follow `AGENTS.md` — it contains the protocol-change
checklist (rebuild native libraries, test matrix, bridge ABI discipline,
e2e verification, spec sync, commit-then-push) that applies to every change
under `go/`. The commonest failure mode in this repo: fixing Go code and
leaving the clients' native libraries stale, so GUI clients silently run
the old protocol stack.

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

## Native Bridge & Flutter Client

`go/cmd/np4bridge` builds the np4 stack into a native library (4 exported
symbols, JSON-in/JSON-out) used by the Flutter client in `clients/flutter`
(Windows/macOS/Linux/Android — iOS deferred) and the PyQt6 desktop client in
`clients/pyqt` (ctypes, same library). Bridge logic lives in
`go/pkg/bridge` (pure Go, unit-tested); events are polled, never pushed via
callbacks. Build + app setup: `clients/flutter/tool/build_native.sh` and
`clients/flutter/tool/setup.sh`. Android builds need `-ldflags=-checklinkname=0`
(go-libp2p's anet dependency); zig cc cross-compiles the Windows DLL and
Linux .so.

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
