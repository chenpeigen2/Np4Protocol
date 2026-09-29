// Package cell implements the fixed-size 4096-byte payload cell.
//
// Cell layout:
//
//	msg_id (16B) ‖ content_len (2B big-endian) ‖ content ‖ 0x00 padding
//
// Cells are the innermost onion payload. The fixed size eliminates size
// fingerprinting: observers and relays cannot learn content length from
// traffic. The real length exists only inside the encrypted envelope.
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
	idSize     = 16
	lenSize    = 2
	overhead   = idSize + lenSize
	maxContent = Size - overhead
)

// ErrTooLarge is returned by Seal when content does not fit in a single cell.
// Larger content requires the [v2] chunking protocol.
var ErrTooLarge = fmt.Errorf("content exceeds cell capacity %d bytes", maxContent)

// Seal wraps content in a padded cell with a fresh random msg_id and returns
// the full Size-byte cell.
func Seal(content []byte) ([]byte, error) {
	if len(content) > maxContent {
		return nil, ErrTooLarge
	}
	out := make([]byte, Size)
	if _, err := rand.Read(out[:idSize]); err != nil {
		return nil, err
	}
	binary.BigEndian.PutUint16(out[idSize:idSize+lenSize], uint16(len(content)))
	copy(out[overhead:], content)
	// Remaining bytes are already zero padding.
	return out, nil
}

// ErrInvalidCell is returned by Open for malformed cells.
var ErrInvalidCell = errors.New("invalid cell")

// Open parses a cell and returns the msg_id and content. The msg_id is a
// fresh 16-byte slice the caller owns; content aliases the input cell.
//
// Open does NOT authenticate: cell integrity is guaranteed by the onion
// layer's AEAD before Open is ever called.
func Open(c []byte) (msgID []byte, content []byte, err error) {
	if len(c) != Size {
		return nil, nil, fmt.Errorf("%w: got %d bytes, want %d", ErrInvalidCell, len(c), Size)
	}
	contentLen := int(binary.BigEndian.Uint16(c[idSize : idSize+lenSize]))
	if overhead+contentLen > Size {
		return nil, nil, fmt.Errorf("%w: content_len %d exceeds capacity", ErrInvalidCell, contentLen)
	}
	msgID = append([]byte(nil), c[:idSize]...)
	return msgID, c[overhead : overhead+contentLen], nil
}
