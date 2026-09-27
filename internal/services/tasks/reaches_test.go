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
