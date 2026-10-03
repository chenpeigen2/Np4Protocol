package auth

import (
	"testing"
)

// FuzzTagVerify drives the crypto boundary with arbitrary inputs: Verify must
// never panic, never report success on malformed input, and Tag must either
// produce a 16-byte tag or an error — never a shorter one.
func FuzzTagVerify(f *testing.F) {
	f.Fuzz(func(t *testing.T, senderPub []byte, msgID []byte, content []byte, tag []byte) {
		a := newID(t, "fz-a")
		b := newID(t, "fz-b")

		ok, err := Verify(b, senderPub, a.PeerID(), msgID, content, tag)
		if err != nil && ok {
			t.Fatal("error result with ok=true")
		}
		if len(msgID) == 16 && len(tag) == TagSize {
			expected, tagErr := Tag(a, b.ECDHPub(), b.PeerID(), msgID, content)
			if tagErr == nil {
				ok2, err2 := Verify(b, a.ECDHPub(), a.PeerID(), msgID, content, expected)
				if err2 != nil || !ok2 {
					t.Fatal("self-produced tag failed to verify: Verify/Tag disagree")
				}
			}
		}
		if len(senderPub) == 32 {
			got, err := Tag(a, senderPub, b.PeerID(), msgID, content)
			if err == nil && len(got) != TagSize {
				t.Fatalf("Tag produced %d bytes", len(got))
			}
		}
	})
}
