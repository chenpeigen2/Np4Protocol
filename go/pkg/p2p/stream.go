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
	var buf [4]byte
	if _, err := io.ReadFull(s, buf[:]); err != nil {
		return nil, err
	}
	length := binary.BigEndian.Uint32(buf[:])
	if length > MaxMessageSize {
		return nil, errors.New("message too large")
	}
	data := make([]byte, length)
	_, err := io.ReadFull(s, data)
	return data, err
}
