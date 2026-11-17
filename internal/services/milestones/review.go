package milestones

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/projects-service/internal/ent"
	entmilestone "github.com/bengobox/projects-service/internal/ent/milestone"
	entproject "github.com/bengobox/projects-service/internal/ent/project"
	"github.com/bengobox/projects-service/internal/platform/marketflow"
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

// projects is client.Project or tx.Project.
func projectName(ctx context.Context, projects *ent.ProjectClient, tenantID, projectID uuid.UUID) (string, error) {
	p, err := projects.Query().Where(entproject.ID(projectID), entproject.TenantID(tenantID)).
		Select(entproject.FieldName).Only(ctx)
	if err != nil {
		return "", fmt.Errorf("load project: %w", err)
	}
	return p.Name, nil
}

// Contacts reads one CRM contact; *marketflow.Client implements it.
type Contacts interface {
	GetContact(ctx context.Context, tenantID, contactID uuid.UUID) (marketflow.Contact, error)
}

// contactTimeout bounds the CRM read for one invited client's review notice.
const contactTimeout = 5 * time.Second

// reviewNotice is the project.milestone.ready_for_review notice of m, linking to the milestone in
// the portal. projects and users are the client's or the transaction's.
func reviewNotice(ctx context.Context, projects *ent.ProjectClient, users *ent.ProjectsUserClient, m *ent.Milestone) (notify.Notice, error) {
	name, err := projectName(ctx, projects, m.TenantID, m.ProjectID)
	if err != nil {
		return notify.Notice{}, err
	}
	actor, err := notify.ActorOf(ctx, users, m.TenantID, activity.ActorFrom(ctx))
	if err != nil {
		return notify.Notice{}, err
	}
	extra := map[string]any{"milestone_id": m.ID.String(), "milestone_name": m.Name, "target_date": ""}
	if !m.TargetDate.IsZero() {
		extra["target_date"] = m.TargetDate.Format(time.DateOnly)
	}
	return notify.Notice{
		TenantID: m.TenantID, ProjectID: m.ProjectID, ProjectName: name, Actor: actor,
		ActionPath: notify.PortalPath(ctx, m.ProjectID, m.ID), Extra: extra,
	}, nil
}

// notifyReadyForReview writes project.milestone.ready_for_review to each signed-in client of the
// project, in the milestone's transaction.
func notifyReadyForReview(ctx context.Context, tx *ent.Tx, m *ent.Milestone) error {
	clients, err := notify.ProjectClients(ctx, tx, m.TenantID, m.ProjectID)
	if err != nil || len(clients) == 0 {
		return err
	}
	n, err := reviewNotice(ctx, tx.Project, tx.ProjectsUser, m)
	if err != nil {
		return err
	}
	return notify.Write(ctx, tx, notify.EventMilestoneReadyForReview, n, clients)
}

// notifyInvitedClients writes project.milestone.ready_for_review to each client still at the
// invite stage (no account yet), at the email and phone marketflow holds for their CRM contact now:
// projects keeps no copy of a client's address, only its keyed hash. recipient_user_id is empty and
// the link opens the project in the portal, where they sign in first.
//
// It runs after the milestone committed, best effort: marketflow must never fail or slow down the
// team's update, so the read sits outside the transaction, each contact read has its own timeout,
// and a client whose contact cannot be read (marketflow down or not configured), was erased or has
// neither an email nor a phone is skipped with a warning. A crash between the commit and these
// writes loses these notices only; the signed-in clients' notices were written with the milestone.
func (s *Service) notifyInvitedClients(ctx context.Context, m *ent.Milestone) {
	if s.crm == nil || m == nil {
		return
	}
	log := s.log.With(zap.String("milestone_id", m.ID.String()))
	invited, err := notify.InvitedClients(ctx, s.client.ProjectMember, m.TenantID, m.ProjectID)
	if err != nil {
		log.Warn("review notice: invited clients not read", zap.Error(err))
		return
	}
	if len(invited) == 0 {
		return
	}
	n, err := reviewNotice(ctx, s.client.Project, s.client.ProjectsUser, m)
	if err != nil {
		log.Warn("review notice: not built for invited clients", zap.Error(err))
		return
	}
	for _, c := range invited {
		cctx, cancel := context.WithTimeout(ctx, contactTimeout)
		contact, err := s.crm.GetContact(cctx, m.TenantID, c.ContactID)
		cancel()
		if err != nil {
			log.Warn("review notice: CRM contact not read, invited client skipped",
				zap.String("member_id", c.MemberID.String()), zap.Error(err))
			continue
		}
		email, phone := strings.TrimSpace(contact.Email), strings.TrimSpace(contact.Phone)
		if contact.Erased() || (email == "" && phone == "") {
			log.Warn("review notice: CRM contact has no email or phone, invited client skipped",
				zap.String("member_id", c.MemberID.String()))
			continue
		}
		name := contact.Name()
		if strings.TrimSpace(name) == "" {
			name = c.Name
		}
		if err := notify.WriteExternal(ctx, s.client, notify.EventMilestoneReadyForReview, n, notify.External{
			MemberID: c.MemberID, Email: email, Phone: phone, Name: name,
		}); err != nil {
			log.Warn("review notice: not written for invited client", zap.String("member_id", c.MemberID.String()), zap.Error(err))
		}
	}
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
		name, err := projectName(ctx, tx.Project, tenantID, projectID)
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
