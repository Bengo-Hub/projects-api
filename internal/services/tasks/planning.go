package tasks

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/bengobox/projects-service/internal/ent"
	entcomment "github.com/bengobox/projects-service/internal/ent/comment"
	entproject "github.com/bengobox/projects-service/internal/ent/project"
	enttask "github.com/bengobox/projects-service/internal/ent/task"
	entassignee "github.com/bengobox/projects-service/internal/ent/taskassignee"
	"github.com/bengobox/projects-service/internal/platform/events"
	"github.com/bengobox/projects-service/internal/platform/realtime"
	"github.com/bengobox/projects-service/internal/services/activity"
	"github.com/bengobox/projects-service/internal/services/notify"
	"github.com/bengobox/projects-service/internal/services/txn"
)

// Events of the sprint 16 rule routes (subject project.<type>).
const (
	EventStatusRulesUpdated = "status_rules.updated"
	EventAutomationsUpdated = "automations.updated"
)

// lockProject loads the project row FOR UPDATE, so two writes of its metadata (statuses, rules,
// automations, commercial terms) never undo each other.
func lockProject(ctx context.Context, tx *ent.Tx, tenantID, projectID uuid.UUID) (*ent.Project, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id FROM projects WHERE id = $1 AND tenant_id = $2 FOR UPDATE`, projectID, tenantID)
	if err != nil {
		return nil, fmt.Errorf("lock project: %w", err)
	}
	found := rows.Next()
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("lock project: %w", err)
	}
	if !found {
		return nil, ErrNotFound
	}
	p, err := tx.Project.Query().Where(entproject.ID(projectID), entproject.TenantID(tenantID)).Only(ctx)
	if ent.IsNotFound(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("lock project: %w", err)
	}
	return p, nil
}

// saveProjectMeta writes one metadata key of a locked project.
func saveProjectMeta(ctx context.Context, p *ent.Project, key string, value any) error {
	meta := make(map[string]any, len(p.Metadata)+1)
	for k, v := range p.Metadata {
		meta[k] = v
	}
	meta[key] = value
	return p.Update().SetMetadata(meta).Exec(ctx)
}

// GetStatusRules returns the project's status rules (empty lists when none).
func (s *Service) GetStatusRules(ctx context.Context, tenantID, projectID uuid.UUID) (StatusRules, error) {
	p, err := s.client.Project.Query().Where(entproject.ID(projectID), entproject.TenantID(tenantID)).
		Select(entproject.FieldMetadata).Only(ctx)
	if ent.IsNotFound(err) {
		return StatusRules{}, ErrNotFound
	}
	if err != nil {
		return StatusRules{}, fmt.Errorf("load status rules: %w", err)
	}
	r := statusRulesOf(p.Metadata)
	if r.Transitions == nil {
		r.Transitions = []Transition{}
	}
	if r.Requirements == nil {
		r.Requirements = []Requirement{}
	}
	return r, nil
}

// SetStatusRules replaces the project's status rules after checking them against its statuses.
func (s *Service) SetStatusRules(ctx context.Context, tenantID, projectID uuid.UUID, in StatusRules) (StatusRules, error) {
	var out StatusRules
	err := txn.Run(ctx, s.client, func(tx *ent.Tx) error {
		p, err := lockProject(ctx, tx, tenantID, projectID)
		if err != nil {
			return err
		}
		if out, err = ValidateStatusRules(in, statusesOf(p.Metadata)); err != nil {
			return err
		}
		if err := saveProjectMeta(ctx, p, MetaStatusRules, out); err != nil {
			return fmt.Errorf("save status rules: %w", err)
		}
		summary := map[string]any{"transitions": len(out.Transitions), "requirements": len(out.Requirements)}
		if err := activity.Record(ctx, tx, activity.Entry{
			TenantID: tenantID, ProjectID: activity.Ref(projectID), EntityType: "project", EntityID: projectID,
			Action: activity.ActionUpdated, Changes: map[string]any{"status_rules": summary},
		}); err != nil {
			return err
		}
		return events.Write(ctx, tx, events.AggregateProject, EventStatusRulesUpdated, projectID, tenantID, map[string]any{
			"project_id": projectID.String(), "rules": out, "actor_id": actorString(ctx),
		})
	})
	return out, err
}

// GetAutomations returns the project's automation rules.
func (s *Service) GetAutomations(ctx context.Context, tenantID, projectID uuid.UUID) ([]Automation, error) {
	p, err := s.client.Project.Query().Where(entproject.ID(projectID), entproject.TenantID(tenantID)).
		Select(entproject.FieldMetadata).Only(ctx)
	if ent.IsNotFound(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load automations: %w", err)
	}
	list := automationsOf(p.Metadata)
	if list == nil {
		list = []Automation{}
	}
	return list, nil
}

// SetAutomations replaces the project's automation rules (at most MaxAutomations). Every person
// a rule assigns or notifies must be on the project's team.
func (s *Service) SetAutomations(ctx context.Context, tenantID, projectID uuid.UUID, in []Automation) ([]Automation, error) {
	var out []Automation
	err := txn.Run(ctx, s.client, func(tx *ent.Tx) error {
		p, err := lockProject(ctx, tx, tenantID, projectID)
		if err != nil {
			return err
		}
		var ids []uuid.UUID
		for _, a := range in {
			for _, act := range a.Actions {
				ids = append(ids, act.UserIDs...)
			}
		}
		members, err := notify.Members(ctx, tx, tenantID, projectID, ids)
		if err != nil {
			return err
		}
		if out, err = ValidateAutomations(in, statusesOf(p.Metadata), func(id uuid.UUID) bool { return members[id] }); err != nil {
			return err
		}
		if err := saveProjectMeta(ctx, p, MetaAutomations, out); err != nil {
			return fmt.Errorf("save automations: %w", err)
		}
		if err := activity.Record(ctx, tx, activity.Entry{
			TenantID: tenantID, ProjectID: activity.Ref(projectID), EntityType: "project", EntityID: projectID,
			Action: activity.ActionUpdated, Changes: map[string]any{"automations": len(out)},
		}); err != nil {
			return err
		}
		return events.Write(ctx, tx, events.AggregateProject, EventAutomationsUpdated, projectID, tenantID, map[string]any{
			"project_id": projectID.String(), "count": len(out), "actor_id": actorString(ctx),
		})
	})
	return out, err
}

// MoveCheck says whether a task may move to Status now, and if not why.
type MoveCheck struct {
	Status  string   `json:"status"`
	Allowed bool     `json:"allowed"`
	Reason  string   `json:"reason,omitempty"`
	Missing []string `json:"missing_fields"`
	Message string   `json:"message,omitempty"`
}

// CheckMoves evaluates every move of one task against the project's status rules (three reads:
// the task, its project and, when a rule needs them, the assignee and comment counts). The board
// shows the reason on a blocked column before the drop.
func (s *Service) CheckMoves(ctx context.Context, tenantID, projectID, taskID uuid.UUID, rulesOn bool) ([]MoveCheck, error) {
	t, err := s.client.Task.Query().Where(enttask.ID(taskID), enttask.TenantID(tenantID), enttask.ProjectID(projectID)).Only(ctx)
	if ent.IsNotFound(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load task: %w", err)
	}
	p, err := s.client.Project.Query().Where(entproject.ID(projectID), entproject.TenantID(tenantID)).Only(ctx)
	if err != nil {
		return nil, fmt.Errorf("load project: %w", err)
	}
	statuses := statusesOf(p.Metadata)
	rules := statusRulesOf(p.Metadata)
	if !rulesOn {
		rules = StatusRules{}
	}
	f := factsOf(t)
	needCounts := false
	for _, st := range statuses {
		needCounts = needCounts || rules.NeedsCounts(t.Status, st.Key)
	}
	if needCounts {
		if f.Assignees, err = s.client.TaskAssignee.Query().Where(entassignee.TenantID(tenantID), entassignee.TaskID(taskID)).Count(ctx); err != nil {
			return nil, fmt.Errorf("count assignees: %w", err)
		}
		if f.Comments, err = s.client.Comment.Query().Where(entcomment.TenantID(tenantID), entcomment.TaskID(taskID)).Count(ctx); err != nil {
			return nil, fmt.Errorf("count comments: %w", err)
		}
	}
	labels := statusLabels(statuses)
	out := make([]MoveCheck, 0, len(statuses))
	for _, st := range statuses {
		if st.Key == t.Status {
			continue
		}
		c := MoveCheck{Status: st.Key, Allowed: true, Missing: []string{}}
		if b := EvaluateMove(rules, t.Status, st.Key, f, labels); b != nil {
			c = MoveCheck{Status: st.Key, Reason: b.Reason, Missing: b.Missing, Message: b.Message}
		}
		out = append(out, c)
	}
	return out, nil
}

// DateShift moves one task's dates: ShiftDays moves both dates by that many days, or StartDate
// and DueDate set new dates ("" clears; absent keeps).
type DateShift struct {
	TaskID    uuid.UUID `json:"task_id"`
	ShiftDays *int      `json:"shift_days,omitempty"`
	StartDate *Date     `json:"start_date,omitempty"`
	DueDate   *Date     `json:"due_date,omitempty"`
}

// MaxDateShifts bounds one change request's shifts.
const MaxDateShifts = 200

// ValidateDateShifts checks shifts are well formed and every task is in the project (one query).
func ValidateDateShifts(ctx context.Context, client *ent.Client, tenantID, projectID uuid.UUID, shifts []DateShift) error {
	if len(shifts) > MaxDateShifts {
		return ErrValidation(fmt.Sprintf("a change request shifts at most %d tasks", MaxDateShifts))
	}
	seen := map[uuid.UUID]bool{}
	ids := make([]uuid.UUID, 0, len(shifts))
	for _, sh := range shifts {
		if sh.TaskID == uuid.Nil {
			return ErrValidation("every date shift needs a task_id")
		}
		if seen[sh.TaskID] {
			return ErrValidation(fmt.Sprintf("task %s is shifted twice", sh.TaskID))
		}
		seen[sh.TaskID] = true
		ids = append(ids, sh.TaskID)
		if sh.ShiftDays == nil && sh.StartDate == nil && sh.DueDate == nil {
			return ErrValidation("every date shift needs shift_days or a start_date or due_date")
		}
		if sh.ShiftDays != nil && (sh.StartDate != nil || sh.DueDate != nil) {
			return ErrValidation("a date shift takes shift_days or dates, not both")
		}
		if sh.ShiftDays != nil && (*sh.ShiftDays == 0 || *sh.ShiftDays > 3650 || *sh.ShiftDays < -3650) {
			return ErrValidation("shift_days must be a non-zero number of days within ten years")
		}
	}
	if len(ids) == 0 {
		return nil
	}
	n, err := client.Task.Query().Where(enttask.TenantID(tenantID), enttask.ProjectID(projectID), enttask.IDIn(ids...)).Count(ctx)
	if err != nil {
		return fmt.Errorf("check shifted tasks: %w", err)
	}
	if n != len(ids) {
		return ErrValidation("some shifted tasks are not in this project")
	}
	return nil
}

// ShiftDatesTx applies date shifts inside the caller's transaction (a change request approval):
// every task is locked and moved, then the finish-to-start links of each moved task are checked
// against the new plan as a whole (so moving a chain together is not a conflict). A broken link
// is *ScheduleConflict. ref is recorded in each task's activity entry. It returns the moved tasks.
func ShiftDatesTx(ctx context.Context, tx *ent.Tx, tenantID, projectID uuid.UUID, shifts []DateShift, ref map[string]any) ([]*ent.Task, error) {
	moved := make([]*ent.Task, 0, len(shifts))
	for _, sh := range shifts {
		if err := lockTask(ctx, tx, tenantID, projectID, sh.TaskID); err != nil {
			return nil, err
		}
		t, err := tx.Task.Query().Where(enttask.ID(sh.TaskID)).Only(ctx)
		if err != nil {
			return nil, fmt.Errorf("fetch task: %w", err)
		}
		start, due := t.StartDate, t.DueDate
		if sh.ShiftDays != nil {
			if start != nil {
				v := start.AddDate(0, 0, *sh.ShiftDays)
				start = &v
			}
			if !due.IsZero() {
				due = due.AddDate(0, 0, *sh.ShiftDays)
			}
		}
		if sh.StartDate != nil {
			start = nil
			if !sh.StartDate.IsZero() {
				v := sh.StartDate.Time
				start = &v
			}
		}
		if sh.DueDate != nil {
			due = sh.DueDate.Time
		}
		if start != nil && !due.IsZero() && dayOf(due).Before(dayOf(*start)) {
			return nil, ErrValidation(fmt.Sprintf("task %q would be due before it starts", t.Title))
		}
		u := t.Update()
		changes := map[string]any{"rescheduled": true}
		for k, v := range ref {
			changes[k] = v
		}
		if start == nil {
			u.ClearStartDate()
			changes["start_date"] = nil
		} else {
			u.SetStartDate(*start)
			changes["start_date"] = start.Format(time.DateOnly)
		}
		if due.IsZero() {
			u.ClearDueDate()
			changes["due_date"] = nil
		} else {
			u.SetDueDate(due)
			changes["due_date"] = due.Format(time.DateOnly)
		}
		if t, err = u.Save(ctx); err != nil {
			return nil, fmt.Errorf("shift task dates: %w", err)
		}
		if err := activity.Record(ctx, tx, activity.Entry{
			TenantID: tenantID, ProjectID: activity.Ref(projectID), TaskID: activity.Ref(t.ID),
			EntityType: "task", EntityID: t.ID, Action: activity.ActionUpdated, Changes: changes,
		}); err != nil {
			return nil, err
		}
		moved = append(moved, t)
	}
	var blocking []uuid.UUID
	for _, t := range moved {
		b, err := scheduleBlockers(ctx, tx, tenantID, t.ID, startOf(t.StartDate, t.DueDate), t.DueDate)
		if err != nil {
			return nil, err
		}
		blocking = append(blocking, b...)
	}
	if len(blocking) > 0 {
		return nil, &ScheduleConflict{BlockingTaskIDs: blocking}
	}
	return moved, nil
}

// EmitUpdated sends the live "task updated" hint for tasks changed outside this service (after
// the caller's transaction committed).
func (s *Service) EmitUpdated(ctx context.Context, tenantID uuid.UUID, moved []*ent.Task) {
	for _, t := range moved {
		s.emit(ctx, tenantID, realtime.TaskUpdated, t)
	}
}
