package tasks

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/bengobox/projects-service/internal/ent"
	"github.com/bengobox/projects-service/internal/ent/predicate"
	enttask "github.com/bengobox/projects-service/internal/ent/task"
	entassignee "github.com/bengobox/projects-service/internal/ent/taskassignee"
	"github.com/bengobox/projects-service/internal/services/notify"
)

// maxAssignees bounds the people on one task.
const maxAssignees = 20

// assignedTo matches the tenant's tasks the user is assigned to (primary or not). It reads as
// tasks.id IN (SELECT task_id FROM task_assignees WHERE tenant_id = ? AND user_id = ?), served by
// the (tenant_id, user_id) index.
func assignedTo(tenantID, userID uuid.UUID) predicate.Task {
	return enttask.HasAssigneesWith(entassignee.TenantID(tenantID), entassignee.UserID(userID))
}

// assigneeOrderBy lists the primary first, then by when they were added.
func assigneeOrderBy() []entassignee.OrderOption {
	return []entassignee.OrderOption{
		ent.Desc(entassignee.FieldIsPrimary), ent.Asc(entassignee.FieldCreatedAt), ent.Asc(entassignee.FieldID),
	}
}

// assigneeOrder is the eager-load option for a task's assignees, the primary first.
func assigneeOrder(q *ent.TaskAssigneeQuery) { q.Order(assigneeOrderBy()...) }

// metaStatusMutations is the server-owned task metadata key holding the idempotency keys of
// recent status changes (see ChangeStatus). Clients never write it.
const metaStatusMutations = "status_mutations"

// metaTimers is the server-owned key of running timers (timetracking: user id to start time),
// written only by the timer routes.
const metaTimers = "timers"

// serverOwnedMeta are the task metadata keys clients never write. The checklist and its
// idempotency keys change only through the checklist operations (see ApplyChecklist).
var serverOwnedMeta = []string{metaStatusMutations, metaTimers, MetaChecklist, metaChecklistMutations}

// clientMetadata is the metadata a client update leaves on a task: the client's map, with the
// server-owned keys kept from current and never taken from the client.
func clientMetadata(current, in map[string]any) map[string]any {
	out := make(map[string]any, len(in)+len(serverOwnedMeta))
	for k, v := range in {
		out[k] = v
	}
	for _, k := range serverOwnedMeta {
		delete(out, k)
		if v, ok := current[k]; ok {
			out[k] = v
		}
	}
	return out
}

// normalizeAssignees drops empty and repeated ids, keeping the first occurrence (the first id is
// the primary assignee).
func normalizeAssignees(ids []uuid.UUID) ([]uuid.UUID, error) {
	seen := make(map[uuid.UUID]bool, len(ids))
	out := make([]uuid.UUID, 0, len(ids))
	for _, id := range ids {
		if id == uuid.Nil || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	if len(out) > maxAssignees {
		return nil, ErrValidation(fmt.Sprintf("a task has at most %d assignees", maxAssignees))
	}
	return out, nil
}

// setAssignees makes ids (first is primary) the task's assignees inside tx and returns the rows,
// primary first, plus the people newly added. Every assignee must own or belong to the project.
func setAssignees(ctx context.Context, tx *ent.Tx, tenantID, projectID, taskID uuid.UUID, ids []uuid.UUID) ([]*ent.TaskAssignee, []uuid.UUID, error) {
	ids, err := normalizeAssignees(ids)
	if err != nil {
		return nil, nil, err
	}
	members, err := notify.Members(ctx, tx, tenantID, projectID, ids)
	if err != nil {
		return nil, nil, err
	}
	for _, id := range ids {
		if !members[id] {
			return nil, nil, ErrValidation(fmt.Sprintf("assignee %s is not a member of this project", id))
		}
	}
	current, err := tx.TaskAssignee.Query().Where(entassignee.TenantID(tenantID), entassignee.TaskID(taskID)).All(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("load assignees: %w", err)
	}
	have := make(map[uuid.UUID]*ent.TaskAssignee, len(current))
	for _, a := range current {
		have[a.UserID] = a
	}
	want := make(map[uuid.UUID]bool, len(ids))
	for _, id := range ids {
		want[id] = true
	}
	var gone []uuid.UUID
	for _, a := range current {
		if !want[a.UserID] {
			gone = append(gone, a.ID)
		}
	}
	if len(gone) > 0 {
		if _, err := tx.TaskAssignee.Delete().Where(entassignee.IDIn(gone...)).Exec(ctx); err != nil {
			return nil, nil, fmt.Errorf("remove assignees: %w", err)
		}
	}
	rows := make([]*ent.TaskAssignee, 0, len(ids))
	var added []uuid.UUID
	for i, id := range ids {
		primary := i == 0
		if a, ok := have[id]; ok {
			if a.IsPrimary != primary {
				if a, err = a.Update().SetIsPrimary(primary).Save(ctx); err != nil {
					return nil, nil, fmt.Errorf("update assignee: %w", err)
				}
			}
			rows = append(rows, a)
			continue
		}
		a, err := tx.TaskAssignee.Create().SetTenantID(tenantID).SetTaskID(taskID).SetUserID(id).SetIsPrimary(primary).Save(ctx)
		if err != nil {
			return nil, nil, fmt.Errorf("add assignee: %w", err)
		}
		rows = append(rows, a)
		added = append(added, id)
	}
	return rows, added, nil
}

// assigneeIDs lists the user ids of assignee rows.
func assigneeIDs(rows []*ent.TaskAssignee) []uuid.UUID {
	out := make([]uuid.UUID, len(rows))
	for i, a := range rows {
		out[i] = a.UserID
	}
	return out
}

// idStrings formats ids for activity entries.
func idStrings(ids []uuid.UUID) []string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = id.String()
	}
	return out
}

// IsAssignee reports whether the user is one of the task's assignees (for the *_own permissions).
func (s *Service) IsAssignee(ctx context.Context, tenantID, projectID, taskID, userID uuid.UUID) (bool, error) {
	return s.client.TaskAssignee.Query().Where(
		entassignee.TenantID(tenantID), entassignee.TaskID(taskID), entassignee.UserID(userID),
		entassignee.HasTaskWith(enttask.TenantID(tenantID), enttask.ProjectID(projectID)),
	).Exist(ctx)
}
