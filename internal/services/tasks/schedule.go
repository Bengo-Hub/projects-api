package tasks

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/bengobox/projects-service/internal/ent"
	"github.com/bengobox/projects-service/internal/ent/predicate"
	enttask "github.com/bengobox/projects-service/internal/ent/task"
	entassignee "github.com/bengobox/projects-service/internal/ent/taskassignee"
	enttaskdep "github.com/bengobox/projects-service/internal/ent/taskdependency"
	"github.com/bengobox/projects-service/internal/platform/realtime"
	"github.com/bengobox/projects-service/internal/services/activity"
	"github.com/bengobox/projects-service/internal/services/txn"
)

// dueRange bounds the due date, both ends inclusive; a task without a due date never matches a
// bounded range.
func dueRange(from, to *time.Time) []predicate.Task {
	var out []predicate.Task
	if from != nil {
		out = append(out, enttask.DueDateGTE(*from))
	}
	if to != nil {
		out = append(out, enttask.DueDateLTE(*to))
	}
	return out
}

// Idempotent status changes: an offline client replays its queued status change with the same
// Idempotency-Key, and a key seen on this task within idempotencyWindow is answered with the task
// as it is, without a second change, event or activity entry. Keys live in the task's metadata
// under status_mutations ({key: RFC 3339 time}), pruned on every write and capped at
// maxKeptMutations, and the task row is locked (SELECT ... FOR UPDATE) for the check, so two
// replays racing on different pods still apply once.
const (
	idempotencyWindow = 24 * time.Hour
	maxKeptMutations  = 50
	maxKeyLength      = 128
)

// ChangeStatus moves a task to another of the project's statuses (a board drag). key is the
// optional Idempotency-Key; replayed reports a duplicate that changed nothing.
func (s *Service) ChangeStatus(ctx context.Context, tenantID, projectID, taskID uuid.UUID, status, key string) (task *ent.Task, replayed bool, err error) {
	status = strings.TrimSpace(status)
	if status == "" {
		return nil, false, ErrValidation("status is required")
	}
	key = strings.TrimSpace(key)
	if len(key) > maxKeyLength {
		return nil, false, ErrValidation(fmt.Sprintf("Idempotency-Key is at most %d characters", maxKeyLength))
	}
	moved := false
	err = txn.Run(ctx, s.client, func(tx *ent.Tx) error {
		if err := lockTask(ctx, tx, tenantID, projectID, taskID); err != nil {
			return err
		}
		t, err := tx.Task.Query().Where(enttask.ID(taskID)).Only(ctx)
		if err != nil {
			return fmt.Errorf("fetch task: %w", err)
		}
		now := time.Now().UTC()
		keys := recentMutations(t.Metadata, metaStatusMutations, now)
		if key != "" {
			if _, seen := keys[key]; seen {
				replayed = true
				task = t
				return loadAssignees(ctx, tx, task)
			}
		}
		p, err := projectForTask(ctx, tx, tenantID, projectID)
		if err != nil {
			return err
		}
		changes := map[string]any{}
		u := t.Update()
		if err := applyStatus(ctx, tx, p, t, u, status, changes); err != nil {
			return err
		}
		if key != "" {
			keys[key] = now
			meta := make(map[string]any, len(t.Metadata)+1)
			for k, v := range t.Metadata {
				meta[k] = v
			}
			meta[metaStatusMutations] = keepNewest(keys, maxKeptMutations)
			u.SetMetadata(meta)
		}
		if task, err = u.Save(ctx); err != nil {
			return fmt.Errorf("change status: %w", err)
		}
		if err := loadAssignees(ctx, tx, task); err != nil {
			return err
		}
		if len(changes) == 0 {
			return nil // already in that status
		}
		moved = true
		return activity.Record(ctx, tx, activity.Entry{
			TenantID: tenantID, ProjectID: activity.Ref(projectID), TaskID: activity.Ref(taskID),
			EntityType: "task", EntityID: taskID, Action: activity.ActionUpdated, Changes: changes,
		})
	})
	if err != nil {
		return nil, false, err
	}
	if moved {
		s.emit(ctx, tenantID, realtime.TaskMoved, task)
	}
	return task, replayed, nil
}

// lockTask locks the task row for the rest of the transaction, ErrNotFound when the project has
// no such task.
func lockTask(ctx context.Context, tx *ent.Tx, tenantID, projectID, taskID uuid.UUID) error {
	rows, err := tx.QueryContext(ctx, `SELECT id FROM tasks WHERE id = $1 AND tenant_id = $2 AND project_id = $3 FOR UPDATE`,
		taskID, tenantID, projectID)
	if err != nil {
		return fmt.Errorf("lock task: %w", err)
	}
	found := rows.Next()
	if err := rows.Close(); err != nil {
		return fmt.Errorf("lock task: %w", err)
	}
	if !found {
		return ErrNotFound
	}
	return nil
}

func loadAssignees(ctx context.Context, tx *ent.Tx, t *ent.Task) error {
	rows, err := tx.TaskAssignee.Query().Where(entassignee.TenantID(t.TenantID), entassignee.TaskID(t.ID)).
		Order(assigneeOrderBy()...).All(ctx)
	if err != nil {
		return fmt.Errorf("load assignees: %w", err)
	}
	t.Edges.Assignees = rows
	return nil
}

// recentMutations reads the idempotency keys a task's metadata keeps under field
// (status_mutations, checklist_mutations) still inside the window.
func recentMutations(meta map[string]any, field string, now time.Time) map[string]time.Time {
	out := map[string]time.Time{}
	raw, _ := meta[field].(map[string]any)
	for k, v := range raw {
		s, _ := v.(string)
		at, err := time.Parse(time.RFC3339Nano, s)
		if err != nil || now.Sub(at) > idempotencyWindow {
			continue
		}
		out[k] = at
	}
	return out
}

// keepNewest returns at most n keys, the most recent, as stored in metadata.
func keepNewest(keys map[string]time.Time, n int) map[string]any {
	type kv struct {
		k  string
		at time.Time
	}
	list := make([]kv, 0, len(keys))
	for k, at := range keys {
		list = append(list, kv{k, at})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].at.After(list[j].at) })
	if len(list) > n {
		list = list[:n]
	}
	out := make(map[string]any, len(list))
	for _, e := range list {
		out[e.k] = e.at.Format(time.RFC3339Nano)
	}
	return out
}

// RescheduleInput moves a task's dates (a Gantt or calendar drag). A nil date stays as it is.
type RescheduleInput struct {
	StartDate *Date `json:"start_date"`
	DueDate   *Date `json:"due_date"`
}

// ScheduleConflict refuses a move that breaks a finish-to-start dependency: the task would start
// before a predecessor finishes, or finish after a successor starts. BlockingTaskIDs names them.
type ScheduleConflict struct {
	BlockingTaskIDs []uuid.UUID
}

func (e *ScheduleConflict) Error() string {
	return "the new dates break a finish-to-start dependency"
}

// finishToStart reports whether a dependency type means the dependent task starts after the other
// finishes: FS, and "blocks" (the original default).
func finishToStart(depType string) bool {
	switch strings.ToUpper(strings.TrimSpace(depType)) {
	case "", "FS", "BLOCKS", "FINISH_TO_START":
		return true
	}
	return false
}

// startOf is when a task starts for scheduling: its start date, else its due date (a one-day
// task); nil when it has neither.
func startOf(start *time.Time, due time.Time) *time.Time {
	if start != nil && !start.IsZero() {
		return start
	}
	if !due.IsZero() {
		return &due
	}
	return nil
}

func dayOf(t time.Time) time.Time {
	y, m, d := t.UTC().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// Reschedule sets a task's start and due dates after checking its finish-to-start dependencies
// both ways (a same-day hand-over is allowed). A conflict is *ScheduleConflict.
func (s *Service) Reschedule(ctx context.Context, tenantID, projectID, taskID uuid.UUID, in RescheduleInput) (*ent.Task, error) {
	if in.StartDate == nil && in.DueDate == nil {
		return nil, ErrValidation("start_date or due_date is required")
	}
	var task *ent.Task
	err := txn.Run(ctx, s.client, func(tx *ent.Tx) error {
		if err := lockTask(ctx, tx, tenantID, projectID, taskID); err != nil {
			return err
		}
		t, err := tx.Task.Query().Where(enttask.ID(taskID)).Only(ctx)
		if err != nil {
			return fmt.Errorf("fetch task: %w", err)
		}
		start, due := t.StartDate, t.DueDate
		if in.StartDate != nil {
			start = nil
			if !in.StartDate.IsZero() {
				v := in.StartDate.Time
				start = &v
			}
		}
		if in.DueDate != nil {
			due = in.DueDate.Time
		}
		if start != nil && !due.IsZero() && dayOf(due).Before(dayOf(*start)) {
			return ErrValidation("due date cannot be before the start date")
		}
		if blocking, err := scheduleBlockers(ctx, tx, tenantID, taskID, startOf(start, due), due); err != nil {
			return err
		} else if len(blocking) > 0 {
			return &ScheduleConflict{BlockingTaskIDs: blocking}
		}
		changes := map[string]any{}
		u := t.Update()
		if in.StartDate != nil {
			if start == nil {
				u.ClearStartDate()
				changes["start_date"] = nil
			} else {
				u.SetStartDate(*start)
				changes["start_date"] = start.Format(time.DateOnly)
			}
		}
		if in.DueDate != nil {
			if due.IsZero() {
				u.ClearDueDate()
				changes["due_date"] = nil
			} else {
				u.SetDueDate(due)
				changes["due_date"] = due.Format(time.DateOnly)
			}
		}
		if task, err = u.Save(ctx); err != nil {
			return fmt.Errorf("reschedule task: %w", err)
		}
		if err := loadAssignees(ctx, tx, task); err != nil {
			return err
		}
		changes["rescheduled"] = true
		return activity.Record(ctx, tx, activity.Entry{
			TenantID: tenantID, ProjectID: activity.Ref(projectID), TaskID: activity.Ref(taskID),
			EntityType: "task", EntityID: taskID, Action: activity.ActionUpdated, Changes: changes,
		})
	})
	if err != nil {
		return nil, err
	}
	s.emit(ctx, tenantID, realtime.TaskUpdated, task)
	return task, nil
}

// scheduleBlockers returns the tasks whose finish-to-start link the new dates break: predecessors
// finishing after start, and successors starting before due. Two queries for the links, one for
// the tasks on their other end.
func scheduleBlockers(ctx context.Context, tx *ent.Tx, tenantID, taskID uuid.UUID, start *time.Time, due time.Time) ([]uuid.UUID, error) {
	deps, err := tx.TaskDependency.Query().
		Where(enttaskdep.Or(enttaskdep.TaskID(taskID), enttaskdep.DependsOnTaskID(taskID))).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("load dependencies: %w", err)
	}
	preds, succs := map[uuid.UUID]bool{}, map[uuid.UUID]bool{}
	var ids []uuid.UUID
	for _, d := range deps {
		if !finishToStart(d.DependencyType) {
			continue
		}
		if d.TaskID == taskID {
			preds[d.DependsOnTaskID] = true
			ids = append(ids, d.DependsOnTaskID)
		} else {
			succs[d.TaskID] = true
			ids = append(ids, d.TaskID)
		}
	}
	if len(ids) == 0 {
		return nil, nil
	}
	others, err := tx.Task.Query().Where(enttask.TenantID(tenantID), enttask.IDIn(ids...)).
		Select(enttask.FieldID, enttask.FieldStartDate, enttask.FieldDueDate).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("load linked tasks: %w", err)
	}
	var out []uuid.UUID
	for _, o := range others {
		switch {
		case preds[o.ID] && start != nil && !o.DueDate.IsZero() && dayOf(*start).Before(dayOf(o.DueDate)):
			out = append(out, o.ID)
		case succs[o.ID] && !due.IsZero():
			if st := startOf(o.StartDate, o.DueDate); st != nil && dayOf(*st).Before(dayOf(due)) {
				out = append(out, o.ID)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].String() < out[j].String() })
	return out, nil
}
