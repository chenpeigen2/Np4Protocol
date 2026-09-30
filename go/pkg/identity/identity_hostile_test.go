package identity

import (
	"bytes"
	"crypto/ed25519"
	"os"
	"path/filepath"
	"testing"
)

// TestLoadOrCreateGarbageFileIsErrorNotPanic: a corrupted identity file must
// produce a clear error, and the file must be left untouched (never
// silently overwritten with a fresh identity — that would silently change
// the node's peer ID).
func TestLoadOrCreateGarbageFileIsErrorNotPanic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "identity")

	for name, content := range map[string][]byte{
		"short":    bytes.Repeat([]byte{0xAB}, 10),
		"31 bytes": bytes.Repeat([]byte{0xCD}, ed25519.SeedSize-1),
		"33 bytes": bytes.Repeat([]byte{0xEF}, ed25519.SeedSize+1),
	} {
		if err := os.WriteFile(path, content, 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
		if _, err := LoadOrCreate(path); err == nil {
			t.Errorf("%s file: LoadOrCreate accepted garbage", name)
		}
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%s: file unreadable after failed load: %v", name, err)
		}
		if !bytes.Equal(got, content) {
			t.Errorf("%s: failed load modified the identity file", name)
		}
	}
}

// TDD: Ed25519PubToX25519 of an all-zero (or otherwise low-order) public key
// currently "succeeds", yielding a low-order X25519 point. A node can publish
// such a key to the DHT; when picked as a relay or destination, onion Build
// fails and pollutes the sender's retry loop. The conversion must reject
// keys whose X25519 image is a low-order point (multiplying by 8 — the
// cofactor — yields the identity exactly for those, which curve25519
// reports as an error).
func TestEd25519PubToX25519RejectsLowOrderPoints(t *testing.T) {
	if _, err := Ed25519PubToX25519(make([]byte, 32)); err == nil {
		t.Fatal("all-zero ed25519 pubkey converted to a low-order X25519 point")
	}
	// y == 1 maps to u == 0 (division by zero case) — already rejected, pin it.
	one := make([]byte, 32)
	one[0] = 1
	if _, err := Ed25519PubToX25519(one); err == nil {
		t.Fatal("y=1 ed25519 pubkey converted")
	}
}

// TestEd25519PubToX25519StillAcceptsRealKeys guards the rejection against
// over-firing: freshly generated real keys must convert, and the result must
// be usable for ECDH with the matching private key.
func TestEd25519PubToX25519StillAcceptsRealKeys(t *testing.T) {
	for i := 0; i < 16; i++ {
		id, err := LoadOrCreate("")
		if err != nil {
			t.Fatalf("generate: %v", err)
		}
		u, err := Ed25519PubToX25519(id.SigningPub())
		if err != nil {
			t.Fatalf("key %d rejected: %v", i, err)
		}
		if !bytes.Equal(u, id.ECDHPub()) {
			t.Fatalf("key %d: conversion disagrees with derived ECDH pub", i)
		}
		if shared, err := id.ECDH(u); err != nil || len(shared) != 32 {
			t.Fatalf("key %d: converted point unusable for ECDH (err=%v)", i, err)
		}
	}
}
