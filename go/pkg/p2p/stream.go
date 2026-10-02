package p2p

import (
	"encoding/binary"
	"errors"
	"io"

	"github.com/libp2p/go-libp2p/core/network"
)

const MaxMessageSize = 1 * 1024 * 1024 // 1 MB

// WriteMsg writes a length-prefixed message to a stream.
func WriteMsg(s network.Stream, data []byte) error {
	if len(data) > MaxMessageSize {
		return errors.New("message too large")
	}
	var buf [4]byte
	binary.BigEndian.PutUint32(buf[:], uint32(len(data)))
	if _, err := s.Write(buf[:]); err != nil {
		return err
	}
	_, err := s.Write(data)
	return err
}

// ReadMsg reads a length-prefixed message from a stream.
func ReadMsg(s network.Stream) ([]byte, error) {
	return ReadMsgCap(s, MaxMessageSize)
}

// ReadMsgCap reads a length-prefixed message, refusing (before any payload
// allocation) lengths above cap. The plain length prefix lets a sender name
// an arbitrary size; allocating first and reading second turns that into a
// memory-allocation amplifier. Callers must pass the tightest bound their
// protocol actually allows.
func ReadMsgCap(s network.Stream, cap int) ([]byte, error) {
	if cap > MaxMessageSize {
		return nil, errors.New("cap exceeds message size limit")
	}
	var buf [4]byte
	if _, err := io.ReadFull(s, buf[:]); err != nil {
		return nil, err
	}
	length := binary.BigEndian.Uint32(buf[:])
	if length > uint32(cap) {
		return nil, errors.New("message too large")
	}
	data := make([]byte, length)
	_, err := io.ReadFull(s, data)
	return data, err
}
