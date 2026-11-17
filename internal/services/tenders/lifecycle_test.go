package tenders

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/bengobox/projects-service/internal/ent"
)

// The weighted score is out of 100 with one decimal, and the recommendation reads it against the
// threshold: go at or above, review within 15 points below, no_go under that.
func TestScoreWeightsAndRecommends(t *testing.T) {
	crit := []Criterion{
		{Key: "fit", Weight: 30, Score: 5},
		{Key: "capability", Weight: 30, Score: 4},
		{Key: "risk", Weight: 40, Score: 2},
	}
	// (30*5 + 30*4 + 40*2) / (100*5) = 350/500 = 70.0
	s, err := Score(crit, 0)
	if err != nil {
		t.Fatal(err)
	}
	if s.WeightedScore != 70 || s.Threshold != DefaultGoThreshold || s.Recommendation != RecommendGo {
		t.Fatalf("scored %+v", s)
	}
	if s.Criteria[0].Label != "fit" {
		t.Errorf("an empty label takes the key: %q", s.Criteria[0].Label)
	}
	if s, _ := Score(crit, 75); s.Recommendation != RecommendReview {
		t.Errorf("70 against 75: %s, want review", s.Recommendation)
	}
	if s, _ := Score(crit, 90); s.Recommendation != RecommendNoGo {
		t.Errorf("70 against 90: %s, want no_go", s.Recommendation)
	}
	// One decimal: 2/3 of the way is 66.7.
	if s, _ := Score([]Criterion{{Key: "a", Weight: 1, Score: 5}, {Key: "b", Weight: 1, Score: 5}, {Key: "c", Weight: 1, Score: 0}}, 0); s.WeightedScore != 66.7 {
		t.Errorf("rounded score %v, want 66.7", s.WeightedScore)
	}
	for name, bad := range map[string][]Criterion{
		"none":        nil,
		"no key":      {{Weight: 1, Score: 1}},
		"twice":       {{Key: "a", Weight: 1, Score: 1}, {Key: "a", Weight: 1, Score: 1}},
		"zero weight": {{Key: "a", Weight: 0, Score: 1}},
		"score above": {{Key: "a", Weight: 1, Score: 6}},
		"negative":    {{Key: "a", Weight: 1, Score: -1}},
	} {
		var verr ValidationError
		if _, err := Score(bad, 0); !errors.As(err, &verr) {
			t.Errorf("%s: %v, want a validation error", name, err)
		}
	}
	if _, err := Score(crit, 101); err == nil {
		t.Error("a threshold above 100 accepted")
	}
}

// A decision against the recommendation needs a comment; review always needs one.
func TestCheckDecisionCommentRule(t *testing.T) {
	goRec := Scored{Recommendation: RecommendGo}
	if err := CheckDecision(goRec, DecisionGo, ""); err != nil {
		t.Errorf("go on a go recommendation without a comment: %v", err)
	}
	if err := CheckDecision(goRec, DecisionNoGo, ""); err == nil {
		t.Error("an override without a comment accepted")
	}
	if err := CheckDecision(goRec, DecisionNoGo, "the client pays late"); err != nil {
		t.Errorf("an override with a comment: %v", err)
	}
	if err := CheckDecision(Scored{Recommendation: RecommendReview}, DecisionGo, " "); err == nil {
		t.Error("a review decision without a comment accepted")
	}
	if err := CheckDecision(goRec, "maybe", "x"); err == nil {
		t.Error("an unknown decision accepted")
	}
}

// Manual moves never reach the statuses the decision, submit and outcome routes own.
func TestManualMoves(t *testing.T) {
	for _, to := range []string{StatusPreparing, StatusNoGo, StatusSubmitted, StatusAwarded, StatusLost} {
		for _, from := range Statuses {
			if CanMove(from, to) {
				t.Errorf("manual move %s to %s allowed", from, to)
			}
		}
	}
	if !CanMove(StatusSubmitted, StatusShortlisted) || !CanMove(StatusNoGo, StatusEvaluating) || !CanMove(StatusCancelled, StatusDraft) {
		t.Error("an expected manual move is refused")
	}
	if CanMove(StatusAwarded, StatusCancelled) || CanMove(StatusLost, StatusDraft) {
		t.Error("a decided tender moved by hand")
	}
	// A status from before the current vocabulary never blocks the tender: it moves back in.
	if !CanMove("bidding", StatusDraft) || !CanMove("bidding", StatusCancelled) || CanMove("bidding", StatusAwarded) ||
		len(ManualMoves("bidding")) != 3 || CanMove(StatusDraft, "bidding") {
		t.Error("legacy status moves")
	}
	if got := ManualMoves(StatusAwarded); got == nil || len(got) != 0 {
		t.Errorf("moves from awarded: %v", got)
	}
	if !CanDecide(StatusPreparing) || CanDecide(StatusSubmitted) || !CanSubmit(StatusShortlisted) || CanSubmit(StatusEvaluating) ||
		!CanRecordOutcome(StatusUnderReview) || CanRecordOutcome(StatusPreparing) {
		t.Error("lifecycle gates")
	}
}

func TestSummarizeSections(t *testing.T) {
	if s := Summarize(nil); s.Ready || s.Total != 0 || len(s.ByStatus) != len(SectionStatuses) {
		t.Errorf("no sections: %+v", s)
	}
	if s := Summarize([]string{SectionApproved, SectionInReview}); s.Ready || s.ByStatus[SectionApproved] != 1 {
		t.Errorf("one open: %+v", s)
	}
	if s := Summarize([]string{SectionApproved, SectionApproved}); !s.Ready {
		t.Errorf("all approved: %+v", s)
	}
}

// The strongest committee seat wins, and the owner is left out (they own the project already).
func TestTeamFromCommittee(t *testing.T) {
	owner, ana, ben, cy := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	seats := []*ent.TenderCommitteeMember{
		{UserID: owner, Role: "chair"},
		{UserID: ana, Role: "member"},
		{UserID: ana, Role: "chair"},
		{UserID: ben, Role: "observer"},
		{UserID: cy, Role: "secretary"},
	}
	team := TeamFromCommittee(seats, owner)
	if len(team) != 3 || team[ana] != "manager" || team[ben] != "viewer" || team[cy] != "member" {
		t.Fatalf("team %v", team)
	}
}

func TestCheckSubmission(t *testing.T) {
	now := time.Now()
	ok := []SubmissionInput{
		{Method: "email", Recipient: "tenders@county.go.ke"},
		{Method: "physical", Recipient: "County HQ, Kisumu", Courier: "G4S", Reference: "TRK-1"},
		{Method: "online", Reference: "IFMIS-123"},
	}
	for _, in := range ok {
		if err := CheckSubmission(&in, now); err != nil {
			t.Errorf("%s: %v", in.Method, err)
		}
	}
	future := now.Add(time.Hour)
	bad := []SubmissionInput{
		{Method: "fax"},
		{Method: "email", Recipient: "not an address"},
		{Method: "email", Recipient: "Tenders <tenders@county.go.ke>"},
		{Method: "physical"},
		{Method: "online", SubmittedAt: &future},
	}
	for _, in := range bad {
		if err := CheckSubmission(&in, now); err == nil {
			t.Errorf("accepted %+v", in)
		}
	}
}

// Server-owned keys never come in through a create or edit; the bid budget is normalised to
// decimal strings with a category.
func TestNormalizeInputMeta(t *testing.T) {
	meta := map[string]any{
		MetaGoDecision: "forged", MetaConversion: map[string]any{"project_id": uuid.NewString()}, MetaOutcome: "won",
		MetaCRMContactID: "  " + "6F1C2B4E-1A2B-4C3D-8E9F-001122334455" + " ",
		MetaBidBudget: map[string]any{"lines": []any{
			map[string]any{"name": "Survey", "planned_amount": 120000.5},
			map[string]any{"name": "Equipment", "category": "capex", "planned_amount": "300000"},
		}},
	}
	if err := normalizeInputMeta(meta); err != nil {
		t.Fatal(err)
	}
	for _, k := range serverOwned {
		if _, ok := meta[k]; ok {
			t.Errorf("server-owned %s kept", k)
		}
	}
	if meta[MetaCRMContactID] != "6f1c2b4e-1a2b-4c3d-8e9f-001122334455" {
		t.Errorf("contact id %v", meta[MetaCRMContactID])
	}
	lines := meta[MetaBidBudget].(map[string]any)["lines"].([]any)
	first := lines[0].(map[string]any)
	if first["planned_amount"] != "120000.5" || first["category"] != "expense" {
		t.Errorf("first line %v", first)
	}
	for _, bad := range []map[string]any{
		{MetaCRMContactID: "nope"},
		{MetaBidBudget: map[string]any{"lines": []any{map[string]any{"name": "", "planned_amount": "1"}}}},
		{MetaBidBudget: map[string]any{"lines": []any{map[string]any{"name": "x", "planned_amount": "-1"}}}},
		{MetaBidBudget: map[string]any{"lines": []any{map[string]any{"name": "x", "planned_amount": "1.234"}}}},
		{MetaBidBudget: map[string]any{"lines": []any{map[string]any{"name": "x", "category": "travel", "planned_amount": "1"}}}},
		{MetaTags: []any{""}},
	} {
		if err := normalizeInputMeta(bad); err == nil {
			t.Errorf("accepted %v", bad)
		}
	}
	// An empty line list removes the bid budget.
	empty := map[string]any{MetaBidBudget: map[string]any{"lines": []any{}}}
	if err := normalizeInputMeta(empty); err != nil {
		t.Fatal(err)
	}
	if _, ok := empty[MetaBidBudget]; ok {
		t.Error("an empty bid budget kept")
	}
}

// The treasury budget is a monthly project budget over the project's dates, a year when there is
// no end, with the bid lines as they are.
func TestBudgetBody(t *testing.T) {
	tn := &ent.Tender{Number: "TND-2026-0007", Metadata: map[string]any{MetaBidBudget: map[string]any{"lines": []any{
		map[string]any{"name": "Survey", "category": "expense", "planned_amount": "1000"},
	}}}}
	pid := uuid.New()
	start := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	b := BudgetBody(tn, pid, "Road", "KES", start, nil)
	if b["end_date"] != "2027-11-30" || b["fiscal_year"] != "2026" || b["budget_type"] != "project" || b["project_id"] != pid.String() ||
		b["period_type"] != "monthly" || b["name"] != "Road budget (tender TND-2026-0007)" || len(b["lines"].([]any)) != 1 {
		t.Fatalf("budget %v", b)
	}
	end := time.Date(2027, 3, 31, 0, 0, 0, 0, time.UTC)
	if b := BudgetBody(tn, pid, "Road", "KES", start, &end); b["end_date"] != "2027-03-31" {
		t.Errorf("end %v", b["end_date"])
	}
}
