// Package auth implements pairwise sender authentication for mix messages.
//
// A sender proves "this message comes from a node the receiver knows" without
// revealing anything to relays: the sender derives a pairwise key from its own
// static X25519 identity key and the receiver's published ECDH key, then HMACs
// msg_id‖content. The tag rides inside the innermost onion layer, so only the
// receiver sees it.
//
// The receiver does NOT know who sent a message before verifying — the sender
// is anonymous by construction. Instead it recomputes the expected tag once
// per known contact (published key + peer ID) and accepts the first match.
// This yields:
//
//   - sender authenticity: only holders of a contact's private key produce a
//     tag that verifies under that contact's published key;
//   - deniability: the tag is a symmetric MAC the receiver could have computed
//     itself, so a third party with the full transcript cannot prove who sent
//     what;
//   - transplant resistance: the HKDF info binds both peer IDs, so a tag made
//     for one conversation does not verify in another.
//
// No forward secrecy yet: keys are static long-term identities ([v2] key
// rotation). Replay protection lives elsewhere (per-hop caches + end-to-end
// msg_id dedup).
package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"errors"
	"fmt"

	"Np4Protocol/go/pkg/identity"

	"golang.org/x/crypto/hkdf"

	"github.com/libp2p/go-libp2p/core/peer"
)

// TagSize is the HMAC-SHA256 truncation length carried in every cell.
const TagSize = 16

const saltString = "np4-auth-v1"

// ErrBadPub is returned when the peer's published key is unusable for ECDH
// (wrong size or low-order point). Verification callers skip the contact.
var ErrBadPub = errors.New("auth: unusable peer public key")

// Tag computes the sender-auth tag over msgID‖content using the pairwise key
// between id (sender side) and the receiver's published ECDH key theirPub.
// msgID must be exactly 16 bytes (cell.NewMsgID).
func Tag(id *identity.Identity, theirPub []byte, theirID peer.ID, msgID, content []byte) ([]byte, error) {
	key, err := pairwiseKey(id, theirPub, theirID)
	if err != nil {
		return nil, err
	}
	return mac(key, msgID, content), nil
}

// Verify recomputes the tag the sender would have produced (theirPub/theirID
// on the sending side, id on the receiving side) and compares in constant
// time. It returns false — not an error — on mismatch; errors are reserved
// for unusable keys so a single bad contact cannot break the scan loop.
func Verify(id *identity.Identity, senderPub []byte, senderID peer.ID, msgID, content, tag []byte) (bool, error) {
	if len(tag) != TagSize {
		return false, fmt.Errorf("%w: tag %d bytes, want %d", ErrBadPub, len(tag), TagSize)
	}
	key, err := pairwiseKey(id, senderPub, senderID)
	if err != nil {
		return false, err
	}
	expected := mac(key, msgID, content)
	return hmac.Equal(expected, tag), nil
}

// pairwiseKey derives the direction-independent shared key. Sender and
// receiver each run X25519 with their own private key against the other's
// published public key; ECDH symmetry gives both the same shared secret, and
// the sorted-pair HKDF info makes the key useless outside this exact pair.
func pairwiseKey(id *identity.Identity, theirPub []byte, theirID peer.ID) ([]byte, error) {
	shared, err := id.ECDH(theirPub)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrBadPub, err)
	}
	a, b := id.PeerID().String(), theirID.String()
	if a > b {
		a, b = b, a
	}
	key := make([]byte, 32)
	h := hkdf.New(sha256.New, shared, []byte(saltString), []byte(a+"|"+b))
	if _, err := h.Read(key); err != nil {
		return nil, err
	}
	return key, nil
}

func mac(key, msgID, content []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write(msgID)
	h.Write(content)
	return h.Sum(nil)[:TagSize]
}
