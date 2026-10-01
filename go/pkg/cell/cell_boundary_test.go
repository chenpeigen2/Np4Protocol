package cell

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"
)

// TestSealOpenBinaryContent: the cell must carry arbitrary bytes, not just
// text — file chunks ([v2]) rely on this.
func TestSealOpenBinaryContent(t *testing.T) {
	content := make([]byte, 1024)
	for i := range content {
		content[i] = byte(i % 256)
	}
	c, id := sealText(t, content)
	msgID, _, _, got, err := Open(c)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Fatal("binary content corrupted by seal/open")
	}
	if !bytes.Equal(msgID, id) {
		t.Fatal("msg_id corrupted by seal/open")
	}
}

// TestSealZeroContent: empty content is valid and decodes to empty.
func TestSealZeroContent(t *testing.T) {
	c, _ := sealText(t, nil)
	_, _, _, got, err := Open(c)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("empty content decoded to %d bytes", len(got))
	}
}

// TestSealExactCapacityBoundary: content of exactly MaxContent bytes must
// seal; one byte more must fail with ErrTooLarge.
func TestSealExactCapacityBoundary(t *testing.T) {
	content := bytes.Repeat([]byte{0x5A}, MaxContent)
	if _, err := Seal(make([]byte, idSize), TypeText, make([]byte, tagSize), content); err != nil {
		t.Fatalf("seal at exact capacity failed: %v", err)
	}
	id, _ := NewMsgID()
	if _, err := Seal(id, TypeText, make([]byte, tagSize), append(content, 0x00)); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("one byte over capacity: got %v, want ErrTooLarge", err)
	}
}

// TestOpenPaddingNotExposed: the decoded content must stop at content_len —
// the zero padding behind it never leaks into the payload.
func TestOpenPaddingNotExposed(t *testing.T) {
	content := []byte("hello")
	c, _ := sealText(t, content)
	_, _, _, got, err := Open(c)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Fatalf("content = %q, want %q", got, content)
	}
	// Padding region must be zero so content_len is the only length signal.
	if !bytes.Equal(c[overhead+len(content):], make([]byte, Size-overhead-len(content))) {
		t.Fatal("padding region is not zeroed")
	}
}

// TestOpenContentLenOverrunMatrix drives the content_len field across its
// 16-bit range; values beyond capacity must fail, valid ones must pass.
func TestOpenContentLenOverrunMatrix(t *testing.T) {
	c := make([]byte, Size)
	for _, l := range []int{0, 1, MaxContent} {
		binary.BigEndian.PutUint16(c[idSize+typeSize+tagSize:], uint16(l))
		if _, _, _, _, err := Open(c); err != nil {
			t.Errorf("content_len=%d must be valid, got %v", l, err)
		}
	}
	for _, l := range []int{MaxContent + 1, MaxContent + 100, 0xFFFF} {
		binary.BigEndian.PutUint16(c[idSize+typeSize+tagSize:], uint16(l))
		if _, _, _, _, err := Open(c); !errors.Is(err, ErrInvalidCell) {
			t.Errorf("content_len=%d must be rejected, got %v", l, err)
		}
	}
}

// TestOpenMsgIDIsCopy: the returned msg_id must be owned by the caller —
// mutating the input cell afterwards must not change it.
func TestOpenMsgIDIsCopy(t *testing.T) {
	c, _ := sealText(t, []byte("x"))
	id, _, _, _, err := Open(c)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	want := append([]byte(nil), id...)
	for i := range c[:idSize] {
		c[i] = 0xFF
	}
	if !bytes.Equal(id, want) {
		t.Fatal("msg_id aliases the input cell")
	}
}

// FuzzCellOpen: Open must never panic on arbitrary input and must always
// agree with its own invariants (valid only for exact-size cells whose
// content_len fits).
func FuzzCellOpen(f *testing.F) {
	good, _ := Seal(make([]byte, idSize), TypeText, make([]byte, tagSize), []byte("seed content"))
	f.Add(good)
	f.Add(good[:Size-1])
	f.Add([]byte{})
	f.Add(make([]byte, Size))
	f.Fuzz(func(t *testing.T, c []byte) {
		id, _, tag, content, err := Open(c)
		if err != nil {
			return
		}
		if len(c) != Size {
			t.Fatalf("accepted %d-byte cell", len(c))
		}
		if len(id) != idSize {
			t.Fatalf("msg_id len %d", len(id))
		}
		if len(tag) != tagSize {
			t.Fatalf("tag len %d", len(tag))
		}
		if overhead+len(content) > Size {
			t.Fatalf("content overruns cell: %d", len(content))
		}
	})
}
