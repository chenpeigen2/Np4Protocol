package onion

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"

	"Np4Protocol/go/pkg/identity"

	"github.com/libp2p/go-libp2p/core/peer"
	"golang.org/x/crypto/chacha20poly1305"
	"golang.org/x/crypto/curve25519"
	"golang.org/x/crypto/hkdf"
)

const (
	ephPubSize = 32
	nonceSize  = 12
	saltString = "np4-onion-v1"
)

const (
	flagRelay = 0
	flagFinal = 1
)

type Hop struct {
	PeerID  peer.ID
	ECDHPub []byte
}

type Onion struct{ data []byte }

func (o *Onion) Bytes() []byte { return o.data }

type Decoded struct {
	IsFinal bool
	NextHop peer.ID
	Inner   []byte
}

// Build constructs an onion by encrypting from the last hop down to the first.
// Innermost layer wraps finalPayload with flagFinal; each outer layer wraps the
// previous ciphertext with flagRelay + next_hop_peer_id.
func Build(path []Hop, finalPayload []byte) (*Onion, error) {
	if len(path) == 0 {
		return nil, errors.New("empty path")
	}

	// Innermost: flagFinal || payload
	current := append([]byte{flagFinal}, finalPayload...)

	for i := len(path) - 1; i >= 0; i-- {
		var plaintext []byte
		if i == len(path)-1 {
			// Wrapping the final layer: plaintext is already flagFinal || payload.
			plaintext = current
		} else {
			// Wrapping an intermediate layer: flagRelay || next_hop_len || next_hop || current
			nextHopBytes := []byte(path[i+1].PeerID)
			lenBuf := make([]byte, 4)
			binary.BigEndian.PutUint32(lenBuf, uint32(len(nextHopBytes)))
			plaintext = append([]byte{flagRelay}, lenBuf...)
			plaintext = append(plaintext, nextHopBytes...)
			plaintext = append(plaintext, current...)
		}
		layer, err := encryptLayer(path[i], plaintext)
		if err != nil {
			return nil, fmt.Errorf("layer %d: %w", i, err)
		}
		current = layer
	}
	return &Onion{data: current}, nil
}

// Decode peels one layer using the recipient's identity.
func Decode(packet []byte, id *identity.Identity) (*Decoded, error) {
	plaintext, err := decryptLayer(packet, id)
	if err != nil {
		return nil, fmt.Errorf("decrypt: %w", err)
	}
	if len(plaintext) < 1 {
		return nil, errors.New("decrypted layer too short")
	}
	flag := plaintext[0]
	rest := plaintext[1:]

	switch flag {
	case flagFinal:
		out := make([]byte, len(rest))
		copy(out, rest)
		return &Decoded{IsFinal: true, Inner: out}, nil
	case flagRelay:
		if len(rest) < 4 {
			return nil, errors.New("relay layer too short for length prefix")
		}
		nextLen := binary.BigEndian.Uint32(rest[:4])
		if uint64(len(rest)) < 4+uint64(nextLen) {
			return nil, errors.New("relay layer truncated")
		}
		nextHopBytes := rest[4 : 4+nextLen]
		nextHop, err := peer.IDFromBytes(nextHopBytes)
		if err != nil {
			return nil, fmt.Errorf("parse next hop: %w", err)
		}
		inner := make([]byte, len(rest)-4-int(nextLen))
		copy(inner, rest[4+nextLen:])
		return &Decoded{IsFinal: false, NextHop: nextHop, Inner: inner}, nil
	default:
		return nil, fmt.Errorf("unknown layer flag: %d", flag)
	}
}

func encryptLayer(hop Hop, plaintext []byte) ([]byte, error) {
	ephPriv := make([]byte, 32)
	if _, err := rand.Read(ephPriv); err != nil {
		return nil, err
	}
	ephPriv[0] &= 248
	ephPriv[31] &= 127
	ephPriv[31] |= 64
	ephPub, err := curve25519.X25519(ephPriv, curve25519.Basepoint)
	if err != nil {
		return nil, err
	}
	shared, err := curve25519.X25519(ephPriv, hop.ECDHPub)
	if err != nil {
		return nil, err
	}
	key, err := deriveKey(shared, []byte(hop.PeerID))
	if err != nil {
		return nil, err
	}
	aead, err := chacha20poly1305.New(key)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, nonceSize)
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	ciphertext := aead.Seal(nil, nonce, plaintext, nil)

	out := make([]byte, 0, ephPubSize+nonceSize+len(ciphertext))
	out = append(out, ephPub...)
	out = append(out, nonce...)
	out = append(out, ciphertext...)
	return out, nil
}

func decryptLayer(packet []byte, id *identity.Identity) ([]byte, error) {
	if len(packet) < ephPubSize+nonceSize+chacha20poly1305.Overhead {
		return nil, errors.New("packet too short")
	}
	ephPub := packet[:ephPubSize]
	nonce := packet[ephPubSize : ephPubSize+nonceSize]
	ciphertext := packet[ephPubSize+nonceSize:]

	shared, err := id.ECDH(ephPub)
	if err != nil {
		return nil, err
	}
	key, err := deriveKey(shared, []byte(id.PeerID()))
	if err != nil {
		return nil, err
	}
	aead, err := chacha20poly1305.New(key)
	if err != nil {
		return nil, err
	}
	plaintext, err := aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, err
	}
	return plaintext, nil
}

func deriveKey(shared, info []byte) ([]byte, error) {
	key := make([]byte, chacha20poly1305.KeySize)
	if _, err := hkdf.New(sha256.New, shared, []byte(saltString), info).Read(key); err != nil {
		return nil, err
	}
	return key, nil
}
