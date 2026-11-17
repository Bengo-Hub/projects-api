package tenders

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	entsql "entgo.io/ent/dialect/sql"
	"github.com/google/uuid"

	"github.com/bengobox/projects-service/internal/ent"
	enttender "github.com/bengobox/projects-service/internal/ent/tender"
	enttendereval "github.com/bengobox/projects-service/internal/ent/tenderevaluation"
	"github.com/bengobox/projects-service/internal/services/activity"
	"github.com/bengobox/projects-service/internal/services/people"
	"github.com/bengobox/projects-service/internal/services/txn"
)

// Go/no-go (sprint 1, US-1.6). The decision maker scores the tender on weighted criteria (each 0
// to MaxCriterionScore); the weighted score out of 100 gives a recommendation against a threshold:
// go at or above it, review within reviewBand below it, no_go under that. The decision itself is
// go or no_go and needs a comment whenever it differs from the recommendation (review always
// does). It is stored on the tender (metadata.go_decision, the earlier ones in
// go_decision_history) and moves the tender to preparing or no_go.
const (
	MaxCriterionScore   = 5
	DefaultGoThreshold  = 65.0
	reviewBand          = 15.0
	maxCriteria         = 20
	maxDecisionHistory  = 10
	maxDecisionComment  = 2000
	RecommendGo         = "go"
	RecommendReview     = "review"
	RecommendNoGo       = "no_go"
	DecisionGo          = "go"
	DecisionNoGo        = "no_go"
	defaultCriteriaNote = "Scores run from 0 (poor) to 5 (strong); for risk, 5 means low risk."
)

// Criterion is one scored line of the decision.
type Criterion struct {
	Key    string  `json:"key"`
	Label  string  `json:"label"`
	Weight float64 `json:"weight"`
	Score  float64 `json:"score"`
	Note   string  `json:"note,omitempty"`
}

// DefaultCriteria are offered when a decision maker brings none of their own (scores unset).
var DefaultCriteria = []Criterion{
	{Key: "strategic_fit", Label: "Strategic fit", Weight: 20},
	{Key: "technical_capability", Label: "Technical capability", Weight: 20},
	{Key: "capacity", Label: "Team capacity and timing", Weight: 15},
	{Key: "financial_value", Label: "Value and margin", Weight: 20},
	{Key: "win_probability", Label: "Chance of winning", Weight: 15},
	{Key: "risk", Label: "Risk (5 is low risk)", Weight: 10},
}

// Scored is a scored set of criteria with its recommendation.
type Scored struct {
	Criteria       []Criterion `json:"criteria"`
	WeightedScore  float64     `json:"weighted_score"`
	Threshold      float64     `json:"threshold"`
	Recommendation string      `json:"recommendation"`
}

// Score validates criteria and computes the weighted score out of 100 (one decimal) and the
// recommendation against threshold (DefaultGoThreshold when 0).
func Score(criteria []Criterion, threshold float64) (Scored, error) {
	if threshold == 0 {
		threshold = DefaultGoThreshold
	}
	if threshold < 1 || threshold > 100 {
		return Scored{}, ErrValidation("threshold must be between 1 and 100")
	}
	if len(criteria) == 0 || len(criteria) > maxCriteria {
		return Scored{}, ErrValidation(fmt.Sprintf("score between 1 and %d criteria", maxCriteria))
	}
	seen := map[string]bool{}
	out := make([]Criterion, len(criteria))
	var weights, total float64
	for i, c := range criteria {
		c.Key = strings.TrimSpace(c.Key)
		c.Label = strings.TrimSpace(c.Label)
		if c.Key == "" || len(c.Key) > 50 {
			return Scored{}, ErrValidation(fmt.Sprintf("criterion %d needs a key (at most 50 characters)", i+1))
		}
		if seen[c.Key] {
			return Scored{}, ErrValidation(fmt.Sprintf("criterion %q appears twice", c.Key))
		}
		seen[c.Key] = true
		if c.Label == "" {
			c.Label = c.Key
		}
		if len([]rune(c.Label)) > 100 || len([]rune(c.Note)) > 500 {
			return Scored{}, ErrValidation(fmt.Sprintf("criterion %q: the label is at most 100 characters and the note 500", c.Key))
		}
		if math.IsNaN(c.Weight) || c.Weight <= 0 || c.Weight > 100 {
			return Scored{}, ErrValidation(fmt.Sprintf("criterion %q needs a weight above 0 and at most 100", c.Key))
		}
		if math.IsNaN(c.Score) || c.Score < 0 || c.Score > MaxCriterionScore {
			return Scored{}, ErrValidation(fmt.Sprintf("criterion %q needs a score from 0 to %d", c.Key, MaxCriterionScore))
		}
		weights += c.Weight
		total += c.Weight * c.Score
		out[i] = c
	}
	score := math.Round(total/(weights*MaxCriterionScore)*1000) / 10
	rec := RecommendNoGo
	switch {
	case score >= threshold:
		rec = RecommendGo
	case score >= threshold-reviewBand:
		rec = RecommendReview
	}
	return Scored{Criteria: out, WeightedScore: score, Threshold: threshold, Recommendation: rec}, nil
}

// DecisionInput is a go/no-go decision.
type DecisionInput struct {
	Criteria  []Criterion `json:"criteria"`
	Threshold float64     `json:"threshold"`
	Decision  string      `json:"decision"`
	Comment   string      `json:"comment"`
}

// CheckDecision validates the decision against the scored recommendation: go or no_go, and a
// comment whenever the decision differs from the recommendation.
func CheckDecision(s Scored, decision, comment string) error {
	if decision != DecisionGo && decision != DecisionNoGo {
		return ErrValidation("decision must be go or no_go")
	}
	if len([]rune(comment)) > maxDecisionComment {
		return ErrValidation("the comment is at most 2000 characters")
	}
	if strings.TrimSpace(comment) == "" && decision != s.Recommendation {
		return ErrValidation("say why: the decision differs from the recommendation")
	}
	return nil
}

// lockTender locks one tender row for the rest of tx and loads it.
func lockTender(ctx context.Context, tx *ent.Tx, tenantID, id uuid.UUID) (*ent.Tender, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id FROM tenders WHERE id = $1 AND tenant_id = $2 FOR UPDATE`, id, tenantID)
	if err != nil {
		return nil, fmt.Errorf("lock tender: %w", err)
	}
	found := rows.Next()
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if !found {
		return nil, ErrNotFound
	}
	return tx.Tender.Query().Where(enttender.ID(id)).Only(ctx)
}

// Decide records the go/no-go decision as the signed-in user and moves the tender to preparing
// (go) or no_go. A decision can be made again until the bid is submitted; the previous one moves
// to go_decision_history.
func (s *Service) Decide(ctx context.Context, tenantID, tenderID uuid.UUID, in DecisionInput) (*ent.Tender, error) {
	decider := activity.ActorFrom(ctx)
	if decider == uuid.Nil {
		return nil, ErrValidation("a decision needs a signed-in decision maker")
	}
	scored, err := Score(in.Criteria, in.Threshold)
	if err != nil {
		return nil, err
	}
	comment := strings.TrimSpace(in.Comment)
	if err := CheckDecision(scored, in.Decision, comment); err != nil {
		return nil, err
	}
	summary, err := s.EvaluationSummary(ctx, tenantID, tenderID)
	if err != nil {
		return nil, err
	}
	var out *ent.Tender
	err = txn.Run(ctx, s.client, func(tx *ent.Tx) error {
		t, err := lockTender(ctx, tx, tenantID, tenderID)
		if err != nil {
			return err
		}
		if !CanDecide(t.Status) {
			return stateErr("tender_decision_closed", "the go/no-go decision is closed once the bid is submitted (status "+t.Status+")")
		}
		meta := copyMeta(t.Metadata)
		if prev, ok := meta[MetaGoDecision]; ok {
			hist, _ := meta[MetaGoHistory].([]any)
			hist = append(hist, prev)
			if len(hist) > maxDecisionHistory {
				hist = hist[len(hist)-maxDecisionHistory:]
			}
			meta[MetaGoHistory] = hist
		}
		criteria := make([]any, len(scored.Criteria))
		for i, c := range scored.Criteria {
			m := map[string]any{"key": c.Key, "label": c.Label, "weight": c.Weight, "score": c.Score}
			if c.Note != "" {
				m["note"] = c.Note
			}
			criteria[i] = m
		}
		now := time.Now().UTC()
		meta[MetaGoDecision] = map[string]any{
			"criteria": criteria, "weighted_score": scored.WeightedScore, "threshold": scored.Threshold,
			"recommendation": scored.Recommendation, "decision": in.Decision, "comment": comment,
			"decided_by": decider.String(), "decided_at": now.Format(time.RFC3339),
			"evaluations": map[string]any{"count": summary.Count, "average_score": summary.AverageScore},
		}
		status := StatusPreparing
		if in.Decision == DecisionNoGo {
			status = StatusNoGo
		}
		prevStatus := t.Status
		if out, err = t.Update().SetStatus(status).SetMetadata(meta).Save(ctx); err != nil {
			return fmt.Errorf("save decision: %w", err)
		}
		if err := record(ctx, tx, tenantID, tenderID, activity.ActionUpdated, map[string]any{
			"go_decision": in.Decision, "recommendation": scored.Recommendation, "weighted_score": scored.WeightedScore,
			"status": map[string]any{"from": prevStatus, "to": status},
		}); err != nil {
			return err
		}
		return emit(ctx, tx, out, EventGoDecision, map[string]any{
			"decision": in.Decision, "recommendation": scored.Recommendation, "weighted_score": scored.WeightedScore,
			"threshold": scored.Threshold, "decided_by": decider.String(), "previous_status": prevStatus,
		})
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// EvaluationSummary is the committee's evaluations of one tender in one aggregate.
type EvaluationSummary struct {
	Count        int     `json:"count"`
	AverageScore float64 `json:"average_score"`
	MinScore     float64 `json:"min_score"`
	MaxScore     float64 `json:"max_score"`
	// Evaluators are the people who scored it with their latest score, named from projects_users
	// in one batched lookup.
	Evaluators []EvaluatorScore `json:"evaluators"`
	// DefaultCriteria and CriteriaNote help the decision form start from the house criteria.
	DefaultCriteria []Criterion `json:"default_criteria"`
	CriteriaNote    string      `json:"criteria_note"`
	Threshold       float64     `json:"default_threshold"`
}

// EvaluatorScore is one evaluator's latest score.
type EvaluatorScore struct {
	EvaluatorID uuid.UUID `json:"evaluator_id"`
	Name        string    `json:"name"`
	Score       float64   `json:"score"`
	EvaluatedAt time.Time `json:"evaluated_at"`
}

// EvaluationSummary aggregates a tender's evaluations in SQL (count, average, min, max) and lists
// each evaluator's latest score (the evaluations of one tender are bounded by subListLimit).
func (s *Service) EvaluationSummary(ctx context.Context, tenantID, tenderID uuid.UUID) (EvaluationSummary, error) {
	ok, err := s.client.Tender.Query().Where(enttender.ID(tenderID), enttender.TenantID(tenantID)).Exist(ctx)
	if err != nil {
		return EvaluationSummary{}, fmt.Errorf("fetch tender: %w", err)
	}
	if !ok {
		return EvaluationSummary{}, ErrNotFound
	}
	out := EvaluationSummary{DefaultCriteria: DefaultCriteria, CriteriaNote: defaultCriteriaNote, Threshold: DefaultGoThreshold, Evaluators: []EvaluatorScore{}}
	var agg []struct {
		Count int     `json:"count"`
		Avg   float64 `json:"avg"`
		Min   float64 `json:"min"`
		Max   float64 `json:"max"`
	}
	err = s.client.TenderEvaluation.Query().
		Where(enttendereval.TenderID(tenderID), enttendereval.TenantID(tenantID)).
		Aggregate(
			ent.Count(),
			func(sel *entsql.Selector) string { return entsql.As("COALESCE(AVG("+sel.C(enttendereval.FieldScore)+"), 0)", "avg") },
			func(sel *entsql.Selector) string { return entsql.As("COALESCE(MIN("+sel.C(enttendereval.FieldScore)+"), 0)", "min") },
			func(sel *entsql.Selector) string { return entsql.As("COALESCE(MAX("+sel.C(enttendereval.FieldScore)+"), 0)", "max") },
		).Scan(ctx, &agg)
	if err != nil {
		return EvaluationSummary{}, fmt.Errorf("evaluation summary: %w", err)
	}
	if len(agg) == 1 {
		out.Count, out.MinScore, out.MaxScore = agg[0].Count, agg[0].Min, agg[0].Max
		out.AverageScore = math.Round(agg[0].Avg*10) / 10
	}
	if out.Count == 0 {
		return out, nil
	}
	rows, err := s.client.TenderEvaluation.Query().
		Where(enttendereval.TenderID(tenderID), enttendereval.TenantID(tenantID)).
		Order(ent.Desc(enttendereval.FieldEvaluatedAt)).Limit(subListLimit).All(ctx)
	if err != nil {
		return EvaluationSummary{}, fmt.Errorf("list evaluations: %w", err)
	}
	seen := map[uuid.UUID]bool{}
	var ids []uuid.UUID
	for _, e := range rows {
		if seen[e.EvaluatorID] {
			continue
		}
		seen[e.EvaluatorID] = true
		ids = append(ids, e.EvaluatorID)
		out.Evaluators = append(out.Evaluators, EvaluatorScore{EvaluatorID: e.EvaluatorID, Score: e.Score, EvaluatedAt: e.EvaluatedAt})
	}
	names, err := people.Lookup(ctx, s.client.ProjectsUser, tenantID, ids)
	if err != nil {
		return EvaluationSummary{}, err
	}
	for i := range out.Evaluators {
		out.Evaluators[i].Name = names[out.Evaluators[i].EvaluatorID].Label()
	}
	return out, nil
}
