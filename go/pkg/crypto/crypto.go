package crypto

type KeyExchange interface {
    GenerateKeyPair() (public, private []byte, err error)
    ComputeSharedSecret(localPrivate, remotePublic []byte) ([]byte, error)
}

type Encryptor interface {
    Encrypt(plaintext, key []byte) ([]byte, error)
    Decrypt(ciphertext, key []byte) ([]byte, error)
}
