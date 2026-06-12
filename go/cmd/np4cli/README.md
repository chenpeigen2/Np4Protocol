# np4cli

Np4Protocol P2P anonymous communication client. Built on libp2p with Noise encryption (X25519 + ChaCha20-Poly1305) and DHT-based peer discovery.

## Build

```bash
cd go
go build -o bin/np4cli ./cmd/np4cli/
```

## Quick Start

### 1. Start a bootstrap node

```bash
go build -o bin/bootstrap ./cmd/bootstrap/
./bin/bootstrap -port 4000
# Output:
# Bootstrap node started
# Peer ID: 12D3KooW...
# Addresses:
#   /ip4/127.0.0.1/tcp/4000/p2p/12D3KooW...
```

### 2. Start clients

```bash
# Terminal A
./bin/np4cli --port 4002 --bootstrap /ip4/127.0.0.1/tcp/4000/p2p/12D3KooW... chat

# Terminal B
./bin/np4cli --port 4003 --bootstrap /ip4/127.0.0.1/tcp/4000/p2p/12D3KooW... chat
```

### 3. Discover and chat

In Terminal A:
```
> peers
  12D3KooW...  [/ip4/127.0.0.1/tcp/4003/...]
1 peer(s) discovered

> connect /ip4/127.0.0.1/tcp/4003/p2p/12D3KooW...
Connected to 12D3KooW...

> send 12D3KooW... hello from A
Sent to 12D3KooW...
```

In Terminal B:
```
[14:32:01] 12D3KooW...: hello from A
>
```

## Commands

### Subcommands

| Command | Description |
|---------|-------------|
| `np4cli id` | Show this node's peer ID and addresses |
| `np4cli peers` | Discover online peers via DHT |
| `np4cli connect <multiaddr>` | Connect to a peer |
| `np4cli send <peer-id> <message>` | Send a message to a peer |
| `np4cli chat` | Enter interactive chat mode |

### Global Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--port` | `0` (random) | TCP listen port |
| `--bootstrap` | | Bootstrap node multiaddr (enables DHT discovery) |
| `--rendezvous` | `np4-network` | DHT rendezvous string for peer discovery |

### Chat Mode Commands

| Command | Description |
|---------|-------------|
| `peers` | Discover online peers |
| `connect <multiaddr>` | Connect to a peer |
| `send <peer-id> <message>` | Send a message |
| `id` | Show this node's info |
| `help` | Show available commands |
| `quit` / `exit` | Exit |

## Architecture

```
np4cli (cobra CLI)
  └── np4.Node
        ├── libp2p Host (TCP + Noise encryption)
        ├── Kademlia DHT (peer discovery)
        ├── Stream handler (/np4/message/1.0.0)
        └── MessageBus (pub/sub message dispatch)
```

All connections are encrypted via libp2p's Noise protocol (X25519 key exchange + ChaCha20-Poly1305). Peer discovery uses Kademlia DHT with a rendezvous string.

## Multiaddr Format

libp2p uses multiaddr to represent network addresses:

```
/ip4/127.0.0.1/tcp/4000/p2p/12D3KooW...
└─ IP ─┘          └port┘    └─ peer ID ─┘
```

The `id` command prints your full multiaddrs. Use these for the `connect` command and `--bootstrap` flag.
