package crypto

import (
    "bytes"
    "testing"
)

func TestX25519KeyExchange(t *testing.T) {
    x25519 := NewX25519KeyExchange()

    alicePub, alicePriv, err := x25519.GenerateKeyPair()
    if err != nil {
        t.Fatal(err)
    }

    bobPub, bobPriv, err := x25519.GenerateKeyPair()
    if err != nil {
        t.Fatal(err)
    }

    aliceShared, err := x25519.ComputeSharedSecret(alicePriv, bobPub)
    if err != nil {
        t.Fatal(err)
    }

    bobShared, err := x25519.ComputeSharedSecret(bobPriv, alicePub)
    if err != nil {
        t.Fatal(err)
    }

    if !bytes.Equal(aliceShared, bobShared) {
        t.Error("shared secrets do not match")
    }
}
