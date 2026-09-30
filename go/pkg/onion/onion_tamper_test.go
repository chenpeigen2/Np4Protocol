package onion

import (
	"bytes"
	"testing"

	"Np4Protocol/go/pkg/identity"
)

// TestDecodeRelayLayerEmptyNextHop: a relay layer whose next-hop length is 0
// is malformed — Decode must reject it rather than produce an empty peer ID
// that would fail later at dial time.
func TestDecodeRelayLayerEmptyNextHop(t *testing.T) {
	id, err := identity.LoadOrCreate("")
	if err != nil {
		t.Fatalf("identity: %v", err)
	}
	// flagRelay || next_len(=0) || empty || inner
	plaintext := append([]byte{flagRelay, 0, 0, 0, 0}, []byte("inner")...)
	layer, err := encryptLayer(Hop{PeerID: id.PeerID(), ECDHPub: id.ECDHPub()}, plaintext)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if _, err := Decode(layer, id); err == nil {
		t.Fatal("Decode accepted a relay layer with an empty next hop")
	}
}

// TestDecodeTruncatedNextHop: a length prefix larger than the remaining
// plaintext must fail, not slice out of bounds.
func TestDecodeTruncatedNextHop(t *testing.T) {
	id, err := identity.LoadOrCreate("")
	if err != nil {
		t.Fatalf("identity: %v", err)
	}
	plaintext := append([]byte{flagRelay}, []byte{0, 0, 0, 64}...) // claims 64 bytes, has 0
	plaintext = append(plaintext, []byte("short")...)
	layer, err := encryptLayer(Hop{PeerID: id.PeerID(), ECDHPub: id.ECDHPub()}, plaintext)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if _, err := Decode(layer, id); err == nil {
		t.Fatal("Decode accepted a relay layer with a truncated next hop")
	}
}

// TestDecodeBitFlipTamperDetection: single-bit changes in every region of a
// layer (ephemeral key, nonce, ciphertext) must fail the AEAD open — the
// relay cannot undetectably modify anything.
func TestDecodeBitFlipTamperDetection(t *testing.T) {
	ids := buildTestIdentities(t, 2)
	on, err := Build(toHops(ids[:1]), bytes.Repeat([]byte{0x11}, 128))
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	layer := on.Bytes()

	// Regions of the layer: [0,32) ephemeral pub, [32,44) nonce, [44,...) ciphertext.
	regions := []struct {
		name string
		at   int
	}{
		{"ephemeral pub", 0},
		{"ephemeral pub high", 31},
		{"nonce", 35},
		{"ciphertext", 50},
		{"ciphertext tail", len(layer) - 1},
	}
	for _, r := range regions {
		tampered := bytes.Clone(layer)
		tampered[r.at] ^= 0x01
		if dec, err := Decode(tampered, ids[0]); err == nil {
			t.Fatalf("%s: bit flip at %d decoded cleanly (final=%v)", r.name, r.at, dec.IsFinal)
		}
	}
}

// TestBuildRejectsUnusableECDHPubkeys: hop pubkeys that make X25519 fail
// (wrong length, low-order points like all-zeros) must surface as Build
// errors, never panics — a poison key in the DHT must not crash a sender.
func TestBuildRejectsUnusableECDHPubkeys(t *testing.T) {
	payload := []byte("payload")
	cases := map[string]Hop{
		"nil pubkey":      {PeerID: buildTestIdentities(t, 1)[0].PeerID()},
		"short pubkey":    {PeerID: buildTestIdentities(t, 1)[0].PeerID(), ECDHPub: bytes.Repeat([]byte{0x01}, 16)},
		"all-zero pubkey": {PeerID: buildTestIdentities(t, 1)[0].PeerID(), ECDHPub: make([]byte, 32)},
	}
	for name, hop := range cases {
		if _, err := Build([]Hop{hop}, payload); err == nil {
			t.Errorf("%s: Build accepted an unusable ECDH pubkey", name)
		}
	}
}
