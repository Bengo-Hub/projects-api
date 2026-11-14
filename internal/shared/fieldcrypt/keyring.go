package fieldcrypt

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Envelope encryption for rotation. FIELD_ENCRYPTION_KEY stays the root (key encryption key). Data
// keys are 32 random bytes the app creates, stores in field_keys sealed under the root, and seals
// values with. A value under a data key reads "v3:<key id>:base64(nonce|ciphertext)", AES-256-GCM
// with the raw data key and the record id as additional data. Values from before the first rotation
// ("v2:" bound and unprefixed legacy) are under the root and keep opening; a re-seal moves them.

// ringPrefix marks a value sealed under a data key.
const ringPrefix = "v3:"

// ErrUnknownKey means a sealed value names a data key that is not in the ring.
var ErrUnknownKey = errors.New("fieldcrypt: sealed value names a key that is not loaded")

var keyIDRe = regexp.MustCompile(`^[a-z0-9]{1,16}$`)

// ValidKeyID reports whether id can name a data key (lower case letters and digits, 1 to 16).
func ValidKeyID(id string) bool { return keyIDRe.MatchString(id) }

// Fingerprint is the first 8 hex characters of the SHA-256 of a key. It tells keys apart in the
// health card, the backup file and the audit trail, and reveals nothing usable about the key.
func Fingerprint(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])[:8]
}

// NewDataKey returns a fresh key id and 32 random key bytes.
func NewDataKey() (string, []byte, error) {
	idRaw := make([]byte, 5)
	if _, err := rand.Read(idRaw); err != nil {
		return "", nil, err
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, err
	}
	return "k" + hex.EncodeToString(idRaw), raw, nil
}

func dataKeyRecord(id string) string { return "field-key:" + id }

// SealDataKey seals a data key under the root, bound to its key id.
func (b *Box) SealDataKey(id string, raw []byte) (string, error) {
	if !ValidKeyID(id) || len(raw) != 32 {
		return "", errors.New("fieldcrypt: a data key needs a valid id and 32 bytes")
	}
	return b.EncryptFor(base64.StdEncoding.EncodeToString(raw), dataKeyRecord(id))
}

// OpenDataKey opens a data key sealed by SealDataKey for the same key id.
func (b *Box) OpenDataKey(id, sealed string) ([]byte, error) {
	if sealed == "" {
		return nil, errors.New("fieldcrypt: data key material is gone (retired)")
	}
	plain, err := b.DecryptFor(sealed, dataKeyRecord(id))
	if err != nil {
		return nil, err
	}
	raw, err := base64.StdEncoding.DecodeString(plain)
	if err != nil || len(raw) != 32 {
		return nil, errors.New("fieldcrypt: data key is not 32 bytes")
	}
	return raw, nil
}

// Ring seals with the current data key and opens values under any loaded data key or the root. It
// is safe for concurrent use; Install swaps the keys while requests run.
type Ring struct {
	root *Box

	mu          sync.RWMutex
	keys        map[string]cipher.AEAD
	current     string // "" seals under the root (v2), before the first rotation
	refresh     func() error
	lastRefresh time.Time
}

// refreshEvery bounds how often a value under an unknown key reloads the ring.
const refreshEvery = 5 * time.Second

// NewRing starts a ring with no data keys: it seals and opens like the root box.
func NewRing(root *Box) *Ring {
	return &Ring{root: root, keys: map[string]cipher.AEAD{}}
}

// SetRefresh sets the reload used when a value names a key this ring has not loaded (another
// replica rotated). Call it before the ring is shared.
func (r *Ring) SetRefresh(f func() error) { r.refresh = f }

// Install replaces the loaded data keys. current names the key new values are sealed under, ""
// for the root.
func (r *Ring) Install(current string, keys map[string][]byte) error {
	ring := make(map[string]cipher.AEAD, len(keys))
	for id, raw := range keys {
		if !ValidKeyID(id) {
			return errors.New("fieldcrypt: invalid key id")
		}
		if len(raw) != 32 {
			return errors.New("fieldcrypt: a data key must be 32 bytes")
		}
		block, err := aes.NewCipher(raw)
		if err != nil {
			return err
		}
		aead, err := cipher.NewGCM(block)
		if err != nil {
			return err
		}
		ring[id] = aead
	}
	if current != "" {
		if _, ok := ring[current]; !ok {
			return errors.New("fieldcrypt: the current key is not in the ring")
		}
	}
	r.mu.Lock()
	r.keys, r.current = ring, current
	r.mu.Unlock()
	return nil
}

// CurrentKeyID names the key new values are sealed under; "" means the root.
func (r *Ring) CurrentKeyID() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.current
}

// KeyIDOf names the data key a sealed value is under; "" for the root forms and for empty input.
func KeyIDOf(enc string) string {
	if !strings.HasPrefix(enc, ringPrefix) {
		return ""
	}
	id, _, ok := strings.Cut(enc[len(ringPrefix):], ":")
	if !ok {
		return ""
	}
	return id
}

// EncryptFor seals plain bound to record under the current key; empty input stays empty.
func (r *Ring) EncryptFor(plain, record string) (string, error) {
	if plain == "" {
		return "", nil
	}
	r.mu.RLock()
	current := r.current
	aead := r.keys[current]
	r.mu.RUnlock()
	if current == "" {
		return r.root.EncryptFor(plain, record)
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	return ringPrefix + current + ":" + base64.StdEncoding.EncodeToString(aead.Seal(nonce, nonce, []byte(plain), []byte(record))), nil
}

// DecryptFor opens a value under any loaded data key, or a root value (v2 bound or legacy).
func (r *Ring) DecryptFor(enc, record string) (string, error) {
	if !strings.HasPrefix(enc, ringPrefix) {
		return r.root.DecryptFor(enc, record)
	}
	id, body, ok := strings.Cut(enc[len(ringPrefix):], ":")
	if !ok {
		return "", errors.New("fieldcrypt: malformed sealed value")
	}
	aead := r.lookup(id)
	if aead == nil && r.tryRefresh() {
		aead = r.lookup(id)
	}
	if aead == nil {
		return "", ErrUnknownKey
	}
	raw, err := base64.StdEncoding.DecodeString(body)
	if err != nil {
		return "", err
	}
	ns := aead.NonceSize()
	if len(raw) < ns {
		return "", errors.New("fieldcrypt: ciphertext too short")
	}
	plain, err := aead.Open(nil, raw[:ns], raw[ns:], []byte(record))
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

func (r *Ring) lookup(id string) cipher.AEAD {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.keys[id]
}

func (r *Ring) tryRefresh() bool {
	if r.refresh == nil {
		return false
	}
	r.mu.Lock()
	if time.Since(r.lastRefresh) < refreshEvery {
		r.mu.Unlock()
		return false
	}
	r.lastRefresh = time.Now()
	r.mu.Unlock()
	return r.refresh() == nil
}

// NeedsReseal reports whether a sealed value is under anything but the current key.
func (r *Ring) NeedsReseal(enc string) bool {
	if enc == "" {
		return false
	}
	current := r.CurrentKeyID()
	if current == "" {
		return strings.HasPrefix(enc, ringPrefix) || !strings.HasPrefix(enc, boundPrefix)
	}
	return KeyIDOf(enc) != current
}

// Reseal opens a value with whichever key it is under and seals it bound to record under the
// current key. A value already under the current key comes back unchanged.
func (r *Ring) Reseal(enc, record string) (string, error) {
	if !r.NeedsReseal(enc) {
		return enc, nil
	}
	plain, err := r.DecryptFor(enc, record)
	if err != nil {
		return "", err
	}
	return r.EncryptFor(plain, record)
}

// CurrentPrefix is the prefix every value under the current key starts with; "" before the first
// rotation.
func (r *Ring) CurrentPrefix() string {
	if c := r.CurrentKeyID(); c != "" {
		return ringPrefix + c + ":"
	}
	return ""
}
