package tenders

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/bengobox/projects-service/internal/ent"
)

func TestCanTransition(t *testing.T) {
	cases := []struct {
		from, to string
		ok       bool
	}{
		{StatusDraft, StatusEvaluating, true},
		{StatusDraft, StatusPreparing, true},
		{StatusEvaluating, StatusNoGo, true},
		{StatusPreparing, StatusSubmitted, true},
		{StatusSubmitted, StatusShortlisted, true},
		{StatusShortlisted, StatusAwarded, true},
		{StatusInterview, StatusLost, true},
		{StatusDraft, StatusSubmitted, false},     // needs a go decision first
		{StatusDraft, StatusAwarded, false},       // cannot win what was never submitted
		{StatusAwarded, StatusLost, false},        // closed
		{StatusNoGo, StatusPreparing, false},      // closed
		{StatusCancelled, StatusDraft, false},     // closed
		{StatusSubmitted, StatusPreparing, false}, // no going back once submitted
	}
	for _, c := range cases {
		if got := CanTransition(c.from, c.to); got != c.ok {
			t.Errorf("%s -> %s: got %v, want %v", c.from, c.to, got, c.ok)
		}
	}
	for _, st := range tenderStatuses {
		for _, to := range transitions[st] {
			if !knownStatus(to) {
				t.Errorf("transition %s -> %s names an unknown status", st, to)
			}
		}
	}
}

func TestNextHistory(t *testing.T) {
	by := uuid.New()
	now := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	tender := &ent.Tender{Status: StatusDraft, StatusHistory: []map[string]any{{"from": "", "to": "draft"}}}
	h, err := nextHistory(tender, StatusEvaluating, "committee formed", by, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(h) != 2 || h[1]["from"] != StatusDraft || h[1]["to"] != StatusEvaluating ||
		h[1]["notes"] != "committee formed" || h[1]["by"] != by.String() || h[1]["at"] != "2026-09-01T10:00:00Z" {
		t.Fatalf("history = %v", h)
	}
	if len(tender.StatusHistory) != 1 {
		t.Fatal("nextHistory must not modify the tender's slice")
	}
	var te ErrTransition
	if _, err := nextHistory(tender, StatusAwarded, "", by, now); !errors.As(err, &te) {
		t.Fatalf("want ErrTransition, got %v", err)
	}
	var ve ValidationError
	if _, err := nextHistory(tender, "bogus", "", by, now); !errors.As(err, &ve) {
		t.Fatalf("want ValidationError, got %v", err)
	}
}

func TestSummarize(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	sum := summarize([]*ent.TenderEvaluation{
		{EvaluatorID: a, Score: 80, Criteria: "technical"},
		{EvaluatorID: a, Score: 60, Criteria: "price"},
		{EvaluatorID: b, Score: 70, Criteria: "technical"},
		{EvaluatorID: b, Score: 90},
	})
	if sum.Count != 4 || sum.Evaluators != 2 || sum.Min != 60 || sum.Max != 90 || sum.Average != 75 {
		t.Fatalf("summary = %+v", sum)
	}
	want := []CriteriaScore{{"overall", 1, 90}, {"price", 1, 60}, {"technical", 2, 75}}
	if len(sum.ByCriteria) != len(want) {
		t.Fatalf("by criteria = %+v", sum.ByCriteria)
	}
	for i := range want {
		if sum.ByCriteria[i] != want[i] {
			t.Errorf("criteria %d = %+v, want %+v", i, sum.ByCriteria[i], want[i])
		}
	}
	if empty := summarize(nil); empty.Count != 0 || empty.ByCriteria == nil {
		t.Fatalf("empty summary = %+v", empty)
	}
}

func TestOutcomeMap(t *testing.T) {
	now := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	v := 1_500_000.0
	out, err := outcomeMap(OutcomeInput{Outcome: StatusAwarded, AwardValue: &v, ContractReference: "C-1", ConvertToProject: true}, now)
	if err != nil {
		t.Fatal(err)
	}
	if out["award_value"] != v || out["contract_reference"] != "C-1" || out["convert_to_project"] != true {
		t.Fatalf("awarded outcome = %v", out)
	}
	if _, err := outcomeMap(OutcomeInput{Outcome: StatusLost}, now); err == nil {
		t.Fatal("lost without a reason must fail")
	}
	out, err = outcomeMap(OutcomeInput{Outcome: StatusLost, LossReason: "price", Competitor: "Acme"}, now)
	if err != nil || out["loss_reason"] != "price" || out["competitor"] != "Acme" {
		t.Fatalf("lost outcome = %v, %v", out, err)
	}
	neg := -1.0
	if _, err := outcomeMap(OutcomeInput{Outcome: StatusAwarded, AwardValue: &neg}, now); err == nil {
		t.Fatal("negative award value must fail")
	}
	if _, err := outcomeMap(OutcomeInput{Outcome: "won"}, now); err == nil {
		t.Fatal("unknown outcome must fail")
	}
}

func TestValidateSubmission(t *testing.T) {
	future := time.Now().Add(time.Hour)
	cases := []struct {
		name string
		in   SubmitTenderInput
		ok   bool
	}{
		{"email ok", SubmitTenderInput{Method: "Email", RecipientEmail: "tenders@client.co.ke", EmailSubject: "TND-1"}, true},
		{"email bad address", SubmitTenderInput{Method: "email", RecipientEmail: "nope", EmailSubject: "x"}, false},
		{"email no subject", SubmitTenderInput{Method: "email", RecipientEmail: "a@b.co"}, false},
		{"physical ok", SubmitTenderInput{Method: "physical", Address: "Box 1, Nairobi"}, true},
		{"physical no address", SubmitTenderInput{Method: "physical", Courier: "G4S"}, false},
		{"online ok", SubmitTenderInput{Method: "online", ConfirmationNumber: "IFMIS-9"}, true},
		{"unknown method", SubmitTenderInput{Method: "fax"}, false},
		{"future date", SubmitTenderInput{Method: "online", SubmittedAt: &future}, false},
	}
	for _, c := range cases {
		in := c.in
		if err := validateSubmission(&in); (err == nil) != c.ok {
			t.Errorf("%s: err = %v", c.name, err)
		}
	}
}

func TestSummarizeSections(t *testing.T) {
	now := time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)
	past := now.Add(-24 * time.Hour)
	sum := summarizeSections([]*ent.TenderSection{
		{Status: SectionApproved, DueDate: &past},
		{Status: SectionReview, DueDate: &past},
		{Status: SectionInProgress},
	}, now)
	if sum.Total != 3 || sum.ByStatus[SectionApproved] != 1 || sum.AllApproved || sum.Overdue != 1 {
		t.Fatalf("summary = %+v", sum)
	}
	if all := summarizeSections([]*ent.TenderSection{{Status: SectionApproved}}, now); !all.AllApproved {
		t.Fatal("one approved section of one is all approved")
	}
	if none := summarizeSections(nil, now); none.AllApproved || none.ByStatus[SectionReview] != 0 {
		t.Fatalf("no sections = %+v", none)
	}
}
