package onion

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"

	"golang.org/x/crypto/chacha20poly1305"
)

// Wire format between hops — CONSTANT SIZE on every link:
//
//	wire packet = ttl(1B) || clen(2B big-endian) || layer_ciphertext(clen) || random_pad
//	total length = WireSize
//
// Layer ciphertexts grow outward by ~65+hopID bytes per hop (inherent to
// nested encryption), so true size information lives in the plaintext clen
// prefix. This leaks remaining path depth to relays — but the plaintext TTL
// leaks the same thing already (declared in the threat model). What it hides
// is content size and path length from EXTERNAL observers, who see only a
// constant-rate stream of fixed-size packets.
//
// Flow: the sender pads to WireSize; each relay strips its layer, then re-pads
// the remaining ciphertext to WireSize for the next hop.

// WireSize is the protocol-constant packet size on every onion link. It is
// NOT negotiable: changing it after deployment is a protocol fork.
//
// Capacity check: innermost layer = 60 + 1 + 4096 (cell) = 4157 bytes; each
// hop adds 65 + len(peerID) ≈ 101. (WireSize-3-4157)/101 ≈ 39 hops of headroom;
// MaxInitialTTL is set well below that.
const WireSize = 8192

// wireHeader = ttl(1) + clen(2).
const wireHeader = 3

// minLayerSize = eph_pub(32) + nonce(12) + AEAD tag(16): the smallest
// decryptable layer.
const minLayerSize = ephPubSize + nonceSize + chacha20poly1305.Overhead

// maxLayerSize is the largest layer ciphertext that fits a wire packet.
const maxLayerSize = WireSize - wireHeader

// ErrLayerTooLarge is returned by Wrap when a layer ciphertext exceeds the
// wire capacity (path too long or payload too large).
var ErrLayerTooLarge = fmt.Errorf("layer exceeds wire capacity %d bytes", maxLayerSize)

// ErrWireInvalid is returned by Unwrap for malformed wire packets.
var ErrWireInvalid = errors.New("invalid wire packet")

// MaxInitialTTL bounds the sender-chosen initial TTL. Must stay within
// WireSize capacity (see capacity check above).
const MaxInitialTTL = 25

// Wrap pads layer to the constant WireSize wire packet with the given ttl.
// Padding is random bytes after the ciphertext, outside the AEAD — observers
// cannot distinguish padding from ciphertext, and receivers use clen.
func Wrap(ttl uint8, layer []byte) ([]byte, error) {
	if len(layer) > maxLayerSize {
		return nil, ErrLayerTooLarge
	}
	out := make([]byte, WireSize)
	out[0] = ttl
	binary.BigEndian.PutUint16(out[1:wireHeader], uint16(len(layer)))
	copy(out[wireHeader:], layer)
	if _, err := rand.Read(out[wireHeader+len(layer):]); err != nil {
		return nil, err
	}
	return out, nil
}

// Unwrap splits a wire packet into ttl and layer ciphertext. Packets MUST be
// exactly WireSize — the link layer is constant-size by spec, and accepting
// other lengths would reintroduce the size side channel Wrap exists to
// remove. The returned layer aliases the input packet.
func Unwrap(packet []byte) (uint8, []byte, error) {
	if len(packet) != WireSize {
		return 0, nil, fmt.Errorf("%w: got %d bytes, want exactly %d", ErrWireInvalid, len(packet), WireSize)
	}
	clen := int(binary.BigEndian.Uint16(packet[1:wireHeader]))
	if clen < minLayerSize || clen > maxLayerSize {
		return 0, nil, fmt.Errorf("%w: clen %d out of range [%d, %d]", ErrWireInvalid, clen, minLayerSize, maxLayerSize)
	}
	return packet[0], packet[wireHeader : wireHeader+clen], nil
}

// ReplayKey extracts the ephemeral X25519 public key that identifies a layer.
// Relays use it for replay detection: it is unique per layer construction and
// sits at a fixed offset.
//
// The boolean is false if the packet is too short to contain a layer header —
// callers should drop such packets before attempting decryption.
func ReplayKey(layer []byte) ([]byte, bool) {
	if len(layer) < ephPubSize {
		return nil, false
	}
	return layer[:ephPubSize], true
}
