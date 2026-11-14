package fieldcrypt

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"
)

func testRoot(t *testing.T) *Box {
	t.Helper()
	// A fixed, obviously fake root: base64 of a repeated hex alphabet (32 bytes).
	b, err := New(base64.StdEncoding.EncodeToString([]byte(strings.Repeat("0123456789abcdef", 2))))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestFingerprintIsShortAndKeyFree(t *testing.T) {
	root := testRoot(t)
	fp := root.Fingerprint()
	if len(fp) != 8 || strings.Trim(fp, "0123456789abcdef") != "" {
		t.Fatalf("fingerprint %q is not 8 hex characters", fp)
	}
	again := testRoot(t)
	if again.Fingerprint() != fp {
		t.Fatal("the same secret must give the same fingerprint")
	}
	other, err := New(strings.Repeat("z!", 20)) // not base64, so used as 40 plain bytes
	if err != nil {
		t.Fatal(err)
	}
	if other.Fingerprint() == fp {
		t.Fatal("different secrets gave the same fingerprint")
	}
	if Fingerprint([]byte("a")) == Fingerprint([]byte("b")) {
		t.Fatal("Fingerprint does not depend on the key")
	}
}

func TestDataKeySealedUnderRoot(t *testing.T) {
	root := testRoot(t)
	id, raw, err := NewDataKey()
	if err != nil || len(raw) != 32 || !ValidKeyID(id) {
		t.Fatalf("NewDataKey: %q %d %v", id, len(raw), err)
	}
	sealed, err := root.SealDataKey(id, raw)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(sealed, string(raw)) {
		t.Fatal("sealed key holds the raw key")
	}
	got, err := root.OpenDataKey(id, sealed)
	if err != nil || string(got) != string(raw) {
		t.Fatalf("OpenDataKey: %v", err)
	}
	if _, err := root.OpenDataKey("kother", sealed); err == nil {
		t.Fatal("a sealed key opened under another key id")
	}
	other, err := New(strings.Repeat("y!", 20))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := other.OpenDataKey(id, sealed); err == nil {
		t.Fatal("a sealed key opened under another root")
	}
}

func TestRingWithoutDataKeysSealsUnderRoot(t *testing.T) {
	root := testRoot(t)
	r := NewRing(root)
	enc, err := r.EncryptFor("a@b.c", "rec1")
	if err != nil || !strings.HasPrefix(enc, "v2:") {
		t.Fatalf("no data key should seal the v2 form, got %q %v", enc, err)
	}
	if got, err := r.DecryptFor(enc, "rec1"); err != nil || got != "a@b.c" {
		t.Fatalf("DecryptFor: %q %v", got, err)
	}
	if r.CurrentKeyID() != "" || r.NeedsReseal(enc) {
		t.Fatal("with no data key the root form is current")
	}
}

func TestRingSealsUnderCurrentDataKeyAndOpensOlderForms(t *testing.T) {
	root := testRoot(t)
	legacy, _ := root.Encrypt("old@b.c")
	v2, _ := root.EncryptFor("v2@b.c", "rec2")

	r := NewRing(root)
	id1, raw1, _ := NewDataKey()
	if err := r.Install(id1, map[string][]byte{id1: raw1}); err != nil {
		t.Fatal(err)
	}
	enc, err := r.EncryptFor("new@b.c", "rec3")
	if err != nil || !strings.HasPrefix(enc, "v3:"+id1+":") {
		t.Fatalf("EncryptFor under data key: %q %v", enc, err)
	}
	if KeyIDOf(enc) != id1 || KeyIDOf(v2) != "" || KeyIDOf(legacy) != "" {
		t.Fatal("KeyIDOf")
	}
	if got, err := r.DecryptFor(enc, "rec3"); err != nil || got != "new@b.c" {
		t.Fatalf("open v3: %q %v", got, err)
	}
	if _, err := r.DecryptFor(enc, "other"); err == nil {
		t.Fatal("a v3 value opened on another record")
	}
	if got, err := r.DecryptFor(v2, "rec2"); err != nil || got != "v2@b.c" {
		t.Fatalf("open v2 after install: %q %v", got, err)
	}
	if got, err := r.DecryptFor(legacy, "any"); err != nil || got != "old@b.c" {
		t.Fatalf("open legacy after install: %q %v", got, err)
	}

	// Rotate: id2 becomes current, id1 stays for decrypt.
	id2, raw2, _ := NewDataKey()
	if err := r.Install(id2, map[string][]byte{id1: raw1, id2: raw2}); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ enc, rec, want string }{{enc, "rec3", "new@b.c"}, {v2, "rec2", "v2@b.c"}, {legacy, "rec9", "old@b.c"}} {
		if !r.NeedsReseal(c.enc) {
			t.Fatalf("%q should need a reseal", c.enc[:6])
		}
		moved, err := r.Reseal(c.enc, c.rec)
		if err != nil || !strings.HasPrefix(moved, "v3:"+id2+":") {
			t.Fatalf("Reseal: %q %v", moved, err)
		}
		if r.NeedsReseal(moved) {
			t.Fatal("a resealed value still needs a reseal")
		}
		if got, err := r.DecryptFor(moved, c.rec); err != nil || got != c.want {
			t.Fatalf("open resealed: %q %v", got, err)
		}
		if same, _ := r.Reseal(moved, c.rec); same != moved {
			t.Fatal("Reseal of a current value must return it unchanged")
		}
	}
	if r.NeedsReseal("") {
		t.Fatal("empty never needs a reseal")
	}
}

func TestRingInstallRejectsBadInput(t *testing.T) {
	r := NewRing(testRoot(t))
	id, raw, _ := NewDataKey()
	if err := r.Install("kmissing", map[string][]byte{id: raw}); err == nil {
		t.Fatal("current key not in the ring was accepted")
	}
	if err := r.Install(id, map[string][]byte{id: raw[:16]}); err == nil {
		t.Fatal("a short key was accepted")
	}
	if err := r.Install("BAD:ID", map[string][]byte{"BAD:ID": raw}); err == nil {
		t.Fatal("a bad key id was accepted")
	}
}

func TestRingRefreshesOnUnknownKey(t *testing.T) {
	root := testRoot(t)
	writer := NewRing(root)
	id, raw, _ := NewDataKey()
	_ = writer.Install(id, map[string][]byte{id: raw})
	enc, _ := writer.EncryptFor("x@y.z", "rec")

	reader := NewRing(root)
	if _, err := reader.DecryptFor(enc, "rec"); !errors.Is(err, ErrUnknownKey) {
		t.Fatalf("want ErrUnknownKey without a refresh, got %v", err)
	}
	calls := 0
	reader.SetRefresh(func() error {
		calls++
		return reader.Install(id, map[string][]byte{id: raw})
	})
	reader.lastRefresh = reader.lastRefresh.AddDate(0, 0, -1)
	if got, err := reader.DecryptFor(enc, "rec"); err != nil || got != "x@y.z" || calls != 1 {
		t.Fatalf("after refresh: %q %v calls=%d", got, err, calls)
	}
}
