package onion

import (
	"bytes"
	"errors"
	"testing"
)

// TDD: the spec fixes the link-layer packet at exactly WireSize ("每个链路上
// 的包都是恒定尺寸") — a relay must refuse anything else, otherwise a
// malformed or hostile peer reintroduces a size side channel. Unwrap is
// expected to enforce this; the check is added in the same change.
func TestUnwrapRejectsNonWireSizePackets(t *testing.T) {
	layer := bytes.Repeat([]byte{0xAB}, minLayerSize)
	wire, err := Wrap(5, layer)
	if err != nil {
		t.Fatalf("wrap: %v", err)
	}

	cases := map[string][]byte{
		"empty":            {},
		"header only":      wire[:wireHeader],
		"one byte short":   wire[:WireSize-1],
		"one byte long":    append(wire, 0x00),
		"min valid layer":  bytes.Repeat([]byte{0x01}, wireHeader+minLayerSize),
		"1MB framed junk":  bytes.Repeat([]byte{0x02}, 1<<20),
	}
	for name, packet := range cases {
		if _, _, err := Unwrap(packet); !errors.Is(err, ErrWireInvalid) {
			t.Errorf("%s: Unwrap accepted a %d-byte packet (want ErrWireInvalid), got %v", name, len(packet), err)
		}
	}

	// The exactly-WireSize packet still decodes.
	if got, _, err := Unwrap(wire); err != nil || got != 5 {
		t.Fatalf("valid packet rejected: ttl=%d err=%v", got, err)
	}
}

// TestUnwrapClenBoundaries walks the accepted clen range: below minLayerSize
// and above maxLayerSize must fail; the exact bounds must pass.
func TestUnwrapClenBoundaries(t *testing.T) {
	build := func(clen int) []byte {
		p := make([]byte, WireSize)
		p[0] = 1
		p[1] = byte(clen >> 8)
		p[2] = byte(clen)
		return p
	}

	for _, clen := range []int{0, 1, minLayerSize - 1} {
		if _, _, err := Unwrap(build(clen)); err == nil {
			t.Errorf("clen=%d below minimum accepted", clen)
		}
	}
	for _, clen := range []int{minLayerSize, minLayerSize + 1, maxLayerSize} {
		if _, _, err := Unwrap(build(clen)); err != nil {
			t.Errorf("clen=%d inside valid range rejected: %v", clen, err)
		}
	}
	// maxLayerSize+1 has nowhere to fit in a 2-byte field under WireSize-3,
	// so the overflow case is the same bound from Wrap's side.
	if _, err := Wrap(1, bytes.Repeat([]byte{0x00}, maxLayerSize+1)); !errors.Is(err, ErrLayerTooLarge) {
		t.Errorf("Wrap accepted layer > maxLayerSize: %v", err)
	}
}

// TestWrapPaddingIsRandom pins that identical layers never produce identical
// wire packets (observers must not be able to fingerprint re-sends by
// comparing pad bytes).
func TestWrapPaddingIsRandom(t *testing.T) {
	layer := bytes.Repeat([]byte{0xCD}, minLayerSize)
	w1, err := Wrap(7, layer)
	if err != nil {
		t.Fatalf("wrap 1: %v", err)
	}
	w2, err := Wrap(7, layer)
	if err != nil {
		t.Fatalf("wrap 2: %v", err)
	}
	if bytes.Equal(w1, w2) {
		t.Fatal("two wraps of the same layer produced identical packets")
	}
	if !bytes.Equal(w1[:wireHeader+len(layer)], w2[:wireHeader+len(layer)]) {
		t.Fatal("ciphertext region differs between wraps of the same layer")
	}
}

// FuzzWireUnwrap runs Unwrap against arbitrary inputs; it must never panic
// and never accept a non-WireSize packet.
func FuzzWireUnwrap(f *testing.F) {
	layer := bytes.Repeat([]byte{0x42}, minLayerSize)
	seed, _ := Wrap(3, layer)
	f.Add(seed)
	f.Add(seed[:10])
	f.Add([]byte{})
	f.Add(bytes.Repeat([]byte{0xFF}, WireSize))
	f.Fuzz(func(t *testing.T, packet []byte) {
		ttl, layer, err := Unwrap(packet)
		if err != nil {
			return
		}
		if len(packet) != WireSize {
			t.Fatalf("Unwrap accepted %d-byte packet", len(packet))
		}
		if len(layer) < minLayerSize || len(layer) > maxLayerSize {
			t.Fatalf("returned layer out of bounds: %d", len(layer))
		}
		_ = ttl
	})
}
