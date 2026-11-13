package qa

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/mail"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Keys holds the two secrets the module uses, both derived from one base key: one for the keyed
// email hashes on invites and results, one for signing invitee access tokens.
type Keys struct {
	email  []byte
	access []byte
}

// NewKeys derives the keys from QA_HASH_KEY, or from INTERNAL_SERVICE_KEY when that is empty.
// With neither (local development only) a random key is used and invites do not survive a restart;
// ephemeral reports that case so the caller can log it.
func NewKeys(hashKey, serviceKey string) (k *Keys, ephemeral bool) {
	base := []byte(hashKey)
	if len(base) == 0 {
		base = []byte(serviceKey)
	}
	if len(base) == 0 {
		base = make([]byte, 32)
		_, _ = rand.Read(base)
		ephemeral = true
	}
	return &Keys{email: derive(base, "projects.qa.email.v1"), access: derive(base, "projects.qa.access.v1")}, ephemeral
}

func derive(base []byte, label string) []byte {
	m := hmac.New(sha256.New, base)
	m.Write([]byte(label))
	return m.Sum(nil)
}

// normalizeEmail lowercases and trims an address and checks it is one bare, well formed address.
func normalizeEmail(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > 254 || strings.ContainsAny(raw, ",;") {
		return "", false
	}
	addr, err := mail.ParseAddress(raw)
	if err != nil {
		return "", false
	}
	e := strings.ToLower(addr.Address)
	at := strings.LastIndex(e, "@")
	if at <= 0 || at == len(e)-1 || !strings.Contains(e[at+1:], ".") {
		return "", false
	}
	return e, true
}

// EmailHash returns the keyed hash of a normalised address, and the address itself.
func (k *Keys) EmailHash(raw string) (hash, email string, err error) {
	email, ok := normalizeEmail(raw)
	if !ok {
		return "", "", invalid("a valid email address is required")
	}
	m := hmac.New(sha256.New, k.email)
	m.Write([]byte(email))
	return hex.EncodeToString(m.Sum(nil)), email, nil
}

// maskEmail keeps the first letter of the local part and the domain: "j***@example.com".
func maskEmail(email string) string {
	at := strings.LastIndex(email, "@")
	if at <= 0 {
		return "***"
	}
	return email[:1] + "***" + email[at:]
}

// AccessClaims is what an invitee access token carries: the session and invite it opens, and when
// it expires. It is bound to one session; the invite is re-checked on every request, so revoking an
// invite closes the door at once.
type AccessClaims struct {
	SessionID uuid.UUID `json:"s"`
	InviteID  uuid.UUID `json:"i"`
	Expires   int64     `json:"e"`
}

var errBadToken = errors.New("qa: bad access token")

// SignAccess returns "<payload>.<mac>", both base64url.
func (k *Keys) SignAccess(c AccessClaims) string {
	body, _ := json.Marshal(c)
	p := base64.RawURLEncoding.EncodeToString(body)
	return p + "." + base64.RawURLEncoding.EncodeToString(k.mac(p))
}

// VerifyAccess checks the signature and expiry of a token.
func (k *Keys) VerifyAccess(token string, now time.Time) (AccessClaims, error) {
	p, sig, ok := strings.Cut(token, ".")
	if !ok || p == "" || sig == "" {
		return AccessClaims{}, errBadToken
	}
	got, err := base64.RawURLEncoding.DecodeString(sig)
	if err != nil || !hmac.Equal(got, k.mac(p)) {
		return AccessClaims{}, errBadToken
	}
	body, err := base64.RawURLEncoding.DecodeString(p)
	if err != nil {
		return AccessClaims{}, errBadToken
	}
	var c AccessClaims
	if json.Unmarshal(body, &c) != nil || c.SessionID == uuid.Nil || c.InviteID == uuid.Nil {
		return AccessClaims{}, errBadToken
	}
	if now.Unix() >= c.Expires {
		return AccessClaims{}, errBadToken
	}
	return c, nil
}

func (k *Keys) mac(payload string) []byte {
	m := hmac.New(sha256.New, k.access)
	m.Write([]byte(payload))
	return m.Sum(nil)
}

// newShareCode is the random part of a /qa/{code} link: 128 bits, base32 without padding.
func newShareCode() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return strings.ToLower(base32Enc.EncodeToString(b)), nil
}

var base32Enc = base32.StdEncoding.WithPadding(base32.NoPadding)
