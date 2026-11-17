package planning

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"entgo.io/ent/dialect/sql"
	"github.com/Bengo-Hub/httpware/richtext"

	"github.com/bengobox/projects-service/internal/ent"
	"github.com/bengobox/projects-service/internal/ent/predicate"
	entproject "github.com/bengobox/projects-service/internal/ent/project"
	entchange "github.com/bengobox/projects-service/internal/ent/projectchangerequest"
	"github.com/bengobox/projects-service/internal/modules/rbac"
	"github.com/bengobox/projects-service/internal/platform/events"
	"github.com/bengobox/projects-service/internal/platform/treasury"
	"github.com/bengobox/projects-service/internal/services/activity"
	"github.com/bengobox/projects-service/internal/services/notify"
	"github.com/bengobox/projects-service/internal/services/people"
	"github.com/bengobox/projects-service/internal/services/tasks"
	"github.com/bengobox/projects-service/internal/services/txn"
)

// Change requests (sprint 16). A requester raises a scope, cost or schedule change; someone
// holding projects.change_approvals.manage (never the requester) approves or rejects it with a
// comment. This sign-off is local to projects, like maskani's approval rules: it is not a money
// decision. On approval the listed date shifts are applied to the tasks in the same transaction,
// and a cost change is pushed to treasury after commit as a project budget (created, or an
// existing draft updated, then submitted), where treasury's own budget approval runs. Treasury's
// answer is kept on the request's metadata under "treasury"; a failed push can be retried.
const (
	CRPending   = "pending"
	CRApproved  = "approved"
	CRRejected  = "rejected"
	CRWithdrawn = "withdrawn"
)

// Kinds of change.
var CRKinds = []string{"scope", "cost", "schedule"}

// Treasury push states (metadata.treasury.status).
const (
	PushNotNeeded = "not_needed"
	PushPending   = "pending"
	PushRunning   = "pushing"
	PushSubmitted = "submitted"
	PushFailed    = "failed"
)

// pushStale lets a retry take over a push that has said "pushing" this long (a crashed pod).
const pushStale = 2 * time.Minute

// Change request events (subject project.change_request.<type>).
const (
	EventCRSubmitted    = "change_request.submitted"
	EventCRUpdated      = "change_request.updated"
	EventCRWithdrawn    = "change_request.withdrawn"
	EventCRApproved     = "change_request.approved"
	EventCRRejected     = "change_request.rejected"
	EventCRBudgetPushed = "change_request.budget_pushed"
)

// Person-addressed change request notices, one event per recipient (notify payload). They are
// written beside the events above, which carry no recipient and stay as they are for other
// consumers.
const (
	// EventCRReviewRequested goes to each approver candidate when a request is raised.
	EventCRReviewRequested = "change_request.review_requested"
	// EventCRDecided goes to the requester when a request is approved or rejected.
	EventCRDecided = "change_request.decided"
)

// crNotice is the notice of request r by actorID, linking to the request in Projects.
func crNotice(ctx context.Context, tx *ent.Tx, r *ent.ProjectChangeRequest, actorID uuid.UUID) (notify.Notice, error) {
	p, err := tx.Project.Query().Where(entproject.ID(r.ProjectID), entproject.TenantID(r.TenantID)).
		Select(entproject.FieldName).Only(ctx)
	if err != nil {
		return notify.Notice{}, fmt.Errorf("load project: %w", err)
	}
	actor, err := notify.Actor(ctx, tx, r.TenantID, actorID)
	if err != nil {
		return notify.Notice{}, err
	}
	shifts, _ := r.Metadata[crMetaShifts].([]any)
	return notify.Notice{
		TenantID: r.TenantID, ProjectID: r.ProjectID, ProjectName: p.Name, Actor: actor,
		ActionPath: notify.ChangeRequestPath(ctx, r.ProjectID, r.ID),
		Extra: map[string]any{
			"change_request_id": r.ID.String(), "title": r.Title, "kind": r.Kind,
			"date_shift_count": len(shifts), "has_budget_change": needsBudgetPush(r.Metadata),
		},
	}, nil
}

// notifyApprovers writes project.change_request.review_requested to each approver candidate: the
// project's owner and its owner and manager members who hold projects.change_approvals.manage
// (one batched RBAC query), never the requester. comment_excerpt is the description as plain text.
func notifyApprovers(ctx context.Context, tx *ent.Tx, r *ent.ProjectChangeRequest) error {
	leads, err := notify.ProjectLeadsNotClients(ctx, tx, r.TenantID, r.ProjectID)
	if err != nil {
		return err
	}
	holders, err := rbac.HoldersAmong(ctx, tx.ProjectsUser, r.TenantID, leads, rbac.PermChangeApprovals)
	if err != nil {
		return err
	}
	approvers := make([]uuid.UUID, 0, len(leads))
	for _, id := range leads {
		if holders[id] && id != r.RequestedBy {
			approvers = append(approvers, id)
		}
	}
	if len(approvers) == 0 {
		return nil
	}
	n, err := crNotice(ctx, tx, r, r.RequestedBy)
	if err != nil {
		return err
	}
	n.CommentExcerpt = richtext.Excerpt(r.Description, notify.ExcerptLength)
	return notify.Write(ctx, tx, EventCRReviewRequested, n, approvers)
}

// notifyRequester writes project.change_request.decided to the requester: decision approved or
// rejected, and the decider's comment as plain text.
func notifyRequester(ctx context.Context, tx *ent.Tx, r *ent.ProjectChangeRequest, decider uuid.UUID) error {
	n, err := crNotice(ctx, tx, r, decider)
	if err != nil {
		return err
	}
	n.CommentExcerpt = richtext.Excerpt(r.DecisionComment, notify.ExcerptLength)
	n.Extra["decision"] = r.Status
	if r.DecidedAt != nil {
		n.Extra["decided_at"] = r.DecidedAt.UTC().Format(time.RFC3339)
	}
	return notify.Write(ctx, tx, EventCRDecided, n, []uuid.UUID{r.RequestedBy})
}

// Metadata keys of a change request.
const (
	crMetaShifts   = "date_shifts"
	crMetaBudget   = "budget"
	crMetaBudgetID = "budget_id"
	crMetaTreasury = "treasury"
	crMetaApplied  = "applied_task_ids"
)

// ChangeRequestInput raises or edits a change request. On an edit, absent fields stay; budget
// null removes the proposed budget and budget_id "" unlinks the draft.
type ChangeRequestInput struct {
	Title       *string            `json:"title"`
	Description *string            `json:"description"`
	Kind        *string            `json:"kind"`
	DateShifts  *[]tasks.DateShift `json:"date_shifts"`
	// Budget is a treasury BudgetInput body (name, fiscal_year, period_type, start_date, end_date,
	// currency, lines...). budget_type and project_id are always set to this project.
	Budget json.RawMessage `json:"budget"`
	// BudgetID names an existing draft project budget in treasury to update (with Budget) or just
	// submit once the request is approved.
	BudgetID *string `json:"budget_id"`
}

// ChangeRequest is a request with the people's names.
type ChangeRequest struct {
	*ent.ProjectChangeRequest
	RequestedByName string `json:"requested_by_name"`
	DecidedByName   string `json:"decided_by_name"`
}

func (s *Service) named(ctx context.Context, tenantID uuid.UUID, rows []*ent.ProjectChangeRequest) ([]ChangeRequest, error) {
	ids := make([]uuid.UUID, 0, 2*len(rows))
	for _, r := range rows {
		ids = append(ids, r.RequestedBy)
		if r.DecidedBy != nil {
			ids = append(ids, *r.DecidedBy)
		}
	}
	names, err := people.Lookup(ctx, s.client.ProjectsUser, tenantID, ids)
	if err != nil {
		return nil, err
	}
	out := make([]ChangeRequest, len(rows))
	for i, r := range rows {
		out[i] = ChangeRequest{ProjectChangeRequest: r, RequestedByName: names[r.RequestedBy].Label()}
		if r.DecidedBy != nil {
			out[i].DecidedByName = names[*r.DecidedBy].Label()
		}
	}
	return out, nil
}

func (s *Service) one(ctx context.Context, tenantID uuid.UUID, r *ent.ProjectChangeRequest) (ChangeRequest, error) {
	out, err := s.named(ctx, tenantID, []*ent.ProjectChangeRequest{r})
	if err != nil {
		return ChangeRequest{}, err
	}
	return out[0], nil
}

func crPayload(r *ent.ProjectChangeRequest) map[string]any {
	shifts, _ := r.Metadata[crMetaShifts].([]any)
	_, hasBudget := r.Metadata[crMetaBudget]
	budgetID, _ := r.Metadata[crMetaBudgetID].(string)
	out := map[string]any{
		"project_id": r.ProjectID.String(), "change_request_id": r.ID.String(), "title": r.Title, "kind": r.Kind,
		"status": r.Status, "requested_by": r.RequestedBy.String(), "date_shift_count": len(shifts),
		"has_budget_change": hasBudget || budgetID != "",
	}
	if r.DecidedBy != nil {
		out["decided_by"] = r.DecidedBy.String()
		out["decision_comment"] = r.DecisionComment
	}
	return out
}

// applyInput validates in onto meta (a copy of the request's metadata) and returns it.
func (s *Service) applyInput(ctx context.Context, tenantID, projectID uuid.UUID, in ChangeRequestInput, meta map[string]any) (map[string]any, error) {
	out := make(map[string]any, len(meta)+3)
	for k, v := range meta {
		out[k] = v
	}
	if in.DateShifts != nil {
		if err := tasks.ValidateDateShifts(ctx, s.client, tenantID, projectID, *in.DateShifts); err != nil {
			var verr tasks.ValidationError
			if errors.As(err, &verr) {
				return nil, ValidationError(verr.Error())
			}
			return nil, err
		}
		if len(*in.DateShifts) == 0 {
			delete(out, crMetaShifts)
		} else {
			raw, _ := json.Marshal(*in.DateShifts)
			var list []any
			_ = json.Unmarshal(raw, &list)
			out[crMetaShifts] = list
		}
	}
	if len(in.Budget) > 0 {
		if string(in.Budget) == "null" {
			delete(out, crMetaBudget)
		} else {
			var body map[string]any
			if err := json.Unmarshal(in.Budget, &body); err != nil || body == nil {
				return nil, ValidationError("budget must be a treasury budget object")
			}
			if len(in.Budget) > 64*1024 {
				return nil, ValidationError("the budget is too large")
			}
			if lines, _ := body["lines"].([]any); len(lines) == 0 {
				return nil, ValidationError("the budget needs at least one line")
			}
			body["budget_type"], body["project_id"] = "project", projectID.String()
			out[crMetaBudget] = body
		}
	}
	if in.BudgetID != nil {
		v := strings.TrimSpace(*in.BudgetID)
		if v == "" {
			delete(out, crMetaBudgetID)
		} else if id, err := uuid.Parse(v); err != nil {
			return nil, ValidationError("budget_id must be a treasury budget id")
		} else {
			out[crMetaBudgetID] = id.String()
		}
	}
	return out, nil
}

func checkKind(kind string, meta map[string]any) error {
	if !slices.Contains(CRKinds, kind) {
		return ValidationError("kind must be scope, cost or schedule")
	}
	_, hasBudget := meta[crMetaBudget]
	_, hasBudgetID := meta[crMetaBudgetID]
	if kind == "cost" && !hasBudget && !hasBudgetID {
		return ValidationError("a cost change needs a budget or a budget_id")
	}
	if shifts, _ := meta[crMetaShifts].([]any); kind == "schedule" && len(shifts) == 0 {
		return ValidationError("a schedule change needs date_shifts")
	}
	return nil
}

// CreateChangeRequest raises a request (status pending) as the signed-in user. The approver
// candidates are told in the same transaction (notifyApprovers).
func (s *Service) CreateChangeRequest(ctx context.Context, tenantID, projectID uuid.UUID, in ChangeRequestInput) (ChangeRequest, error) {
	requester := activity.ActorFrom(ctx)
	if requester == uuid.Nil {
		return ChangeRequest{}, ValidationError("a change request needs a signed-in requester")
	}
	if in.Title == nil || strings.TrimSpace(*in.Title) == "" || len([]rune(*in.Title)) > 200 {
		return ChangeRequest{}, ValidationError("title is required (at most 200 characters)")
	}
	kind := "scope"
	if in.Kind != nil && *in.Kind != "" {
		kind = *in.Kind
	}
	if _, err := s.project(ctx, s.client.Project, tenantID, projectID); err != nil {
		return ChangeRequest{}, err
	}
	meta, err := s.applyInput(ctx, tenantID, projectID, in, map[string]any{})
	if err != nil {
		return ChangeRequest{}, err
	}
	if err := checkKind(kind, meta); err != nil {
		return ChangeRequest{}, err
	}
	var r *ent.ProjectChangeRequest
	err = txn.Run(ctx, s.client, func(tx *ent.Tx) error {
		c := tx.ProjectChangeRequest.Create().SetTenantID(tenantID).SetProjectID(projectID).
			SetTitle(strings.TrimSpace(*in.Title)).SetKind(kind).SetStatus(CRPending).SetRequestedBy(requester).SetMetadata(meta)
		if in.Description != nil {
			c.SetDescription(*in.Description)
		}
		var err error
		if r, err = c.Save(ctx); err != nil {
			return fmt.Errorf("create change request: %w", err)
		}
		if err := record(ctx, tx, tenantID, projectID, "change_request", r.ID, activity.ActionCreated,
			map[string]any{"title": r.Title, "kind": kind}); err != nil {
			return err
		}
		if err := events.Write(ctx, tx, events.AggregateProject, EventCRSubmitted, projectID, tenantID, crPayload(r)); err != nil {
			return err
		}
		return notifyApprovers(ctx, tx, r)
	})
	if err != nil {
		return ChangeRequest{}, err
	}
	return s.one(ctx, tenantID, r)
}

// lockCR locks one change request row for the rest of tx and loads it.
func lockCR(ctx context.Context, tx *ent.Tx, tenantID, projectID, id uuid.UUID) (*ent.ProjectChangeRequest, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id FROM project_change_requests WHERE id = $1 AND tenant_id = $2 AND project_id = $3 FOR UPDATE`,
		id, tenantID, projectID)
	if err != nil {
		return nil, fmt.Errorf("lock change request: %w", err)
	}
	found := rows.Next()
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if !found {
		return nil, ErrNotFound
	}
	return tx.ProjectChangeRequest.Get(ctx, id)
}

// UpdateChangeRequest edits a pending request: the requester, or anyone with manage.
func (s *Service) UpdateChangeRequest(ctx context.Context, tenantID, projectID, id uuid.UUID, in ChangeRequestInput, canManage bool) (ChangeRequest, error) {
	var r *ent.ProjectChangeRequest
	err := txn.Run(ctx, s.client, func(tx *ent.Tx) error {
		cur, err := lockCR(ctx, tx, tenantID, projectID, id)
		if err != nil {
			return err
		}
		if cur.Status != CRPending {
			return &StateError{Code: "change_request_not_pending", Msg: "only a pending change request can be edited"}
		}
		if !canManage && cur.RequestedBy != activity.ActorFrom(ctx) {
			return ErrNotYours
		}
		meta, err := s.applyInput(ctx, tenantID, projectID, in, cur.Metadata)
		if err != nil {
			return err
		}
		kind := cur.Kind
		if in.Kind != nil && *in.Kind != "" {
			kind = *in.Kind
		}
		if err := checkKind(kind, meta); err != nil {
			return err
		}
		u := cur.Update().SetKind(kind).SetMetadata(meta)
		if in.Title != nil {
			t := strings.TrimSpace(*in.Title)
			if t == "" || len([]rune(t)) > 200 {
				return ValidationError("title is required (at most 200 characters)")
			}
			u.SetTitle(t)
		}
		if in.Description != nil {
			u.SetDescription(*in.Description)
		}
		if r, err = u.Save(ctx); err != nil {
			return fmt.Errorf("update change request: %w", err)
		}
		if err := record(ctx, tx, tenantID, projectID, "change_request", id, activity.ActionUpdated, map[string]any{"title": r.Title}); err != nil {
			return err
		}
		return events.Write(ctx, tx, events.AggregateProject, EventCRUpdated, projectID, tenantID, crPayload(r))
	})
	if err != nil {
		return ChangeRequest{}, err
	}
	return s.one(ctx, tenantID, r)
}

// WithdrawChangeRequest withdraws a pending request (the requester, or anyone with manage).
func (s *Service) WithdrawChangeRequest(ctx context.Context, tenantID, projectID, id uuid.UUID, canManage bool) (ChangeRequest, error) {
	var r *ent.ProjectChangeRequest
	err := txn.Run(ctx, s.client, func(tx *ent.Tx) error {
		cur, err := lockCR(ctx, tx, tenantID, projectID, id)
		if err != nil {
			return err
		}
		if cur.Status != CRPending {
			return &StateError{Code: "change_request_not_pending", Msg: "only a pending change request can be withdrawn"}
		}
		if !canManage && cur.RequestedBy != activity.ActorFrom(ctx) {
			return ErrNotYours
		}
		if r, err = cur.Update().SetStatus(CRWithdrawn).Save(ctx); err != nil {
			return fmt.Errorf("withdraw change request: %w", err)
		}
		if err := record(ctx, tx, tenantID, projectID, "change_request", id, activity.ActionUpdated,
			map[string]any{"status": map[string]any{"from": CRPending, "to": CRWithdrawn}}); err != nil {
			return err
		}
		return events.Write(ctx, tx, events.AggregateProject, EventCRWithdrawn, projectID, tenantID, crPayload(r))
	})
	if err != nil {
		return ChangeRequest{}, err
	}
	return s.one(ctx, tenantID, r)
}

// shiftsOf reads the stored date shifts.
func shiftsOf(meta map[string]any) []tasks.DateShift {
	var out []tasks.DateShift
	if raw, ok := meta[crMetaShifts]; ok {
		b, _ := json.Marshal(raw)
		_ = json.Unmarshal(b, &out)
	}
	return out
}

func needsBudgetPush(meta map[string]any) bool {
	_, body := meta[crMetaBudget]
	id, _ := meta[crMetaBudgetID].(string)
	return body || id != ""
}

// Decide approves or rejects a pending request as the signed-in user, never the requester. An
// approval applies the date shifts in the same transaction (a broken finish-to-start link is
// *tasks.ScheduleConflict and nothing changes) and then pushes any budget change to treasury. The
// requester is told the decision in the same transaction (notifyRequester).
func (s *Service) Decide(ctx context.Context, tenantID, projectID, id uuid.UUID, approve bool, comment string) (ChangeRequest, error) {
	decider := activity.ActorFrom(ctx)
	comment = strings.TrimSpace(comment)
	if !approve && comment == "" {
		return ChangeRequest{}, ValidationError("say why the change is rejected")
	}
	if len([]rune(comment)) > 2000 {
		return ChangeRequest{}, ValidationError("the comment is at most 2000 characters")
	}
	var r *ent.ProjectChangeRequest
	var moved []*ent.Task
	push := false
	err := txn.Run(ctx, s.client, func(tx *ent.Tx) error {
		cur, err := lockCR(ctx, tx, tenantID, projectID, id)
		if err != nil {
			return err
		}
		if cur.Status != CRPending {
			return &StateError{Code: "change_request_not_pending", Msg: "this change request has already been " + cur.Status}
		}
		if decider == uuid.Nil || cur.RequestedBy == decider {
			return ErrOwnRequest
		}
		meta := make(map[string]any, len(cur.Metadata)+2)
		for k, v := range cur.Metadata {
			meta[k] = v
		}
		status, event := CRRejected, EventCRRejected
		if approve {
			status, event = CRApproved, EventCRApproved
			if shifts := shiftsOf(cur.Metadata); len(shifts) > 0 {
				if moved, err = tasks.ShiftDatesTx(ctx, tx, tenantID, projectID, shifts, map[string]any{"change_request_id": id.String()}); err != nil {
					return err
				}
				ids := make([]string, len(moved))
				for i, t := range moved {
					ids[i] = t.ID.String()
				}
				meta[crMetaApplied] = ids
			}
			push = needsBudgetPush(meta)
			state := PushNotNeeded
			if push {
				state = PushPending
			}
			meta[crMetaTreasury] = map[string]any{"status": state, "attempts": 0}
		}
		if r, err = cur.Update().SetStatus(status).SetDecidedBy(decider).SetDecidedAt(time.Now().UTC()).
			SetDecisionComment(comment).SetMetadata(meta).Save(ctx); err != nil {
			return fmt.Errorf("decide change request: %w", err)
		}
		if err := record(ctx, tx, tenantID, projectID, "change_request", id, activity.ActionUpdated,
			map[string]any{"status": map[string]any{"from": CRPending, "to": status}, "comment": comment}); err != nil {
			return err
		}
		payload := crPayload(r)
		if ids, ok := meta[crMetaApplied]; ok {
			payload["shifted_task_ids"] = ids
		}
		if err := events.Write(ctx, tx, events.AggregateProject, event, projectID, tenantID, payload); err != nil {
			return err
		}
		return notifyRequester(ctx, tx, r, decider)
	})
	if err != nil {
		return ChangeRequest{}, err
	}
	if s.tasks != nil {
		s.tasks.EmitUpdated(ctx, tenantID, moved)
	}
	if push {
		if pushed, perr := s.PushBudget(ctx, tenantID, projectID, id); perr == nil {
			return pushed, nil
		} else {
			s.log.Warn("change request budget push not run", zap.String("change_request_id", id.String()), zap.Error(perr))
		}
		if r, err = s.client.ProjectChangeRequest.Get(ctx, id); err != nil {
			return ChangeRequest{}, fmt.Errorf("reload change request: %w", err)
		}
	}
	return s.one(ctx, tenantID, r)
}

// treasuryState reads metadata.treasury.
func treasuryState(meta map[string]any) map[string]any {
	m, _ := meta[crMetaTreasury].(map[string]any)
	out := map[string]any{}
	for k, v := range m {
		out[k] = v
	}
	return out
}

// saveTreasury writes metadata.treasury of a request (outside any other transaction).
func (s *Service) saveTreasury(ctx context.Context, tenantID, projectID, id uuid.UUID, state map[string]any, final bool) (*ent.ProjectChangeRequest, error) {
	var r *ent.ProjectChangeRequest
	err := txn.Run(ctx, s.client, func(tx *ent.Tx) error {
		cur, err := lockCR(ctx, tx, tenantID, projectID, id)
		if err != nil {
			return err
		}
		meta := make(map[string]any, len(cur.Metadata))
		for k, v := range cur.Metadata {
			meta[k] = v
		}
		meta[crMetaTreasury] = state
		if r, err = cur.Update().SetMetadata(meta).Save(ctx); err != nil {
			return fmt.Errorf("save treasury state: %w", err)
		}
		if !final {
			return nil
		}
		if err := record(ctx, tx, tenantID, projectID, "change_request", id, activity.ActionUpdated,
			map[string]any{"treasury": state["status"], "budget_id": state["budget_id"]}); err != nil {
			return err
		}
		payload := crPayload(r)
		payload["treasury_status"], payload["budget_id"], payload["error"] = state["status"], state["budget_id"], state["error"]
		return events.Write(ctx, tx, events.AggregateProject, EventCRBudgetPushed, projectID, tenantID, payload)
	})
	return r, err
}

// treasuryMessage is treasury's reason for a refusal, or a plain one.
func treasuryMessage(err error) string {
	var te *treasury.Error
	if errors.As(err, &te) {
		var body struct {
			Error   string `json:"error"`
			Message string `json:"message"`
		}
		_ = json.Unmarshal(te.Body, &body)
		if body.Error != "" {
			return body.Error
		}
		if body.Message != "" {
			return body.Message
		}
		return fmt.Sprintf("treasury answered %d", te.Status)
	}
	if errors.Is(err, treasury.ErrNotConfigured) {
		return "treasury is not configured"
	}
	return "treasury could not be reached"
}

// PushBudget sends an approved request's budget change to treasury as the signed-in user: an
// existing draft (budget_id) is checked to belong to the project and updated with the proposed
// body when there is one, otherwise a project budget is created; then it is submitted for
// treasury's approval. The budget id is saved as soon as it exists, so a retry never creates a
// second budget. Only an approved request whose push is pending or failed (or stuck pushing for
// two minutes) can be pushed; anything else is a StateError.
func (s *Service) PushBudget(ctx context.Context, tenantID, projectID, id uuid.UUID) (ChangeRequest, error) {
	userID := activity.ActorFrom(ctx)
	var cur *ent.ProjectChangeRequest
	var state map[string]any
	err := txn.Run(ctx, s.client, func(tx *ent.Tx) error {
		var err error
		if cur, err = lockCR(ctx, tx, tenantID, projectID, id); err != nil {
			return err
		}
		if cur.Status != CRApproved || !needsBudgetPush(cur.Metadata) {
			return &StateError{Code: "budget_push_not_needed", Msg: "this change request has no approved budget change to send"}
		}
		state = treasuryState(cur.Metadata)
		st, _ := state["status"].(string)
		since, _ := time.Parse(time.RFC3339, fmt.Sprint(state["pushing_since"]))
		switch {
		case st == PushPending || st == PushFailed || st == "":
		case st == PushRunning && time.Since(since) > pushStale:
		case st == PushRunning:
			return &StateError{Code: "budget_push_in_progress", Msg: "the budget is being sent to treasury right now"}
		default:
			return &StateError{Code: "budget_push_done", Msg: "the budget has already been sent to treasury"}
		}
		attempts, _ := state["attempts"].(float64)
		state["status"], state["pushing_since"], state["attempts"] = PushRunning, time.Now().UTC().Format(time.RFC3339), int(attempts)+1
		delete(state, "error")
		meta := make(map[string]any, len(cur.Metadata))
		for k, v := range cur.Metadata {
			meta[k] = v
		}
		meta[crMetaTreasury] = state
		return cur.Update().SetMetadata(meta).Exec(ctx)
	})
	if err != nil {
		return ChangeRequest{}, err
	}
	budgetID, perr := s.sendBudget(ctx, tenantID, projectID, id, userID, cur.Metadata, state)
	delete(state, "pushing_since")
	state["attempted_at"] = time.Now().UTC().Format(time.RFC3339)
	if budgetID != "" {
		state["budget_id"] = budgetID
	}
	if perr != nil {
		state["status"], state["error"] = PushFailed, treasuryMessage(perr)
		s.log.Warn("change request budget push failed", zap.String("change_request_id", id.String()), zap.Error(perr))
	} else {
		state["status"] = PushSubmitted
	}
	r, err := s.saveTreasury(context.WithoutCancel(ctx), tenantID, projectID, id, state, true)
	if err != nil {
		return ChangeRequest{}, err
	}
	return s.one(ctx, tenantID, r)
}

// sendBudget does the treasury calls and returns the budget id (set as soon as it is known).
func (s *Service) sendBudget(ctx context.Context, tenantID, projectID, id, userID uuid.UUID, meta, state map[string]any) (string, error) {
	if !s.treasury.Enabled() {
		return "", treasury.ErrNotConfigured
	}
	body, _ := meta[crMetaBudget].(map[string]any)
	budgetID, _ := state["budget_id"].(string)
	if budgetID == "" {
		if draft, _ := meta[crMetaBudgetID].(string); draft != "" {
			did := uuid.MustParse(draft)
			raw, err := s.treasury.GetBudget(ctx, tenantID, did)
			if err != nil {
				return "", err
			}
			var b struct {
				ProjectID *uuid.UUID `json:"project_id"`
			}
			if json.Unmarshal(raw, &b) != nil || b.ProjectID == nil || *b.ProjectID != projectID {
				return "", fmt.Errorf("budget %s does not belong to this project", draft)
			}
			if body != nil {
				if _, err := s.treasury.UpdateBudget(ctx, tenantID, userID, did, body); err != nil {
					return "", err
				}
			}
			budgetID = draft
		} else {
			raw, err := s.treasury.CreateBudget(ctx, tenantID, userID, body)
			if err != nil {
				return "", err
			}
			var created struct {
				ID uuid.UUID `json:"id"`
			}
			if json.Unmarshal(raw, &created) != nil || created.ID == uuid.Nil {
				return "", fmt.Errorf("treasury budget create returned no id")
			}
			budgetID = created.ID.String()
		}
		// Keep the id before submitting, so a retry submits this budget instead of creating another.
		progress := map[string]any{}
		for k, v := range state {
			progress[k] = v
		}
		progress["budget_id"] = budgetID
		if _, err := s.saveTreasury(context.WithoutCancel(ctx), tenantID, projectID, id, progress, false); err != nil {
			return budgetID, err
		}
	}
	_, err := s.treasury.SubmitBudget(ctx, tenantID, userID, uuid.MustParse(budgetID))
	return budgetID, err
}

// CRFilter narrows a change request list.
type CRFilter struct {
	Status    string
	VisibleTo *uuid.UUID
	Limit     int
	Offset    int
}

// ListChangeRequests returns one page of a project's requests (projectID nil: the tenant's, scoped
// to the projects VisibleTo may see), newest first.
func (s *Service) ListChangeRequests(ctx context.Context, tenantID uuid.UUID, projectID *uuid.UUID, f CRFilter) ([]ChangeRequest, int, error) {
	q := s.client.ProjectChangeRequest.Query().Where(entchange.TenantID(tenantID))
	if projectID != nil {
		q = q.Where(entchange.ProjectID(*projectID))
	} else if f.VisibleTo != nil {
		q = q.Where(visibleRequests(*f.VisibleTo))
	}
	if f.Status != "" {
		q = q.Where(entchange.Status(f.Status))
	}
	total, err := q.Clone().Count(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("count change requests: %w", err)
	}
	if f.Limit <= 0 {
		f.Limit = 20
	}
	rows, err := q.Order(ent.Desc(entchange.FieldCreatedAt), ent.Desc(entchange.FieldID)).Offset(f.Offset).Limit(f.Limit).All(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("list change requests: %w", err)
	}
	out, err := s.named(ctx, tenantID, rows)
	return out, total, err
}

// visibleRequests limits requests to projects the user owns or is a staff member of (the
// membership rule, as one correlated subquery rather than a list of project ids).
func visibleRequests(userID uuid.UUID) predicate.ProjectChangeRequest {
	return func(sel *sql.Selector) {
		sel.Where(sql.P(func(b *sql.Builder) {
			b.WriteString("EXISTS (SELECT 1 FROM projects p WHERE p.id = ").WriteString(sel.C(entchange.FieldProjectID)).
				WriteString(" AND (p.owner_id = ").Arg(userID).
				WriteString(" OR EXISTS (SELECT 1 FROM project_members m WHERE m.project_id = p.id AND m.user_id = ").Arg(userID).
				WriteString(" AND m.left_at IS NULL AND m.role_code <> 'client')))")
		}))
	}
}

// GetChangeRequest returns one request.
func (s *Service) GetChangeRequest(ctx context.Context, tenantID, projectID, id uuid.UUID) (ChangeRequest, error) {
	r, err := s.client.ProjectChangeRequest.Query().
		Where(entchange.ID(id), entchange.TenantID(tenantID), entchange.ProjectID(projectID)).Only(ctx)
	if ent.IsNotFound(err) {
		return ChangeRequest{}, ErrNotFound
	}
	if err != nil {
		return ChangeRequest{}, fmt.Errorf("load change request: %w", err)
	}
	return s.one(ctx, tenantID, r)
}
