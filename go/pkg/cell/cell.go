// Package cell implements the fixed-size 4096-byte payload cell (v2).
//
// Cell layout:
//
//	msg_id (16B) ‖ type (1B) ‖ tag (16B) ‖ content_len (2B big-endian) ‖ content ‖ 0x00 padding
//
// Cells are the innermost onion payload. The fixed size eliminates size
// fingerprinting: observers and relays cannot learn content length from
// traffic. The real length, type, and auth tag exist only inside the
// encrypted envelope.
//
// The tag carries the pairwise sender authentication (see package auth); it
// is opaque padding here. Type discriminates real traffic from dummy traffic
// so the receiver can silently discard the latter — relays cannot tell the
// difference because both look identical on the wire.
package cell

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
)

// Size is the protocol-constant cell size in bytes. It is NOT negotiable:
// changing it after deployment is a protocol fork (size negotiation itself
// leaks information).
const Size = 4096

const (
	idSize   = 16
	typeSize = 1
	tagSize  = 16
	lenSize  = 2
	overhead = idSize + typeSize + tagSize + lenSize
	// MaxContent is the largest content a single cell can carry (4061 bytes).
	// Larger content requires the [v2] chunking protocol.
	MaxContent = Size - overhead
)

// TagSize is the auth-tag field size in the cell; package auth produces tags
// of exactly this length.
const TagSize = tagSize

// Type discriminates payloads inside the cell. Only visible to the receiver
// (innermost onion layer); relays never see it.
type Type byte

const (
	// TypeDummy is cover traffic: injected to flatten the rate signature.
	// Tag is all-zero, content empty; receivers drop it before dispatch.
	TypeDummy Type = 0x00
	// TypeText is an application message carrying a sender-auth tag.
	TypeText Type = 0x01
)

// ErrTooLarge is returned by Seal when content does not fit in a single cell.
var ErrTooLarge = fmt.Errorf("content exceeds cell capacity %d bytes", MaxContent)

// NewMsgID returns a fresh random 16-byte message ID. Senders generate it
// before sealing so the auth tag can cover it.
func NewMsgID() ([]byte, error) {
	id := make([]byte, idSize)
	if _, err := rand.Read(id); err != nil {
		return nil, err
	}
	return id, nil
}

// Seal wraps content in a padded cell with the given msg_id, payload type,
// and auth tag, returning the full Size-byte cell.
func Seal(msgID []byte, typ Type, tag []byte, content []byte) ([]byte, error) {
	if len(msgID) != idSize {
		return nil, fmt.Errorf("%w: msg_id %d bytes, want %d", ErrInvalidCell, len(msgID), idSize)
	}
	if len(tag) != tagSize {
		return nil, fmt.Errorf("%w: tag %d bytes, want %d", ErrInvalidCell, len(tag), tagSize)
	}
	if len(content) > MaxContent {
		return nil, ErrTooLarge
	}
	out := make([]byte, Size)
	copy(out[:idSize], msgID)
	out[idSize] = byte(typ)
	copy(out[idSize+typeSize:idSize+typeSize+tagSize], tag)
	binary.BigEndian.PutUint16(out[idSize+typeSize+tagSize:idSize+typeSize+tagSize+lenSize], uint16(len(content)))
	copy(out[overhead:], content)
	// Remaining bytes are already zero padding.
	return out, nil
}

// ErrInvalidCell is returned by Open for malformed cells.
var ErrInvalidCell = errors.New("invalid cell")

// Open parses a cell and returns the msg_id, payload type, auth tag, and
// content. The msg_id is a fresh 16-byte slice the caller owns; content and
// tag alias the input cell.
//
// Open does NOT authenticate: cell integrity is guaranteed by the onion
// layer's AEAD before Open is ever called, and tag verification is the
// receiver's job (package auth) once candidate senders are known.
func Open(c []byte) (msgID []byte, typ Type, tag []byte, content []byte, err error) {
	if len(c) != Size {
		return nil, 0, nil, nil, fmt.Errorf("%w: got %d bytes, want %d", ErrInvalidCell, len(c), Size)
	}
	contentLen := int(binary.BigEndian.Uint16(c[idSize+typeSize+tagSize : idSize+typeSize+tagSize+lenSize]))
	if overhead+contentLen > Size {
		return nil, 0, nil, nil, fmt.Errorf("%w: content_len %d exceeds capacity", ErrInvalidCell, contentLen)
	}
	msgID = append([]byte(nil), c[:idSize]...)
	typ = Type(c[idSize])
	tag = c[idSize+typeSize : idSize+typeSize+tagSize]
	return msgID, typ, tag, c[overhead : overhead+contentLen], nil
}
