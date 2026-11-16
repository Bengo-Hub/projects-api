package tasks

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"

	"github.com/google/uuid"

	"github.com/bengobox/projects-service/internal/ent"
	entproject "github.com/bengobox/projects-service/internal/ent/project"
	enttask "github.com/bengobox/projects-service/internal/ent/task"
	"github.com/bengobox/projects-service/internal/services/activity"
	"github.com/bengobox/projects-service/internal/services/txn"
)

// MetaStatuses is the project metadata key holding the board's status columns. Only the statuses
// routes write it; the project update strips it from its metadata.
const MetaStatuses = "statuses"

// Status categories: every status falls in one, so reports and My Work can treat custom columns
// alike.
const (
	CategoryTodo       = "todo"
	CategoryInProgress = "in_progress"
	CategoryDone       = "done"
)

// StatusDone is the one status that completes a task (progress 100, completed_at set). Earned
// value, the trend and the project summary count it, so every project keeps it.
const StatusDone = "done"

// maxStatuses bounds a project's columns.
const maxStatuses = 20

// Status is one board column.
type Status struct {
	Key      string `json:"key"`
	Label    string `json:"label"`
	Category string `json:"category"`
	Order    int    `json:"order"`
}

// DefaultStatuses is the set a project uses until someone changes it.
func DefaultStatuses() []Status {
	return []Status{
		{Key: "todo", Label: "To do", Category: CategoryTodo, Order: 0},
		{Key: "in_progress", Label: "In progress", Category: CategoryInProgress, Order: 1},
		{Key: "review", Label: "In review", Category: CategoryInProgress, Order: 2},
		{Key: StatusDone, Label: "Done", Category: CategoryDone, Order: 3},
	}
}

var statusKey = regexp.MustCompile(`^[a-z][a-z0-9_]{0,39}$`)

var categories = map[string]bool{CategoryTodo: true, CategoryInProgress: true, CategoryDone: true}

// ValidateStatuses checks a column set and returns it sorted by order (ties by position): keys
// are lower case snake case and unique, labels are set, categories known, and "done" is present
// in the done category.
func ValidateStatuses(in []Status) ([]Status, error) {
	if len(in) == 0 {
		return nil, ErrValidation("at least one status is required")
	}
	if len(in) > maxStatuses {
		return nil, ErrValidation(fmt.Sprintf("a project has at most %d statuses", maxStatuses))
	}
	out := make([]Status, len(in))
	copy(out, in)
	seen := map[string]bool{}
	hasDone := false
	for i, s := range out {
		if !statusKey.MatchString(s.Key) {
			return nil, ErrValidation(fmt.Sprintf("status key %q must be lower case letters, digits and underscores", s.Key))
		}
		if seen[s.Key] {
			return nil, ErrValidation(fmt.Sprintf("status key %q is listed twice", s.Key))
		}
		seen[s.Key] = true
		if s.Label == "" || len([]rune(s.Label)) > 60 {
			return nil, ErrValidation(fmt.Sprintf("status %q needs a label of at most 60 characters", s.Key))
		}
		if !categories[s.Category] {
			return nil, ErrValidation(fmt.Sprintf("status %q: category must be todo, in_progress or done", s.Key))
		}
		if s.Key == StatusDone {
			if s.Category != CategoryDone {
				return nil, ErrValidation(`status "done" must be in the done category`)
			}
			hasDone = true
		}
		out[i] = s
	}
	if !hasDone {
		return nil, ErrValidation(`the statuses must include "done"`)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Order < out[j].Order })
	for i := range out {
		out[i].Order = i
	}
	return out, nil
}

// statusesOf reads a project's columns from its metadata, the default set when none are stored
// (or the stored value no longer validates).
func statusesOf(meta map[string]any) []Status {
	raw, ok := meta[MetaStatuses]
	if !ok || raw == nil {
		return DefaultStatuses()
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return DefaultStatuses()
	}
	var list []Status
	if json.Unmarshal(b, &list) != nil {
		return DefaultStatuses()
	}
	valid, err := ValidateStatuses(list)
	if err != nil {
		return DefaultStatuses()
	}
	return valid
}

func hasStatus(list []Status, key string) bool {
	for _, s := range list {
		if s.Key == key {
			return true
		}
	}
	return false
}

// projectForTask loads the project a task write needs (name for notices, metadata for statuses).
func projectForTask(ctx context.Context, tx *ent.Tx, tenantID, projectID uuid.UUID) (*ent.Project, error) {
	p, err := tx.Project.Query().Where(entproject.ID(projectID), entproject.TenantID(tenantID)).Only(ctx)
	if ent.IsNotFound(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load project: %w", err)
	}
	return p, nil
}

// checkStatus refuses a status the project does not have.
func checkStatus(p *ent.Project, status string) error {
	if !hasStatus(statusesOf(p.Metadata), status) {
		return ErrValidation(fmt.Sprintf("status %q is not one of this project's statuses", status))
	}
	return nil
}

// GetStatuses returns the project's board columns in order.
func (s *Service) GetStatuses(ctx context.Context, tenantID, projectID uuid.UUID) ([]Status, error) {
	p, err := s.client.Project.Query().Where(entproject.ID(projectID), entproject.TenantID(tenantID)).
		Select(entproject.FieldMetadata).Only(ctx)
	if ent.IsNotFound(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load project statuses: %w", err)
	}
	return statusesOf(p.Metadata), nil
}

// SetStatuses replaces the project's board columns. A status still held by tasks cannot be
// removed: move those tasks first (the error names the keys).
func (s *Service) SetStatuses(ctx context.Context, tenantID, projectID uuid.UUID, in []Status) ([]Status, error) {
	list, err := ValidateStatuses(in)
	if err != nil {
		return nil, err
	}
	err = txn.Run(ctx, s.client, func(tx *ent.Tx) error {
		p, err := projectForTask(ctx, tx, tenantID, projectID)
		if err != nil {
			return err
		}
		keys := make([]string, len(list))
		for i, st := range list {
			keys[i] = st.Key
		}
		var used []struct {
			Status string `json:"status"`
		}
		if err := tx.Task.Query().Where(enttask.TenantID(tenantID), enttask.ProjectID(projectID), enttask.StatusNotIn(keys...)).
			GroupBy(enttask.FieldStatus).Scan(ctx, &used); err != nil {
			return fmt.Errorf("check statuses in use: %w", err)
		}
		if len(used) > 0 {
			held := make([]string, len(used))
			for i, u := range used {
				held[i] = u.Status
			}
			sort.Strings(held)
			return ErrValidation(fmt.Sprintf("tasks still use these statuses; move them first: %v", held))
		}
		meta := make(map[string]any, len(p.Metadata)+1)
		for k, v := range p.Metadata {
			meta[k] = v
		}
		meta[MetaStatuses] = list
		if err := p.Update().SetMetadata(meta).Exec(ctx); err != nil {
			return fmt.Errorf("save statuses: %w", err)
		}
		return activity.Record(ctx, tx, activity.Entry{
			TenantID: tenantID, ProjectID: activity.Ref(projectID), EntityType: "project", EntityID: projectID,
			Action: activity.ActionUpdated, Changes: map[string]any{"statuses": keys},
		})
	})
	if err != nil {
		return nil, err
	}
	return list, nil
}

// StatusCount is one board column's task count.
type StatusCount struct {
	Status string `json:"status"`
	Count  int    `json:"count"`
}

// BoardCounts counts the project's tasks per status in one GROUP BY: every configured column (zero
// included) in board order, then any status tasks hold that is no longer configured. assignee
// limits the count to one person's tasks (callers holding only tasks.view_own).
func (s *Service) BoardCounts(ctx context.Context, tenantID, projectID uuid.UUID, assignee *uuid.UUID) ([]StatusCount, error) {
	statuses, err := s.GetStatuses(ctx, tenantID, projectID)
	if err != nil {
		return nil, err
	}
	q := s.client.Task.Query().Where(enttask.TenantID(tenantID), enttask.ProjectID(projectID))
	if assignee != nil {
		q = q.Where(assignedTo(tenantID, *assignee))
	}
	var rows []StatusCount
	if err := q.GroupBy(enttask.FieldStatus).Aggregate(ent.As(ent.Count(), "count")).Scan(ctx, &rows); err != nil {
		return nil, fmt.Errorf("board counts: %w", err)
	}
	byStatus := make(map[string]int, len(rows))
	for _, r := range rows {
		byStatus[r.Status] = r.Count
	}
	out := make([]StatusCount, 0, len(statuses)+len(rows))
	for _, st := range statuses {
		out = append(out, StatusCount{Status: st.Key, Count: byStatus[st.Key]})
		delete(byStatus, st.Key)
	}
	extra := make([]string, 0, len(byStatus))
	for k := range byStatus {
		extra = append(extra, k)
	}
	sort.Strings(extra)
	for _, k := range extra {
		out = append(out, StatusCount{Status: k, Count: byStatus[k]})
	}
	return out, nil
}
