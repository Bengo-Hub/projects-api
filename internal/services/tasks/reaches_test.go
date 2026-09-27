package tasks

import (
	"testing"

	"github.com/google/uuid"
)

func TestReaches(t *testing.T) {
	a, b, c, d := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	// a depends on b, b depends on c; d is unrelated. A cycle b -> a -> b is also present to prove
	// the walk terminates.
	g := map[uuid.UUID][]uuid.UUID{a: {b}, b: {c, a}}
	if !reaches(g, a, c) {
		t.Error("a reaches c through b")
	}
	if reaches(g, c, a) {
		t.Error("c depends on nothing")
	}
	if reaches(g, a, d) {
		t.Error("d is unrelated")
	}
	if !reaches(g, d, d) {
		t.Error("a node reaches itself")
	}
}

func TestDateAcceptsPlainAndRFC3339(t *testing.T) {
	for in, want := range map[string]string{
		`"2026-10-01"`:           "2026-10-01",
		`"2026-10-01T09:30:00Z"`: "2026-10-01",
		`"2026-10-01T09:30:00"`:  "2026-10-01",
	} {
		var d Date
		if err := d.UnmarshalJSON([]byte(in)); err != nil {
			t.Fatalf("%s: %v", in, err)
		}
		if got := d.Format("2006-01-02"); got != want {
			t.Errorf("%s = %s, want %s", in, got, want)
		}
	}
	var d Date
	if err := d.UnmarshalJSON([]byte(`"01/10/2026"`)); err == nil {
		t.Error("an unknown format must be refused")
	}
	if err := d.UnmarshalJSON([]byte(`""`)); err != nil || !d.IsZero() {
		t.Error("empty is no date")
	}
}
