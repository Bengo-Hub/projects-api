package qa

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestEmailHash(t *testing.T) {
	k, eph := NewKeys("test-key", "")
	if eph {
		t.Fatal("a configured key must not be ephemeral")
	}
	a, email, err := k.EmailHash("  Jane.Doe@Example.COM ")
	if err != nil || email != "jane.doe@example.com" {
		t.Fatalf("normalise: %q %v", email, err)
	}
	b, _, _ := k.EmailHash("jane.doe@example.com")
	if a != b || len(a) != 64 {
		t.Fatalf("same address hashed differently: %s %s", a, b)
	}
	other, _ := NewKeys("another-key", "")
	if c, _, _ := other.EmailHash("jane.doe@example.com"); c == a {
		t.Fatal("the hash is not keyed")
	}
	for _, bad := range []string{"", "nope", "a@b", "a@b.com, c@d.com"} {
		if _, _, err := k.EmailHash(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	if got := maskEmail("jane.doe@example.com"); got != "j***@example.com" {
		t.Fatalf("mask = %q", got)
	}
}

func TestAccessToken(t *testing.T) {
	k, _ := NewKeys("", "service-key")
	now := time.Now()
	c := AccessClaims{SessionID: uuid.New(), InviteID: uuid.New(), Expires: now.Add(time.Hour).Unix()}
	tok := k.SignAccess(c)
	got, err := k.VerifyAccess(tok, now)
	if err != nil || got != c {
		t.Fatalf("round trip: %+v %v", got, err)
	}
	if _, err := k.VerifyAccess(tok, now.Add(2*time.Hour)); err == nil {
		t.Fatal("an expired token was accepted")
	}
	p, sig, _ := strings.Cut(tok, ".")
	forged := AccessClaims{SessionID: uuid.New(), InviteID: c.InviteID, Expires: c.Expires}
	if _, err := k.VerifyAccess(k.SignAccess(forged)[:len(p)]+"."+sig, now); err == nil {
		t.Fatal("a payload swapped under an old signature was accepted")
	}
	other, _ := NewKeys("different", "")
	if _, err := other.VerifyAccess(tok, now); err == nil {
		t.Fatal("a token from another key was accepted")
	}
	code, err := newShareCode()
	if err != nil || len(code) != 26 || strings.ToLower(code) != code {
		t.Fatalf("share code %q %v", code, err)
	}
}
