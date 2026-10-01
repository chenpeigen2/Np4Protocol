package pathsel

import (
	"context"
	"crypto/ed25519"
	"encoding/base32"
	"encoding/binary"
	"errors"
	"fmt"

	dht "github.com/libp2p/go-libp2p-kad-dht"
	ic "github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
)

// Key record v2 (forward secrecy, M4): the published /np4/ecdh record carries
// the CURRENT rotation-bucket X25519 subkey, signed by the master ed25519 key.
//
//	value = master_marshaled(36B) ‖ bucket(8B BE) ‖ x25519_pub(32B) ‖ sig(64B)
//	sig   = Ed25519(master_priv, "np4-rotation-v1" ‖ bucket ‖ x25519_pub)
//
// The peer-ID binding stays on the MASTER key (IDFromPublicKey(master) ==
// record's peer ID), so the anti-poisoning validator is unchanged in
// strength; the signature stops anyone without the master private key from
// publishing a subkey under someone else's identity.
const (
	rotationDomain = "np4-rotation-v1"

	edMarshaledLen  = 36 // ic.MarshalPublicKey of an ed25519 key: 4B protobuf header + 32B key
	bucketLen       = 8
	x25519PubLen    = 32
	rotationSigLen  = ed25519.SignatureSize
	rotationRecSize = edMarshaledLen + bucketLen + x25519PubLen + rotationSigLen
)

// ErrBadRecord marks malformed /np4/ecdh records (framing, binding, or
// signature failures).
var ErrBadRecord = errors.New("np4: bad key record")

func rotationMessage(bucket int64, x25519Pub []byte) []byte {
	msg := make([]byte, 0, len(rotationDomain)+bucketLen+x25519PubLen)
	msg = append(msg, rotationDomain...)
	var b [bucketLen]byte
	binary.BigEndian.PutUint64(b[:], uint64(bucket))
	msg = append(msg, b[:]...)
	return append(msg, x25519Pub...)
}

// EncodeRotationRecord frames and signs a rotation record. sign receives the
// domain-separated message and must return a 64-byte Ed25519 signature made
// with the MASTER private key (identity.Sign).
func EncodeRotationRecord(masterPub ic.PubKey, bucket int64, x25519Pub []byte, sign func([]byte) []byte) ([]byte, error) {
	if sign == nil {
		return nil, fmt.Errorf("%w: nil signer", ErrBadRecord)
	}
	if len(x25519Pub) != x25519PubLen {
		return nil, fmt.Errorf("%w: x25519 pub %d bytes", ErrBadRecord, len(x25519Pub))
	}
	marshaled, err := ic.MarshalPublicKey(masterPub)
	if err != nil {
		return nil, fmt.Errorf("%w: master key: %v", ErrBadRecord, err)
	}
	if len(marshaled) != edMarshaledLen {
		return nil, fmt.Errorf("%w: master key marshals to %d bytes, want %d (ed25519 only)", ErrBadRecord, len(marshaled), edMarshaledLen)
	}
	sig := sign(rotationMessage(bucket, x25519Pub))
	if len(sig) != rotationSigLen {
		return nil, fmt.Errorf("%w: signature %d bytes, want %d", ErrBadRecord, len(sig), rotationSigLen)
	}
	rec := make([]byte, 0, rotationRecSize)
	rec = append(rec, marshaled...)
	var b [bucketLen]byte
	binary.BigEndian.PutUint64(b[:], uint64(bucket))
	rec = append(rec, b[:]...)
	rec = append(rec, x25519Pub...)
	return append(rec, sig...), nil
}

// ParseRotationRecord verifies the full record: framing, master-key peer-ID
// binding (the anti-poisoning check — unchanged from v1), and the master's
// signature over the subkey. Returns the verified X25519 subkey.
func ParseRotationRecord(pid peer.ID, value []byte) ([]byte, error) {
	if len(value) != rotationRecSize {
		return nil, fmt.Errorf("%w: size %d, want %d", ErrBadRecord, len(value), rotationRecSize)
	}
	master, err := ic.UnmarshalPublicKey(value[:edMarshaledLen])
	if err != nil {
		return nil, fmt.Errorf("%w: master key: %v", ErrBadRecord, err)
	}
	bound, err := peer.IDFromPublicKey(master)
	if err != nil {
		return nil, fmt.Errorf("%w: master key id: %v", ErrBadRecord, err)
	}
	if bound != pid {
		return nil, fmt.Errorf("%w: master key binds to peer %s, wanted %s", ErrBadRecord, bound, pid)
	}
	bucket := int64(binary.BigEndian.Uint64(value[edMarshaledLen : edMarshaledLen+bucketLen]))
	x25519Pub := value[edMarshaledLen+bucketLen : edMarshaledLen+bucketLen+x25519PubLen]
	sig := value[edMarshaledLen+bucketLen+x25519PubLen:]
	ok, err := master.Verify(rotationMessage(bucket, x25519Pub), sig)
	if err != nil {
		return nil, fmt.Errorf("%w: signature check: %v", ErrBadRecord, err)
	}
	if !ok {
		return nil, fmt.Errorf("%w: subkey signature invalid", ErrBadRecord)
	}
	out := make([]byte, x25519PubLen)
	copy(out, x25519Pub)
	return out, nil
}

// PublishKey stores the node's current rotation record in the DHT under
// /np4/ecdh/<peerID>. masterPub is the identity's master ed25519 public key
// (identity.SigningPubKey — the peer-ID binding anchor); x25519Pub is the
// current subkey (identity.ECDHPub); sign is identity.Sign.
func PublishKey(ctx context.Context, d *dht.IpfsDHT, pid peer.ID, masterPub ic.PubKey, x25519Pub []byte, bucket int64, sign func([]byte) []byte) error {
	if d == nil {
		return errors.New("DHT not initialized")
	}
	rec, err := EncodeRotationRecord(masterPub, bucket, x25519Pub, sign)
	if err != nil {
		return err
	}
	key := ecdhKeyPrefix + base32.StdEncoding.EncodeToString([]byte(pid))
	return d.PutValue(ctx, key, rec)
}
