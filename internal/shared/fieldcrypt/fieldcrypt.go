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

// ErrWeakKey is returned by New for a secret shorter than 32 bytes (decoded when it is base64).
var ErrWeakKey = errors.New("fieldcrypt: key must be at least 32 bytes (openssl rand -base64 32)")

// boundPrefix marks a value sealed with EncryptFor, bound to its record.
const boundPrefix = "v2:"

// Box seals and opens values under one key.
type Box struct {
	aead cipher.AEAD
}

// New derives the AES-256 key from secret: base64 of at least 32 random bytes, or a plain string
// of at least 32 bytes.
func New(secret string) (*Box, error) {
	if secret == "" {
		return nil, ErrNoKey
	}
	raw, err := base64.StdEncoding.DecodeString(secret)
	if err != nil || len(raw) < 16 {
		raw = []byte(secret)
	}
	if len(raw) < 32 {
		return nil, ErrWeakKey
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

// EncryptFor seals plain bound to record (the row id): the record is the GCM additional data, so
// a sealed value copied onto another row does not open there.
func (b *Box) EncryptFor(plain, record string) (string, error) {
	if plain == "" {
		return "", nil
	}
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	return boundPrefix + base64.StdEncoding.EncodeToString(b.aead.Seal(nonce, nonce, []byte(plain), []byte(record))), nil
}

// DecryptFor opens a value sealed by EncryptFor for the same record, or an older unbound value
// sealed by Encrypt.
func (b *Box) DecryptFor(enc, record string) (string, error) {
	if len(enc) > len(boundPrefix) && enc[:len(boundPrefix)] == boundPrefix {
		return b.open(enc[len(boundPrefix):], []byte(record))
	}
	return b.Decrypt(enc)
}

// Decrypt opens a value sealed by Encrypt; empty input stays empty.
func (b *Box) Decrypt(enc string) (string, error) {
	return b.open(enc, nil)
}

func (b *Box) open(enc string, aad []byte) (string, error) {
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
	plain, err := b.aead.Open(nil, raw[:ns], raw[ns:], aad)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}
