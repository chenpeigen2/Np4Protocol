package onion

import (
	"bytes"
	"path/filepath"
	"testing"

	"Np4Protocol/go/pkg/identity"
)

func TestBuildDecodeSingleHop(t *testing.T) {
	dest, _ := identity.LoadOrCreate(filepath.Join(t.TempDir(), "dest"))

	payload := []byte("hello final")
	on, err := Build([]Hop{{PeerID: dest.PeerID(), ECDHPub: dest.ECDHPub()}}, payload)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	dec, err := Decode(on.Bytes(), dest)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if !dec.IsFinal {
		t.Errorf("expected IsFinal=true")
	}
	if !bytes.Equal(dec.Inner, payload) {
		t.Errorf("payload mismatch: got %q want %q", dec.Inner, payload)
	}
}

func TestBuildDecodeMultiHop(t *testing.T) {
	r1, _ := identity.LoadOrCreate(filepath.Join(t.TempDir(), "r1"))
	r2, _ := identity.LoadOrCreate(filepath.Join(t.TempDir(), "r2"))
	dest, _ := identity.LoadOrCreate(filepath.Join(t.TempDir(), "dest"))

	payload := []byte("multi-hop secret")
	hops := []Hop{
		{PeerID: r1.PeerID(), ECDHPub: r1.ECDHPub()},
		{PeerID: r2.PeerID(), ECDHPub: r2.ECDHPub()},
		{PeerID: dest.PeerID(), ECDHPub: dest.ECDHPub()},
	}
	on, err := Build(hops, payload)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	// r1 decodes outer
	d1, err := Decode(on.Bytes(), r1)
	if err != nil {
		t.Fatalf("r1 Decode: %v", err)
	}
	if d1.IsFinal {
		t.Fatal("r1 should not be final")
	}
	if d1.NextHop != r2.PeerID() {
		t.Errorf("r1 NextHop: got %s want %s", d1.NextHop, r2.PeerID())
	}

	// r2 decodes middle
	d2, err := Decode(d1.Inner, r2)
	if err != nil {
		t.Fatalf("r2 Decode: %v", err)
	}
	if d2.IsFinal {
		t.Fatal("r2 should not be final")
	}
	if d2.NextHop != dest.PeerID() {
		t.Errorf("r2 NextHop: got %s want %s", d2.NextHop, dest.PeerID())
	}

	// dest decodes final
	d3, err := Decode(d2.Inner, dest)
	if err != nil {
		t.Fatalf("dest Decode: %v", err)
	}
	if !d3.IsFinal {
		t.Fatal("dest should be final")
	}
	if !bytes.Equal(d3.Inner, payload) {
		t.Errorf("final payload: got %q want %q", d3.Inner, payload)
	}
}

func TestDecodeWrongKeyFails(t *testing.T) {
	r1, _ := identity.LoadOrCreate(filepath.Join(t.TempDir(), "r1"))
	other, _ := identity.LoadOrCreate(filepath.Join(t.TempDir(), "other"))

	on, _ := Build([]Hop{{PeerID: r1.PeerID(), ECDHPub: r1.ECDHPub()}}, []byte("x"))
	if _, err := Decode(on.Bytes(), other); err == nil {
		t.Fatal("expected error with wrong key")
	}
}

func FuzzDecode(f *testing.F) {
	dir := f.TempDir()
	dest, _ := identity.LoadOrCreate(filepath.Join(dir, "d"))
	on, _ := Build([]Hop{{PeerID: dest.PeerID(), ECDHPub: dest.ECDHPub()}}, []byte("seed"))
	f.Add(on.Bytes())
	f.Add([]byte{0, 1, 2, 3})
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, data []byte) {
		// Must not panic on arbitrary input.
		_, _ = Decode(data, dest)
	})
}
