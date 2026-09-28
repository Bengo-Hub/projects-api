package tenders

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/bengobox/projects-service/internal/ent"
	enttender "github.com/bengobox/projects-service/internal/ent/tender"
	enttendereval "github.com/bengobox/projects-service/internal/ent/tenderevaluation"
)

// Tender statuses. Open ones make up the pipeline; closed ones are final.
const (
	StatusDraft       = "draft"
	StatusEvaluating  = "evaluating"
	StatusPreparing   = "preparing" // go decision made, document being prepared
	StatusSubmitted   = "submitted"
	StatusUnderReview = "under_review"
	StatusShortlisted = "shortlisted"
	StatusInterview   = "interview"
	StatusAwarded     = "awarded"
	StatusLost        = "lost"
	StatusNoGo        = "no_go"
	StatusCancelled   = "cancelled"
)

// tenderStatuses are always reported (zero when a tenant has none) so the UI cards never read a
// missing key.
var tenderStatuses = []string{
	StatusDraft, StatusEvaluating, StatusPreparing, StatusSubmitted, StatusUnderReview,
	StatusShortlisted, StatusInterview, StatusAwarded, StatusLost, StatusNoGo, StatusCancelled,
}

// openStatuses are the stages whose estimated value counts as pipeline.
var openStatuses = []string{
	StatusDraft, StatusEvaluating, StatusPreparing, StatusSubmitted, StatusUnderReview,
	StatusShortlisted, StatusInterview,
}

// transitions lists the statuses each status may move to. Closed statuses have none.
var transitions = map[string][]string{
	StatusDraft:       {StatusEvaluating, StatusPreparing, StatusNoGo, StatusCancelled},
	StatusEvaluating:  {StatusDraft, StatusPreparing, StatusNoGo, StatusCancelled},
	StatusPreparing:   {StatusEvaluating, StatusSubmitted, StatusCancelled},
	StatusSubmitted:   {StatusUnderReview, StatusShortlisted, StatusInterview, StatusAwarded, StatusLost, StatusCancelled},
	StatusUnderReview: {StatusShortlisted, StatusInterview, StatusAwarded, StatusLost, StatusCancelled},
	StatusShortlisted: {StatusInterview, StatusAwarded, StatusLost, StatusCancelled},
	StatusInterview:   {StatusAwarded, StatusLost, StatusCancelled},
}

// CanTransition reports whether a tender may move from one status to another.
func CanTransition(from, to string) bool {
	for _, s := range transitions[from] {
		if s == to {
			return true
		}
	}
	return false
}

func knownStatus(s string) bool {
	for _, st := range tenderStatuses {
		if st == s {
			return true
		}
	}
	return false
}

// historyEntry is one status_history row.
func historyEntry(from, to, notes string, by uuid.UUID, at time.Time) map[string]any {
	e := map[string]any{"from": from, "to": to, "at": at.UTC().Format(time.RFC3339)}
	if by != uuid.Nil {
		e["by"] = by.String()
	}
	if notes != "" {
		e["notes"] = notes
	}
	return e
}

// tenderOf loads a tender of the tenant or returns ErrNotFound.
func (s *Service) tenderOf(ctx context.Context, tenantID, id uuid.UUID) (*ent.Tender, error) {
	t, err := s.client.Tender.Query().Where(enttender.ID(id), enttender.TenantID(tenantID)).Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("fetch tender: %w", err)
	}
	return t, nil
}

// nextHistory validates a transition and returns the status history with it appended.
func nextHistory(t *ent.Tender, to, notes string, by uuid.UUID, now time.Time) ([]map[string]any, error) {
	if !knownStatus(to) {
		return nil, ErrValidation(fmt.Sprintf("unknown status %q", to))
	}
	if !CanTransition(t.Status, to) {
		return nil, ErrTransition{From: t.Status, To: to}
	}
	return append(append([]map[string]any{}, t.StatusHistory...), historyEntry(t.Status, to, notes, by, now)), nil
}

// moveTo validates a transition and returns an update that sets the status and appends to the
// history. Callers add their own fields and save.
func moveTo(t *ent.Tender, to, notes string, by uuid.UUID, now time.Time) (*ent.TenderUpdateOne, error) {
	history, err := nextHistory(t, to, notes, by, now)
	if err != nil {
		return nil, err
	}
	u := t.Update().SetStatus(to).SetStatusHistory(history)
	if to == StatusSubmitted {
		u = u.SetSubmittedAt(now)
	}
	return u, nil
}

// --- Evaluation summary (US-1.6) ---

// CriteriaScore is the average score for one evaluation criterion.
type CriteriaScore struct {
	Criteria string  `json:"criteria"`
	Count    int     `json:"count"`
	Average  float64 `json:"average"`
}

// EvaluationSummary aggregates a tender's committee evaluations for the go/no-go decision.
type EvaluationSummary struct {
	TenderID   uuid.UUID       `json:"tender_id"`
	Count      int             `json:"count"`
	Evaluators int             `json:"evaluators"`
	Average    float64         `json:"average"`
	Min        float64         `json:"min"`
	Max        float64         `json:"max"`
	ByCriteria []CriteriaScore `json:"by_criteria"`
	Decision   string          `json:"decision,omitempty"`
	DecidedAt  *time.Time      `json:"decided_at,omitempty"`
}

// summarize builds the score statistics from evaluations. Evaluations without criteria group
// under "overall".
func summarize(evals []*ent.TenderEvaluation) EvaluationSummary {
	var sum EvaluationSummary
	if len(evals) == 0 {
		sum.ByCriteria = []CriteriaScore{}
		return sum
	}
	evaluators := map[uuid.UUID]bool{}
	type acc struct {
		n   int
		sum float64
	}
	crit := map[string]*acc{}
	total := 0.0
	sum.Min, sum.Max = evals[0].Score, evals[0].Score
	for _, e := range evals {
		total += e.Score
		if e.Score < sum.Min {
			sum.Min = e.Score
		}
		if e.Score > sum.Max {
			sum.Max = e.Score
		}
		evaluators[e.EvaluatorID] = true
		k := strings.TrimSpace(e.Criteria)
		if k == "" {
			k = "overall"
		}
		if crit[k] == nil {
			crit[k] = &acc{}
		}
		crit[k].n++
		crit[k].sum += e.Score
	}
	sum.Count = len(evals)
	sum.Evaluators = len(evaluators)
	sum.Average = total / float64(len(evals))
	for k, a := range crit {
		sum.ByCriteria = append(sum.ByCriteria, CriteriaScore{Criteria: k, Count: a.n, Average: a.sum / float64(a.n)})
	}
	sort.Slice(sum.ByCriteria, func(i, j int) bool { return sum.ByCriteria[i].Criteria < sum.ByCriteria[j].Criteria })
	return sum
}

// GetEvaluationSummary returns score statistics over the tender's evaluations and any decision.
func (s *Service) GetEvaluationSummary(ctx context.Context, tenantID, tenderID uuid.UUID) (*EvaluationSummary, error) {
	t, err := s.tenderOf(ctx, tenantID, tenderID)
	if err != nil {
		return nil, err
	}
	evals, err := s.client.TenderEvaluation.Query().
		Where(enttendereval.TenderID(tenderID), enttendereval.TenantID(tenantID)).
		Limit(subListLimit).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list evaluations: %w", err)
	}
	sum := summarize(evals)
	sum.TenderID = tenderID
	sum.Decision = t.Decision
	sum.DecidedAt = t.DecidedAt
	return &sum, nil
}

// --- Go/no-go decision (US-1.6) ---

// DecisionInput is a go/no-go decision with its rationale.
type DecisionInput struct {
	Decision  string    `json:"decision"` // go or no_go
	Rationale string    `json:"rationale"`
	DecidedBy uuid.UUID `json:"-"`
}

// MakeDecision records the go/no-go decision on a draft or evaluating tender. Go moves it to
// preparing; no-go closes it as no_go.
func (s *Service) MakeDecision(ctx context.Context, tenantID, tenderID uuid.UUID, input DecisionInput) (*ent.Tender, error) {
	var to string
	switch input.Decision {
	case "go":
		to = StatusPreparing
	case "no_go", "no-go", "nogo":
		input.Decision, to = "no_go", StatusNoGo
	default:
		return nil, ErrValidation(`decision must be "go" or "no_go"`)
	}
	if strings.TrimSpace(input.Rationale) == "" {
		return nil, ErrValidation("rationale is required")
	}
	t, err := s.tenderOf(ctx, tenantID, tenderID)
	if err != nil {
		return nil, err
	}
	if t.Status != StatusDraft && t.Status != StatusEvaluating {
		return nil, ErrTransition{From: t.Status, To: to}
	}
	now := time.Now()
	u, err := moveTo(t, to, input.Rationale, input.DecidedBy, now)
	if err != nil {
		return nil, err
	}
	u = u.SetDecision(input.Decision).SetDecisionRationale(input.Rationale).SetDecidedAt(now)
	if input.DecidedBy != uuid.Nil {
		u = u.SetDecidedBy(input.DecidedBy)
	}
	updated, err := u.Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("record decision: %w", err)
	}
	summary, _ := s.GetEvaluationSummary(ctx, tenantID, tenderID)
	payload := s.tenderPayload(updated)
	payload["decision"] = input.Decision
	payload["rationale"] = input.Rationale
	if summary != nil {
		payload["average_score"] = summary.Average
		payload["evaluations"] = summary.Count
	}
	s.emit(ctx, tenantID, tenderID, "tender.decision.made", payload)
	s.emitStatusChanged(ctx, t.Status, updated, input.Rationale)
	return updated, nil
}

// --- Status updates (US-1.12) ---

// StatusInput moves a tender to a new status with an optional note.
type StatusInput struct {
	Status    string    `json:"status"`
	Notes     string    `json:"notes"`
	ChangedBy uuid.UUID `json:"-"`
}

// UpdateStatus moves a tender along its lifecycle, validating the transition and logging it in
// status_history. Awarded and lost are better recorded through RecordOutcome, which also keeps
// the award or loss details; both paths publish the same events.
func (s *Service) UpdateStatus(ctx context.Context, tenantID, tenderID uuid.UUID, input StatusInput) (*ent.Tender, error) {
	t, err := s.tenderOf(ctx, tenantID, tenderID)
	if err != nil {
		return nil, err
	}
	u, err := moveTo(t, input.Status, input.Notes, input.ChangedBy, time.Now())
	if err != nil {
		return nil, err
	}
	updated, err := u.Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("update status: %w", err)
	}
	s.emitStatusChanged(ctx, t.Status, updated, input.Notes)
	return updated, nil
}

// --- Outcome (US-1.13) ---

// OutcomeInput records a final result. Award fields apply when awarded, loss fields when lost.
type OutcomeInput struct {
	Outcome           string     `json:"outcome"` // awarded or lost
	AwardDate         *time.Time `json:"award_date"`
	AwardValue        *float64   `json:"award_value"`
	ContractReference string     `json:"contract_reference"`
	ContractDetails   string     `json:"contract_details"`
	LossReason        string     `json:"loss_reason"`
	Competitor        string     `json:"competitor"`
	CompetitorPrice   *float64   `json:"competitor_price"`
	LessonsLearned    string     `json:"lessons_learned"`
	ConvertToProject  bool       `json:"convert_to_project"`
	RecordedBy        uuid.UUID  `json:"-"`
}

// outcomeMap validates the input and returns the stored outcome document.
func outcomeMap(in OutcomeInput, now time.Time) (map[string]any, error) {
	out := map[string]any{"outcome": in.Outcome, "recorded_at": now.UTC().Format(time.RFC3339)}
	if in.RecordedBy != uuid.Nil {
		out["recorded_by"] = in.RecordedBy.String()
	}
	if in.LessonsLearned != "" {
		out["lessons_learned"] = in.LessonsLearned
	}
	switch in.Outcome {
	case StatusAwarded:
		if in.AwardValue != nil {
			if *in.AwardValue < 0 {
				return nil, ErrValidation("award_value cannot be negative")
			}
			out["award_value"] = *in.AwardValue
		}
		if in.AwardDate != nil {
			out["award_date"] = in.AwardDate.UTC().Format(time.RFC3339)
		}
		if in.ContractReference != "" {
			out["contract_reference"] = in.ContractReference
		}
		if in.ContractDetails != "" {
			out["contract_details"] = in.ContractDetails
		}
		out["convert_to_project"] = in.ConvertToProject
	case StatusLost:
		if strings.TrimSpace(in.LossReason) == "" {
			return nil, ErrValidation("loss_reason is required when the tender was lost")
		}
		out["loss_reason"] = in.LossReason
		if in.Competitor != "" {
			out["competitor"] = in.Competitor
		}
		if in.CompetitorPrice != nil {
			out["competitor_price"] = *in.CompetitorPrice
		}
	default:
		return nil, ErrValidation(`outcome must be "awarded" or "lost"`)
	}
	return out, nil
}

// RecordOutcome closes a submitted tender as awarded or lost and keeps the details. Win rate in
// the metrics counts awarded out of awarded plus lost.
func (s *Service) RecordOutcome(ctx context.Context, tenantID, tenderID uuid.UUID, input OutcomeInput) (*ent.Tender, error) {
	now := time.Now()
	outcome, err := outcomeMap(input, now)
	if err != nil {
		return nil, err
	}
	t, err := s.tenderOf(ctx, tenantID, tenderID)
	if err != nil {
		return nil, err
	}
	note := input.LossReason
	if input.Outcome == StatusAwarded {
		note = input.ContractReference
	}
	u, err := moveTo(t, input.Outcome, note, input.RecordedBy, now)
	if err != nil {
		return nil, err
	}
	updated, err := u.SetOutcome(outcome).Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("record outcome: %w", err)
	}
	s.emitStatusChanged(ctx, t.Status, updated, note)
	return updated, nil
}
