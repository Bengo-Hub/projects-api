package projects

import "testing"

func TestClosesProject(t *testing.T) {
	cases := []struct {
		prev, next string
		want       bool
	}{
		{"active", "completed", true},
		{"on_hold", "cancelled", true},
		{"planning", "archived", true},
		{"active", "closed", true},
		{"completed", "archived", false}, // already closed: no second close
		{"active", "on_hold", false},
		{"completed", "active", false}, // reopening is not a close
		{"active", "active", false},
	}
	for _, c := range cases {
		if got := closesProject(c.prev, c.next); got != c.want {
			t.Errorf("%s -> %s = %v, want %v", c.prev, c.next, got, c.want)
		}
	}
}
