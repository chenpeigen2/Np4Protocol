package identity

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"testing"

	"github.com/libp2p/go-libp2p/core/crypto"
	"golang.org/x/crypto/curve25519"
)

// TestEd25519ToX25519MatchesDerivedKey pins the NaCl-compatible mapping:
// converting a marshaled ed25519 pubkey equals deriving X25519 from the seed
// scalar (RFC 8032 §5.1.5 + RFC 7748 clamp). Since rotation (M4) this
// conversion is no longer the DHT key-delivery path — records carry subkeys
// signed by the master key — but the mapping itself must stay correct for
// any code that still relies on it.
func TestEd25519ToX25519MatchesDerivedKey(t *testing.T) {
	for i := 0; i < 8; i++ {
		_, edPriv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatalf("key %d: %v", i, err)
		}
		libp2pPriv, _, err := crypto.KeyPairFromStdKey(&edPriv)
		if err != nil {
			t.Fatalf("wrap %d: %v", i, err)
		}
		raw, ok := libp2pPriv.GetPublic().(*crypto.Ed25519PublicKey)
		if !ok {
			t.Fatalf("key %d: not an ed25519 key", i)
		}
		rawBytes, err := raw.Raw()
		if err != nil {
			t.Fatalf("key %d: raw: %v", i, err)
		}
		converted, err := Ed25519PubToX25519(rawBytes)
		if err != nil {
			t.Fatalf("convert %d: %v", i, err)
		}
		fromSeedScalar, err := deriveX25519Priv(edPriv.Seed())
		if err != nil {
			t.Fatalf("derive %d: %v", i, err)
		}
		expected, err := curve25519.X25519(fromSeedScalar, curve25519.Basepoint)
		if err != nil {
			t.Fatalf("x25519 %d: %v", i, err)
		}
		if !bytes.Equal(converted, expected) {
			t.Fatalf("key %d: converted X25519 pub does not match seed-derived pub", i)
		}
	}
}

func TestEd25519ToX25519RejectsBadInput(t *testing.T) {
	for _, in := range [][]byte{nil, {}, make([]byte, 16), make([]byte, 64)} {
		if _, err := Ed25519PubToX25519(in); err == nil {
			t.Errorf("len %d: expected error", len(in))
		}
	}
	// y = p (out of range): bytes LE of 2^255-19 = 0xed followed by 0xff...7f
	bad := make([]byte, 32)
	bad[0] = 0xed
	for i := 1; i < 31; i++ {
		bad[i] = 0xff
	}
	bad[31] = 0x7f
	if _, err := Ed25519PubToX25519(bad); err == nil {
		t.Error("y == p must be rejected")
	}
}
