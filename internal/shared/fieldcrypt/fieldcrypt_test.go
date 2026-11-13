package fieldcrypt

import (
	"errors"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	b, err := New("a-test-key")
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
	other, _ := New("another-key")
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
}
