package cell

import (
	"bytes"
	"testing"
)

// sealText builds a text cell with a fixed tag, for tests that don't care
// about auth specifics.
func sealText(t *testing.T, content []byte) ([]byte, []byte) {
	t.Helper()
	id, err := NewMsgID()
	if err != nil {
		t.Fatalf("NewMsgID: %v", err)
	}
	c, err := Seal(id, TypeText, make([]byte, tagSize), content)
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	return c, id
}

func TestSealOpenRoundTrip(t *testing.T) {
	content := []byte("hello cell")
	c, id := sealText(t, content)
	if len(c) != Size {
		t.Fatalf("cell size: got %d want %d", len(c), Size)
	}
	msgID, typ, tag, opened, err := Open(c)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if !bytes.Equal(opened, content) {
		t.Errorf("content: got %q want %q", opened, content)
	}
	if !bytes.Equal(msgID, id) {
		t.Errorf("msgID mismatch")
	}
	if typ != TypeText {
		t.Errorf("type: got %#x want text", typ)
	}
	if len(tag) != tagSize {
		t.Errorf("tag len: got %d want %d", len(tag), tagSize)
	}
	if len(msgID) != idSize {
		t.Errorf("msgID len: got %d want %d", len(msgID), idSize)
	}
}

func TestSealOpenTypeAndTagRoundTrip(t *testing.T) {
	id, _ := NewMsgID()
	tag := bytes.Repeat([]byte{0xAB}, tagSize)
	c, err := Seal(id, TypeDummy, tag, nil)
	if err != nil {
		t.Fatalf("Seal dummy: %v", err)
	}
	_, typ, gotTag, content, err := Open(c)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if typ != TypeDummy {
		t.Errorf("dummy type lost: got %#x", typ)
	}
	if !bytes.Equal(gotTag, tag) {
		t.Errorf("tag corrupted")
	}
	if len(content) != 0 {
		t.Errorf("dummy content should be empty, got %d bytes", len(content))
	}
}

func TestSealRejectsBadMsgID(t *testing.T) {
	if _, err := Seal(make([]byte, 15), TypeText, make([]byte, tagSize), nil); err == nil {
		t.Fatal("expected error for short msg_id")
	}
	if _, err := Seal(make([]byte, 17), TypeText, make([]byte, tagSize), nil); err == nil {
		t.Fatal("expected error for long msg_id")
	}
}

func TestSealRejectsBadTag(t *testing.T) {
	id, _ := NewMsgID()
	if _, err := Seal(id, TypeText, make([]byte, 15), nil); err == nil {
		t.Fatal("expected error for short tag")
	}
	if _, err := Seal(id, TypeText, nil, nil); err == nil {
		t.Fatal("expected error for nil tag")
	}
}

func TestSealMaxContent(t *testing.T) {
	c, err := Seal(make([]byte, idSize), TypeText, make([]byte, tagSize), make([]byte, MaxContent))
	if err != nil {
		t.Fatalf("Seal at max content: %v", err)
	}
	if len(c) != Size {
		t.Fatalf("cell size: got %d", len(c))
	}
}

func TestSealRejectsOversize(t *testing.T) {
	id, _ := NewMsgID()
	if _, err := Seal(id, TypeText, make([]byte, tagSize), make([]byte, MaxContent+1)); err == nil {
		t.Fatal("expected error for oversize content")
	}
}

func TestOpenRejectsWrongSize(t *testing.T) {
	for _, n := range []int{0, 100, Size - 1, Size + 1} {
		if _, _, _, _, err := Open(make([]byte, n)); err == nil {
			t.Errorf("size %d: expected error", n)
		}
	}
}

func TestOpenRejectsBadLengthField(t *testing.T) {
	c := make([]byte, Size)
	c[idSize+typeSize+tagSize] = 0xff     // content_len = 65280 > capacity
	c[idSize+typeSize+tagSize+1] = 0xff   //
	if _, _, _, _, err := Open(c); err == nil {
		t.Fatal("expected error for bad content_len")
	}
}

func TestMsgIDsAreUnique(t *testing.T) {
	id1, err := NewMsgID()
	if err != nil {
		t.Fatalf("NewMsgID: %v", err)
	}
	id2, _ := NewMsgID()
	if bytes.Equal(id1, id2) {
		t.Fatal("two message IDs are identical")
	}
}
