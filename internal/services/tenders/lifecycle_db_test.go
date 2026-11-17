package tenders

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/projects-service/internal/ent"
	entattachment "github.com/bengobox/projects-service/internal/ent/attachment"
	entoutbox "github.com/bengobox/projects-service/internal/ent/outboxevent"
	entproject "github.com/bengobox/projects-service/internal/ent/project"
	entmember "github.com/bengobox/projects-service/internal/ent/projectmember"
	enttask "github.com/bengobox/projects-service/internal/ent/task"
	"github.com/bengobox/projects-service/internal/platform/treasury"
	"github.com/bengobox/projects-service/internal/services/activity"
	"github.com/bengobox/projects-service/internal/services/planning"
	"github.com/bengobox/projects-service/internal/services/tasks"
	"github.com/bengobox/projects-service/internal/testutil"
)

type tfx struct {
	client            *ent.Client
	svc               *Service
	tenant            uuid.UUID
	lead, ana, ben    uuid.UUID
	asLead, asAna     context.Context
	asBen             context.Context
	tender            *ent.Tender
	evalCriteria      []Criterion
}

func newTFx(t *testing.T) *tfx {
	t.Helper()
	client := testutil.Client(t)
	f := &tfx{client: client, tenant: uuid.New(), lead: uuid.New(), ana: uuid.New(), ben: uuid.New()}
	f.svc = NewService(client, nil, zap.NewNop()).WithConversion(planning.NewService(client, nil, nil, zap.NewNop()), nil)
	f.asLead = activity.WithActor(context.Background(), f.lead)
	f.asAna = activity.WithActor(context.Background(), f.ana)
	f.asBen = activity.WithActor(context.Background(), f.ben)
	value := 2_500_000.0
	tn, err := f.svc.CreateTender(f.asLead, f.tenant, CreateTenderInput{Title: "County roads", ClientName: "Kisumu County",
		EstimatedValue: &value, CreatedBy: f.lead})
	if err != nil {
		t.Fatal(err)
	}
	f.tender = tn
	f.evalCriteria = []Criterion{{Key: "fit", Weight: 50, Score: 4}, {Key: "risk", Weight: 50, Score: 4}} // 80: go
	return f
}

func day(s string) *tasks.Date {
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		panic(err)
	}
	return &tasks.Date{Time: t}
}

func (f *tfx) events(t *testing.T, eventType string) []map[string]any {
	t.Helper()
	rows := f.client.OutboxEvent.Query().Where(entoutbox.EventType(eventType)).AllX(context.Background())
	out := make([]map[string]any, 0, len(rows))
	for _, r := range rows {
		var env struct {
			Payload map[string]any `json:"payload"`
		}
		_ = json.Unmarshal(r.Payload, &env)
		out = append(out, env.Payload)
	}
	return out
}

func (f *tfx) decideGo(t *testing.T) {
	t.Helper()
	if _, err := f.svc.Decide(f.asLead, f.tenant, f.tender.ID, DecisionInput{Criteria: f.evalCriteria, Decision: DecisionGo}); err != nil {
		t.Fatal(err)
	}
}

func (f *tfx) submit(t *testing.T) {
	t.Helper()
	if _, err := f.svc.Submit(f.asLead, f.tenant, f.tender.ID, SubmissionInput{Method: "online", Reference: "PPIP-1"}); err != nil {
		t.Fatal(err)
	}
}

func (f *tfx) win(t *testing.T, in OutcomeInput) *ent.Tender {
	t.Helper()
	in.Outcome = OutcomeWon
	tn, err := f.svc.RecordOutcome(f.asLead, f.tenant, f.tender.ID, in)
	if err != nil {
		t.Fatal(err)
	}
	return tn
}

func stateCode(err error) string {
	var se *StateError
	if errors.As(err, &se) {
		return se.Code
	}
	return ""
}

// The go/no-go decision is scored, stored with the committee's evaluations, moves the tender to
// preparing or no_go, keeps the earlier decision in the history, writes tender.go_decision, and
// closes once the bid is submitted.
func TestDecisionScoresAndMovesTheTender(t *testing.T) {
	f := newTFx(t)
	if _, err := f.svc.SubmitEvaluation(f.asAna, f.tenant, f.tender.ID, SubmitEvaluationInput{EvaluatorID: f.ana, Score: 7}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.SubmitEvaluation(f.asBen, f.tenant, f.tender.ID, SubmitEvaluationInput{EvaluatorID: f.ben, Score: 9}); err != nil {
		t.Fatal(err)
	}
	sum, err := f.svc.EvaluationSummary(context.Background(), f.tenant, f.tender.ID)
	if err != nil {
		t.Fatal(err)
	}
	if sum.Count != 2 || sum.AverageScore != 8 || sum.MinScore != 7 || sum.MaxScore != 9 || len(sum.Evaluators) != 2 || len(sum.DefaultCriteria) == 0 {
		t.Fatalf("summary %+v", sum)
	}

	low := []Criterion{{Key: "fit", Weight: 1, Score: 1}} // 20: no_go
	if _, err := f.svc.Decide(f.asLead, f.tenant, f.tender.ID, DecisionInput{Criteria: low, Decision: DecisionGo}); err == nil {
		t.Fatal("a go against a no_go recommendation without a comment accepted")
	}
	tn, err := f.svc.Decide(f.asLead, f.tenant, f.tender.ID, DecisionInput{Criteria: low, Decision: DecisionNoGo})
	if err != nil {
		t.Fatal(err)
	}
	if tn.Status != StatusNoGo {
		t.Fatalf("status %s, want no_go", tn.Status)
	}
	tn, err = f.svc.Decide(f.asLead, f.tenant, f.tender.ID, DecisionInput{Criteria: f.evalCriteria, Decision: DecisionGo})
	if err != nil {
		t.Fatal(err)
	}
	dec := tn.Metadata[MetaGoDecision].(map[string]any)
	if tn.Status != StatusPreparing || dec["weighted_score"] != 80.0 || dec["recommendation"] != RecommendGo ||
		dec["decided_by"] != f.lead.String() || dec["evaluations"].(map[string]any)["count"] != 2.0 {
		t.Fatalf("decision %v, status %s", dec, tn.Status)
	}
	if hist, _ := tn.Metadata[MetaGoHistory].([]any); len(hist) != 1 {
		t.Fatalf("history %v", tn.Metadata[MetaGoHistory])
	}
	evs := f.events(t, EventGoDecision)
	decisions := map[any]int{}
	for _, e := range evs {
		decisions[e["decision"]]++
	}
	if len(evs) != 2 || decisions[DecisionGo] != 1 || decisions[DecisionNoGo] != 1 {
		t.Fatalf("go_decision events %v", evs)
	}
	// An edit cannot forge or drop the decision.
	if _, err := f.svc.UpdateTender(f.asLead, f.tenant, f.tender.ID, UpdateTenderInput{Metadata: map[string]any{MetaGoDecision: nil, "tags": []any{"roads"}}}); err != nil {
		t.Fatal(err)
	}
	got, _ := f.svc.GetTender(context.Background(), f.tenant, f.tender.ID)
	if _, ok := got.Metadata[MetaGoDecision]; !ok || got.Metadata["tags"] == nil {
		t.Fatalf("metadata after edit %v", got.Metadata)
	}
	f.submit(t)
	if _, err := f.svc.Decide(f.asLead, f.tenant, f.tender.ID, DecisionInput{Criteria: f.evalCriteria, Decision: DecisionGo}); stateCode(err) != "tender_decision_closed" {
		t.Fatalf("decision after submission: %v", err)
	}
}

// Sections: the assignee works and submits, never reviews their own; only the reviewer or a
// manager reviews; changes need a comment; the first submission waits for every section unless
// overridden.
func TestSectionsReviewAndSubmissionGate(t *testing.T) {
	f := newTFx(t)
	title := "Technical proposal"
	ana, ben := f.ana.String(), f.ben.String()
	sec, err := f.svc.CreateSection(f.asLead, f.tenant, f.tender.ID, SectionInput{Title: &title, AssigneeID: &ana, ReviewerID: &ben})
	if err != nil {
		t.Fatal(err)
	}
	same := f.ana.String()
	if _, err := f.svc.CreateSection(f.asLead, f.tenant, f.tender.ID, SectionInput{Title: &title, AssigneeID: &ana, ReviewerID: &same}); err == nil {
		t.Fatal("assignee as their own reviewer accepted")
	}
	// Ben (not the assignee, no manage) cannot work on it; Ana can move it along but not rename it.
	inProgress := SectionInProgress
	if _, err := f.svc.UpdateSection(f.asBen, f.tenant, f.tender.ID, sec.ID, SectionInput{Status: &inProgress}, false); !errors.Is(err, ErrNotYours) {
		t.Fatalf("someone else's section: %v", err)
	}
	other := "Renamed"
	if _, err := f.svc.UpdateSection(f.asAna, f.tenant, f.tender.ID, sec.ID, SectionInput{Title: &other}, false); !errors.Is(err, ErrNotYours) {
		t.Fatalf("assignee renaming: %v", err)
	}
	if s, err := f.svc.UpdateSection(f.asAna, f.tenant, f.tender.ID, sec.ID, SectionInput{Status: &inProgress}, false); err != nil || s.Status != SectionInProgress {
		t.Fatalf("assignee starts: %v", err)
	}
	if _, err := f.svc.SubmitSection(f.asBen, f.tenant, f.tender.ID, sec.ID, false); !errors.Is(err, ErrNotYours) {
		t.Fatalf("submit by someone else: %v", err)
	}
	if s, err := f.svc.SubmitSection(f.asAna, f.tenant, f.tender.ID, sec.ID, false); err != nil || s.Status != SectionInReview || s.SubmittedAt == nil {
		t.Fatalf("submit: %v", err)
	}
	if _, err := f.svc.ReviewSection(f.asAna, f.tenant, f.tender.ID, sec.ID, true, "", true); !errors.Is(err, ErrOwnSection) {
		t.Fatalf("assignee approving (even as a manager): %v", err)
	}
	stranger := activity.WithActor(context.Background(), uuid.New())
	if _, err := f.svc.ReviewSection(stranger, f.tenant, f.tender.ID, sec.ID, true, "", false); !errors.Is(err, ErrNotReviewer) {
		t.Fatalf("non-reviewer approving: %v", err)
	}
	if _, err := f.svc.ReviewSection(f.asBen, f.tenant, f.tender.ID, sec.ID, false, " ", false); err == nil {
		t.Fatal("changes requested without a comment")
	}
	s, err := f.svc.ReviewSection(f.asBen, f.tenant, f.tender.ID, sec.ID, false, "Add the method statement", false)
	if err != nil || s.Status != SectionChangesRequested || s.ReviewComment != "Add the method statement" {
		t.Fatalf("request changes: %v %+v", err, s)
	}
	if len(f.events(t, EventSectionReview)) != 1 || len(f.events(t, EventSectionReviewed)) != 1 {
		t.Fatal("section events")
	}

	// Submitting before the go decision is refused; after it, the open section blocks the first
	// submission until it is approved or overridden.
	if _, err := f.svc.Submit(f.asLead, f.tenant, f.tender.ID, SubmissionInput{Method: "online"}); stateCode(err) != "tender_no_go_decision" {
		t.Fatalf("submit before go: %v", err)
	}
	f.decideGo(t)
	if _, err := f.svc.Submit(f.asLead, f.tenant, f.tender.ID, SubmissionInput{Method: "online"}); stateCode(err) != "sections_not_approved" {
		t.Fatalf("submit with an open section: %v", err)
	}
	if _, err := f.svc.SubmitSection(f.asAna, f.tenant, f.tender.ID, sec.ID, false); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.ReviewSection(f.asBen, f.tenant, f.tender.ID, sec.ID, true, "", false); err != nil {
		t.Fatal(err)
	}
	_, sum, err := f.svc.ListSections(context.Background(), f.tenant, f.tender.ID)
	if err != nil || !sum.Ready {
		t.Fatalf("summary %+v %v", sum, err)
	}
	sub, err := f.svc.Submit(f.asLead, f.tenant, f.tender.ID, SubmissionInput{Method: "email", Recipient: "tenders@kisumu.go.ke", EmailSubject: "Bid TND"})
	if err != nil {
		t.Fatal(err)
	}
	if sub.Metadata["resubmission"] != false || sub.SubmittedBy != f.lead {
		t.Fatalf("submission %+v", sub)
	}
	got, _ := f.svc.GetTender(context.Background(), f.tenant, f.tender.ID)
	if got.Status != StatusSubmitted || got.SubmittedAt.IsZero() || got.SubmissionType != "email" {
		t.Fatalf("tender after submission: %s %v %s", got.Status, got.SubmittedAt, got.SubmissionType)
	}
	// A clarification later is a second record and leaves the status alone.
	if _, err := f.svc.ChangeStatus(f.asLead, f.tenant, f.tender.ID, StatusInput{Status: StatusUnderReview}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Submit(f.asLead, f.tenant, f.tender.ID, SubmissionInput{Method: "physical", Recipient: "County HQ", Courier: "G4S", Reference: "TRK-9"}); err != nil {
		t.Fatal(err)
	}
	list, err := f.svc.ListSubmissions(context.Background(), f.tenant, f.tender.ID)
	if err != nil || len(list) != 2 {
		t.Fatalf("submissions %d %v", len(list), err)
	}
	got, _ = f.svc.GetTender(context.Background(), f.tenant, f.tender.ID)
	if got.Status != StatusUnderReview {
		t.Fatalf("a resubmission moved the status to %s", got.Status)
	}
	if evs := f.events(t, EventSubmitted); len(evs) != 2 {
		t.Fatalf("submitted events %d", len(evs))
	}
	// Sections are closed with the tender: none can be added once it is lost.
	if _, err := f.svc.RecordOutcome(f.asLead, f.tenant, f.tender.ID, OutcomeInput{Outcome: OutcomeLost}); err == nil {
		t.Fatal("a loss without a reason accepted")
	}
	if _, err := f.svc.RecordOutcome(f.asLead, f.tenant, f.tender.ID, OutcomeInput{Outcome: OutcomeLost, LossReason: "Price", Competitor: "Acme"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.CreateSection(f.asLead, f.tenant, f.tender.ID, SectionInput{Title: &title}); stateCode(err) != "tender_closed" {
		t.Fatalf("section on a lost tender: %v", err)
	}
	if evs := f.events(t, EventLost); len(evs) != 1 || evs[0]["loss_reason"] != "Price" {
		t.Fatalf("lost events %v", evs)
	}
}

// Manual status moves follow the table, through the status route and an edit alike.
func TestManualStatusMoves(t *testing.T) {
	f := newTFx(t)
	if _, err := f.svc.ChangeStatus(f.asLead, f.tenant, f.tender.ID, StatusInput{Status: StatusAwarded}); stateCode(err) != "tender_status_move" {
		t.Fatalf("draft to awarded by hand: %v", err)
	}
	submitted := StatusSubmitted
	if _, err := f.svc.UpdateTender(f.asLead, f.tenant, f.tender.ID, UpdateTenderInput{Status: &submitted}); stateCode(err) != "tender_status_move" {
		t.Fatalf("an edit to submitted: %v", err)
	}
	if tn, err := f.svc.ChangeStatus(f.asLead, f.tenant, f.tender.ID, StatusInput{Status: StatusEvaluating, Note: "Committee formed"}); err != nil || tn.Status != StatusEvaluating {
		t.Fatalf("draft to evaluating: %v", err)
	}
	if evs := f.events(t, EventStatusChanged); len(evs) != 1 || evs[0]["previous_status"] != StatusDraft {
		t.Fatalf("status events %v", evs)
	}
	if _, err := f.svc.RecordOutcome(f.asLead, f.tenant, f.tender.ID, OutcomeInput{Outcome: OutcomeWon}); stateCode(err) != "tender_not_submitted" {
		t.Fatalf("outcome before submission: %v", err)
	}
}

// fakeTreasury answers the budget create: the first call fails (422) when failFirst, later ones
// return a budget id. It records every body.
type fakeTreasury struct {
	mu        sync.Mutex
	failFirst bool
	calls     int
	bodies    []map[string]any
	id        uuid.UUID
}

func (ft *fakeTreasury) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ft.mu.Lock()
	defer ft.mu.Unlock()
	if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/budgets") {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	ft.calls++
	raw, _ := io.ReadAll(r.Body)
	var body map[string]any
	_ = json.Unmarshal(raw, &body)
	ft.bodies = append(ft.bodies, body)
	w.Header().Set("Content-Type", "application/json")
	if ft.failFirst && ft.calls == 1 {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"error":"line 1 has an unknown category"}`))
		return
	}
	w.WriteHeader(http.StatusCreated)
	_, _ = w.Write([]byte(`{"id":"` + ft.id.String() + `"}`))
}

// Converting a won tender creates one project with the commercial terms, the CRM client, the
// committee as the team, the documents linked (not copied), an optional template plan and the bid
// budget as a draft treasury budget after the commit, retried after a failure. Converting again
// returns the same project; a deleted project lets the tender convert again; a converted tender
// cannot be deleted.
func TestConvertWonTender(t *testing.T) {
	f := newTFx(t)
	ft := &fakeTreasury{failFirst: true, id: uuid.New()}
	srv := httptest.NewServer(ft)
	defer srv.Close()
	f.svc.WithConversion(planning.NewService(f.client, nil, nil, zap.NewNop()), treasury.NewClient(srv.URL, "test-key"))
	ctx := context.Background()
	if err := planning.SeedSystemTemplates(ctx, f.client); err != nil {
		t.Fatal(err)
	}
	tpls, _, err := planning.NewService(f.client, nil, nil, zap.NewNop()).ListTemplates(ctx, f.tenant, "", 10, 0)
	if err != nil || len(tpls) == 0 {
		t.Fatalf("templates %v", err)
	}

	contact := uuid.New()
	if _, err := f.svc.UpdateTender(f.asLead, f.tenant, f.tender.ID, UpdateTenderInput{Metadata: map[string]any{
		MetaCRMContactID: contact.String(), MetaCRMContactName: "Kisumu County Government",
		MetaBidBudget: map[string]any{"lines": []any{map[string]any{"name": "Survey", "planned_amount": "150000"}}},
	}}); err != nil {
		t.Fatal(err)
	}
	panel, err := f.svc.CreateCommittee(f.asLead, f.tenant, f.tender.ID, CreateCommitteeInput{Name: "Bid team"})
	if err != nil {
		t.Fatal(err)
	}
	for _, seat := range []struct {
		user uuid.UUID
		role string
	}{{f.ana, "chair"}, {f.ben, "observer"}, {f.lead, "member"}} {
		if _, err := f.svc.AddCommitteeMember(f.asLead, f.tenant, f.tender.ID, panel.ID, AddCommitteeMemberInput{UserID: seat.user, Role: seat.role}); err != nil {
			t.Fatal(err)
		}
	}
	doc, err := f.svc.AddDocument(f.asLead, f.tenant, f.tender.ID, AddDocumentInput{FileURL: "https://files.example.test/rfp.pdf", FileName: "RFP.pdf", UploadedBy: f.lead})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.AddDocument(f.asLead, f.tenant, f.tender.ID, AddDocumentInput{FileURL: "javascript:alert(1)", FileName: "x"}); err == nil {
		t.Fatal("a javascript: link accepted")
	}

	access := ConvertAccess{BudgetAllowed: true}
	if _, err := f.svc.Convert(f.asLead, f.tenant, f.tender.ID, ConvertInput{}, access); stateCode(err) != "tender_not_won" {
		t.Fatalf("converting a draft: %v", err)
	}
	f.decideGo(t)
	f.submit(t)
	award := 2_300_000.0
	f.win(t, OutcomeInput{AwardValue: &award, ContractStart: day("2026-12-01"), ContractEnd: day("2027-05-31")})
	if evs := f.events(t, EventWon); len(evs) != 1 || evs[0]["award_value"] != award {
		t.Fatalf("won events %v", evs)
	}
	stranger := uuid.New()
	if _, err := f.svc.Convert(f.asLead, f.tenant, f.tender.ID, ConvertInput{OwnerID: &stranger}, access); err == nil {
		t.Fatal("an owner off the committee accepted")
	}

	res, err := f.svc.Convert(f.asLead, f.tenant, f.tender.ID, ConvertInput{TemplateID: &tpls[0].ID, BillingType: "fixed"}, access)
	if err != nil {
		t.Fatal(err)
	}
	p := res.Project
	if res.AlreadyConverted || p.Name != "County roads" || p.OwnerID != f.lead || p.Metadata["contract_value"] != award ||
		p.Metadata["billing_type"] != "fixed" || p.Metadata["crm_contact_id"] != contact.String() ||
		p.Metadata["crm_contact_name"] != "Kisumu County Government" || p.Metadata["tender_id"] != f.tender.ID.String() ||
		p.StartDate.Format(time.DateOnly) != "2026-12-01" || p.EndDate.Format(time.DateOnly) != "2027-05-31" {
		t.Fatalf("project %+v", p)
	}
	members := f.client.ProjectMember.Query().Where(entmember.ProjectID(p.ID)).AllX(ctx)
	roles := map[uuid.UUID]string{}
	for _, m := range members {
		roles[m.UserID] = m.RoleCode
	}
	if len(roles) != 2 || roles[f.ana] != "manager" || roles[f.ben] != "viewer" || res.MembersAdded != 2 {
		t.Fatalf("team %v (owner %s left out)", roles, f.lead)
	}
	att := f.client.Attachment.Query().Where(entattachment.ProjectID(p.ID)).OnlyX(ctx)
	if att.FileURL != doc.FileURL || att.Metadata["linked_from"] != "tender_document" || res.DocumentsLinked != 1 {
		t.Fatalf("linked document %+v", att)
	}
	if res.Template == nil || res.Template.Tasks == 0 ||
		f.client.Task.Query().Where(enttask.ProjectID(p.ID)).CountX(ctx) != res.Template.Tasks {
		t.Fatalf("template %+v", res.Template)
	}
	// The budget push failed once (treasury refused); the reason is kept and the retry creates it.
	if res.Budget["status"] != BudgetFailed || res.Budget["error"] != "line 1 has an unknown category" {
		t.Fatalf("budget after the first push %v", res.Budget)
	}
	if body := ft.bodies[0]; body["project_id"] != p.ID.String() || body["budget_type"] != "project" || body["end_date"] != "2027-05-31" {
		t.Fatalf("treasury body %v", body)
	}
	state, err := f.svc.PushConversionBudget(f.asLead, f.tenant, f.tender.ID)
	if err != nil || state["status"] != BudgetCreated || state["budget_id"] != ft.id.String() {
		t.Fatalf("retry: %v %v", state, err)
	}
	if _, err := f.svc.PushConversionBudget(f.asLead, f.tenant, f.tender.ID); stateCode(err) != "budget_push_done" {
		t.Fatalf("second retry: %v", err)
	}

	// Idempotent: the same project, nothing new, one converted event.
	again, err := f.svc.Convert(f.asLead, f.tenant, f.tender.ID, ConvertInput{Name: "Other"}, access)
	if err != nil || !again.AlreadyConverted || again.Project.ID != p.ID {
		t.Fatalf("second conversion: %v %+v", err, again)
	}
	if n := f.client.Project.Query().Where(entproject.TenantID(f.tenant)).CountX(ctx); n != 1 {
		t.Fatalf("projects %d", n)
	}
	if evs := f.events(t, EventConverted); len(evs) != 1 || evs[0]["project_id"] != p.ID.String() {
		t.Fatalf("converted events %v", evs)
	}
	if ft.calls != 2 {
		t.Fatalf("treasury calls %d", ft.calls)
	}
	// Membership scoping: someone who cannot see the project learns only that it exists.
	hidden := ConvertAccess{CanSeeProject: func(context.Context, uuid.UUID) (bool, error) { return false, nil }}
	if _, err := f.svc.Convert(f.asBen, f.tenant, f.tender.ID, ConvertInput{}, hidden); stateCode(err) != "tender_already_converted" {
		t.Fatalf("already converted, project hidden: %v", err)
	}
	if err := f.svc.DeleteTender(f.asLead, f.tenant, f.tender.ID); stateCode(err) != "tender_converted" {
		t.Fatalf("deleting a converted tender: %v", err)
	}
	// The project goes: the tender can become a project again (without the budget this time).
	f.client.Attachment.Delete().Where(entattachment.ProjectID(p.ID)).ExecX(ctx)
	f.client.ProjectMember.Delete().Where(entmember.ProjectID(p.ID)).ExecX(ctx)
	f.client.TaskDependency.Delete().ExecX(ctx)
	f.client.Task.Delete().Where(enttask.ProjectID(p.ID)).ExecX(ctx)
	f.client.Milestone.Delete().ExecX(ctx)
	f.client.Activity.Update().ClearProjectID().ExecX(ctx)
	f.client.Project.DeleteOneID(p.ID).ExecX(ctx)
	third, err := f.svc.Convert(f.asLead, f.tenant, f.tender.ID, ConvertInput{SkipBudget: true, SkipTeam: true}, access)
	if err != nil || third.AlreadyConverted || third.Project.ID == p.ID || third.Budget["status"] != BudgetSkipped || third.MembersAdded != 0 {
		t.Fatalf("reconversion: %v %+v", err, third)
	}
}

// Without budget rights the conversion still runs and says why no budget was created.
func TestConvertWithoutBudgetRights(t *testing.T) {
	f := newTFx(t)
	if _, err := f.svc.UpdateTender(f.asLead, f.tenant, f.tender.ID, UpdateTenderInput{Metadata: map[string]any{
		MetaBidBudget: map[string]any{"lines": []any{map[string]any{"name": "Survey", "planned_amount": "1"}}},
	}}); err != nil {
		t.Fatal(err)
	}
	f.decideGo(t)
	f.submit(t)
	f.win(t, OutcomeInput{})
	res, err := f.svc.Convert(f.asLead, f.tenant, f.tender.ID, ConvertInput{BillingType: "time_and_materials"}, ConvertAccess{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Budget["status"] != BudgetNotAllowed || res.Project.Metadata["contract_value"] != 2_500_000.0 {
		t.Fatalf("result %+v", res)
	}
	if _, err := f.svc.PushConversionBudget(f.asLead, f.tenant, f.tender.ID); stateCode(err) != "budget_push_not_needed" {
		t.Fatalf("push when not allowed: %v", err)
	}
	if _, err := f.svc.Convert(f.asLead, f.tenant, f.tender.ID, ConvertInput{BillingType: "barter"}, ConvertAccess{}); err != nil {
		// Already converted: the billing type of a second call is never read.
		t.Fatalf("second call: %v", err)
	}
}

// A bad billing type is a validation error and nothing is created.
func TestConvertRefusesBadTerms(t *testing.T) {
	f := newTFx(t)
	f.decideGo(t)
	f.submit(t)
	f.win(t, OutcomeInput{})
	var verr ValidationError
	if _, err := f.svc.Convert(f.asLead, f.tenant, f.tender.ID, ConvertInput{BillingType: "barter"}, ConvertAccess{}); !errors.As(err, &verr) {
		t.Fatalf("bad billing type: %v", err)
	}
	if n := f.client.Project.Query().CountX(context.Background()); n != 0 {
		t.Fatalf("projects after a refused conversion: %d", n)
	}
	start, end := day("2027-01-10"), day("2027-01-01")
	if _, err := f.svc.Convert(f.asLead, f.tenant, f.tender.ID, ConvertInput{StartDate: start, EndDate: end}, ConvertAccess{}); !errors.As(err, &verr) {
		t.Fatalf("end before start: %v", err)
	}
}

// Search covers the title, client and number; sorts keep missing deadlines last.
func TestListSearchAndSort(t *testing.T) {
	f := newTFx(t)
	soon, later := day("2026-12-01"), day("2027-02-01")
	if _, err := f.svc.CreateTender(f.asLead, f.tenant, CreateTenderInput{Title: "Water pans", ClientName: "Kitui", Deadline: later, CreatedBy: f.lead}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.CreateTender(f.asLead, f.tenant, CreateTenderInput{Title: "Clinic fit-out", ClientName: "Kisumu County", Deadline: soon, CreatedBy: f.lead}); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	items, total, err := f.svc.ListTenders(ctx, f.tenant, ListTendersFilter{Q: "kisumu"})
	if err != nil || total != 2 || len(items) != 2 {
		t.Fatalf("search kisumu: %d %v", total, err)
	}
	if _, total, _ := f.svc.ListTenders(ctx, f.tenant, ListTendersFilter{Q: f.tender.Number}); total != 1 {
		t.Fatalf("search by number: %d", total)
	}
	items, _, _ = f.svc.ListTenders(ctx, f.tenant, ListTendersFilter{Sort: "deadline"})
	if len(items) != 3 || items[0].Title != "Clinic fit-out" || items[1].Title != "Water pans" || items[2].ID != f.tender.ID {
		t.Fatalf("deadline order %s, %s, %s", items[0].Title, items[1].Title, items[2].Title)
	}
	items, _, _ = f.svc.ListTenders(ctx, f.tenant, ListTendersFilter{Sort: "-deadline"})
	if items[0].Title != "Water pans" || items[2].ID != f.tender.ID {
		t.Fatalf("descending deadline keeps the undated last: %s ... %s", items[0].Title, items[2].Title)
	}
	if _, err := f.svc.CreateTender(f.asLead, f.tenant, CreateTenderInput{Title: "x", ClientName: "y", Status: StatusAwarded}); err == nil {
		t.Fatal("a tender created as awarded")
	}
}

// A document a submission records stays.
func TestDocumentInUse(t *testing.T) {
	f := newTFx(t)
	doc, err := f.svc.AddDocument(f.asLead, f.tenant, f.tender.ID, AddDocumentInput{FileURL: "https://files.example.test/bid.pdf", FileName: "Bid.pdf", UploadedBy: f.lead})
	if err != nil {
		t.Fatal(err)
	}
	f.decideGo(t)
	if _, err := f.svc.Submit(f.asLead, f.tenant, f.tender.ID, SubmissionInput{Method: "online", DocumentID: &doc.ID}); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.DeleteDocument(f.asLead, f.tenant, f.tender.ID, doc.ID); stateCode(err) != "document_in_use" {
		t.Fatalf("deleting a submitted document: %v", err)
	}
	other := uuid.New()
	if _, err := f.svc.Submit(f.asLead, f.tenant, f.tender.ID, SubmissionInput{Method: "online", ProofDocumentID: &other}); err == nil {
		t.Fatal("a proof that is not one of the tender's documents accepted")
	}
}
