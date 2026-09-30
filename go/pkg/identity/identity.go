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
	signPub  []byte // raw ed25519 public key (32 bytes)
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

	if data, err := os.ReadFile(path); err == nil {
		return fromSeed(data)
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
			return fromSeed(data)
		}
		return nil, fmt.Errorf("create identity: %w", err)
	}
	return fromSeed(seed)
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
		signPub:  append([]byte(nil), edPriv.Public().(ed25519.PublicKey)...),
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
