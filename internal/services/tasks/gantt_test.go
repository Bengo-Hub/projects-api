package tasks

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func newIDs(n int) []uuid.UUID {
	out := make([]uuid.UUID, n)
	for i := range out {
		out[i] = uuid.New()
	}
	return out
}

// A -> B -> D (3 + 4 + 2 = 9 days) beats A -> C -> D (3 + 1 + 2 = 6): A, B and D are critical, C
// has three days of slack.
func TestCriticalPathLongestChain(t *testing.T) {
	id := newIDs(4)
	a, b, c, d := id[0], id[1], id[2], id[3]
	nodes := []cpNode{{a, 3}, {b, 4}, {c, 1}, {d, 2}}
	links := []cpLink{{a, b}, {a, c}, {b, d}, {c, d}}
	crit, days := criticalPath(nodes, links)
	if days != 9 {
		t.Errorf("plan length = %d, want 9", days)
	}
	for _, want := range []uuid.UUID{a, b, d} {
		if !crit[want] {
			t.Errorf("task on the longest chain not critical")
		}
	}
	if crit[c] {
		t.Error("task with slack marked critical")
	}
}

// Independent tasks: only the longest one is critical; undated tasks never are.
func TestCriticalPathIndependentAndUndated(t *testing.T) {
	id := newIDs(3)
	crit, days := criticalPath([]cpNode{{id[0], 5}, {id[1], 2}, {id[2], 0}}, nil)
	if days != 5 || !crit[id[0]] || crit[id[1]] || crit[id[2]] {
		t.Errorf("crit=%v days=%d", crit, days)
	}
	if crit, days := criticalPath([]cpNode{{id[0], 0}}, nil); days != 0 || len(crit) != 0 {
		t.Errorf("no dated task: crit=%v days=%d", crit, days)
	}
}

// Two equally long chains are both critical.
func TestCriticalPathTies(t *testing.T) {
	id := newIDs(4)
	a, b, c, d := id[0], id[1], id[2], id[3]
	crit, days := criticalPath([]cpNode{{a, 2}, {b, 3}, {c, 3}, {d, 1}}, []cpLink{{a, b}, {a, c}, {b, d}, {c, d}})
	if days != 6 {
		t.Errorf("plan length = %d, want 6", days)
	}
	for _, x := range id {
		if !crit[x] {
			t.Errorf("every task of two equal chains is critical")
		}
	}
}

// A cycle in old data and links to tasks outside the set are ignored without looping.
func TestCriticalPathCycleAndOutsideLinks(t *testing.T) {
	id := newIDs(4)
	a, b, c, outside := id[0], id[1], id[2], id[3]
	crit, days := criticalPath([]cpNode{{a, 4}, {b, 1}, {c, 1}}, []cpLink{{b, c}, {c, b}, {outside, a}, {a, outside}})
	if days != 4 || !crit[a] || crit[b] || crit[c] {
		t.Errorf("crit=%v days=%d", crit, days)
	}
}

func TestDurationDays(t *testing.T) {
	d := func(s string) time.Time { v, _ := time.Parse(time.DateOnly, s); return v }
	start := d("2026-11-02")
	if got := durationDays(&start, d("2026-11-06")); got != 5 {
		t.Errorf("Mon to Fri = %d days, want 5", got)
	}
	if got := durationDays(&start, d("2026-11-02")); got != 1 {
		t.Errorf("same day = %d, want 1", got)
	}
	if got := durationDays(nil, d("2026-11-02")); got != 1 {
		t.Errorf("due only = %d, want 1", got)
	}
	if got := durationDays(nil, time.Time{}); got != 0 {
		t.Errorf("undated = %d, want 0", got)
	}
}
