package identity

import (
	"path/filepath"
	"testing"
)

func TestLoadOrCreatePersists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "id")

	id1, err := LoadOrCreate(path)
	if err != nil {
		t.Fatalf("first LoadOrCreate: %v", err)
	}
	id2, err := LoadOrCreate(path)
	if err != nil {
		t.Fatalf("second LoadOrCreate: %v", err)
	}
	if id1.PeerID() != id2.PeerID() {
		t.Errorf("identity not persistent: %s vs %s", id1.PeerID(), id2.PeerID())
	}
}

func TestECDHSymmetric(t *testing.T) {
	a, _ := LoadOrCreate(t.TempDir() + "/a")
	b, _ := LoadOrCreate(t.TempDir() + "/b")

	s1, err := a.ECDH(b.ECDHPub())
	if err != nil {
		t.Fatalf("a.ECDH(b): %v", err)
	}
	s2, err := b.ECDH(a.ECDHPub())
	if err != nil {
		t.Fatalf("b.ECDH(a): %v", err)
	}
	if string(s1) != string(s2) {
		t.Errorf("ECDH not symmetric: %x vs %x", s1, s2)
	}
}
