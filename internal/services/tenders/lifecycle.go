package tenders

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/bengobox/projects-service/internal/ent"
	"github.com/bengobox/projects-service/internal/services/activity"
	"github.com/bengobox/projects-service/internal/services/tasks"
	"github.com/bengobox/projects-service/internal/services/txn"
)

// StatusInput is a manual status change (US-1.12) with an optional note.
type StatusInput struct {
	Status string `json:"status"`
	Note   string `json:"note"`
}

// ChangeStatus makes one of the manual moves (CanMove) and records it with its note. The decision,
// submit and outcome routes make the other moves.
func (s *Service) ChangeStatus(ctx context.Context, tenantID, tenderID uuid.UUID, in StatusInput) (*ent.Tender, error) {
	note := strings.TrimSpace(in.Note)
	if len([]rune(note)) > 2000 {
		return nil, ErrValidation("the note is at most 2000 characters")
	}
	var out *ent.Tender
	err := txn.Run(ctx, s.client, func(tx *ent.Tx) error {
		t, err := lockTender(ctx, tx, tenantID, tenderID)
		if err != nil {
			return err
		}
		if !CanMove(t.Status, in.Status) {
			return stateErr("tender_status_move", fmt.Sprintf("a %s tender cannot be moved to %s by hand", t.Status, in.Status))
		}
		prev := t.Status
		if out, err = t.Update().SetStatus(in.Status).Save(ctx); err != nil {
			return fmt.Errorf("change status: %w", err)
		}
		changes := map[string]any{"status": map[string]any{"from": prev, "to": in.Status}}
		if note != "" {
			changes["note"] = note
		}
		if err := record(ctx, tx, tenantID, tenderID, activity.ActionUpdated, changes); err != nil {
			return err
		}
		return emit(ctx, tx, out, EventStatusChanged, map[string]any{"previous_status": prev, "note": note})
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// OutcomeInput records the award or the loss (US-1.13).
type OutcomeInput struct {
	Outcome           string      `json:"outcome"` // won or lost
	AwardDate         *tasks.Date `json:"award_date"`
	AwardValue        *float64    `json:"award_value"`
	ContractReference string      `json:"contract_reference"`
	ContractStart     *tasks.Date `json:"contract_start"`
	ContractEnd       *tasks.Date `json:"contract_end"`
	LossReason        string      `json:"loss_reason"`
	Competitor        string      `json:"competitor"`
	CompetitorPrice   *float64    `json:"competitor_price"`
	Lessons           string      `json:"lessons"`
}

// Outcomes.
const (
	OutcomeWon  = "won"
	OutcomeLost = "lost"
)

func checkText(name, v string, max int) error {
	if len([]rune(v)) > max {
		return ErrValidation(fmt.Sprintf("%s is at most %d characters", name, max))
	}
	return nil
}

// RecordOutcome records the award (status awarded, event tender.won) or the loss (status lost,
// event tender.lost) of a submitted tender in metadata.outcome. A loss needs its reason. The
// award value defaults to the estimated value.
func (s *Service) RecordOutcome(ctx context.Context, tenantID, tenderID uuid.UUID, in OutcomeInput) (*ent.Tender, error) {
	actor := activity.ActorFrom(ctx)
	if in.Outcome != OutcomeWon && in.Outcome != OutcomeLost {
		return nil, ErrValidation("outcome must be won or lost")
	}
	in.LossReason, in.Competitor = strings.TrimSpace(in.LossReason), strings.TrimSpace(in.Competitor)
	in.ContractReference, in.Lessons = strings.TrimSpace(in.ContractReference), strings.TrimSpace(in.Lessons)
	if in.Outcome == OutcomeLost && in.LossReason == "" {
		return nil, ErrValidation("say why the tender was lost")
	}
	for name, v := range map[string]string{"loss_reason": in.LossReason, "competitor": in.Competitor, "contract_reference": in.ContractReference} {
		if err := checkText(name, v, 500); err != nil {
			return nil, err
		}
	}
	if err := checkText("lessons", in.Lessons, 4000); err != nil {
		return nil, err
	}
	for name, v := range map[string]*float64{"award_value": in.AwardValue, "competitor_price": in.CompetitorPrice} {
		if v != nil && *v < 0 {
			return nil, ErrValidation(name + " must be zero or more")
		}
	}
	if in.ContractStart != nil && in.ContractEnd != nil && !in.ContractStart.IsZero() && !in.ContractEnd.IsZero() &&
		in.ContractEnd.Before(in.ContractStart.Time) {
		return nil, ErrValidation("contract_end must be on or after contract_start")
	}
	var out *ent.Tender
	err := txn.Run(ctx, s.client, func(tx *ent.Tx) error {
		t, err := lockTender(ctx, tx, tenantID, tenderID)
		if err != nil {
			return err
		}
		if !CanRecordOutcome(t.Status) {
			return stateErr("tender_not_submitted", "the outcome is recorded once the bid is submitted (status "+t.Status+")")
		}
		now := time.Now().UTC()
		rec := map[string]any{"outcome": in.Outcome, "recorded_at": now.Format(time.RFC3339)}
		if actor != uuid.Nil {
			rec["recorded_by"] = actor.String()
		}
		if in.Lessons != "" {
			rec["lessons"] = in.Lessons
		}
		status, event := StatusLost, EventLost
		extra := map[string]any{}
		if in.Outcome == OutcomeWon {
			status, event = StatusAwarded, EventWon
			value := t.EstimatedValue
			if in.AwardValue != nil {
				value = *in.AwardValue
			}
			rec["award_value"] = value
			awardDate := now
			if in.AwardDate != nil && !in.AwardDate.IsZero() {
				awardDate = in.AwardDate.Time
			}
			rec["award_date"] = awardDate.Format(time.DateOnly)
			if in.ContractReference != "" {
				rec["contract_reference"] = in.ContractReference
			}
			if in.ContractStart != nil && !in.ContractStart.IsZero() {
				rec["contract_start"] = in.ContractStart.Format(time.DateOnly)
			}
			if in.ContractEnd != nil && !in.ContractEnd.IsZero() {
				rec["contract_end"] = in.ContractEnd.Format(time.DateOnly)
			}
			extra["award_value"], extra["award_date"] = value, rec["award_date"]
		} else {
			rec["loss_reason"] = in.LossReason
			if in.Competitor != "" {
				rec["competitor"] = in.Competitor
			}
			if in.CompetitorPrice != nil {
				rec["competitor_price"] = *in.CompetitorPrice
			}
			extra["loss_reason"] = in.LossReason
		}
		meta := copyMeta(t.Metadata)
		meta[MetaOutcome] = rec
		prev := t.Status
		if out, err = t.Update().SetStatus(status).SetMetadata(meta).Save(ctx); err != nil {
			return fmt.Errorf("record outcome: %w", err)
		}
		if err := record(ctx, tx, tenantID, tenderID, activity.ActionUpdated, map[string]any{
			"outcome": in.Outcome, "status": map[string]any{"from": prev, "to": status},
		}); err != nil {
			return err
		}
		extra["previous_status"] = prev
		return emit(ctx, tx, out, event, extra)
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
