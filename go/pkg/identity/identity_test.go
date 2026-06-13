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

func TestLoadOrCreateEphemeral(t *testing.T) {
	// Empty path must produce a usable in-memory identity without touching disk.
	a, err := LoadOrCreate("")
	if err != nil {
		t.Fatalf("LoadOrCreate(\"\"): %v", err)
	}
	if a.PeerID() == "" {
		t.Fatal("ephemeral identity has empty PeerID")
	}
	b, _ := LoadOrCreate("")
	if a.PeerID() == b.PeerID() {
		t.Fatal("two ephemeral identities collided")
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

func TestECDHRejectsBadPubkey(t *testing.T) {
	a, _ := LoadOrCreate(t.TempDir() + "/a")

	cases := [][]byte{nil, {}, make([]byte, 16), make([]byte, 64)}
	for i, pub := range cases {
		if _, err := a.ECDH(pub); err == nil {
			t.Errorf("case %d: expected error for size %d", i, len(pub))
		}
	}
}

func TestDistinctIdentitiesProduceDistinctSharedSecrets(t *testing.T) {
	a, _ := LoadOrCreate(t.TempDir() + "/a")
	b, _ := LoadOrCreate(t.TempDir() + "/b")
	c, _ := LoadOrCreate(t.TempDir() + "/c")

	sAB, _ := a.ECDH(b.ECDHPub())
	sAC, _ := a.ECDH(c.ECDHPub())
	if string(sAB) == string(sAC) {
		t.Fatal("ECDH produced same shared secret with different peers")
	}
}

func TestECDHPubReturnsIndependentCopy(t *testing.T) {
	a, _ := LoadOrCreate(t.TempDir() + "/a")
	pub1 := a.ECDHPub()
	pub1[0] ^= 0xff
	pub2 := a.ECDHPub()
	if pub1[0] == pub2[0] {
		t.Fatal("ECDHPub did not return an independent copy")
	}
}
