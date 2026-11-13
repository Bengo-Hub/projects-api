// Package fieldcrypt seals single fields at rest with AES-256-GCM under FIELD_ENCRYPTION_KEY. It is
// the sealed-value part of the field encryption box used by hadia-api and maskani-api
// (internal/shared/secure), which is internal to those services and so cannot be imported. The
// construction is the same, base64(nonce|ciphertext) with the key derived by SHA-256 from the
// secret, under its own derivation label, so one secret reused by mistake across services never
// opens another service's values. Key rings (FIELD_ENCRYPTION_KEYS) are not ported yet. It belongs
// in a shared module once a third service needs it.
package fieldcrypt

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
)

// ErrNoKey is returned by New for an empty secret.
var ErrNoKey = errors.New("fieldcrypt: empty key")

// Box seals and opens values under one key.
type Box struct {
	aead cipher.AEAD
}

// New derives the AES-256 key from secret (base64 of at least 16 bytes, or any string).
func New(secret string) (*Box, error) {
	if secret == "" {
		return nil, ErrNoKey
	}
	raw, err := base64.StdEncoding.DecodeString(secret)
	if err != nil || len(raw) < 16 {
		raw = []byte(secret)
	}
	key := sha256.Sum256(append([]byte("projects-field-enc:"), raw...))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Box{aead: aead}, nil
}

// Encrypt seals plain with a fresh random nonce; empty input stays empty.
func (b *Box) Encrypt(plain string) (string, error) {
	if plain == "" {
		return "", nil
	}
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(b.aead.Seal(nonce, nonce, []byte(plain), nil)), nil
}

// Decrypt opens a value sealed by Encrypt; empty input stays empty.
func (b *Box) Decrypt(enc string) (string, error) {
	if enc == "" {
		return "", nil
	}
	raw, err := base64.StdEncoding.DecodeString(enc)
	if err != nil {
		return "", err
	}
	ns := b.aead.NonceSize()
	if len(raw) < ns {
		return "", errors.New("fieldcrypt: ciphertext too short")
	}
	plain, err := b.aead.Open(nil, raw[:ns], raw[ns:], nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}
