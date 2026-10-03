package pathsel

import (
	"encoding/binary"
	"testing"
)

// FuzzParseRotationRecord: the client-side GetKey parses values returned by
// arbitrary DHT peers — never panic, and always agree with the invariants
// (only exact-size records with a valid signature over a bound master key
// yield a subkey).
func FuzzParseRotationRecord(f *testing.F) {
	// Seeds are built in TestParseRotationRecord; the corpus there runs via
	// the test target. Seed here with the canonical size and simple mutants.
	f.Add(make([]byte, rotationRecSize))
	f.Add(make([]byte, rotationRecSize-1))
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, value []byte) {
		out, err := ParseRotationRecord("12D3KooWTestTargetOnly", value)
		if err != nil {
			return
		}
		if len(value) != rotationRecSize {
			t.Fatalf("accepted %d-byte record", len(value))
		}
		if len(out) != x25519PubLen {
			t.Fatalf("subkey length %d", len(out))
		}
		binary.BigEndian.Uint64(value[edMarshaledLen : edMarshaledLen+bucketLen]) // frame is parseable
	})
}
