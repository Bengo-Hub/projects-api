package tasks

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
)

func hrs(v float64) *float64 { return &v }

func TestEvaluateMoveTransitions(t *testing.T) {
	rules := StatusRules{Transitions: []Transition{{From: "todo", To: []string{"in_progress"}}}}
	labels := map[string]string{"todo": "To do", "done": "Done"}
	if b := EvaluateMove(rules, "todo", "done", MoveFacts{}, labels); b == nil || b.Reason != ReasonTransition {
		t.Fatalf("todo to done should be refused, got %+v", b)
	}
	if b := EvaluateMove(rules, "todo", "in_progress", MoveFacts{}, labels); b != nil {
		t.Fatalf("todo to in_progress is listed: %+v", b)
	}
	// A status without a transition entry moves anywhere; a non-move is never blocked.
	if b := EvaluateMove(rules, "review", "todo", MoveFacts{}, labels); b != nil {
		t.Fatalf("review has no entry: %+v", b)
	}
	if b := EvaluateMove(rules, "todo", "todo", MoveFacts{}, labels); b != nil {
		t.Fatal("same status is not a move")
	}
}

func TestEvaluateMoveRequirements(t *testing.T) {
	rules := StatusRules{Requirements: []Requirement{
		{To: "done", Fields: []string{FieldActualHours, FieldComment}},
		{To: "done", From: "review", Fields: []string{"metadata.signoff"}},
	}}
	b := EvaluateMove(rules, "in_progress", "done", MoveFacts{}, map[string]string{"done": "Done"})
	if b == nil || b.Reason != ReasonMissing || !slices.Equal(b.Missing, []string{FieldActualHours, FieldComment}) {
		t.Fatalf("missing fields: %+v", b)
	}
	if b.Message != "Before moving to Done, this task needs actual hours, a comment." {
		t.Fatalf("message: %q", b.Message)
	}
	ok := MoveFacts{Comments: 1, Metadata: map[string]any{MetaActualHours: 2.5}}
	if b := EvaluateMove(rules, "in_progress", "done", ok, nil); b != nil {
		t.Fatalf("satisfied: %+v", b)
	}
	// The from-specific requirement adds to the general one.
	if b := EvaluateMove(rules, "review", "done", ok, nil); b == nil || !slices.Equal(b.Missing, []string{"metadata.signoff"}) {
		t.Fatalf("from review: %+v", b)
	}
	ok.Metadata["signoff"] = "Jane"
	if b := EvaluateMove(rules, "review", "done", ok, nil); b != nil {
		t.Fatalf("signed off: %+v", b)
	}
	if !rules.NeedsCounts("x", "done") || rules.NeedsCounts("x", "review") {
		t.Fatal("NeedsCounts")
	}
}

func TestMoveFactsFields(t *testing.T) {
	now := time.Now()
	f := MoveFacts{Assignees: 1, EstimatedHours: hrs(3), StartDate: &now, DueDate: now, Description: " x "}
	for _, code := range []string{FieldAssignee, FieldEstimatedHours, FieldStartDate, FieldDueDate, FieldDescription} {
		if !f.has(code) {
			t.Errorf("%s should be present", code)
		}
	}
	empty := MoveFacts{EstimatedHours: hrs(0), Description: "  ", Metadata: map[string]any{"k": "", MetaActualHours: 0.0}}
	for _, code := range []string{FieldAssignee, FieldEstimatedHours, FieldActualHours, FieldStartDate, FieldDueDate, FieldDescription, FieldComment, "metadata.k"} {
		if empty.has(code) {
			t.Errorf("%s should be missing", code)
		}
	}
}

func TestValidateStatusRules(t *testing.T) {
	st := DefaultStatuses()
	if _, err := ValidateStatusRules(StatusRules{Transitions: []Transition{{From: "nope", To: []string{"done"}}}}, st); err == nil {
		t.Fatal("unknown from accepted")
	}
	if _, err := ValidateStatusRules(StatusRules{Requirements: []Requirement{{To: "done", Fields: []string{"hours"}}}}, st); err == nil {
		t.Fatal("unknown field accepted")
	}
	if _, err := ValidateStatusRules(StatusRules{Requirements: []Requirement{{To: "done"}}}, st); err == nil {
		t.Fatal("empty fields accepted")
	}
	out, err := ValidateStatusRules(StatusRules{
		Transitions:  []Transition{{From: "todo", To: []string{"in_progress", "todo", "in_progress"}}},
		Requirements: []Requirement{{To: "done", Fields: []string{"comment", "comment", "metadata.qa_ok"}}},
	}, st)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(out.Transitions[0].To, []string{"in_progress"}) || !slices.Equal(out.Requirements[0].Fields, []string{"comment", "metadata.qa_ok"}) {
		t.Fatalf("not de-duplicated: %+v", out)
	}
}

func TestMatchAndValidateAutomations(t *testing.T) {
	ana := uuid.New()
	list := []Automation{
		{Name: "to review", Enabled: true, Trigger: Trigger{To: "review"}, Actions: []AutomationAction{{Type: ActionAssign, UserIDs: []uuid.UUID{ana}}}},
		{Name: "done from review", Enabled: true, Trigger: Trigger{To: "done", From: "review"}, Actions: []AutomationAction{{Type: ActionSetField, Field: "priority", Value: "low"}}},
		{Name: "off", Enabled: false, Trigger: Trigger{To: "done"}, Actions: []AutomationAction{{Type: ActionNotify, Recipients: []string{"assignees"}}}},
	}
	member := func(id uuid.UUID) bool { return id == ana }
	out, err := ValidateAutomations(list, DefaultStatuses(), member)
	if err != nil {
		t.Fatal(err)
	}
	if out[0].ID == "" || out[0].Actions[0].Mode != "add" {
		t.Fatalf("defaults not filled: %+v", out[0])
	}
	if got := MatchAutomations(out, "todo", "done"); len(got) != 0 {
		t.Fatalf("from todo: %+v", got)
	}
	if got := MatchAutomations(out, "review", "done"); len(got) != 1 || got[0].Name != "done from review" {
		t.Fatalf("from review: %+v", got)
	}
	if got := MatchAutomations(out, "review", "review"); got != nil {
		t.Fatal("no move fires nothing")
	}
	bad := []Automation{{Name: "x", Trigger: Trigger{To: "done"}, Actions: []AutomationAction{{Type: ActionAssign, UserIDs: []uuid.UUID{uuid.New()}}}}}
	if _, err := ValidateAutomations(bad, DefaultStatuses(), member); err == nil {
		t.Fatal("non-member assignee accepted")
	}
	for _, act := range []AutomationAction{
		{Type: ActionSetField, Field: "status", Value: "done"},
		{Type: ActionSetField, Field: "metadata.checklist", Value: 1},
		{Type: ActionSetField, Field: "metadata.repeat", Value: 1},
		{Type: ActionSetField, Field: "priority", Value: "extreme"},
		{Type: "delete"},
	} {
		if _, err := ValidateAutomations([]Automation{{Name: "x", Trigger: Trigger{To: "done"}, Actions: []AutomationAction{act}}}, DefaultStatuses(), member); err == nil {
			t.Errorf("accepted %+v", act)
		}
	}
	many := make([]Automation, MaxAutomations+1)
	if _, err := ValidateAutomations(many, DefaultStatuses(), member); err == nil {
		t.Fatal("over the limit accepted")
	}
}

func date(s string) time.Time {
	t, _ := time.Parse(time.DateOnly, s)
	return t
}

func TestNextOccurrence(t *testing.T) {
	cases := []struct {
		from   string
		rule   RepeatRule
		anchor int
		want   string
	}{
		{"2026-11-16", RepeatRule{Freq: FreqDaily, Interval: 1}, 0, "2026-11-17"},
		{"2026-11-16", RepeatRule{Freq: FreqDaily, Interval: 3}, 0, "2026-11-19"},
		{"2026-11-16", RepeatRule{Freq: FreqWeekly, Interval: 2}, 0, "2026-11-30"},
		{"2026-01-31", RepeatRule{Freq: FreqMonthly, Interval: 1}, 31, "2026-02-28"},
		{"2026-02-28", RepeatRule{Freq: FreqMonthly, Interval: 1}, 31, "2026-03-31"},
		{"2028-01-31", RepeatRule{Freq: FreqMonthly, Interval: 1}, 0, "2028-02-29"},
		{"2026-11-15", RepeatRule{Freq: FreqMonthly, Interval: 3}, 0, "2027-02-15"},
	}
	for _, c := range cases {
		if got := NextOccurrence(date(c.from), c.rule, c.anchor).Format(time.DateOnly); got != c.want {
			t.Errorf("%s %+v: %s, want %s", c.from, c.rule, got, c.want)
		}
	}
}

func TestNextDueSkipsMissedRoundsAndEnds(t *testing.T) {
	weekly := RepeatRule{Freq: FreqWeekly, Interval: 1}
	got, ok := NextDue(date("2026-10-01"), date("2026-11-17"), weekly, 0)
	if !ok || got.Format(time.DateOnly) != "2026-11-19" {
		t.Fatalf("skip: %s %v", got.Format(time.DateOnly), ok)
	}
	weekly.Until = "2026-11-18"
	if _, ok := NextDue(date("2026-10-01"), date("2026-11-17"), weekly, 0); ok {
		t.Fatal("past until should end the series")
	}
}

func TestParseRepeat(t *testing.T) {
	r, err := ParseRepeat(map[string]any{"freq": "weekly"})
	if err != nil || r.Interval != 1 {
		t.Fatalf("default interval: %+v %v", r, err)
	}
	for _, bad := range []any{"weekly", map[string]any{"freq": "hourly"}, map[string]any{"freq": "daily", "interval": 400},
		map[string]any{"freq": "daily", "until": "soon"}} {
		if _, err := ParseRepeat(bad); err == nil {
			t.Errorf("accepted %v", bad)
		}
	}
	meta := map[string]any{MetaRepeat: map[string]any{"freq": "daily", "interval": 2.0, "extra": true}}
	if err := normalizeRepeat(meta); err != nil {
		t.Fatal(err)
	}
	if m := meta[MetaRepeat].(map[string]any); len(m) != 2 || m["interval"] != 2 {
		t.Fatalf("not canonical: %v", m)
	}
	var verr ValidationError
	if err := normalizeRepeat(map[string]any{MetaRepeat: "x"}); !errors.As(err, &verr) {
		t.Fatalf("want a validation error, got %v", err)
	}
}
