# Mixnet Deep Refactor Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Wire the existing (dead) MixEngine into the live Send path with a working layered-onion protocol, fix all critical bugs (bootstrap id, MessageBus goroutine explosion, MixEngine timer leak), and ship end-to-end anonymous messaging through 3-hop relay paths.

**Architecture:** New `pkg/onion` does layered ECIES (X25519 + ChaCha20-Poly1305) so each relay can only decrypt one layer. `pkg/pathsel` queries the DHT (`"np4-relay"` rendezvous + `/np4/ecdh/<peerID>` records) and picks N hops. `pkg/identity` persists an Ed25519 keypair so `bootstrap id` is stable. `pkg/np4.Node.Send` enqueues the built onion into a `MixEngine` that batches + shuffles before forwarding. The MessageBus becomes a worker pool with no Broadcast side effects.

**Tech Stack:** go-libp2p v0.48 (Noise, DHT, mDNS), go-libp2p-kad-dht, spf13/cobra, gin-gonic/gin, golang.org/x/crypto/curve25519 + chacha20poly1305, hkdf.

---

## File Structure

**New files:**
- `go/pkg/identity/identity.go` + `identity_test.go` — persistent Ed25519 + X25519 ECDH
- `go/pkg/onion/onion.go` + `onion_test.go` + `fuzz_test.go` — layered encryption
- `go/pkg/pathsel/pathsel.go` + `pathsel_test.go` — DHT-aware path selection
- `go/cmd/np4cli/cmd_relay.go` — relay-mode subcommand
- `go/cmd/np4cli/cmd_path.go` — path inspection subcommand

**Modified files:**
- `go/pkg/mix/engine.go` — add `Close()`, secure RNG, sync `flush`
- `go/pkg/message/bus.go` — worker pool, side-effect-free `Broadcast`
- `go/pkg/p2p/host.go` — `NewHostWithIdentity(identity, port)`
- `go/pkg/p2p/stream.go` — `WriteMsgCtx`/`ReadMsgCtx` with deadline
- `go/pkg/np4/node.go` — rewrite: onion send, relay handler, options pattern
- `go/cmd/np4cli/root.go` — remove global `node`, use `cmd.Context()`
- `go/cmd/np4cli/cmd_send.go` — `--direct`, `--hops` flags
- `go/cmd/np4cli/cmd_chat.go` — slim down (use new helpers)
- `go/cmd/np4cli/cmd_id.go`, `cmd_peers.go`, `cmd_connect.go` — adapt to new Node API
- `go/cmd/bootstrap/cmd_id.go`, `cmd_start.go` — use `identity.LoadOrCreate`
- `go/shell/*.sh` — replace `bootstrap id` with reading `bootstrap start` stdout

**Deleted files:**
- `printPeerID` dead function in `cmd/np4cli/root.go`

---

## Phase 1: Identity Package + Fix bootstrap id

### Task 1.1: Skeleton identity package with failing test

**Files:**
- Create: `go/pkg/identity/identity.go`
- Create: `go/pkg/identity/identity_test.go`

- [ ] **Step 1: Write the failing test**

```go
// go/pkg/identity/identity_test.go
package identity

import (
	"path/filepath"
	"testing"
)

func TestLoadOrCreatePersists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "id")

	id1, err := LoadOrCreate(path)
	if err != nil {
		t.Fatalf("first LoadOrCreate: %v", err)
	}
	id2, err := LoadOrCreate(path)
	if err != nil {
		t.Fatalf("second LoadOrCreate: %v", err)
	}
	if id1.PeerID() != id2.PeerID() {
		t.Errorf("identity not persistent: %s vs %s", id1.PeerID(), id2.PeerID())
	}
}

func TestECDHSymmetric(t *testing.T) {
	a, _ := LoadOrCreate(t.TempDir()+"/a")
	b, _ := LoadOrCreate(t.TempDir()+"/b")

	s1, err := a.ECDH(b.ECDHPub())
	if err != nil {
		t.Fatalf("a.ECDH(b): %v", err)
	}
	s2, err := b.ECDH(a.ECDHPub())
	if err != nil {
		t.Fatalf("b.ECDH(a): %v", err)
	}
	if string(s1) != string(s2) {
		t.Errorf("ECDH not symmetric: %x vs %x", s1, s2)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /Users/chenpeigen/GolandProjects/Np4Protocol/go
go test ./pkg/identity/ -v
```
Expected: FAIL with "package identity is not in std" or compile error.

- [ ] **Step 3: Write minimal implementation**

```go
// go/pkg/identity/identity.go
package identity

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
	"golang.org/x/crypto/curve25519"
)

const ecdhPubSize = 32

type Identity struct {
	priv       crypto.PrivKey
	ecdhPriv   []byte // X25519 private (derived from ed25519 seed)
	ecdhPub    []byte // X25519 public
}

func LoadOrCreate(path string) (*Identity, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("mkdir: %w", err)
	}

	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		_, edPriv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return nil, fmt.Errorf("generate ed25519: %w", err)
		}
		data = edPriv.Seed()
		if err := os.WriteFile(path, data, 0o600); err != nil {
			return nil, fmt.Errorf("write identity: %w", err)
		}
		return fromSeed(data)
	}
	if err != nil {
		return nil, fmt.Errorf("read identity: %w", err)
	}
	return fromSeed(data)
}

func fromSeed(seed []byte) (*Identity, error) {
	if len(seed) != ed25519.SeedSize {
		return nil, fmt.Errorf("invalid seed size: %d", ed25519.SeedSize)
	}
	edPriv := ed25519.NewKeyFromSeed(seed)
	libp2pPriv, _, err := crypto.KeyPairFromStdKey(&edPriv)
	if err != nil {
		return nil, fmt.Errorf("convert to libp2p key: %w", err)
	}

	// Derive X25519 private from ed25519 seed (clamp per RFC 7748).
	// Hash the seed first so ed25519 structure doesn't leak into X25519.
	ecdhPriv, err := deriveX25519Priv(seed)
	if err != nil {
		return nil, err
	}
	ecdhPub, err := curve25519.X25519(ecdhPriv, curve25519.Basepoint)
	if err != nil {
		return nil, fmt.Errorf("derive x25519 pub: %w", err)
	}

	return &Identity{
		priv:     libp2pPriv,
		ecdhPriv: ecdhPriv,
		ecdhPub:  ecdhPub,
	}, nil
}

func (i *Identity) PeerID() peer.ID {
	pid, _ := peer.IDFromPrivateKey(i.priv)
	return pid
}

func (i *Identity) PrivKey() crypto.PrivKey { return i.priv }

func (i *Identity) ECDHPub() []byte {
	out := make([]byte, ecdhPubSize)
	copy(out, i.ecdhPub)
	return out
}

func (i *Identity) ECDH(theirPub []byte) ([]byte, error) {
	if len(theirPub) != ecdhPubSize {
		return nil, fmt.Errorf("invalid pubkey size: %d", len(theirPub))
	}
	return curve25519.X25519(i.ecdhPriv, theirPub)
}

func (i *Identity) HexShort() string {
	return hex.EncodeToString(i.ecdhPub[:4])
}
```

```go
// Append to go/pkg/identity/identity.go
import (
	// ... existing imports ...
	"crypto/sha512"
)

func deriveX25519Priv(ed25519Seed []byte) ([]byte, error) {
	h := sha512.Sum512(ed25519Seed)
	scalar := h[:32]
	// Clamp per RFC 7748 section 5.
	scalar[0] &= 248
	scalar[31] &= 127
	scalar[31] |= 64
	return scalar, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./pkg/identity/ -v
```
Expected: PASS for both tests.

- [ ] **Step 5: Commit**

```bash
git add go/pkg/identity/
git commit -m "feat(identity): persistent Ed25519 + X25519 ECDH package"
```

### Task 1.2: Use identity in p2p.NewHost

**Files:**
- Modify: `go/pkg/p2p/host.go`
- Create: `go/pkg/p2p/host_test.go` (append if exists)

- [ ] **Step 1: Write the failing test**

```go
// go/pkg/p2p/host_test.go (append)
func TestNewHostWithIdentityStable(t *testing.T) {
	dir := t.TempDir()
	id1, _ := identity.LoadOrCreate(dir + "/a")
	id2, _ := identity.LoadOrCreate(dir + "/a") // same file

	h1, err := NewHostWithIdentity(id1, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer h1.Close()

	h2, err := NewHostWithIdentity(id2, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer h2.Close()

	if h1.ID() != h2.ID() {
		t.Errorf("identity-derived peer IDs differ: %s vs %s", h1.ID(), h2.ID())
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./pkg/p2p/ -v -run TestNewHostWithIdentityStable
```
Expected: FAIL "undefined: NewHostWithIdentity".

- [ ] **Step 3: Implement NewHostWithIdentity**

```go
// go/pkg/p2p/host.go (modify)
package p2p

import (
	"context"
	"fmt"

	"Np4Protocol/go/pkg/identity"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/security/noise"
	"github.com/libp2p/go-libp2p/p2p/transport/tcp"
	"github.com/multiformats/go-multiaddr"
)

func NewHost(port int) (host.Host, error) {
	addr := fmt.Sprintf("/ip4/0.0.0.0/tcp/%d", port)
	return libp2p.New(
		libp2p.ListenAddrStrings(addr),
		libp2p.Security(noise.ID, noise.New),
		libp2p.Transport(tcp.NewTCPTransport),
	)
}

func NewHostWithIdentity(id *identity.Identity, port int) (host.Host, error) {
	addr := fmt.Sprintf("/ip4/0.0.0.0/tcp/%d", port)
	return libp2p.New(
		libp2p.Identity(id.PrivKey()),
		libp2p.ListenAddrStrings(addr),
		libp2p.Security(noise.ID, noise.New),
		libp2p.Transport(tcp.NewTCPTransport),
	)
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./pkg/p2p/ -v -run TestNewHostWithIdentityStable
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add go/pkg/p2p/host.go go/pkg/p2p/host_test.go
git commit -m "feat(p2p): NewHostWithIdentity for stable peer IDs"
```

### Task 1.3: Fix bootstrap id to use persistent identity

**Files:**
- Modify: `go/cmd/bootstrap/cmd_id.go`
- Modify: `go/cmd/bootstrap/cmd_start.go` (use the same identity)
- Modify: `go/cmd/bootstrap/root.go` (add `--identity` flag)

- [ ] **Step 1: Write a manual verification (shell)**

Run twice and compare Peer IDs — this is the bug we're fixing. We will verify via shell after implementation.

- [ ] **Step 2: Add `--identity` flag**

```go
// go/cmd/bootstrap/root.go
package main

import (
	"github.com/spf13/cobra"
)

var (
	port       int
	identityPath string
)

var rootCmd = &cobra.Command{
	Use:   "np4bootstrap",
	Short: "Np4Protocol DHT bootstrap node",
	Long:  "Np4Protocol bootstrap node - a long-lived DHT server that helps peers discover each other",
}

func init() {
	defaultPath := os.ExpandEnv("$HOME/.np4/identity")
	rootCmd.PersistentFlags().IntVar(&port, "port", 4000, "TCP listen port")
	rootCmd.PersistentFlags().StringVar(&identityPath, "identity", defaultPath, "Path to persistent identity file")
}
```

```go
// Append imports in root.go
import "os"
```

- [ ] **Step 3: Rewrite cmd_id.go to use identity**

```go
// go/cmd/bootstrap/cmd_id.go
package main

import (
	"Np4Protocol/go/pkg/identity"
	"fmt"
	"github.com/multiformats/go-multiaddr"
	"github.com/spf13/cobra"
)

var idCmd = &cobra.Command{
	Use:   "id",
	Short: "Show bootstrap node's peer ID and multiaddr (from persistent identity)",
	RunE: func(cmd *cobra.Command, args []string) error {
		id, err := identity.LoadOrCreate(identityPath)
		if err != nil {
			return fmt.Errorf("load identity: %w", err)
		}
		addr, _ := multiaddr.NewMultiaddr(fmt.Sprintf("/ip4/127.0.0.1/tcp/%d", port))
		full, _ := multiaddr.NewMultiaddrBytes(append(addr.Bytes(), []byte("/p2p/"+id.PeerID().String())...))
		fmt.Printf("Peer ID: %s\n", id.PeerID())
		fmt.Printf("Multiaddr: %s\n", full.String())
		return nil
	},
}

func init() {
	rootCmd.AddCommand(idCmd)
}
```

- [ ] **Step 4: Update cmd_start.go to use identity**

```go
// go/cmd/bootstrap/cmd_start.go (modify the top of RunE)
RunE: func(cmd *cobra.Command, args []string) error {
	id, err := identity.LoadOrCreate(identityPath)
	if err != nil {
		return fmt.Errorf("load identity: %w", err)
	}
	h, err := p2p.NewHostWithIdentity(id, port)
	if err != nil {
		return fmt.Errorf("failed to create host: %w", err)
	}
	defer h.Close()
	// ... rest unchanged ...
}
```
Add `"Np4Protocol/go/pkg/identity"` and `"Np4Protocol/go/pkg/p2p"` to imports; remove the now-unused host.NewHost call.

- [ ] **Step 5: Build and verify**

```bash
cd /Users/chenpeigen/GolandProjects/Np4Protocol/go
go build -o bin/bootstrap ./cmd/bootstrap/
./bin/bootstrap id --port 4400 --identity /tmp/np4_test_id
./bin/bootstrap id --port 4400 --identity /tmp/np4_test_id
```
Expected: both invocations print the **same** Peer ID.

- [ ] **Step 6: Commit**

```bash
git add go/cmd/bootstrap/
git commit -m "fix(bootstrap): persistent identity makes 'bootstrap id' stable"
```

---

## Phase 2: Onion Package

### Task 2.1: Onion wire format and Build/Decode skeleton with failing test

**Files:**
- Create: `go/pkg/onion/onion.go`
- Create: `go/pkg/onion/onion_test.go`

- [ ] **Step 1: Write the failing test**

```go
// go/pkg/onion/onion_test.go
package onion

import (
	"bytes"
	"path/filepath"
	"testing"

	"Np4Protocol/go/pkg/identity"
)

func TestBuildDecodeSingleHop(t *testing.T) {
	dest, _ := identity.LoadOrCreate(filepath.Join(t.TempDir(), "dest"))

	payload := []byte("hello final")
	on, err := Build([]Hop{{PeerID: dest.PeerID(), ECDHPub: dest.ECDHPub()}}, payload)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	dec, err := Decode(on.Bytes(), dest)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if !dec.IsFinal {
		t.Errorf("expected IsFinal=true")
	}
	if !bytes.Equal(dec.Inner, payload) {
		t.Errorf("payload mismatch: got %q want %q", dec.Inner, payload)
	}
}

func TestBuildDecodeMultiHop(t *testing.T) {
	r1, _ := identity.LoadOrCreate(filepath.Join(t.TempDir(), "r1"))
	r2, _ := identity.LoadOrCreate(filepath.Join(t.TempDir(), "r2"))
	dest, _ := identity.LoadOrCreate(filepath.Join(t.TempDir(), "dest"))

	payload := []byte("multi-hop secret")
	hops := []Hop{
		{PeerID: r1.PeerID(), ECDHPub: r1.ECDHPub()},
		{PeerID: r2.PeerID(), ECDHPub: r2.ECDHPub()},
		{PeerID: dest.PeerID(), ECDHPub: dest.ECDHPub()},
	}
	on, err := Build(hops, payload)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	// r1 decodes outer
	d1, err := Decode(on.Bytes(), r1)
	if err != nil {
		t.Fatalf("r1 Decode: %v", err)
	}
	if d1.IsFinal {
		t.Fatal("r1 should not be final")
	}
	if d1.NextHop != r2.PeerID() {
		t.Errorf("r1 NextHop: got %s want %s", d1.NextHop, r2.PeerID())
	}

	// r2 decodes middle
	d2, err := Decode(d1.Inner, r2)
	if err != nil {
		t.Fatalf("r2 Decode: %v", err)
	}
	if d2.IsFinal {
		t.Fatal("r2 should not be final")
	}
	if d2.NextHop != dest.PeerID() {
		t.Errorf("r2 NextHop: got %s want %s", d2.NextHop, dest.PeerID())
	}

	// dest decodes final
	d3, err := Decode(d2.Inner, dest)
	if err != nil {
		t.Fatalf("dest Decode: %v", err)
	}
	if !d3.IsFinal {
		t.Fatal("dest should be final")
	}
	if !bytes.Equal(d3.Inner, payload) {
		t.Errorf("final payload: got %q want %q", d3.Inner, payload)
	}
}

func TestDecodeWrongKeyFails(t *testing.T) {
	r1, _ := identity.LoadOrCreate(filepath.Join(t.TempDir(), "r1"))
	other, _ := identity.LoadOrCreate(filepath.Join(t.TempDir(), "other"))

	on, _ := Build([]Hop{{PeerID: r1.PeerID(), ECDHPub: r1.ECDHPub()}}, []byte("x"))
	if _, err := Decode(on.Bytes(), other); err == nil {
		t.Fatal("expected error with wrong key")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./pkg/onion/ -v
```
Expected: FAIL "package onion not found".

- [ ] **Step 3: Write minimal implementation**

```go
// go/pkg/onion/onion.go
package onion

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"

	"Np4Protocol/go/pkg/identity"

	"github.com/libp2p/go-libp2p/core/peer"
	"golang.org/x/crypto/chacha20poly1305"
	"golang.org/x/crypto/curve25519"
	"golang.org/x/crypto/hkdf"
)

const (
	ephPubSize = 32
	nonceSize  = 12
	saltString = "np4-onion-v1"
)

const (
	flagRelay = 0
	flagFinal = 1
)

type Hop struct {
	PeerID  peer.ID
	ECDHPub []byte
}

type Onion struct{ data []byte }

func (o *Onion) Bytes() []byte { return o.data }

type Decoded struct {
	IsFinal bool
	NextHop peer.ID
	Inner   []byte
}

// Build constructs an onion by encrypting from the last hop down to the first.
// Innermost layer wraps finalPayload with flagFinal; each outer layer wraps the
// previous ciphertext with flagRelay + next_hop_peer_id.
func Build(path []Hop, finalPayload []byte) (*Onion, error) {
	if len(path) == 0 {
		return nil, errors.New("empty path")
	}

	// Innermost: flagFinal || payload
	current := append([]byte{flagFinal}, finalPayload...)

	for i := len(path) - 1; i >= 0; i-- {
		var plaintext []byte
		if i == len(path)-1 {
			// Wrapping the final layer: plaintext is already flagFinal || payload.
			plaintext = current
		} else {
			// Wrapping an intermediate layer: flagRelay || next_hop_len || next_hop || current
			nextHopBytes := []byte(path[i+1].PeerID)
			lenBuf := make([]byte, 4)
			binary.BigEndian.PutUint32(lenBuf, uint32(len(nextHopBytes)))
			plaintext = append([]byte{flagRelay}, lenBuf...)
			plaintext = append(plaintext, nextHopBytes...)
			plaintext = append(plaintext, current...)
		}
		layer, err := encryptLayer(path[i], plaintext)
		if err != nil {
			return nil, fmt.Errorf("layer %d: %w", i, err)
		}
		current = layer
	}
	return &Onion{data: current}, nil
}

// Decode peels one layer using the recipient's identity.
func Decode(packet []byte, id *identity.Identity) (*Decoded, error) {
	plaintext, err := decryptLayer(packet, id)
	if err != nil {
		return nil, fmt.Errorf("decrypt: %w", err)
	}
	if len(plaintext) < 1 {
		return nil, errors.New("decrypted layer too short")
	}
	flag := plaintext[0]
	rest := plaintext[1:]

	switch flag {
	case flagFinal:
		out := make([]byte, len(rest))
		copy(out, rest)
		return &Decoded{IsFinal: true, Inner: out}, nil
	case flagRelay:
		if len(rest) < 4 {
			return nil, errors.New("relay layer too short for length prefix")
		}
		nextLen := binary.BigEndian.Uint32(rest[:4])
		if uint64(len(rest)) < 4+uint64(nextLen) {
			return nil, errors.New("relay layer truncated")
		}
		nextHopBytes := rest[4 : 4+nextLen]
		nextHop, err := peer.IDFromBytes(nextHopBytes)
		if err != nil {
			return nil, fmt.Errorf("parse next hop: %w", err)
		}
		inner := make([]byte, len(rest)-4-int(nextLen))
		copy(inner, rest[4+nextLen:])
		return &Decoded{IsFinal: false, NextHop: nextHop, Inner: inner}, nil
	default:
		return nil, fmt.Errorf("unknown layer flag: %d", flag)
	}
}

func encryptLayer(hop Hop, plaintext []byte) ([]byte, error) {
	ephPriv := make([]byte, 32)
	if _, err := rand.Read(ephPriv); err != nil {
		return nil, err
	}
	ephPriv[0] &= 248
	ephPriv[31] &= 127
	ephPriv[31] |= 64
	ephPub, err := curve25519.X25519(ephPriv, curve25519.Basepoint)
	if err != nil {
		return nil, err
	}
	shared, err := curve25519.X25519(ephPriv, hop.ECDHPub)
	if err != nil {
		return nil, err
	}
	key, err := deriveKey(shared, []byte(hop.PeerID))
	if err != nil {
		return nil, err
	}
	aead, err := chacha20poly1305.New(key)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, nonceSize)
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	ciphertext := aead.Seal(nil, nonce, plaintext, nil)

	out := make([]byte, 0, ephPubSize+nonceSize+len(ciphertext))
	out = append(out, ephPub...)
	out = append(out, nonce...)
	out = append(out, ciphertext...)
	return out, nil
}

func decryptLayer(packet []byte, id *identity.Identity) ([]byte, error) {
	if len(packet) < ephPubSize+nonceSize+chacha20poly1305.Overhead {
		return nil, errors.New("packet too short")
	}
	ephPub := packet[:ephPubSize]
	nonce := packet[ephPubSize : ephPubSize+nonceSize]
	ciphertext := packet[ephPubSize+nonceSize:]

	shared, err := id.ECDH(ephPub)
	if err != nil {
		return nil, err
	}
	key, err := deriveKey(shared, []byte(id.PeerID()))
	if err != nil {
		return nil, err
	}
	aead, err := chacha20poly1305.New(key)
	if err != nil {
		return nil, err
	}
	plaintext, err := aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, err
	}
	return plaintext, nil
}

func deriveKey(shared, info []byte) ([]byte, error) {
	key := make([]byte, chacha20poly1305.KeySize)
	if _, err := hkdf.New(sha256.New, shared, []byte(saltString), info).Read(key); err != nil {
		return nil, err
	}
	return key, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./pkg/onion/ -v
```
Expected: PASS for all three tests (TestBuildDecodeSingleHop, TestBuildDecodeMultiHop, TestDecodeWrongKeyFails).

- [ ] **Step 5: Commit**

```bash
git add go/pkg/onion/
git commit -m "feat(onion): layered ECIES build/decode with multi-hop support"
```

```go
// go/pkg/onion/onion.go (REPLACE)
package onion

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"

	"Np4Protocol/go/pkg/identity"

	"github.com/libp2p/go-libp2p/core/peer"
	"golang.org/x/crypto/chacha20poly1305"
	"golang.org/x/crypto/curve25519"
	"golang.org/x/crypto/hkdf"
)

const (
	ephPubSize = 32
	nonceSize  = 12
	saltString = "np4-onion-v1"
)

const (
	flagRelay = 0
	flagFinal = 1
)

type Hop struct {
	PeerID  peer.ID
	ECDHPub []byte
}

type Onion struct{ data []byte }

func (o *Onion) Bytes() []byte { return o.data }

type Decoded struct {
	IsFinal bool
	NextHop peer.ID
	Inner   []byte
}

func Build(path []Hop, finalPayload []byte) (*Onion, error) {
	if len(path) == 0 {
		return nil, errors.New("empty path")
	}
	current := append([]byte{flagFinal}, finalPayload...)
	for i := len(path) - 1; i >= 0; i-- {
		layer, err := encryptLayer(path[i], current)
		if err != nil {
			return nil, fmt.Errorf("layer %d: %w", i, err)
		}
		current = layer
	}
	return &Onion{data: current}, nil
}

func Decode(packet []byte, id *identity.Identity) (*Decoded, error) {
	plaintext, err := decryptLayer(packet, id)
	if err != nil {
		return nil, fmt.Errorf("decrypt: %w", err)
	}
	if len(plaintext) < 1 {
		return nil, errors.New("decrypted layer too short")
	}
	flag := plaintext[0]
	rest := plaintext[1:]

	switch flag {
	case flagFinal:
		out := make([]byte, len(rest))
		copy(out, rest)
		return &Decoded{IsFinal: true, Inner: out}, nil
	case flagRelay:
		if len(rest) < 4 {
			return nil, errors.New("relay layer too short for length prefix")
		}
		nextLen := binary.BigEndian.Uint32(rest[:4])
		if uint64(len(rest)) < 4+uint64(nextLen) {
			return nil, errors.New("relay layer truncated")
		}
		nextHopBytes := rest[4 : 4+nextLen]
		nextHop, err := peer.IDFromBytes(nextHopBytes)
		if err != nil {
			return nil, fmt.Errorf("parse next hop: %w", err)
		}
		inner := make([]byte, len(rest)-4-int(nextLen))
		copy(inner, rest[4+nextLen:])
		return &Decoded{IsFinal: false, NextHop: nextHop, Inner: inner}, nil
	default:
		return nil, fmt.Errorf("unknown layer flag: %d", flag)
	}
}

func encryptLayer(hop Hop, plaintext []byte) ([]byte, error) {
	ephPriv := make([]byte, 32)
	if _, err := rand.Read(ephPriv); err != nil {
		return nil, err
	}
	ephPriv[0] &= 248
	ephPriv[31] &= 127
	ephPriv[31] |= 64
	ephPub, err := curve25519.X25519(ephPriv, curve25519.Basepoint)
	if err != nil {
		return nil, err
	}
	shared, err := curve25519.X25519(ephPriv, hop.ECDHPub)
	if err != nil {
		return nil, err
	}
	key, err := deriveKey(shared, []byte(hop.PeerID))
	if err != nil {
		return nil, err
	}
	aead, err := chacha20poly1305.New(key)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, nonceSize)
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	ciphertext := aead.Seal(nil, nonce, plaintext, nil)

	out := make([]byte, 0, ephPubSize+nonceSize+len(ciphertext))
	out = append(out, ephPub...)
	out = append(out, nonce...)
	out = append(out, ciphertext...)
	return out, nil
}

func decryptLayer(packet []byte, id *identity.Identity) ([]byte, error) {
	if len(packet) < ephPubSize+nonceSize+chacha20poly1305.Overhead {
		return nil, errors.New("packet too short")
	}
	ephPub := packet[:ephPubSize]
	nonce := packet[ephPubSize : ephPubSize+nonceSize]
	ciphertext := packet[ephPubSize+nonceSize:]

	shared, err := id.ECDH(ephPub)
	if err != nil {
		return nil, err
	}
	key, err := deriveKey(shared, []byte(id.PeerID()))
	if err != nil {
		return nil, err
	}
	aead, err := chacha20poly1305.New(key)
	if err != nil {
		return nil, err
	}
	plaintext, err := aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, err
	}
	return plaintext, nil
}

func deriveKey(shared, info []byte) ([]byte, error) {
	key := make([]byte, chacha20poly1305.KeySize)
	if _, err := hkdf.New(sha256.New, shared, []byte(saltString), info).Read(key); err != nil {
		return nil, err
	}
	return key, nil
}
```

### Task 2.2: Fuzz test for Decode

**Files:**
- Modify: `go/pkg/onion/onion_test.go`

- [ ] **Step 1: Add fuzz test**

```go
// Append to go/pkg/onion/onion_test.go
import "path/filepath"

func FuzzDecode(f *testing.F) {
	// Seed with valid packets.
	dest, _ := identity.LoadOrCreate(filepath.Join(f.TempDir(), "d"))
	on, _ := Build([]Hop{{PeerID: dest.PeerID(), ECDHPub: dest.ECDHPub()}}, []byte("seed"))
	f.Add(on.Bytes())
	f.Add([]byte{0, 1, 2, 3})

	f.Fuzz(func(t *testing.T, data []byte) {
		// Must not panic on arbitrary input.
		_, _ = Decode(data, dest)
	})
}
```

- [ ] **Step 2: Run fuzz for a few seconds**

```bash
go test ./pkg/onion/ -fuzz=FuzzDecode -fuzztime=10s
```
Expected: PASS, no panic.

- [ ] **Step 3: Commit**

```bash
git add go/pkg/onion/onion_test.go
git commit -m "test(onion): fuzz Decode against arbitrary input"
```

---

## Phase 3: MixEngine Close + Secure RNG

### Task 3.1: Add Close() to MixEngine with failing test

**Files:**
- Modify: `go/pkg/mix/engine.go`
- Modify: `go/pkg/mix/engine_test.go`

- [ ] **Step 1: Write the failing test**

```go
// Append to go/pkg/mix/engine_test.go
func TestMixEngineClose(t *testing.T) {
	engine := NewMixEngine(10, 1*time.Second, func(batch []*MockEnvelope) {})
	if err := engine.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := engine.Add(&MockEnvelope{ID: 1}); err == nil {
		t.Fatal("Add after Close should error")
	}
}

func TestMixEngineCloseFlushesPending(t *testing.T) {
	var mu sync.Mutex
	var got []*MockEnvelope
	engine := NewMixEngine(10, 1*time.Hour, func(b []*MockEnvelope) {
		mu.Lock()
		got = b
		mu.Unlock()
	})
	_ = engine.Add(&MockEnvelope{ID: 1})
	_ = engine.Add(&MockEnvelope{ID: 2})
	_ = engine.Close()

	mu.Lock()
	defer mu.Unlock()
	if len(got) != 2 {
		t.Errorf("expected 2 flushed on Close, got %d", len(got))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./pkg/mix/ -v -run TestMixEngineClose
```
Expected: FAIL "engine.Close undefined".

- [ ] **Step 3: Implement Close()**

```go
// go/pkg/mix/engine.go (REPLACE)
package mix

import (
	"crypto/rand"
	"errors"
	"math/big"
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
	closed    bool
	rnd       lockedRand
}

type lockedRand struct {
	mu sync.Mutex
	r  interface {
		Int31n(n int32) int32
		Shuffle(n int, swap func(i, j int))
	}
}

func (lr *lockedRand) Shuffle(n int, swap func(i, j int)) {
	lr.mu.Lock()
	defer lr.mu.Unlock()
	lr.r.Shuffle(n, swap)
}

func NewMixEngine[T any](batchSize int, maxDelay time.Duration, onFlush func([]*T)) *MixEngine[T] {
	seed, _ := rand.Int(rand.Reader, big.NewInt(1<<62))
	return &MixEngine[T]{
		batchSize: batchSize,
		maxDelay:  maxDelay,
		onFlush:   onFlush,
		rnd: lockedRand{r: newMathRand(int(seed.Int64()))},
	}
}

func (m *MixEngine[T]) Add(msg *T) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return errors.New("mix engine closed")
	}
	m.buffer = append(m.buffer, msg)
	if len(m.buffer) >= m.batchSize {
		m.flushLocked()
		return nil
	}
	if m.timer == nil {
		m.timer = time.AfterFunc(m.maxDelay, func() {
			m.mu.Lock()
			m.flushLocked()
			m.mu.Unlock()
		})
	}
	return nil
}

func (m *MixEngine[T]) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil
	}
	m.closed = true
	if m.timer != nil {
		m.timer.Stop()
		m.timer = nil
	}
	m.flushLocked()
	return nil
}

func (m *MixEngine[T]) flushLocked() {
	if len(m.buffer) == 0 {
		return
	}
	m.rnd.Shuffle(len(m.buffer), func(i, j int) {
		m.buffer[i], m.buffer[j] = m.buffer[j], m.buffer[i]
	})
	batch := m.buffer
	m.buffer = nil
	m.onFlush(batch)
}

// newMathRand creates a non-concurrent-safe source; callers wrap with lockedRand.
func newMathRand(seed int) interface {
	Int31n(n int32) int32
	Shuffle(n int, swap func(i, j int))
} {
	return mathRandNew(seed)
}
```

```go
// Append a thin adapter at the bottom of engine.go
import "math/rand"

func mathRandNew(seed int) interface {
	Int31n(n int32) int32
	Shuffle(n int, swap func(i, j int))
} {
	return rand.New(rand.NewSource(int64(seed)))
}
```

> The interface indirection keeps the crypto/rand seeding visible at the call site while letting us drop in `math/rand` for the actual shuffles (which is fine for batch reordering — we just need unpredictability, not key-grade randomness).

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./pkg/mix/ -v
```
Expected: PASS all (existing + new).

- [ ] **Step 5: Commit**

```bash
git add go/pkg/mix/engine.go go/pkg/mix/engine_test.go
git commit -m "feat(mix): Close() flushes pending and rejects future Adds; secure seed"
```

---

## Phase 4: Path Selection

### Task 4.1: PathSel skeleton with failing test using fake DHT

**Files:**
- Create: `go/pkg/pathsel/pathsel.go`
- Create: `go/pkg/pathsel/pathsel_test.go`

- [ ] **Step 1: Write the failing test**

```go
// go/pkg/pathsel/pathsel_test.go
package pathsel

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"Np4Protocol/go/pkg/identity"
	"github.com/libp2p/go-libp2p/core/peer"
)

func TestPickReturnsRequestedHops(t *testing.T) {
	dir := t.TempDir()
	candidates := make([]TestPeer, 5)
	for i := range candidates {
		id, _ := identity.LoadOrCreate(filepath.Join(dir, "p"+string(rune('a'+i))))
		candidates[i] = TestPeer{ID: id.PeerID(), ECDHPub: id.ECDHPub()}
	}

	finder := &FakeFinder{Peers: candidates}
	sel := Selector{Hops: 3, Finder: finder}
	path, err := sel.Pick(context.Background(), peer.ID("self"))
	if err != nil {
		t.Fatalf("Pick: %v", err)
	}
	if len(path) != 3 {
		t.Fatalf("expected 3 hops, got %d", len(path))
	}
	seen := map[peer.ID]bool{}
	for _, h := range path {
		if h.PeerID == peer.ID("self") {
			t.Error("self in path")
		}
		if seen[h.PeerID] {
			t.Errorf("duplicate %s in path", h.PeerID)
		}
		seen[h.PeerID] = true
	}
}

func TestPickExcludesListedPeers(t *testing.T) {
	dir := t.TempDir()
	candidates := make([]TestPeer, 4)
	for i := range candidates {
		id, _ := identity.LoadOrCreate(filepath.Join(dir, "p"+string(rune('a'+i))))
		candidates[i] = TestPeer{ID: id.PeerID(), ECDHPub: id.ECDHPub()}
	}
	excluded := candidates[0].ID

	finder := &FakeFinder{Peers: candidates}
	sel := Selector{Hops: 3, Finder: finder}
	path, err := sel.Pick(context.Background(), peer.ID("self"), excluded)
	if err != nil {
		t.Fatalf("Pick: %v", err)
	}
	for _, h := range path {
		if h.PeerID == excluded {
			t.Errorf("excluded peer %s appeared in path", excluded)
		}
	}
}

func TestPickErrorsWhenNotEnough(t *testing.T) {
	finder := &FakeFinder{Peers: []TestPeer{}}
	sel := Selector{Hops: 3, Finder: finder}
	_, err := sel.Pick(context.Background(), peer.ID("self"))
	if !errors.Is(err, ErrNotEnoughRelays) {
		t.Errorf("expected ErrNotEnoughRelays, got %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./pkg/pathsel/ -v
```
Expected: FAIL "package pathsel not found".

- [ ] **Step 3: Implement Selector with Finder abstraction**

```go
// go/pkg/pathsel/pathsel.go
package pathsel

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"

	"Np4Protocol/go/pkg/onion"

	"github.com/libp2p/go-libp2p/core/peer"
)

var ErrNotEnoughRelays = errors.New("not enough relays available")

// PeerInfo is the minimal info needed to build an onion Hop.
type PeerInfo struct {
	ID       peer.ID
	ECDHPub  []byte
}

// Finder abstracts relay discovery so we can test without a real DHT.
type Finder interface {
	FindRelays(ctx context.Context) ([]PeerInfo, error)
}

type Selector struct {
	Hops    int
	Finder  Finder
}

func (s *Selector) Pick(ctx context.Context, self peer.ID, exclude ...peer.ID) ([]onion.Hop, error) {
	if s.Hops <= 0 {
		return nil, errors.New("Hops must be > 0")
	}
	candidates, err := s.Finder.FindRelays(ctx)
	if err != nil {
		return nil, fmt.Errorf("find relays: %w", err)
	}

	excluded := make(map[peer.ID]struct{})
	excluded[self] = struct{}{}
	for _, p := range exclude {
		excluded[p] = struct{}{}
	}

	eligible := make([]PeerInfo, 0, len(candidates))
	for _, c := range candidates {
		if _, skip := excluded[c.ID]; skip {
			continue
		}
		if len(c.ECDHPub) == 0 {
			continue
		}
		eligible = append(eligible, c)
	}
	if len(eligible) < s.Hops {
		return nil, fmt.Errorf("%w: have %d, want %d", ErrNotEnoughRelays, len(eligible), s.Hops)
	}

	// Random subset without replacement.
	chosen := make([]onion.Hop, 0, s.Hops)
	used := make(map[int]struct{})
	for len(chosen) < s.Hops {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(eligible))))
		if err != nil {
			return nil, err
		}
		idx := int(n.Int64())
		if _, dup := used[idx]; dup {
			continue
		}
		used[idx] = struct{}{}
		c := eligible[idx]
		chosen = append(chosen, onion.Hop{PeerID: c.ID, ECDHPub: c.ECDHPub})
	}
	return chosen, nil
}
```

```go
// go/pkg/pathsel/testing.go (separate file for test helpers)
package pathsel

// TestPeer and FakeFinder live here so production code doesn't depend on them.
type TestPeer struct {
	ID       peer.ID
	ECDHPub  []byte
}

type FakeFinder struct {
	Peers []TestPeer
	Err   error
}

func (f *FakeFinder) FindRelays(ctx context.Context) ([]PeerInfo, error) {
	if f.Err != nil {
		return nil, f.Err
	}
	out := make([]PeerInfo, len(f.Peers))
	for i, p := range f.Peers {
		out[i] = PeerInfo{ID: p.ID, ECDHPub: p.ECDHPub}
	}
	return out, nil
}
```

> Note: The test file imports `peer` from libp2p; the testing.go file must also import it. Adjust imports accordingly.

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./pkg/pathsel/ -v
```
Expected: PASS all three.

- [ ] **Step 5: Commit**

```bash
git add go/pkg/pathsel/
git commit -m "feat(pathsel): random N-hop selection with Finder abstraction"
```

### Task 4.2: DHT-backed Finder

**Files:**
- Modify: `go/pkg/pathsel/pathsel.go` (add DHTFinder)
- Modify: `go/pkg/pathsel/pathsel_test.go`

- [ ] **Step 1: Add DHTFinder type**

```go
// Append to go/pkg/pathsel/pathsel.go
import (
	// ... existing ...
	"context"
	"encoding/base32"
	"time"

	dht "github.com/libp2p/go-libp2p-kad-dht"
	drouting "github.com/libp2p/go-libp2p/p2p/discovery/routing"
)

const rendezvousString = "np4-relay"
const ecdhKeyPrefix = "/np4/ecdh/"

type DHTFinder struct {
	DHT     *dht.IpfsDHT
	Timeout time.Duration
}

func (f *DHTFinder) FindRelays(ctx context.Context) ([]PeerInfo, error) {
	if f.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, f.Timeout)
		defer cancel()
	}
	rd := drouting.NewRoutingDiscovery(f.DHT)
	peerChan, err := rd.FindPeers(ctx, rendezvousString)
	if err != nil {
		return nil, err
	}

	var out []PeerInfo
	for pi := range peerChan {
		pub, err := f.lookupECDH(ctx, pi.ID)
		if err != nil || len(pub) == 0 {
			continue
		}
		out = append(out, PeerInfo{ID: pi.ID, ECDHPub: pub})
	}
	return out, nil
}

func (f *DHTFinder) lookupECDH(ctx context.Context, pid peer.ID) ([]byte, error) {
	key := ecdhKeyPrefix + base32.StdEncoding.EncodeToString([]byte(pid))
	data, err := f.DHT.GetValue(ctx, key)
	if err != nil {
		return nil, err
	}
	return data, nil
}

// PublishECDH stores a node's own ECDH pubkey so other nodes can build paths through it.
func PublishECDH(ctx context.Context, d *dht.IpfsDHT, pid peer.ID, ecdhPub []byte) error {
	key := ecdhKeyPrefix + base32.StdEncoding.EncodeToString([]byte(pid))
	return d.PutValue(ctx, key, ecdhPub)
}
```

- [ ] **Step 2: Smoke test (manual; DHT integration is hard to unit-test)**

Add a note in the test file but skip automated testing for now — DHTFinder requires a real DHT. We'll cover it in the integration test in Phase 8.

- [ ] **Step 3: Commit**

```bash
git add go/pkg/pathsel/
git commit -m "feat(pathsel): DHTFinder reads relay list and ECDH pubkeys"
```

---

## Phase 5: Rewrite np4.Node

### Task 5.1: New Node struct with options pattern

**Files:**
- Modify: `go/pkg/np4/node.go`

- [ ] **Step 1: Rewrite node.go top half**

```go
// go/pkg/np4/node.go (REPLACE the type, constructors, and Send section)
package np4

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"Np4Protocol/go/pkg/identity"
	"Np4Protocol/go/pkg/message"
	"Np4Protocol/go/pkg/mix"
	"Np4Protocol/go/pkg/onion"
	"Np4Protocol/go/pkg/p2p"
	"Np4Protocol/go/pkg/pathsel"

	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/multiformats/go-multiaddr"
)

const (
	ProtocolOnion  = protocol.ID("/np4/onion/1.0.0")
	ProtocolDirect = protocol.ID("/np4/direct/1.0.0")
)

const (
	defaultHops           = 3
	defaultMixBatch       = 10
	defaultMixDelay       = 500 * time.Millisecond
	defaultSendTimeout    = 30 * time.Second
	defaultMixMaxMessage  = 1 << 20 // 1MB
)

type Node struct {
	host     host.Host
	identity *identity.Identity
	bus      *message.MessageBus
	mix      *mix.MixEngine[*pendingPacket]
	dht      *dht.IpfsDHT
	pathSel  *pathsel.Selector

	ctx      context.Context
	cancel   context.CancelFunc
	stopOnce sync.Once
}

// pendingPacket is what the MixEngine flushes: a ready-to-send onion packet.
type pendingPacket struct {
	firstHop peer.ID
	onion    *onion.Onion
}

type Option func(*config)

type config struct {
	identityPath string
	bootstrap    []peer.AddrInfo
	rendezvous   string
	hops         int
}

func WithIdentity(path string) Option       { return func(c *config) { c.identityPath = path } }
func WithBootstrap(p []peer.AddrInfo) Option { return func(c *config) { c.bootstrap = p } }
func WithRendezvous(r string) Option         { return func(c *config) { c.rendezvous = r } }
func WithHops(h int) Option                  { return func(c *config) { c.hops = h } }

func NewNode(port int, opts ...Option) (*Node, error) {
	cfg := config{identityPath: "", rendezvous: "np4-network", hops: defaultHops}
	for _, opt := range opts {
		opt(&cfg)
	}

	id, err := identity.LoadOrCreate(cfg.identityPath)
	if err != nil {
		return nil, fmt.Errorf("identity: %w", err)
	}
	h, err := p2p.NewHostWithIdentity(id, port)
	if err != nil {
		return nil, fmt.Errorf("host: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	n := &Node{
		host:     h,
		identity: id,
		bus:      message.NewMessageBus(0),
		ctx:      ctx,
		cancel:   cancel,
	}

	n.mix = mix.NewMixEngine[*pendingPacket](defaultMixBatch, defaultMixDelay, n.flushBatch)

	if len(cfg.bootstrap) > 0 {
		kdht, err := p2p.StartDHT(ctx, h, cfg.bootstrap)
		if err != nil {
			cancel()
			h.Close()
			return nil, fmt.Errorf("dht: %w", err)
		}
		n.dht = kdht
		p2p.AdvertiseRendezvous(ctx, kdht, cfg.rendezvous)

		n.pathSel = &pathsel.Selector{
			Hops:   cfg.hops,
			Finder: &pathsel.DHTFinder{DHT: kdht, Timeout: 15 * time.Second},
		}
	}

	h.SetStreamHandler(ProtocolOnion, n.handleOnionStream)
	h.SetStreamHandler(ProtocolDirect, n.handleDirectStream)

	return n, nil
}

func (n *Node) ID() peer.ID               { return n.host.ID() }
func (n *Node) Host() host.Host           { return n.host }
func (n *Node) DHT() *dht.IpfsDHT         { return n.dht }
func (n *Node) Addrs() []string {
	addrs := n.host.Addrs()
	out := make([]string, len(addrs))
	for i, a := range addrs {
		out[i] = fmt.Sprintf("%s/p2p/%s", a.String(), n.host.ID().String())
	}
	return out
}

func (n *Node) OnMessage(handler func(*message.Message)) {
	n.bus.OnMessage(handler)
}

func (n *Node) Connect(info peer.AddrInfo) error {
	ctx, cancel := context.WithTimeout(n.ctx, 15*time.Second)
	defer cancel()
	return n.host.Connect(ctx, info)
}
```

- [ ] **Step 2: Verify compile (no test yet)**

```bash
go build ./pkg/np4/
```
Expected: errors about missing methods (Send, SendDirect, etc.) — those are added in the next task.

- [ ] **Step 3: Commit partial (no build verification yet)**

Hold commit until Task 5.2.

### Task 5.2: Send / SendDirect / handlers

**Files:**
- Modify: `go/pkg/np4/node.go`

- [ ] **Step 1: Append Send methods**

```go
// Append to go/pkg/np4/node.go

// Send routes a message through the mix (default). Falls back to SendDirect if no relays available.
func (n *Node) Send(dest peer.ID, content []byte) error {
	if n.pathSel == nil {
		return n.SendDirect(dest, content)
	}
	path, err := n.pathSel.Pick(n.ctx, n.ID(), dest)
	if err != nil {
		// Log and fall back to direct.
		fmt.Fprintf(osStderr(), "[np4] mix path selection failed (%v); falling back to direct\n", err)
		return n.SendDirect(dest, content)
	}
	// Append destination as the final hop.
	hops := append(path, onion.Hop{PeerID: dest, ECDHPub: n.lookupDestPub(dest)})

	on, err := onion.Build(hops, content)
	if err != nil {
		return fmt.Errorf("build onion: %w", err)
	}
	pkt := &pendingPacket{firstHop: hops[0].PeerID, onion: on}
	return n.mix.Add(pkt)
}

// SendDirect sends a single-hop message bypassing the mix (debug).
func (n *Node) SendDirect(dest peer.ID, content []byte) error {
	ctx, cancel := context.WithTimeout(n.ctx, defaultSendTimeout)
	defer cancel()
	s, err := n.host.NewStream(ctx, dest, ProtocolDirect)
	if err != nil {
		return fmt.Errorf("open stream: %w", err)
	}
	defer s.Close()

	msg := &message.Message{
		Type:     message.TypeAsync,
		DestID:   dest.String(),
		SenderID: n.ID().String(),
		Content:  content,
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	return p2p.WriteMsg(s, data)
}

func (n *Node) lookupDestPub(dest peer.ID) []byte {
	if n.dht == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(n.ctx, 5*time.Second)
	defer cancel()
	pub, err := pathsel.GetECDH(ctx, n.dht, dest)
	if err != nil {
		return nil
	}
	return pub
}

// flushBatch is the MixEngine's onFlush callback.
func (n *Node) flushBatch(batch []*pendingPacket) {
	for _, pkt := range batch {
		go n.sendToRelay(pkt)
	}
}

func (n *Node) sendToRelay(pkt *pendingPacket) {
	ctx, cancel := context.WithTimeout(n.ctx, defaultSendTimeout)
	defer cancel()
	s, err := n.host.NewStream(ctx, pkt.firstHop, ProtocolOnion)
	if err != nil {
		fmt.Fprintf(osStderr(), "[np4] open relay stream: %v\n", err)
		return
	}
	defer s.Close()
	if err := p2p.WriteMsg(s, pkt.onion.Bytes()); err != nil {
		fmt.Fprintf(osStderr(), "[np4] write to relay: %v\n", err)
	}
}

// handleOnionStream peels one layer and either dispatches locally or forwards.
func (n *Node) handleOnionStream(s network.Stream) {
	defer s.Close()
	data, err := p2p.ReadMsg(s)
	if err != nil {
		fmt.Fprintf(osStderr(), "[np4] onion read: %v\n", err)
		return
	}
	dec, err := onion.Decode(data, n.identity)
	if err != nil {
		fmt.Fprintf(osStderr(), "[np4] onion decode: %v\n", err)
		return
	}
	if dec.IsFinal {
		n.bus.Send(&message.Message{
			Type:     message.TypeAsync,
			SenderID: "anonymous",
			Content:  dec.Inner,
		})
		return
	}
	// Forward to next hop.
	ctx, cancel := context.WithTimeout(n.ctx, defaultSendTimeout)
	defer cancel()
	next, err := n.host.NewStream(ctx, dec.NextHop, ProtocolOnion)
	if err != nil {
		fmt.Fprintf(osStderr(), "[np4] forward stream: %v\n", err)
		return
	}
	defer next.Close()
	if err := p2p.WriteMsg(next, dec.Inner); err != nil {
		fmt.Fprintf(osStderr(), "[np4] forward write: %v\n", err)
	}
}

func (n *Node) handleDirectStream(s network.Stream) {
	defer s.Close()
	data, err := p2p.ReadMsg(s)
	if err != nil {
		return
	}
	var msg message.Message
	if err := json.Unmarshal(data, &msg); err != nil {
		return
	}
	n.bus.Send(&msg)
}

// Close stops the node: mix engine, DHT, host.
func (n *Node) Close() error {
	var firstErr error
	n.stopOnce.Do(func() {
		n.cancel()
		if err := n.mix.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
		if n.dht != nil {
			if err := n.dht.Close(); err != nil && firstErr == nil {
				firstErr = err
			}
		}
		if err := n.host.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	})
	return firstErr
}

// Stop kept for backward compat with existing tests; aliases Close.
func (n *Node) Stop() { _ = n.Close() }

// FindPeers remains for the CLI's `peers` command.
func (n *Node) FindPeers(ctx context.Context, rendezvous string) (<-chan peer.AddrInfo, error) {
	if n.dht == nil {
		return nil, errors.New("DHT not initialized")
	}
	return p2p.FindPeers(ctx, n.dht, rendezvous)
}

// osStderr indirection so tests can redirect; defaults to os.Stderr.
func osStderr() *os.File { return os.Stderr }
```

```go
// Add to imports
import "os"
```

- [ ] **Step 2: Add GetECDH wrapper to pathsel**

```go
// Append to go/pkg/pathsel/pathsel.go
func GetECDH(ctx context.Context, d *dht.IpfsDHT, pid peer.ID) ([]byte, error) {
	key := ecdhKeyPrefix + base32.StdEncoding.EncodeToString([]byte(pid))
	return d.GetValue(ctx, key)
}
```

- [ ] **Step 3: Build the package**

```bash
go build ./pkg/np4/
```
Expected: clean build.

- [ ] **Step 4: Update existing tests to use new API**

```bash
# Find usages of NewNode and Connect in tests:
grep -rn "np4.NewNode\|node.Connect" go/pkg/np4/
```

The existing `node_test.go` calls `nodeA.Connect(nodeB.Host().Addrs(), nodeB.ID())`. Update each call to `nodeA.Connect(peer.AddrInfo{ID: nodeB.ID(), Addrs: nodeB.Host().Addrs()})`.

Replace each test's `defer nodeA.Stop()` with `defer nodeA.Close()` (Stop is aliased so both work, but Close is preferred).

- [ ] **Step 5: Run tests**

```bash
go test ./pkg/np4/ -v
```
Expected: existing P2P tests PASS (they use SendDirect path under the hood via fallback).

- [ ] **Step 6: Commit**

```bash
git add go/pkg/np4/ go/pkg/pathsel/
git commit -m "feat(np4): rewrite Node with mix+onion, options pattern, Close()"
```

### Task 5.3: ServeRelay — advertise as relay

**Files:**
- Modify: `go/pkg/np4/node.go`

- [ ] **Step 1: Add ServeRelay method**

```go
// Append to go/pkg/np4/node.go
// ServeRelay publishes this node's ECDH pubkey so others can route through it.
func (n *Node) ServeRelay() error {
	if n.dht == nil {
		return errors.New("DHT not initialized; pass WithBootstrap when creating the node")
	}
	p2p.AdvertiseRendezvous(n.ctx, n.dht, "np4-relay")
	if err := pathsel.PublishECDH(n.ctx, n.dht, n.ID(), n.identity.ECDHPub()); err != nil {
		return fmt.Errorf("publish ECDH: %w", err)
	}
	return nil
}
```

- [ ] **Step 2: Build**

```bash
go build ./pkg/np4/
```

- [ ] **Step 3: Commit**

```bash
git add go/pkg/np4/node.go
git commit -m "feat(np4): ServeRelay advertises node in DHT for mix routing"
```

---

## Phase 6: MessageBus Worker Pool

### Task 6.1: Worker pool with failing test

**Files:**
- Modify: `go/pkg/message/bus.go`
- Modify: `go/pkg/message/bus_test.go`

- [ ] **Step 1: Write the failing test**

```go
// Append to go/pkg/message/bus_test.go
func TestBroadcastDoesNotMutateInput(t *testing.T) {
	b := NewMessageBus(0)
	b.Start()
	defer b.Stop()

	original := &Message{Type: TypeAsync, DestID: "dest", Content: []byte("x")}
	snapshot := *original

	_ = b.Broadcast(original)

	if original.Type != snapshot.Type || original.DestID != snapshot.DestID {
		t.Errorf("Broadcast mutated input: %+v vs %+v", original, snapshot)
	}
}

func TestBusStopsCleanly(t *testing.T) {
	b := NewMessageBus(0)
	b.Start()
	b.Send(&Message{Content: []byte("x")})
	b.Stop()
	// Calling Stop twice must not panic.
	b.Stop()
}

func TestBusWorkersDrain(t *testing.T) {
	b := NewMessageBus(4)
	b.Start()
	defer b.Stop()

	var got int32
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		b.OnMessage(func(*Message) {
			atomic.AddInt32(&got, 1)
			wg.Done()
		})
	}
	// Only one handler; send 100 messages.
	for i := 0; i < 100; i++ {
		b.Send(&Message{Content: []byte("x")})
	}
	wg.Wait()
	if atomic.LoadInt32(&got) != 100 {
		t.Errorf("expected 100, got %d", got)
	}
}
```

> Add the necessary imports (`sync`, `sync/atomic`).

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./pkg/message/ -v -run TestBroadcast
```
Expected: FAIL (no Start/Stop, Broadcast mutates).

- [ ] **Step 3: Rewrite bus.go**

```go
// go/pkg/message/bus.go (REPLACE)
package message

import (
	"errors"
	"runtime"
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
	Type       MessageType
	DestID     string
	SenderID   string
	Content    []byte
	SessionKey []byte
}

type MessageHandler func(*Message)

type MessageBus struct {
	handlers []MessageHandler
	hu       sync.RWMutex

	ch    chan *Message
	quit  chan struct{}
	once  sync.Once
}

func NewMessageBus(workers int) *MessageBus {
	if workers <= 0 {
		workers = runtime.GOMAXPROCS(0) * 2
	}
	return &MessageBus{
		ch:   make(chan *Message, 1024),
		quit: make(chan struct{}),
	}
}

func (b *MessageBus) Start() {
	workers := cap(b.ch) // not quite right; use a separate field
	// Use GOMAXPROCS*2 by default.
	n := runtime.GOMAXPROCS(0) * 2
	for i := 0; i < n; i++ {
		go b.worker()
	}
}

func (b *MessageBus) worker() {
	for {
		select {
		case msg := <-b.ch:
			if msg == nil {
				return
			}
			b.hu.RLock()
			handlers := make([]MessageHandler, len(b.handlers))
			copy(handlers, b.handlers)
			b.hu.RUnlock()
			for _, h := range handlers {
				h(msg)
			}
		case <-b.quit:
			return
		}
	}
}

func (b *MessageBus) OnMessage(handler MessageHandler) {
	b.hu.Lock()
	b.handlers = append(b.handlers, handler)
	b.hu.Unlock()
}

var ErrBusFull = errors.New("message bus queue full")
var ErrBusClosed = errors.New("message bus closed")

func (b *MessageBus) Send(msg *Message) error {
	if msg == nil {
		return errors.New("message is nil")
	}
	select {
	case b.ch <- msg:
		return nil
	case <-b.quit:
		return ErrBusClosed
	default:
		return ErrBusFull
	}
}

func (b *MessageBus) Broadcast(msg *Message) error {
	cp := *msg
	cp.Type = TypeBroadcast
	cp.DestID = ""
	return b.Send(&cp)
}

func (b *MessageBus) Stop() {
	b.once.Do(func() {
		close(b.quit)
	})
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./pkg/message/ -v
```
Expected: PASS.

- [ ] **Step 5: Update callers to call Start/Stop**

The `np4.Node` uses `NewMessageBus` and must call `Start()` after creation and `Stop()` in `Close()`. Update:

```go
// go/pkg/np4/node.go in NewNode (after n := &Node{...})
n.bus.Start()

// In Close() before n.cancel():
n.bus.Stop()
```

- [ ] **Step 6: Run all tests**

```bash
go test ./...
```
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add go/pkg/message/ go/pkg/np4/node.go
git commit -m "feat(message): worker pool, side-effect-free Broadcast, Start/Stop"
```

---

## Phase 7: CLI Updates

### Task 7.1: Remove global `node` variable from np4cli

**Files:**
- Modify: `go/cmd/np4cli/root.go`
- Modify: `go/cmd/np4cli/cmd_id.go`
- Modify: `go/cmd/np4cli/cmd_peers.go`
- Modify: `go/cmd/np4cli/cmd_connect.go`
- Modify: `go/cmd/np4cli/cmd_send.go`
- Modify: `go/cmd/np4cli/cmd_chat.go`

- [ ] **Step 1: Rewrite root.go to use cmd.SetContext**

```go
// go/cmd/np4cli/root.go (REPLACE)
package main

import (
	"context"
	"fmt"
	"os"

	"Np4Protocol/go/pkg/np4"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
	"github.com/spf13/cobra"
)

var (
	port          int
	bootstrap     string
	rendezvous    string
	hops          int
	identityPath  string
)

var rootCmd = &cobra.Command{
	Use:   "np4cli",
	Short: "Np4Protocol P2P client",
	Long:  "Np4Protocol anonymous communication client with libp2p peer discovery",
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		n, err := initNode()
		if err != nil {
			return err
		}
		ctx := context.WithValue(cmd.Context(), nodeKey{}, n)
		cmd.SetContext(ctx)
		return nil
	},
}

type nodeKey struct{}

func getNode(cmd *cobra.Command) *np4.Node {
	return cmd.Context().Value(nodeKey{}).(*np4.Node)
}

func init() {
	defaultID := os.ExpandEnv("$HOME/.np4/identity")
	rootCmd.PersistentFlags().IntVar(&port, "port", 0, "Listen port (0 = random)")
	rootCmd.PersistentFlags().StringVar(&bootstrap, "bootstrap", "", "Bootstrap multiaddr")
	rootCmd.PersistentFlags().StringVar(&rendezvous, "rendezvous", "np4-network", "DHT rendezvous")
	rootCmd.PersistentFlags().IntVar(&hops, "hops", 3, "Number of mix hops")
	rootCmd.PersistentFlags().StringVar(&identityPath, "identity", defaultID, "Persistent identity file")
}

func initNode() (*np4.Node, error) {
	opts := []np4.Option{
		np4.WithIdentity(identityPath),
		np4.WithRendezvous(rendezvous),
		np4.WithHops(hops),
	}
	if bootstrap != "" {
		maddr, err := multiaddr.NewMultiaddr(bootstrap)
		if err != nil {
			return nil, fmt.Errorf("invalid bootstrap: %w", err)
		}
		info, err := peer.AddrInfoFromP2pAddr(maddr)
		if err != nil {
			return nil, fmt.Errorf("invalid bootstrap peer info: %w", err)
		}
		opts = append(opts, np4.WithBootstrap([]peer.AddrInfo{*info}))
	}
	n, err := np4.NewNode(port, opts...)
	if err != nil {
		return nil, fmt.Errorf("create node: %w", err)
	}
	return n, nil
}
```

- [ ] **Step 2: Update cmd_id.go**

```go
// go/cmd/np4cli/cmd_id.go (REPLACE)
package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

var idCmd = &cobra.Command{
	Use:   "id",
	Short: "Show this node's peer ID and addresses",
	Run: func(cmd *cobra.Command, args []string) {
		n := getNode(cmd)
		fmt.Printf("Peer ID: %s\n", n.ID())
		fmt.Println("Addresses:")
		for _, addr := range n.Addrs() {
			fmt.Printf("  %s\n", addr)
		}
	},
}

func init() {
	rootCmd.AddCommand(idCmd)
}
```

- [ ] **Step 3: Update cmd_peers.go**

```go
// go/cmd/np4cli/cmd_peers.go (REPLACE)
package main

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"
)

var peersCmd = &cobra.Command{
	Use:   "peers",
	Short: "Discover online peers via DHT",
	RunE: func(cmd *cobra.Command, args []string) error {
		n := getNode(cmd)
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		peerChan, err := n.FindPeers(ctx, rendezvous)
		if err != nil {
			return fmt.Errorf("DHT not available (use --bootstrap): %w", err)
		}
		count := 0
		for pi := range peerChan {
			if pi.ID == n.ID() {
				continue
			}
			fmt.Printf("Peer: %s\n", pi.ID)
			for _, addr := range pi.Addrs {
				fmt.Printf("  %s\n", addr)
			}
			count++
		}
		if count == 0 {
			fmt.Println("No peers found")
		} else {
			fmt.Printf("\n%d peer(s) discovered\n", count)
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(peersCmd)
}
```

- [ ] **Step 4: Update cmd_connect.go**

```go
// go/cmd/np4cli/cmd_connect.go (REPLACE)
package main

import (
	"fmt"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
	"github.com/spf13/cobra"
)

var connectCmd = &cobra.Command{
	Use:   "connect <multiaddr>",
	Short: "Connect to a peer",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		n := getNode(cmd)
		maddr, err := multiaddr.NewMultiaddr(args[0])
		if err != nil {
			return fmt.Errorf("invalid multiaddr: %w", err)
		}
		info, err := peer.AddrInfoFromP2pAddr(maddr)
		if err != nil {
			return fmt.Errorf("invalid peer info: %w", err)
		}
		if err := n.Connect(*info); err != nil {
			return fmt.Errorf("connect failed: %w", err)
		}
		fmt.Printf("Connected to %s\n", info.ID)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(connectCmd)
}
```

- [ ] **Step 5: Build (chat/send still need updating, but check the others)**

```bash
go build ./cmd/np4cli/ 2>&1 | head -20
```
Expected: errors only in `cmd_send.go` and `cmd_chat.go` (still referencing global `node`).

- [ ] **Step 6: Hold commit; finalize in Task 7.2 & 7.3**

### Task 7.2: Update cmd_send.go with --direct flag

**Files:**
- Modify: `go/cmd/np4cli/cmd_send.go`

- [ ] **Step 1: Rewrite cmd_send.go**

```go
// go/cmd/np4cli/cmd_send.go (REPLACE)
package main

import (
	"fmt"
	"strings"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
	"github.com/spf13/cobra"
)

var (
	sendDirect bool
	sendAddr   string
)

var sendCmd = &cobra.Command{
	Use:   "send <peer-id> <message>",
	Short: "Send a message (through mix by default; --direct bypasses)",
	Args:  cobra.MinimumNArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		n := getNode(cmd)
		pid, err := peer.Decode(args[0])
		if err != nil {
			return fmt.Errorf("invalid peer ID: %w", err)
		}

		if sendAddr != "" {
			maddr, err := multiaddr.NewMultiaddr(sendAddr)
			if err != nil {
				return fmt.Errorf("invalid multiaddr: %w", err)
			}
			info, err := peer.AddrInfoFromP2pAddr(maddr)
			if err != nil {
				return fmt.Errorf("invalid peer info: %w", err)
			}
			if err := n.Connect(*info); err != nil {
				return fmt.Errorf("connect failed: %w", err)
			}
		}

		content := strings.Join(args[1:], " ")
		if sendDirect {
			if err := n.SendDirect(pid, []byte(content)); err != nil {
				return fmt.Errorf("send direct failed: %w", err)
			}
			fmt.Printf("Sent (direct) to %s\n", pid)
			return nil
		}
		if err := n.Send(pid, []byte(content)); err != nil {
			return fmt.Errorf("send failed: %w", err)
		}
		fmt.Printf("Sent (mix, %d hops) to %s\n", hops, pid)
		return nil
	},
}

func init() {
	sendCmd.Flags().BoolVar(&sendDirect, "direct", false, "Bypass mix (single-hop direct)")
	sendCmd.Flags().StringVar(&sendAddr, "addr", "", "Peer multiaddr (connect before sending)")
	rootCmd.AddCommand(sendCmd)
}
```

- [ ] **Step 2: Build**

```bash
go build ./cmd/np4cli/
```

### Task 7.3: Slim cmd_chat.go

**Files:**
- Modify: `go/cmd/np4cli/cmd_chat.go`

- [ ] **Step 1: Rewrite cmd_chat.go**

```go
// go/cmd/np4cli/cmd_chat.go (REPLACE)
package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"Np4Protocol/go/pkg/message"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
	"github.com/spf13/cobra"
)

var chatCmd = &cobra.Command{
	Use:   "chat",
	Short: "Enter interactive chat mode",
	RunE: func(cmd *cobra.Command, args []string) error {
		n := getNode(cmd)
		fmt.Printf("Peer ID: %s\n", n.ID())
		fmt.Println("Addresses:")
		for _, addr := range n.Addrs() {
			fmt.Printf("  %s\n", addr)
		}
		fmt.Println()
		fmt.Println("Commands: peers, connect <multiaddr>, send <peer-id> <msg>, id, help, quit")
		fmt.Println()

		n.OnMessage(func(msg *message.Message) {
			fmt.Printf("\n[%s] %s: %s\n> ", time.Now().Format("15:04:05"), msg.SenderID, string(msg.Content))
		})

		scanner := bufio.NewScanner(os.Stdin)
		fmt.Print("> ")
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				fmt.Print("> ")
				continue
			}
			parts := strings.SplitN(line, " ", 2)
			switch parts[0] {
			case "quit", "exit":
				fmt.Println("Bye!")
				return nil
			case "id":
				fmt.Printf("Peer ID: %s\n", n.ID())
				for _, addr := range n.Addrs() {
					fmt.Printf("  %s\n", addr)
				}
			case "peers":
				runChatPeers(n)
			case "connect":
				runChatConnect(n, parts)
			case "send":
				runChatSend(n, parts)
			case "help":
				printChatHelp()
			default:
				fmt.Printf("Unknown command: %s (type 'help')\n", parts[0])
			}
			fmt.Print("> ")
		}
		return nil
	},
}

func runChatPeers(n *np4Node) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	peerChan, err := n.FindPeers(ctx, rendezvous)
	if err != nil {
		fmt.Printf("DHT not available: %v\n", err)
		return
	}
	count := 0
	for pi := range peerChan {
		if pi.ID == n.ID() {
			continue
		}
		fmt.Printf("  %s  %v\n", pi.ID, pi.Addrs)
		count++
	}
	if count == 0 {
		fmt.Println("No peers found")
	} else {
		fmt.Printf("%d peer(s) discovered\n", count)
	}
}

func runChatConnect(n *np4Node, parts []string) {
	if len(parts) < 2 {
		fmt.Println("Usage: connect <multiaddr>")
		return
	}
	maddr, err := multiaddr.NewMultiaddr(parts[1])
	if err != nil {
		fmt.Printf("Invalid multiaddr: %v\n", err)
		return
	}
	info, err := peer.AddrInfoFromP2pAddr(maddr)
	if err != nil {
		fmt.Printf("Invalid peer info: %v\n", err)
		return
	}
	if err := n.Connect(*info); err != nil {
		fmt.Printf("Connect failed: %v\n", err)
		return
	}
	fmt.Printf("Connected to %s\n", info.ID)
}

func runChatSend(n *np4Node, parts []string) {
	if len(parts) < 2 {
		fmt.Println("Usage: send <peer-id> <message>")
		return
	}
	sendParts := strings.SplitN(parts[1], " ", 2)
	if len(sendParts) < 2 {
		fmt.Println("Usage: send <peer-id> <message>")
		return
	}
	pid, err := peer.Decode(sendParts[0])
	if err != nil {
		fmt.Printf("Invalid peer ID: %v\n", err)
		return
	}
	if err := n.Send(pid, []byte(sendParts[1])); err != nil {
		fmt.Printf("Send failed: %v\n", err)
		return
	}
	fmt.Printf("Sent to %s\n", pid)
}

func printChatHelp() {
	fmt.Println("Commands:")
	fmt.Println("  peers                    - Discover online peers via DHT")
	fmt.Println("  connect <multiaddr>      - Connect to a peer")
	fmt.Println("  send <peer-id> <message> - Send a message")
	fmt.Println("  id                       - Show this node's info")
	fmt.Println("  quit / exit              - Exit")
}

func init() {
	rootCmd.AddCommand(chatCmd)
}
```

- [ ] **Step 2: Add np4Node type alias**

```go
// Append to go/cmd/np4cli/root.go
type np4Node = np4.Node
```

- [ ] **Step 3: Build**

```bash
go build ./cmd/np4cli/
```
Expected: clean build.

- [ ] **Step 4: Commit all of Task 7**

```bash
git add go/cmd/np4cli/
git commit -m "refactor(np4cli): remove global node, add --direct, slim chat"
```

### Task 7.4: New cmd_relay and cmd_path

**Files:**
- Create: `go/cmd/np4cli/cmd_relay.go`
- Create: `go/cmd/np4cli/cmd_path.go`

- [ ] **Step 1: Write cmd_relay.go**

```go
// go/cmd/np4cli/cmd_relay.go
package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
)

var relayCmd = &cobra.Command{
	Use:   "relay",
	Short: "Run this node as a mix relay (advertises in DHT)",
	RunE: func(cmd *cobra.Command, args []string) error {
		n := getNode(cmd)
		if err := n.ServeRelay(); err != nil {
			return fmt.Errorf("serve relay: %w", err)
		}
		fmt.Printf("Relaying as %s\n", n.ID())
		fmt.Println("Addresses:")
		for _, addr := range n.Addrs() {
			fmt.Printf("  %s\n", addr)
		}
		fmt.Println("Press Ctrl+C to stop")

		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		return nil
	},
}

func init() {
	rootCmd.AddCommand(relayCmd)
}
```

- [ ] **Step 2: Write cmd_path.go**

```go
// go/cmd/np4cli/cmd_path.go
package main

import (
	"context"
	"fmt"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/spf13/cobra"
)

var pathCmd = &cobra.Command{
	Use:   "path <peer-id>",
	Short: "Show the mix path that would be used to reach a peer",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		n := getNode(cmd)
		dest, err := peer.Decode(args[0])
		if err != nil {
			return fmt.Errorf("invalid peer ID: %w", err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		// Selector.Pick is on the node's pathSel; expose via a helper.
		path, err := n.PickPath(ctx, dest)
		if err != nil {
			return fmt.Errorf("pick path: %w", err)
		}
		fmt.Printf("Path (%d hops + dest):\n", len(path))
		for i, hop := range path {
			fmt.Printf("  [%d] %s\n", i+1, hop)
		}
		fmt.Printf("  [dest] %s\n", dest)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(pathCmd)
}
```

- [ ] **Step 3: Add PickPath helper to Node**

```go
// Append to go/pkg/np4/node.go
func (n *Node) PickPath(ctx context.Context, dest peer.ID) ([]peer.ID, error) {
	if n.pathSel == nil {
		return nil, errors.New("DHT not initialized")
	}
	hops, err := n.pathSel.Pick(ctx, n.ID(), dest)
	if err != nil {
		return nil, err
	}
	out := make([]peer.ID, len(hops))
	for i, h := range hops {
		out[i] = h.PeerID
	}
	return out, nil
}
```

- [ ] **Step 4: Build**

```bash
go build ./cmd/np4cli/ ./pkg/np4/
```

- [ ] **Step 5: Commit**

```bash
git add go/cmd/np4cli/cmd_relay.go go/cmd/np4cli/cmd_path.go go/pkg/np4/node.go
git commit -m "feat(np4cli): add relay and path subcommands"
```

---

## Phase 8: Bootstrap Upgrades + Integration Tests + Shell Test Fixes

### Task 8.1: Bootstrap dashboard — real DHT peer count and /api/relays

**Files:**
- Modify: `go/cmd/bootstrap/cmd_start.go`

- [ ] **Step 1: Update status and add relays endpoint**

```go
// In startGinServer, replace the /api/status handler:
r.GET("/api/status", func(c *gin.Context) {
	addrs := make([]string, len(h.Addrs()))
	for i, addr := range h.Addrs() {
		addrs[i] = addr.String() + "/p2p/" + h.ID().String()
	}
	rtSize := 0
	if dhtInstance != nil {
		rtSize = dhtInstance.RoutingTable().Size()
	}
	c.JSON(http.StatusOK, gin.H{
		"peer_id":      h.ID().String(),
		"addresses":    addrs,
		"uptime":       time.Since(startTime).Round(time.Second).String(),
		"dht_peers":    rtSize,
		"status":       "online",
	})
})

// Add new /api/relays endpoint
r.GET("/api/relays", func(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	finder := &pathsel.DHTFinder{DHT: dhtInstance, Timeout: 5 * time.Second}
	relays, err := finder.FindRelays(ctx)
	if err != nil {
		c.JSON(http.StatusOK, []interface{}{}) // empty on error
		return
	}
	out := make([]gin.H, 0, len(relays))
	for _, r := range relays {
		out = append(out, gin.H{
			"id":        r.ID.String(),
			"ecdh_pub":  hex.EncodeToString(r.ECDHPub),
		})
	}
	c.JSON(http.StatusOK, out)
})
```

Add imports: `"encoding/hex"`, `"Np4Protocol/go/pkg/pathsel"`.

- [ ] **Step 2: Build**

```bash
go build ./cmd/bootstrap/
```

- [ ] **Step 3: Commit**

```bash
git add go/cmd/bootstrap/cmd_start.go
git commit -m "feat(bootstrap): real DHT routing table size and /api/relays endpoint"
```

### Task 8.2: Integration test for end-to-end mix

**Files:**
- Create: `go/pkg/np4/mix_integration_test.go`

- [ ] **Step 1: Write the test**

```go
// go/pkg/np4/mix_integration_test.go
package np4

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"Np4Protocol/go/pkg/message"

	"github.com/libp2p/go-libp2p/core/peer"
)

// TestEndToEndMix starts a bootstrap node, three relays, a sender, and a receiver.
// Verifies the message arrives at the destination.
func TestEndToEndMix(t *testing.T) {
	dir := t.TempDir()

	// Bootstrap node.
	boot, err := NewNode(0, WithIdentity(filepath.Join(dir, "boot")))
	if err != nil {
		t.Fatal(err)
	}
	defer boot.Close()
	bootAddr := peer.AddrInfo{ID: boot.ID(), Addrs: boot.Host().Addrs()}

	// 3 relays.
	relays := make([]*Node, 3)
	for i := range relays {
		n, err := NewNode(0,
			WithIdentity(filepath.Join(dir, "relay"+string(rune('a'+i)))),
			WithBootstrap([]peer.AddrInfo{bootAddr}),
		)
		if err != nil {
			t.Fatal(err)
		}
		defer n.Close()
		relays[i] = n
		if err := n.ServeRelay(); err != nil {
			t.Fatal(err)
		}
	}

	// Receiver.
	recv, err := NewNode(0,
		WithIdentity(filepath.Join(dir, "recv")),
		WithBootstrap([]peer.AddrInfo{bootAddr}),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer recv.Close()
	if err := pathsel.PublishECDH(context.Background(), recv.DHT(), recv.ID(), recv.identity.ECDHPub()); err != nil {
		t.Fatal(err)
	}

	// Sender.
	snd, err := NewNode(0,
		WithIdentity(filepath.Join(dir, "snd")),
		WithBootstrap([]peer.AddrInfo{bootAddr}),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer snd.Close()

	// Give DHT time to propagate.
	time.Sleep(5 * time.Second)

	var (
		gotContent []byte
		mu         sync.Mutex
	)
	recv.OnMessage(func(msg *message.Message) {
		mu.Lock()
		gotContent = msg.Content
		mu.Unlock()
	})

	if err := snd.Send(recv.ID(), []byte("hello-mix")); err != nil {
		t.Fatalf("Send: %v", err)
	}

	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		if string(gotContent) == "hello-mix" {
			mu.Unlock()
			return
		}
		mu.Unlock()
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("message never arrived; got %q", gotContent)
}
```

- [ ] **Step 2: Expose identity field for test**

Either expose a getter on Node or use `recv.identity.ECDHPub()` via a test helper in the same package. Since the test is in package `np4`, it can access the unexported field directly. Keep as-is.

- [ ] **Step 3: Run the integration test**

```bash
go test ./pkg/np4/ -v -run TestEndToEndMix -timeout 60s
```
Expected: PASS (may be flaky on first run; retry if DHT propagation is slow).

- [ ] **Step 4: Commit**

```bash
git add go/pkg/np4/mix_integration_test.go
git commit -m "test(np4): end-to-end mix path through 3 relays to destination"
```

### Task 8.3: Fix shell tests to use bootstrap start output

**Files:**
- Modify: `go/shell/test_bootstrap.sh`
- Modify: `go/shell/test_api.sh`
- Modify: `go/shell/test_node.sh`
- Modify: `go/shell/test_communication.sh`
- Modify: `go/shell/test_multi_node.sh`
- Modify: `go/shell/test_stress.sh`
- Modify: `go/shell/test_reconnect.sh`
- Modify: `go/shell/test_dht_discovery.sh`
- Modify: `go/shell/test_full_flow.sh`

- [ ] **Step 1: Define new bootstrap startup pattern**

The bug: tests use `bootstrap id` to get the multiaddr, but `id` creates a fresh host. New pattern: capture `bootstrap start` stdout, parse the multiaddr from there.

In each shell script, replace:
```bash
"$BIN_DIR/bootstrap" start --port $BOOTSTRAP_PORT --web $WEB_PORT &
sleep 2
BOOTSTRAP_MULTIADDR=$("$BIN_DIR/bootstrap" id --port $BOOTSTRAP_PORT 2>&1 | grep "/ip4/127.0.0.1" | head -1 | awk '{print $1}')
```
with:
```bash
"$BIN_DIR/bootstrap" start --port $BOOTSTRAP_PORT --web $WEB_PORT --identity /tmp/np4_test_boot.id > /tmp/np4_boot_$BOOTSTRAP_PORT.log 2>&1 &
sleep 2
BOOTSTRAP_MULTIADDR=$(grep "Multiaddr:" /tmp/np4_boot_$BOOTSTRAP_PORT.log | head -1 | awk '{print $2}')
```

> Note: Task 1.3 changed cmd_id.go to print `Multiaddr: <addr>` format. Verify the actual format matches by reading cmd_id.go again before doing the sed.

- [ ] **Step 2: Update each script**

Apply the substitution to each of the 9 shell scripts. Use sed or manual edits.

- [ ] **Step 3: Run all shell tests**

```bash
cd /Users/chenpeigen/GolandProjects/Np4Protocol/go
for script in shell/test_*.sh; do
    echo "=== $script ==="
    bash "$script" 2>&1 | tail -5
done
```
Expected: all suites PASS.

- [ ] **Step 4: Commit**

```bash
git add go/shell/
git commit -m "fix(shell): use bootstrap start output instead of broken 'id' for multiaddr"
```

### Task 8.4: Final verification — go vet, gofmt, full test suite

- [ ] **Step 1: Run go vet**

```bash
cd /Users/chenpeigen/GolandProjects/Np4Protocol/go
go vet ./...
```
Expected: no warnings.

- [ ] **Step 2: Run gofmt check**

```bash
gofmt -l .
```
Expected: empty output. If files appear, run `gofmt -w <files>` and commit.

- [ ] **Step 3: Run all Go tests**

```bash
go test ./... -timeout 120s
```
Expected: all PASS.

- [ ] **Step 4: Run all shell tests**

```bash
for script in shell/test_*.sh; do bash "$script" || break; done
```
Expected: all PASS.

- [ ] **Step 5: Manual end-to-end smoke test**

```bash
# Terminal 1
./bin/bootstrap start --port 4000 --web 8000 --identity /tmp/b.id
# Terminal 2
./bin/np4cli relay --port 4001 --bootstrap <multiaddr-from-terminal-1> --identity /tmp/r1.id
# Terminal 3
./bin/np4cli relay --port 4002 --bootstrap <multiaddr> --identity /tmp/r2.id
# Terminal 4
./bin/np4cli relay --port 4003 --bootstrap <multiaddr> --identity /tmp/r3.id
# Terminal 5 (receiver)
./bin/np4cli --port 4004 --bootstrap <multiaddr> --identity /tmp/recv.id chat
# Terminal 6 (sender)
./bin/np4cli --port 4005 --bootstrap <multiaddr> --identity /tmp/snd.id send <recv-peer-id> "hello through mix"
```
Expected: Terminal 5 receives the message.

- [ ] **Step 6: Commit any cleanup**

```bash
git add -A
git commit -m "chore: final cleanup after mixnet refactor"
```

---

## Acceptance Criteria Recap

- [ ] `bootstrap id` invoked twice with same `--identity` returns the same Peer ID (Task 1.3)
- [ ] `pkg/onion` Build/Decode round-trips through N hops; fuzz test passes (Task 2.1, 2.2)
- [ ] `MixEngine.Close()` flushes pending and rejects future Adds (Task 3.1)
- [ ] `pathsel.Selector.Pick` returns N unique hops and excludes self/dest (Task 4.1)
- [ ] `np4cli send <peer-id> <msg>` reaches destination through 3 relays (Task 8.2)
- [ ] `np4cli send --direct` still works (Task 7.2)
- [ ] `MessageBus.Broadcast` does not mutate input (Task 6.1)
- [ ] All Go tests pass: `go test ./...` (Task 8.4)
- [ ] All shell tests pass (Task 8.3)
- [ ] `go vet ./...` clean and `gofmt -l .` empty (Task 8.4)
