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

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
	"golang.org/x/crypto/curve25519"
)

const ecdhPubSize = 32

type Identity struct {
	priv     crypto.PrivKey
	ecdhPriv []byte // X25519 private (derived from ed25519 seed)
	ecdhPub  []byte // X25519 public
}

func LoadOrCreate(path string) (*Identity, error) {
	// Empty path = ephemeral in-memory identity (no persistence). Used by
	// nodes created without WithIdentity (tests, ad-hoc CLI runs).
	if path == "" {
		_, edPriv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return nil, fmt.Errorf("generate ed25519: %w", err)
		}
		return fromSeed(edPriv.Seed())
	}

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

	// Derive X25519 private scalar from the ed25519 seed.
	// SHA-512 mirrors Ed25519's internal scalar derivation (RFC 8032 §5.1.5);
	// do NOT simplify to seed[:32] or swap hash — that would leak Ed25519
	// key structure into the X25519 scalar.
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
