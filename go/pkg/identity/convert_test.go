package identity

import (
	"bytes"
	"path/filepath"
	"testing"
)

// TestEd25519ToX25519MatchesDerivedKey is the load-bearing correctness test
// for the DHT key-binding scheme: the X25519 pubkey obtained by converting
// the published ed25519 pubkey MUST equal the one derived from the seed
// (NaCl-compatible derivation).
func TestEd25519ToX25519MatchesDerivedKey(t *testing.T) {
	for i := 0; i < 8; i++ {
		id, err := LoadOrCreate(filepath.Join(t.TempDir(), "id"))
		if err != nil {
			t.Fatalf("identity %d: %v", i, err)
		}
		converted, err := Ed25519PubToX25519(id.SigningPub())
		if err != nil {
			t.Fatalf("convert %d: %v", i, err)
		}
		if !bytes.Equal(converted, id.ECDHPub()) {
			t.Fatalf("identity %d: converted X25519 pub does not match derived pub", i)
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
