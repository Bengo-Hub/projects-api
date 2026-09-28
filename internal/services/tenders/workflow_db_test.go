package tenders

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/projects-service/internal/testutil"
)

type fakePublisher struct{ events []string }

func (f *fakePublisher) PublishTenderEvent(_ context.Context, _, _ uuid.UUID, eventType string, _ map[string]any) error {
	f.events = append(f.events, eventType)
	return nil
}

func (f *fakePublisher) has(e string) bool {
	for _, x := range f.events {
		if x == e {
			return true
		}
	}
	return false
}

// TestTenderWorkflow_DB walks one tender from creation to award on a real schema: evaluations,
// go decision, sections with review, final document, ready flag, email submission, outcome,
// metrics and delete.
func TestTenderWorkflow_DB(t *testing.T) {
	client := testutil.PGClient(t)
	ctx := context.Background()
	pub := &fakePublisher{}
	svc := NewService(client, nil, zap.NewNop())
	svc.SetPublisher(pub)
	tenant, user := uuid.New(), uuid.New()

	value := 2_000_000.0
	tender, err := svc.CreateTender(ctx, tenant, CreateTenderInput{Title: "Water works", ClientName: "County", EstimatedValue: &value, CreatedBy: user})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateTender(ctx, tenant, CreateTenderInput{Title: "x", ClientName: "y", Status: StatusAwarded}); err == nil {
		t.Fatal("a new tender cannot start as awarded")
	}

	// Another tenant cannot reach it.
	if _, err := svc.SubmitEvaluation(ctx, uuid.New(), tender.ID, SubmitEvaluationInput{Score: 1}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant evaluation: %v", err)
	}
	for _, score := range []float64{70, 90} {
		if _, err := svc.SubmitEvaluation(ctx, tenant, tender.ID, SubmitEvaluationInput{EvaluatorID: uuid.New(), Score: score, Criteria: "technical"}); err != nil {
			t.Fatal(err)
		}
	}
	sum, err := svc.GetEvaluationSummary(ctx, tenant, tender.ID)
	if err != nil || sum.Average != 80 || sum.Evaluators != 2 {
		t.Fatalf("summary = %+v, %v", sum, err)
	}

	// Submitting before a go decision is refused.
	if _, err := svc.SubmitTender(ctx, tenant, tender.ID, SubmitTenderInput{Method: "online"}); !errors.As(err, new(ErrTransition)) {
		t.Fatalf("submit before decision: %v", err)
	}
	if _, err := svc.MakeDecision(ctx, tenant, tender.ID, DecisionInput{Decision: "go"}); err == nil {
		t.Fatal("a decision needs a rationale")
	}
	tender, err = svc.MakeDecision(ctx, tenant, tender.ID, DecisionInput{Decision: "go", Rationale: "strong fit", DecidedBy: user})
	if err != nil || tender.Status != StatusPreparing || tender.Decision != "go" || tender.DecidedBy == nil || len(tender.StatusHistory) != 1 {
		t.Fatalf("decision: %+v, %v", tender, err)
	}

	// Sections: two, one approved first time, one after changes.
	writer, reviewer := uuid.New(), uuid.New()
	s1, err := svc.CreateSection(ctx, tenant, tender.ID, CreateSectionInput{Title: "Technical", AssigneeID: &writer, ReviewerID: &reviewer})
	if err != nil || s1.SortOrder != 1 {
		t.Fatalf("section 1: %+v, %v", s1, err)
	}
	s2, err := svc.CreateSection(ctx, tenant, tender.ID, CreateSectionInput{Title: "Financial"})
	if err != nil || s2.SortOrder != 2 {
		t.Fatalf("section 2: %+v, %v", s2, err)
	}
	if _, err := svc.SubmitSection(ctx, tenant, tender.ID, s1.ID, SubmitSectionInput{}); err == nil {
		t.Fatal("submitting without a document must fail")
	}
	if _, err := svc.ApproveSection(ctx, tenant, tender.ID, s1.ID, ReviewInput{}); !errors.As(err, new(ErrTransition)) {
		t.Fatalf("approve before submit: %v", err)
	}
	for _, id := range []uuid.UUID{s1.ID, s2.ID} {
		if _, err := svc.SubmitSection(ctx, tenant, tender.ID, id, SubmitSectionInput{DocumentURL: "https://drive.example.com/s.docx"}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := svc.RequestSectionChanges(ctx, tenant, tender.ID, s2.ID, ReviewInput{}); err == nil {
		t.Fatal("requesting changes needs comments")
	}
	if _, err := svc.RequestSectionChanges(ctx, tenant, tender.ID, s2.ID, ReviewInput{Comments: "add VAT", ReviewedBy: reviewer}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ApproveSection(ctx, tenant, tender.ID, s1.ID, ReviewInput{ReviewedBy: reviewer}); err != nil {
		t.Fatal(err)
	}

	// Ready needs every section approved and a final document.
	if _, err := svc.SetReady(ctx, tenant, tender.ID, true); err == nil {
		t.Fatal("ready with a section awaiting changes must fail")
	}
	if _, err := svc.SubmitSection(ctx, tenant, tender.ID, s2.ID, SubmitSectionInput{}); err != nil {
		t.Fatal(err) // resubmits with the stored document
	}
	if _, err := svc.ApproveSection(ctx, tenant, tender.ID, s2.ID, ReviewInput{}); err != nil {
		t.Fatal(err)
	}
	_, ss, err := svc.ListSections(ctx, tenant, tender.ID)
	if err != nil || !ss.AllApproved {
		t.Fatalf("sections summary = %+v, %v", ss, err)
	}
	if _, err := svc.SetReady(ctx, tenant, tender.ID, true); err == nil {
		t.Fatal("ready without a final document must fail")
	}
	for v := 1; v <= 2; v++ {
		doc, err := svc.AddFinalDocument(ctx, tenant, tender.ID, FinalDocumentInput{FileURL: "https://drive.example.com/final.pdf", UploadedBy: user})
		if err != nil || doc.Version != v || doc.Kind != "final" {
			t.Fatalf("final document v%d: %+v, %v", v, doc, err)
		}
	}
	if tender, err = svc.SetReady(ctx, tenant, tender.ID, true); err != nil || !tender.ReadyForSubmission || tender.FinalDocumentVersion != 2 {
		t.Fatalf("ready: %+v, %v", tender, err)
	}

	// Email submission moves it to submitted and carries the final document.
	sub, err := svc.SubmitTender(ctx, tenant, tender.ID, SubmitTenderInput{
		Method: "email", RecipientEmail: "procurement@county.go.ke", EmailSubject: "Tender TND", SubmittedBy: user,
	})
	if err != nil || sub.DeliveryStatus != "queued" || sub.DocumentURL != "https://drive.example.com/final.pdf" {
		t.Fatalf("submission: %+v, %v", sub, err)
	}
	// A physical copy afterwards is recorded without another status change.
	if _, err := svc.SubmitTender(ctx, tenant, tender.ID, SubmitTenderInput{Method: "physical", Address: "County HQ"}); err != nil {
		t.Fatal(err)
	}
	subs, err := svc.ListSubmissions(ctx, tenant, tender.ID)
	if err != nil || len(subs) != 2 {
		t.Fatalf("submissions = %d, %v", len(subs), err)
	}

	if _, err := svc.UpdateStatus(ctx, tenant, tender.ID, StatusInput{Status: StatusDraft}); !errors.As(err, new(ErrTransition)) {
		t.Fatalf("submitted -> draft: %v", err)
	}
	if _, err := svc.UpdateStatus(ctx, tenant, tender.ID, StatusInput{Status: StatusShortlisted, Notes: "letter received", ChangedBy: user}); err != nil {
		t.Fatal(err)
	}
	award := 1_800_000.0
	tender, err = svc.RecordOutcome(ctx, tenant, tender.ID, OutcomeInput{Outcome: StatusAwarded, AwardValue: &award, ContractReference: "CT-7"})
	if err != nil || tender.Status != StatusAwarded || tender.Outcome["award_value"] != award {
		t.Fatalf("outcome: %+v, %v", tender, err)
	}
	// decision, submitted, shortlisted, awarded
	if len(tender.StatusHistory) != 4 {
		t.Fatalf("history = %v", tender.StatusHistory)
	}

	// A second tender lost, so the win rate is 50%.
	lost, _ := svc.CreateTender(ctx, tenant, CreateTenderInput{Title: "Roads", ClientName: "KeNHA", CreatedBy: user})
	if _, err := svc.MakeDecision(ctx, tenant, lost.ID, DecisionInput{Decision: "go", Rationale: "ok"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SubmitTender(ctx, tenant, lost.ID, SubmitTenderInput{Method: "online"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RecordOutcome(ctx, tenant, lost.ID, OutcomeInput{Outcome: StatusLost, LossReason: "price"}); err != nil {
		t.Fatal(err)
	}
	m, err := svc.GetTenderMetrics(ctx, tenant)
	if err != nil || m["win_rate"] != 50.0 || m["awarded_value"] != value || m["total"] != 2 {
		t.Fatalf("metrics = %v, %v", m, err)
	}

	for _, e := range []string{
		"tender.created", "tender.evaluation.submitted", "tender.decision.made", "tender.section.assigned",
		"tender.section.submitted", "tender.section.changes_requested", "tender.section.approved",
		"tender.submitted", "tender.status.changed", "tender.awarded", "tender.lost",
	} {
		if !pub.has(e) {
			t.Errorf("event %s not published (got %v)", e, pub.events)
		}
	}

	// Delete removes the tender and every child row.
	if err := svc.DeleteTender(ctx, tenant, tender.ID); err != nil {
		t.Fatal(err)
	}
	if n, _ := client.TenderSection.Query().Count(ctx); n != 0 {
		t.Fatalf("%d sections left after delete", n)
	}
	if err := svc.DeleteTender(ctx, tenant, tender.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second delete: %v", err)
	}
}
