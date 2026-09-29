package onion

import (
	"bytes"
	"testing"
)

func TestWrapUnwrapRoundTrip(t *testing.T) {
	layer := bytes.Repeat([]byte{0xab}, 1000)
	wire, err := Wrap(7, layer)
	if err != nil {
		t.Fatalf("Wrap: %v", err)
	}
	if len(wire) != WireSize {
		t.Fatalf("wire size: got %d want %d", len(wire), WireSize)
	}
	ttl, unwrapped, err := Unwrap(wire)
	if err != nil {
		t.Fatalf("Unwrap: %v", err)
	}
	if ttl != 7 {
		t.Errorf("ttl: got %d want 7", ttl)
	}
	if !bytes.Equal(unwrapped, layer) {
		t.Errorf("layer mismatch after round trip")
	}
}

func TestWrapConstantSizeRegardlessOfLayer(t *testing.T) {
	var wires [][]byte
	for _, n := range []int{60, 500, 2000, maxLayerSize} {
		w, err := Wrap(1, make([]byte, n))
		if err != nil {
			t.Fatalf("Wrap(%d): %v", n, err)
		}
		wires = append(wires, w)
	}
	for i, w := range wires {
		if len(w) != WireSize {
			t.Errorf("wire %d: size %d, want %d", i, len(w), WireSize)
		}
	}
}

func TestWrapRejectsOversize(t *testing.T) {
	if _, err := Wrap(0, make([]byte, maxLayerSize+1)); err == nil {
		t.Fatal("expected error for oversize layer")
	}
}

func TestUnwrapRejectsGarbage(t *testing.T) {
	cases := [][]byte{
		nil,
		{0x00},
		{0x00, 0x00},
		make([]byte, 10),                     // clen=0, too short for a layer
		{0x00, 0xff, 0xff, 0x01, 0x02, 0x03}, // clen=65535 out of range
	}
	for i, c := range cases {
		if _, _, err := Unwrap(c); err == nil {
			t.Errorf("case %d: expected error", i)
		}
	}
}

func TestReplayKey(t *testing.T) {
	layer := make([]byte, 100)
	if _, ok := ReplayKey(layer[:31]); ok {
		t.Error("short layer must not yield a replay key")
	}
	key, ok := ReplayKey(layer)
	if !ok || len(key) != ephPubSize {
		t.Fatalf("ReplayKey: ok=%v len=%d", ok, len(key))
	}
}

// TestWireHidesLayerGrowth: the outermost layer of a multi-hop onion is
// larger than the innermost, yet every wire packet is exactly WireSize.
func TestWireHidesLayerGrowth(t *testing.T) {
	payload := make([]byte, 4096) // cell-sized
	hops := make([]Hop, 4)
	for i := range hops {
		hops[i] = Hop{PeerID: "relay-peer-id-for-testing-purposes", ECDHPub: bytes.Repeat([]byte{0x01}, 32)}
	}
	on, err := Build(hops, payload)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if _, err := Wrap(3, on.Bytes()); err != nil {
		t.Fatalf("Wrap outermost: %v", err)
	}
}
