package crypto

import (
    "bytes"
    "crypto/rand"
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

func TestChaCha20Poly1305EncryptDecrypt(t *testing.T) {
    encryptor := NewChaCha20Encryptor()

    key := make([]byte, 32)
    rand.Read(key)

    plaintext := []byte("Hello, Np4Protocol!")

    ciphertext, err := encryptor.Encrypt(plaintext, key)
    if err != nil {
        t.Fatal(err)
    }

    if bytes.Equal(ciphertext, plaintext) {
        t.Error("ciphertext should differ from plaintext")
    }

    decrypted, err := encryptor.Decrypt(ciphertext, key)
    if err != nil {
        t.Fatal(err)
    }

    if !bytes.Equal(decrypted, plaintext) {
        t.Error("decrypted text should match original")
    }
}
