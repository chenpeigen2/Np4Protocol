package crypto

import (
    "crypto/rand"
    "errors"

    "golang.org/x/crypto/curve25519"
)

type X25519KeyExchange struct{}

func NewX25519KeyExchange() *X25519KeyExchange {
    return &X25519KeyExchange{}
}

func (x *X25519KeyExchange) GenerateKeyPair() (public, private []byte, err error) {
    private = make([]byte, 32)
    _, err = rand.Read(private)
    if err != nil {
        return nil, nil, err
    }

    public, err = curve25519.X25519(private, curve25519.Basepoint)
    if err != nil {
        return nil, nil, err
    }

    return public, private, nil
}

func (x *X25519KeyExchange) ComputeSharedSecret(localPrivate, remotePublic []byte) ([]byte, error) {
    if len(localPrivate) != 32 || len(remotePublic) != 32 {
        return nil, errors.New("invalid key length")
    }

    shared, err := curve25519.X25519(localPrivate, remotePublic)
    if err != nil {
        return nil, err
    }

    return shared, nil
}
