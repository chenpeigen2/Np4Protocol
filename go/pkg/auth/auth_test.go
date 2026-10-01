package auth

import (
	"bytes"
	"path/filepath"
	"testing"

	"Np4Protocol/go/pkg/identity"
)

func newID(t *testing.T, name string) *identity.Identity {
	t.Helper()
	id, err := identity.LoadOrCreate(filepath.Join(t.TempDir(), name))
	if err != nil {
		t.Fatalf("identity %s: %v", name, err)
	}
	return id
}

// TestTagVerifyRoundTrip pins the core contract: A tags a message addressed
// to B; B verifies it using A's published key and peer ID.
func TestTagVerifyRoundTrip(t *testing.T) {
	a, b := newID(t, "a"), newID(t, "b")
	msgID := bytes.Repeat([]byte{0x01}, 16)
	content := []byte("meet at the usual place")

	tag, err := Tag(a, b.ECDHPub(), b.PeerID(), msgID, content)
	if err != nil {
		t.Fatalf("Tag: %v", err)
	}
	if len(tag) != TagSize {
		t.Fatalf("tag size %d, want %d", len(tag), TagSize)
	}
	ok, err := Verify(b, a.ECDHPub(), a.PeerID(), msgID, content, tag)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !ok {
		t.Fatal("valid tag rejected")
	}
}

// TestVerifyRejectsWrongSender: C cannot impersonate A against B — the tag
// binds the sender's key, and C's pairwise key with B differs.
func TestVerifyRejectsWrongSender(t *testing.T) {
	a, b, c := newID(t, "a"), newID(t, "b"), newID(t, "c")
	msgID := bytes.Repeat([]byte{0x02}, 16)
	content := []byte("from a")

	tag, err := Tag(a, b.ECDHPub(), b.PeerID(), msgID, content)
	if err != nil {
		t.Fatalf("Tag: %v", err)
	}
	ok, err := Verify(b, c.ECDHPub(), c.PeerID(), msgID, content, tag)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if ok {
		t.Fatal("tag from A accepted under C's identity")
	}
}

// TestVerifyRejectsTampering: any content or msg_id change must break the tag.
func TestVerifyRejectsTampering(t *testing.T) {
	a, b := newID(t, "a"), newID(t, "b")
	msgID := bytes.Repeat([]byte{0x03}, 16)
	tag, err := Tag(a, b.ECDHPub(), b.PeerID(), msgID, []byte("original"))
	if err != nil {
		t.Fatalf("Tag: %v", err)
	}
	ok, err := Verify(b, a.ECDHPub(), a.PeerID(), msgID, []byte("tampered"), tag)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if ok {
		t.Fatal("tampered content accepted")
	}
	ok, _ = Verify(b, a.ECDHPub(), a.PeerID(), bytes.Repeat([]byte{0x04}, 16), []byte("original"), tag)
	if ok {
		t.Fatal("different msg_id accepted")
	}
}

// TestTagNotTransplantableBetweenPairs: a tag made for the A→B conversation
// must not verify as an A→C tag — the HKDF info binds both peer IDs, so a
// leaked tag cannot be reused against a different recipient.
func TestTagNotTransplantableBetweenPairs(t *testing.T) {
	a, b, c := newID(t, "a"), newID(t, "b"), newID(t, "c")
	msgID := bytes.Repeat([]byte{0x05}, 16)
	content := []byte("for b only")

	tag, err := Tag(a, b.ECDHPub(), b.PeerID(), msgID, content)
	if err != nil {
		t.Fatalf("Tag for B: %v", err)
	}
	ok, err := Verify(c, a.ECDHPub(), a.PeerID(), msgID, content, tag)
	if err != nil {
		t.Fatalf("Verify at C: %v", err)
	}
	if ok {
		t.Fatal("A→B tag verified at C: pair binding broken")
	}
}

// TestTagDeterministic: same inputs produce the same tag — required because
// the receiver recomputes rather than decrypts.
func TestTagDeterministic(t *testing.T) {
	a, b := newID(t, "a"), newID(t, "b")
	msgID := bytes.Repeat([]byte{0x06}, 16)
	t1, err := Tag(a, b.ECDHPub(), b.PeerID(), msgID, []byte("same"))
	if err != nil {
		t.Fatalf("Tag: %v", err)
	}
	t2, _ := Tag(a, b.ECDHPub(), b.PeerID(), msgID, []byte("same"))
	if !bytes.Equal(t1, t2) {
		t.Fatal("tag is not deterministic")
	}
}

// TestVerifyRejectsBadKeySizes: malformed inputs must error (not panic) so a
// contact scan can skip them.
func TestVerifyRejectsBadKeySizes(t *testing.T) {
	a, b := newID(t, "a"), newID(t, "b")
	msgID := bytes.Repeat([]byte{0x07}, 16)
	if _, err := Verify(b, a.ECDHPub(), a.PeerID(), msgID, nil, make([]byte, 8)); err == nil {
		t.Fatal("short tag accepted")
	}
	if _, err := Verify(b, make([]byte, 31), a.PeerID(), msgID, nil, make([]byte, TagSize)); err == nil {
		t.Fatal("short sender pub accepted")
	}
	if _, err := Tag(a, make([]byte, 10), b.PeerID(), msgID, nil); err == nil {
		t.Fatal("short receiver pub accepted")
	}
}
