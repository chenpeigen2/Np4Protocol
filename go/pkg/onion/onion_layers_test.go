package onion

import (
	"bytes"
	"errors"
	"testing"

	"Np4Protocol/go/pkg/identity"
)

// buildTestIdentities makes n fresh identities (ephemeral, no disk IO).
func buildTestIdentities(t *testing.T, n int) []*identity.Identity {
	t.Helper()
	ids := make([]*identity.Identity, 0, n)
	for i := 0; i < n; i++ {
		id, err := identity.LoadOrCreate("")
		if err != nil {
			t.Fatalf("identity %d: %v", i, err)
		}
		ids = append(ids, id)
	}
	return ids
}

func toHops(ids []*identity.Identity) []Hop {
	hops := make([]Hop, 0, len(ids))
	for _, id := range ids {
		hops = append(hops, Hop{PeerID: id.PeerID(), ECDHPub: id.ECDHPub()})
	}
	return hops
}

// TestBuildPeelFullJourney: a 3-hop onion must peel hop by hop — each
// intermediate identity learns only its next hop and nothing about the
// payload, the final identity sees IsFinal with the original cell, and every
// layer is uniquely identifiable by its replay key.
func TestBuildPeelFullJourney(t *testing.T) {
	ids := buildTestIdentities(t, 3)
	hops := toHops(ids)
	payload := bytes.Repeat([]byte{0x77}, 1000)

	on, err := Build(hops, payload)
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	seenKeys := map[string]bool{}
	layer := on.Bytes()
	for hop := 0; hop < 2; hop++ {
		key, ok := ReplayKey(layer)
		if !ok {
			t.Fatalf("hop %d: no replay key", hop)
		}
		if seenKeys[string(key)] {
			t.Fatalf("hop %d: replay key reused across layers", hop)
		}
		seenKeys[string(key)] = true

		dec, err := Decode(layer, ids[hop])
		if err != nil {
			t.Fatalf("hop %d: decode: %v", hop, err)
		}
		if dec.IsFinal {
			t.Fatalf("hop %d: peeled final at an intermediate hop", hop)
		}
		if dec.NextHop != hops[hop+1].PeerID {
			t.Fatalf("hop %d: next hop %s, want %s", hop, dec.NextHop, hops[hop+1].PeerID)
		}
		if bytes.Contains(dec.Inner, payload[:32]) {
			t.Fatalf("hop %d: inner bytes contain plaintext payload", hop)
		}
		layer = dec.Inner
	}

	dec, err := Decode(layer, ids[2])
	if err != nil {
		t.Fatalf("final decode: %v", err)
	}
	if !dec.IsFinal {
		t.Fatal("final hop not marked IsFinal")
	}
	if !bytes.Equal(dec.Inner, payload) {
		t.Fatalf("payload mismatch: got %d bytes, want %d", len(dec.Inner), len(payload))
	}
}

// TestDecodeWithAnyOtherIdentityFails: a layer encrypted for hop k must be
// undecodable by the other hops in the path AND by unrelated identities.
func TestDecodeWithAnyOtherIdentityFails(t *testing.T) {
	pathIds := buildTestIdentities(t, 3)
	on, err := Build(toHops(pathIds), []byte("secret"))
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	foreignIds := buildTestIdentities(t, 2)

	try := func(tag string, id *identity.Identity) {
		t.Helper()
		if dec, err := Decode(on.Bytes(), id); err == nil {
			t.Errorf("%s decoded a layer addressed to someone else (final=%v)", tag, dec.IsFinal)
		}
	}
	// The outermost layer is for pathIds[0]; every other key must fail.
	try("hop1-of-same-path", pathIds[1])
	try("hop2-of-same-path", pathIds[2])
	try("foreign-a", foreignIds[0])
	try("foreign-b", foreignIds[1])
}

// TestBuildVeryDeepPathExceedsWire: nested layers grow ~101 bytes each;
// enough hops must exceed the wire capacity and fail loudly instead of
// producing an undeliverable packet.
func TestBuildVeryDeepPathExceedsWire(t *testing.T) {
	ids := buildTestIdentities(t, 60)
	payload := bytes.Repeat([]byte{0x01}, maxLayerSize-minLayerSize-1)
	if _, err := Build(toHops(ids), payload); !errors.Is(err, ErrLayerTooLarge) {
		t.Fatalf("60-hop onion with near-max payload: got %v, want ErrLayerTooLarge", err)
	}
}

// TestReplayKeyShortLayer: ReplayKey must refuse layers shorter than the
// ephemeral key instead of panicking.
func TestReplayKeyShortLayer(t *testing.T) {
	for _, n := range []int{0, 1, ephPubSize - 1} {
		if _, ok := ReplayKey(bytes.Repeat([]byte{0x00}, n)); ok {
			t.Errorf("ReplayKey accepted %d-byte layer", n)
		}
	}
	if _, ok := ReplayKey(bytes.Repeat([]byte{0x00}, ephPubSize)); !ok {
		t.Error("ReplayKey rejected an exactly-sized layer")
	}
}
