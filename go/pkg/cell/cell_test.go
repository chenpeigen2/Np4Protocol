package cell

import (
	"bytes"
	"testing"
)

func TestSealOpenRoundTrip(t *testing.T) {
	content := []byte("hello cell")
	c, err := Seal(content)
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	if len(c) != Size {
		t.Fatalf("cell size: got %d want %d", len(c), Size)
	}
	msgID, opened, err := Open(c)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if !bytes.Equal(opened, content) {
		t.Errorf("content: got %q want %q", opened, content)
	}
	if len(msgID) != idSize {
		t.Errorf("msgID len: got %d want %d", len(msgID), idSize)
	}
}

func TestSealMaxContent(t *testing.T) {
	c, err := Seal(make([]byte, maxContent))
	if err != nil {
		t.Fatalf("Seal at max content: %v", err)
	}
	if len(c) != Size {
		t.Fatalf("cell size: got %d", len(c))
	}
}

func TestSealRejectsOversize(t *testing.T) {
	if _, err := Seal(make([]byte, maxContent+1)); err == nil {
		t.Fatal("expected error for oversize content")
	}
}

func TestOpenRejectsWrongSize(t *testing.T) {
	for _, n := range []int{0, 100, Size - 1, Size + 1} {
		if _, _, err := Open(make([]byte, n)); err == nil {
			t.Errorf("size %d: expected error", n)
		}
	}
}

func TestOpenRejectsBadLengthField(t *testing.T) {
	c := make([]byte, Size)
	c[idSize] = 0xff // content_len = 65280 > capacity
	c[idSize+1] = 0xff
	if _, _, err := Open(c); err == nil {
		t.Fatal("expected error for bad content_len")
	}
}

func TestMsgIDsAreUnique(t *testing.T) {
	c1, _ := Seal([]byte("same"))
	c2, _ := Seal([]byte("same"))
	if bytes.Equal(c1[:idSize], c2[:idSize]) {
		t.Fatal("two seals produced identical msg_id")
	}
}
