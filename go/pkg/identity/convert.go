package identity

import (
	"errors"
	"math/big"
)

// ed25519 field prime: 2^255 - 19.
var edwardsP = func() *big.Int {
	p := new(big.Int).Lsh(big.NewInt(1), 255)
	return p.Sub(p, big.NewInt(19))
}()

// Ed25519PubToX25519 converts a raw 32-byte ed25519 public key to the
// corresponding X25519 (Curve25519) public key via the standard birational
// map u = (1 + y) / (1 - y) over the field GF(2^255 - 19).
//
// This is the public-key half of NaCl's crypto_sign_ed25519_pk_to_curve25519
// and is compatible with our key derivation: deriveX25519Priv applies the
// standard SHA-512+clamp conversion to the ed25519 seed, whose public key is
// exactly this conversion of the ed25519 public key.
//
// The top bit of ed25519 public keys carries the x-coordinate sign and is
// discarded (the Montgomery u-coordinate does not depend on it).
func Ed25519PubToX25519(edPub []byte) ([]byte, error) {
	if len(edPub) != 32 {
		return nil, errors.New("ed25519 public key must be 32 bytes")
	}

	// Decode little-endian y-coordinate, masking the sign bit.
	var yBytes [32]byte
	copy(yBytes[:], edPub)
	yBytes[31] &= 0x7f
	y := leToBig(yBytes[:])
	if y.Cmp(edwardsP) >= 0 {
		return nil, errors.New("ed25519 public key out of range")
	}

	// u = (1 + y) / (1 - y) mod p.
	denom := new(big.Int).Sub(big.NewInt(1), y)
	denom.Mod(denom, edwardsP)
	if denom.Sign() == 0 {
		return nil, errors.New("invalid ed25519 public key: y == 1")
	}
	num := new(big.Int).Add(big.NewInt(1), y)
	num.Mod(num, edwardsP)
	u := new(big.Int).ModInverse(denom, edwardsP)
	if u == nil {
		return nil, errors.New("no modular inverse")
	}
	u.Mul(u, num)
	u.Mod(u, edwardsP)

	return bigToLE(u), nil
}

func leToBig(b []byte) *big.Int {
	// Reverse little-endian bytes into big-endian for big.Int.
	be := make([]byte, len(b))
	for i := range b {
		be[len(b)-1-i] = b[i]
	}
	return new(big.Int).SetBytes(be)
}

func bigToLE(x *big.Int) []byte {
	be := x.Bytes()
	out := make([]byte, 32)
	// Left-pad to 32 bytes then reverse.
	for i := 0; i < len(be); i++ {
		out[i] = be[len(be)-1-i]
	}
	return out
}
