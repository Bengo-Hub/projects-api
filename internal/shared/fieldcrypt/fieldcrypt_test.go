package fieldcrypt

import (
	"errors"
	"testing"
)

const testKey = "a-test-key-that-is-at-least-32-bytes"

func TestRoundTrip(t *testing.T) {
	b, err := New(testKey)
	if err != nil {
		t.Fatal(err)
	}
	enc, err := b.Encrypt("signer@example.com")
	if err != nil || enc == "" || enc == "signer@example.com" {
		t.Fatalf("encrypt: %q %v", enc, err)
	}
	again, _ := b.Encrypt("signer@example.com")
	if again == enc {
		t.Fatal("two seals of one value are equal; the nonce is not random")
	}
	if got, err := b.Decrypt(enc); err != nil || got != "signer@example.com" {
		t.Fatalf("decrypt: %q %v", got, err)
	}
	other, _ := New("another-key-that-is-also-32-bytes-long")
	if _, err := other.Decrypt(enc); err == nil {
		t.Fatal("opened under the wrong key")
	}
	if _, err := b.Decrypt(enc[:len(enc)-4] + "AAAA"); err == nil {
		t.Fatal("a tampered value opened")
	}
	if e, _ := b.Encrypt(""); e != "" {
		t.Fatal("empty input must stay empty")
	}
	if _, err := New(""); !errors.Is(err, ErrNoKey) {
		t.Fatal("an empty key was accepted")
	}
	if _, err := New("short-key"); !errors.Is(err, ErrWeakKey) {
		t.Fatal("a key under 32 bytes was accepted")
	}
}

// A value sealed for one record opens only for that record, and older unbound values still open.
func TestBoundToRecord(t *testing.T) {
	b, err := New(testKey)
	if err != nil {
		t.Fatal(err)
	}
	enc, err := b.EncryptFor("signer@example.com", "invite-1")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := b.DecryptFor(enc, "invite-1"); err != nil || got != "signer@example.com" {
		t.Fatalf("own record: %q %v", got, err)
	}
	if _, err := b.DecryptFor(enc, "invite-2"); err == nil {
		t.Fatal("a value copied onto another record opened")
	}
	legacy, _ := b.Encrypt("old@example.com")
	if got, err := b.DecryptFor(legacy, "invite-1"); err != nil || got != "old@example.com" {
		t.Fatalf("legacy unbound value: %q %v", got, err)
	}
}
