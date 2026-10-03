package identity

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha512"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
	"golang.org/x/crypto/curve25519"
)

const ecdhPubSize = 32

type Identity struct {
	priv     crypto.PrivKey
	stdPriv  ed25519.PrivateKey
	signPub  []byte // raw ed25519 public key (32 bytes)
	ecdhPriv []byte // X25519 private of the CURRENT bucket subkey
	ecdhPub  []byte // X25519 public of the CURRENT bucket subkey

	// Forward-secrecy key schedule (rotation.go): random per-bucket subkeys
	// with a persisted retention window. mu guards the window — it mutates
	// on the rotation loop while send/receive paths read it.
	mu                sync.RWMutex
	rotKeys           []rotatedKey // current first, oldest last
	bucketPeriod      time.Duration
	retentionOverride *int64 // TEST-ONLY retention window in buckets
	nowFn             func() time.Time
	sidecarPath       string // empty = ephemeral (no persistence)
}

func nowDefault() time.Time { return time.Now() }

func LoadOrCreate(path string) (*Identity, error) {
	// Empty path = ephemeral in-memory identity (no persistence). Used by
	// nodes created without WithIdentity (tests, ad-hoc CLI runs). The
	// subkey window still exists — it just never touches disk.
	if path == "" {
		_, edPriv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return nil, fmt.Errorf("generate ed25519: %w", err)
		}
		id, err := fromSeed(edPriv.Seed())
		if err != nil {
			return nil, err
		}
		if err := id.initRotation(); err != nil {
			return nil, fmt.Errorf("init key rotation: %w", err)
		}
		return id, nil
	}

	if data, err := os.ReadFile(path); err == nil {
		id, err := fromSeed(data)
		if err != nil {
			return nil, err
		}
		id.sidecarPath = sidecarPath(path)
		if err := id.initRotation(); err != nil {
			return nil, fmt.Errorf("init key rotation: %w", err)
		}
		return id, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read identity: %w", err)
	}

	// Not exists → create. Racing creators (threads or processes) must agree
	// on ONE identity, so the seed is staged in a temp file and published
	// with link(2): the winner's file appears atomically and complete, and
	// losers discard theirs and load the winner's. A plain WriteFile would
	// let every racer return its own freshly generated identity while the
	// file contents flip-flop.
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("mkdir: %w", err)
	}
	_, edPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate ed25519: %w", err)
	}
	seed := edPriv.Seed()

	tmp, err := os.CreateTemp(filepath.Dir(path), ".np4-identity-*")
	if err != nil {
		return nil, fmt.Errorf("stage identity: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op once linked
	if _, err := tmp.Write(seed); err != nil {
		tmp.Close()
		return nil, fmt.Errorf("stage identity: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return nil, fmt.Errorf("stage identity: %w", err)
	}

	if err := os.Link(tmpName, path); err != nil {
		if errors.Is(err, os.ErrExist) {
			// Lost the race: load the winner's identity.
			data, err := os.ReadFile(path)
			if err != nil {
				return nil, fmt.Errorf("read identity: %w", err)
			}
			id, err := fromSeed(data)
			if err != nil {
				return nil, err
			}
			id.sidecarPath = sidecarPath(path)
			if err := id.initRotation(); err != nil {
				return nil, fmt.Errorf("init key rotation: %w", err)
			}
			return id, nil
		}
		return nil, fmt.Errorf("create identity: %w", err)
	}
	id, err := fromSeed(seed)
	if err != nil {
		return nil, err
	}
	id.sidecarPath = sidecarPath(path)
	if err := id.initRotation(); err != nil {
		return nil, fmt.Errorf("init key rotation: %w", err)
	}
	return id, nil
}

// now returns the (test-replaceable) wall clock. Callers hold the window
// lock or run before concurrency starts.
func (i *Identity) now() time.Time {
	if i.nowFn != nil {
		return i.nowFn()
	}
	return time.Now()
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

	// The master ed25519 key never rotates: it IS the peer identity (address
	// book, allowlist, record binding all hang off it). ECDH keys are random
	// per-bucket subkeys (rotation.go) — deliberately NOT derived from the
	// seed, so a stolen seed file cannot decrypt recorded history beyond the
	// retention window.
	return &Identity{
		priv:         libp2pPriv,
		stdPriv:      edPriv,
		signPub:      append([]byte(nil), edPriv.Public().(ed25519.PublicKey)...),
		bucketPeriod: RotationPeriod,
		nowFn:        nowDefault,
	}, nil
}

func deriveX25519Priv(ed25519Seed []byte) ([]byte, error) {
	if len(ed25519Seed) != ed25519.SeedSize {
		return nil, fmt.Errorf("deriveX25519Priv: bad seed size %d", len(ed25519Seed))
	}
	h := sha512.Sum512(ed25519Seed)
	scalar := h[:32]
	// Clamp per RFC 7748 section 5.
	scalar[0] &= 248
	scalar[31] &= 127
	scalar[31] |= 64
	return scalar, nil
}

func (i *Identity) PeerID() peer.ID {
	pid, _ := peer.IDFromPrivateKey(i.priv)
	return pid
}

func (i *Identity) PrivKey() crypto.PrivKey { return i.priv }

// SigningPub returns the raw 32-byte ed25519 public key. It is what gets
// published to the DHT so peers can (a) verify the peer-ID binding and (b)
// derive our X25519 pubkey via Ed25519PubToX25519.
func (i *Identity) SigningPub() []byte {
	out := make([]byte, len(i.signPub))
	copy(out, i.signPub)
	return out
}

// SigningPubKey returns the libp2p public key wrapper, for publishing the
// marshaled form to the DHT (mirrors how /pk records store keys).
func (i *Identity) SigningPubKey() crypto.PubKey { return i.priv.GetPublic() }

// ECDHPub returns the CURRENT rotation bucket's X25519 public key — the one
// being published and the one senders must use.
func (i *Identity) ECDHPub() []byte {
	i.mu.RLock()
	defer i.mu.RUnlock()
	out := make([]byte, ecdhPubSize)
	copy(out, i.ecdhPub)
	return out
}

// ECDH computes X25519 against the CURRENT subkey private. Senders use this
// (they address the receiver's current published key); receivers should scan
// the retention window via ECDHPrivs instead — their key may have rotated
// between the sender's fetch and the message's arrival.
func (i *Identity) ECDH(theirPub []byte) ([]byte, error) {
	if len(theirPub) != ecdhPubSize {
		return nil, fmt.Errorf("invalid pubkey size: %d", len(theirPub))
	}
	i.mu.RLock()
	defer i.mu.RUnlock()
	return curve25519.X25519(i.ecdhPriv, theirPub)
}

func (i *Identity) HexShort() string {
	i.mu.RLock()
	defer i.mu.RUnlock()
	return hex.EncodeToString(i.ecdhPub[:4])
}
