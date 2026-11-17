package milestones

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/bengobox/projects-service/internal/ent"
	entmilestone "github.com/bengobox/projects-service/internal/ent/milestone"
	entproject "github.com/bengobox/projects-service/internal/ent/project"
	"github.com/bengobox/projects-service/internal/services/activity"
	"github.com/bengobox/projects-service/internal/services/notify"
	"github.com/bengobox/projects-service/internal/services/txn"
)

// Client review (sprint 14). The team marks a milestone ready_for_review; the project's clients
// then approve it or ask for changes in the portal. The review lives in the milestone status
// (milestones have no metadata column) and the decision's who, when and comment in its activity
// entry, written in the same transaction.
const (
	StatusReadyForReview   = "ready_for_review"
	StatusApproved         = "approved"
	StatusChangesRequested = "changes_requested"

	DecisionApprove        = "approve"
	DecisionRequestChanges = "request_changes"

	// MaxDecisionComment bounds a decision comment (plain text).
	MaxDecisionComment = 2000
	// decisionExcerpt is how much of the comment a notice carries.
	decisionExcerpt = 200
	// ChangeClientDecision is the activity change key marking a client decision.
	ChangeClientDecision = "client_decision"
)

// ErrNotReady is a decision on a milestone the team has not put up for review, answered 409.
var ErrNotReady = errors.New("this milestone is not waiting for your review")

// errClientStatus refuses the team setting a status only a client's decision sets.
var errClientStatus = ValidationError("approved and changes_requested are set by the client's decision in the portal")

// clientDecided reports a status only Decide may set.
func clientDecided(status string) bool {
	return status == StatusApproved || status == StatusChangesRequested
}

func projectName(ctx context.Context, tx *ent.Tx, tenantID, projectID uuid.UUID) (string, error) {
	p, err := tx.Project.Query().Where(entproject.ID(projectID), entproject.TenantID(tenantID)).
		Select(entproject.FieldName).Only(ctx)
	if err != nil {
		return "", fmt.Errorf("load project: %w", err)
	}
	return p.Name, nil
}

// notifyReadyForReview writes project.milestone.ready_for_review to each signed-in client of the
// project, linking to the milestone in the portal.
func notifyReadyForReview(ctx context.Context, tx *ent.Tx, m *ent.Milestone) error {
	clients, err := notify.ProjectClients(ctx, tx, m.TenantID, m.ProjectID)
	if err != nil || len(clients) == 0 {
		return err
	}
	name, err := projectName(ctx, tx, m.TenantID, m.ProjectID)
	if err != nil {
		return err
	}
	actor, err := notify.Actor(ctx, tx, m.TenantID, activity.ActorFrom(ctx))
	if err != nil {
		return err
	}
	extra := map[string]any{"milestone_id": m.ID.String(), "milestone_name": m.Name, "target_date": ""}
	if !m.TargetDate.IsZero() {
		extra["target_date"] = m.TargetDate.Format(time.DateOnly)
	}
	return notify.Write(ctx, tx, notify.EventMilestoneReadyForReview, notify.Notice{
		TenantID: m.TenantID, ProjectID: m.ProjectID, ProjectName: name, Actor: actor,
		ActionPath: notify.PortalPath(ctx, m.ProjectID, m.ID), Extra: extra,
	}, clients)
}

// DecisionInput is a client's verdict on a milestone up for review.
type DecisionInput struct {
	Decision string `json:"decision"`
	Comment  string `json:"comment"`
}

// Decide records a client's decision on a milestone that is ready_for_review: approve moves it to
// approved, request_changes (comment required) to changes_requested. The activity entry keeps who
// decided, when and the comment; project.milestone.approved or .changes_requested goes to the
// project's owner and its owner and manager members. The caller has checked that clientID is an
// active client of the project.
func (s *Service) Decide(ctx context.Context, tenantID, projectID, id, clientID uuid.UUID, in DecisionInput) (*ent.Milestone, error) {
	comment := strings.TrimSpace(in.Comment)
	if len([]rune(comment)) > MaxDecisionComment {
		return nil, ValidationError(fmt.Sprintf("comment is at most %d characters", MaxDecisionComment))
	}
	var status, event string
	switch in.Decision {
	case DecisionApprove:
		status, event = StatusApproved, notify.EventMilestoneApproved
	case DecisionRequestChanges:
		if comment == "" {
			return nil, ValidationError("say what needs to change")
		}
		status, event = StatusChangesRequested, notify.EventMilestoneChangesRequested
	default:
		return nil, ValidationError("decision must be approve or request_changes")
	}
	var updated *ent.Milestone
	err := txn.Run(ctx, s.client, func(tx *ent.Tx) error {
		// The row lock makes two clients deciding at once apply one decision; the second sees the
		// new status and gets ErrNotReady.
		rows, err := tx.QueryContext(ctx, `SELECT id FROM milestones WHERE id = $1 AND tenant_id = $2 AND project_id = $3 FOR UPDATE`,
			id, tenantID, projectID)
		if err != nil {
			return fmt.Errorf("lock milestone: %w", err)
		}
		found := rows.Next()
		if err := rows.Close(); err != nil {
			return fmt.Errorf("lock milestone: %w", err)
		}
		if !found {
			return ErrNotFound
		}
		m, err := tx.Milestone.Query().Where(entmilestone.ID(id)).Only(ctx)
		if err != nil {
			return fmt.Errorf("fetch milestone: %w", err)
		}
		if m.Status != StatusReadyForReview {
			return ErrNotReady
		}
		if updated, err = m.Update().SetStatus(status).Save(ctx); err != nil {
			return fmt.Errorf("decide milestone: %w", err)
		}
		decidedAt := time.Now().UTC().Format(time.RFC3339)
		if err := record(ctx, tx, updated, activity.ActionUpdated, map[string]any{
			"status":             map[string]any{"from": m.Status, "to": status},
			ChangeClientDecision: in.Decision,
			"comment":            comment,
			"decided_by":         clientID.String(),
			"decided_at":         decidedAt,
		}); err != nil {
			return err
		}
		leads, err := notify.ProjectLeads(ctx, tx, tenantID, projectID)
		if err != nil {
			return err
		}
		name, err := projectName(ctx, tx, tenantID, projectID)
		if err != nil {
			return err
		}
		actor, err := notify.Actor(ctx, tx, tenantID, clientID)
		if err != nil {
			return err
		}
		return notify.Write(ctx, tx, event, notify.Notice{
			TenantID: tenantID, ProjectID: projectID, ProjectName: name, Actor: actor,
			ActionPath: notify.MilestonePath(ctx, projectID, id),
			Extra: map[string]any{
				"milestone_id": id.String(), "milestone_name": updated.Name, "decision": in.Decision,
				"comment_excerpt": excerpt(comment), "decided_at": decidedAt,
			},
		}, leads)
	})
	if err != nil {
		return nil, err
	}
	return updated, nil
}

func excerpt(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > decisionExcerpt {
		return strings.TrimSpace(string(r[:decisionExcerpt])) + "..."
	}
	return s
}
