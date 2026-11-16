package tasks

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	sharedcache "github.com/Bengo-Hub/cache"
	"github.com/bengobox/projects-service/internal/ent"
	entcomment "github.com/bengobox/projects-service/internal/ent/comment"
	entproject "github.com/bengobox/projects-service/internal/ent/project"
	enttask "github.com/bengobox/projects-service/internal/ent/task"
	enttaskdep "github.com/bengobox/projects-service/internal/ent/taskdependency"
	"github.com/bengobox/projects-service/internal/services/activity"
	"github.com/bengobox/projects-service/internal/services/txn"
)

// Service handles task CRUD and dependency operations.
type Service struct {
	client *ent.Client
	cache  *sharedcache.Aside
	log    *zap.Logger
}

// NewService creates a new tasks service.
func NewService(client *ent.Client, cache *sharedcache.Aside, log *zap.Logger) *Service {
	return &Service{client: client, cache: cache, log: log.Named("tasks.svc")}
}

// CreateTaskInput holds the data needed to create a task.
type CreateTaskInput struct {
	Title          string         `json:"title"`
	Description    string         `json:"description"`
	Status         string         `json:"status"`
	Priority       string         `json:"priority"`
	AssigneeID     *uuid.UUID     `json:"assignee_id"`
	DueDate        *Date          `json:"due_date"`
	StartDate      *Date          `json:"start_date"`
	EstimatedHours *float64       `json:"estimated_hours"`
	ProgressPct    *int           `json:"progress_pct"`
	ParentID       *uuid.UUID     `json:"parent_id"`
	WbsCode        string         `json:"wbs_code"`
	Metadata       map[string]any `json:"metadata"`
}

// UpdateTaskInput holds the data needed to update a task.
type UpdateTaskInput struct {
	Title          *string        `json:"title"`
	Description    *string        `json:"description"`
	Status         *string        `json:"status"`
	Priority       *string        `json:"priority"`
	AssigneeID     *uuid.UUID     `json:"assignee_id"`
	DueDate        *Date          `json:"due_date"`
	StartDate      *Date          `json:"start_date"`
	EstimatedHours *float64       `json:"estimated_hours"`
	ProgressPct    *int           `json:"progress_pct"`
	ParentID       *uuid.UUID     `json:"parent_id"`
	WbsCode        *string        `json:"wbs_code"`
	Metadata       map[string]any `json:"metadata"`
}

// ListTasksFilter holds filter options for listing tasks. Limit and Offset come from the shared
// pagination parser.
type ListTasksFilter struct {
	Status     string
	Priority   string
	AssigneeID *uuid.UUID
	Limit      int
	Offset     int
}

// AddDependencyInput holds data to add a task dependency.
type AddDependencyInput struct {
	DependsOnTaskID uuid.UUID `json:"depends_on_task_id"`
	DependencyType  string    `json:"dependency_type"`
}

// ListTasks returns one page of the project's tasks with the total.
func (s *Service) ListTasks(ctx context.Context, tenantID, projectID uuid.UUID, filter ListTasksFilter) ([]*ent.Task, int, error) {
	if filter.Limit <= 0 {
		filter.Limit = 20
	}
	q := s.client.Task.Query().Where(enttask.TenantID(tenantID), enttask.ProjectID(projectID))
	if filter.Status != "" {
		q = q.Where(enttask.Status(filter.Status))
	}
	if filter.Priority != "" {
		q = q.Where(enttask.Priority(filter.Priority))
	}
	if filter.AssigneeID != nil {
		q = q.Where(enttask.AssigneeID(*filter.AssigneeID))
	}
	total, err := q.Clone().Count(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("count tasks: %w", err)
	}
	items, err := q.Order(ent.Asc(enttask.FieldCreatedAt), ent.Asc(enttask.FieldID)).
		Offset(filter.Offset).Limit(filter.Limit).All(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("list tasks: %w", err)
	}
	return items, total, nil
}

// TaskDetail is a task with its dependencies (under edges), its latest comments oldest first and
// the task's comment count. Older comments come from the paged task comments route.
type TaskDetail struct {
	*ent.Task
	Comments      []*ent.Comment `json:"comments"`
	CommentsTotal int            `json:"comments_total"`
}

// GetTask fetches a single task with its dependencies and its latest 50 comments.
func (s *Service) GetTask(ctx context.Context, tenantID, projectID, taskID uuid.UUID) (*TaskDetail, error) {
	t, err := s.client.Task.Query().
		Where(enttask.ID(taskID), enttask.TenantID(tenantID), enttask.ProjectID(projectID)).
		WithDependencies().
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get task: %w", err)
	}
	cq := s.client.Comment.Query().Where(entcomment.TenantID(tenantID), entcomment.TaskID(taskID))
	total, err := cq.Clone().Count(ctx)
	if err != nil {
		return nil, fmt.Errorf("count task comments: %w", err)
	}
	latest, err := cq.Order(ent.Desc(entcomment.FieldCreatedAt)).Limit(taskCommentsLimit).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("task comments: %w", err)
	}
	slices.Reverse(latest)
	return &TaskDetail{Task: t, Comments: latest, CommentsTotal: total}, nil
}

// IsAssignee reports whether the user is the task's assignee (for the *_own permissions).
func (s *Service) IsAssignee(ctx context.Context, tenantID, projectID, taskID, userID uuid.UUID) (bool, error) {
	return s.client.Task.Query().Where(enttask.ID(taskID), enttask.TenantID(tenantID), enttask.ProjectID(projectID),
		enttask.AssigneeID(userID)).Exist(ctx)
}

// CreateTask creates a new task, validating the project belongs to the tenant first.
func (s *Service) CreateTask(ctx context.Context, tenantID, projectID uuid.UUID, input CreateTaskInput) (*ent.Task, error) {
	if input.Title == "" {
		return nil, ErrValidation("title is required")
	}
	status := input.Status
	if status == "" {
		status = "todo"
	}
	priority := input.Priority
	if priority == "" {
		priority = "medium"
	}
	var task *ent.Task
	err := txn.Run(ctx, s.client, func(tx *ent.Tx) error {
		exists, err := tx.Project.Query().Where(entproject.ID(projectID), entproject.TenantID(tenantID)).Exist(ctx)
		if err != nil {
			return fmt.Errorf("validate project: %w", err)
		}
		if !exists {
			return ErrNotFound
		}
		if input.ParentID != nil {
			ok, err := tx.Task.Query().Where(enttask.ID(*input.ParentID), enttask.TenantID(tenantID), enttask.ProjectID(projectID)).Exist(ctx)
			if err != nil {
				return fmt.Errorf("validate parent: %w", err)
			}
			if !ok {
				return ErrValidation("parent task not found in this project")
			}
		}
		c := tx.Task.Create().
			SetTenantID(tenantID).
			SetProjectID(projectID).
			SetTitle(input.Title).
			SetDescription(input.Description).
			SetStatus(status).
			SetPriority(priority)
		if input.AssigneeID != nil {
			c = c.SetAssigneeID(*input.AssigneeID)
		}
		if input.DueDate != nil {
			c = c.SetDueDate(input.DueDate.Time)
		}
		if input.StartDate != nil {
			c = c.SetStartDate(input.StartDate.Time)
		}
		if input.EstimatedHours != nil {
			c = c.SetEstimatedHours(*input.EstimatedHours)
		}
		if input.ProgressPct != nil {
			c = c.SetProgressPct(clampPct(*input.ProgressPct))
		}
		if status == "done" {
			c = c.SetProgressPct(100).SetCompletedAt(time.Now())
		}
		if input.ParentID != nil {
			c = c.SetParentID(*input.ParentID)
		}
		if input.WbsCode != "" {
			c = c.SetWbsCode(input.WbsCode)
		}
		if input.Metadata != nil {
			c = c.SetMetadata(input.Metadata)
		}
		if task, err = c.Save(ctx); err != nil {
			return fmt.Errorf("create task: %w", err)
		}
		changes := map[string]any{"title": task.Title, "status": task.Status}
		if input.AssigneeID != nil {
			changes["assignee_id"] = input.AssigneeID.String()
		}
		return activity.Record(ctx, tx, activity.Entry{
			TenantID: tenantID, ProjectID: activity.Ref(projectID), TaskID: activity.Ref(task.ID),
			EntityType: "task", EntityID: task.ID, Action: activity.ActionCreated, Changes: changes,
		})
	})
	if err != nil {
		return nil, err
	}
	return task, nil
}

// UpdateTask updates an existing task.
func (s *Service) UpdateTask(ctx context.Context, tenantID, projectID, taskID uuid.UUID, input UpdateTaskInput) (*ent.Task, error) {
	if input.ParentID != nil && *input.ParentID == taskID {
		return nil, ErrValidation("a task cannot be its own parent")
	}
	var updated *ent.Task
	err := txn.Run(ctx, s.client, func(tx *ent.Tx) error {
		t, err := tx.Task.Query().
			Where(enttask.ID(taskID), enttask.TenantID(tenantID), enttask.ProjectID(projectID)).Only(ctx)
		if err != nil {
			if ent.IsNotFound(err) {
				return ErrNotFound
			}
			return fmt.Errorf("fetch task: %w", err)
		}
		changes := map[string]any{}
		u := t.Update()
		if input.Title != nil {
			u = u.SetTitle(*input.Title)
			changes["title"] = *input.Title
		}
		if input.Description != nil {
			u = u.SetDescription(*input.Description)
			changes["description"] = "changed"
		}
		if input.Status != nil {
			u = u.SetStatus(*input.Status)
			changes["status"] = map[string]any{"from": t.Status, "to": *input.Status}
			if *input.Status == "done" {
				u = u.SetCompletedAt(time.Now()).SetProgressPct(100)
			}
		}
		if input.Priority != nil {
			u = u.SetPriority(*input.Priority)
			changes["priority"] = *input.Priority
		}
		if input.AssigneeID != nil {
			u = u.SetAssigneeID(*input.AssigneeID)
			changes["assignee_id"] = input.AssigneeID.String()
		}
		if input.DueDate != nil {
			u = u.SetDueDate(input.DueDate.Time)
			changes["due_date"] = input.DueDate.Format(time.DateOnly)
		}
		if input.StartDate != nil {
			u = u.SetStartDate(input.StartDate.Time)
			changes["start_date"] = input.StartDate.Format(time.DateOnly)
		}
		if input.EstimatedHours != nil {
			u = u.SetEstimatedHours(*input.EstimatedHours)
			changes["estimated_hours"] = *input.EstimatedHours
		}
		if input.ProgressPct != nil {
			u = u.SetProgressPct(clampPct(*input.ProgressPct))
			changes["progress_pct"] = clampPct(*input.ProgressPct)
		}
		if input.ParentID != nil {
			u = u.SetParentID(*input.ParentID)
			changes["parent_id"] = input.ParentID.String()
		}
		if input.WbsCode != nil {
			u = u.SetWbsCode(*input.WbsCode)
			changes["wbs_code"] = *input.WbsCode
		}
		if input.Metadata != nil {
			u = u.SetMetadata(input.Metadata)
			changes["metadata"] = "changed"
		}
		if updated, err = u.Save(ctx); err != nil {
			return fmt.Errorf("update task: %w", err)
		}
		return activity.Record(ctx, tx, activity.Entry{
			TenantID: tenantID, ProjectID: activity.Ref(projectID), TaskID: activity.Ref(taskID),
			EntityType: "task", EntityID: taskID, Action: activity.ActionUpdated, Changes: changes,
		})
	})
	if err != nil {
		return nil, err
	}
	return updated, nil
}

// DeleteTask deletes a task by ID.
func (s *Service) DeleteTask(ctx context.Context, tenantID, projectID, taskID uuid.UUID) error {
	return txn.Run(ctx, s.client, func(tx *ent.Tx) error {
		t, err := tx.Task.Query().
			Where(enttask.ID(taskID), enttask.TenantID(tenantID), enttask.ProjectID(projectID)).Only(ctx)
		if ent.IsNotFound(err) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("fetch task: %w", err)
		}
		if _, err := tx.TaskDependency.Delete().
			Where(enttaskdep.Or(enttaskdep.TaskID(taskID), enttaskdep.DependsOnTaskID(taskID))).Exec(ctx); err != nil {
			return fmt.Errorf("delete task dependencies: %w", err)
		}
		if err := tx.Task.DeleteOne(t).Exec(ctx); err != nil {
			return fmt.Errorf("delete task: %w", err)
		}
		return activity.Record(ctx, tx, activity.Entry{
			TenantID: tenantID, ProjectID: activity.Ref(projectID), EntityType: "task", EntityID: taskID,
			Action: activity.ActionDeleted, Changes: map[string]any{"title": t.Title},
		})
	})
}

// AddDependency records that taskID (in the project) depends on dependsOnID (any task of the
// tenant), refusing a dependency that would close a cycle.
func (s *Service) AddDependency(ctx context.Context, tenantID, projectID, taskID, dependsOnID uuid.UUID, depType string) error {
	if depType == "" {
		depType = "FS"
	}
	if taskID == dependsOnID {
		return ErrCircularDependency
	}
	return txn.Run(ctx, s.client, func(tx *ent.Tx) error {
		inProject, err := tx.Task.Query().Where(enttask.ID(taskID), enttask.TenantID(tenantID), enttask.ProjectID(projectID)).Exist(ctx)
		if err != nil {
			return fmt.Errorf("check task: %w", err)
		}
		other, err := tx.Task.Query().Where(enttask.ID(dependsOnID), enttask.TenantID(tenantID)).Exist(ctx)
		if err != nil {
			return fmt.Errorf("check task: %w", err)
		}
		if !inProject || !other {
			return ErrNotFound
		}
		// Adding "taskID depends on dependsOnID" closes a cycle when dependsOnID already depends,
		// directly or through others, on taskID.
		cycle, err := reachesInDB(ctx, tx, tenantID, dependsOnID, taskID)
		if err != nil {
			return err
		}
		if cycle {
			return ErrCircularDependency
		}
		if _, err := tx.TaskDependency.Create().
			SetTaskID(taskID).SetDependsOnTaskID(dependsOnID).SetDependencyType(depType).Save(ctx); err != nil {
			return fmt.Errorf("add dependency: %w", err)
		}
		return activity.Record(ctx, tx, activity.Entry{
			TenantID: tenantID, ProjectID: activity.Ref(projectID), TaskID: activity.Ref(taskID), EntityType: "task",
			EntityID: taskID, Action: activity.ActionUpdated,
			Changes: map[string]any{"dependency_added": dependsOnID.String(), "dependency_type": depType},
		})
	})
}

// reachesQuery walks the dependency graph from one task only (never the whole tenant), as one
// recursive CTE: $1 start, $2 target, $3 tenant, $4 depth bound. UNION drops rows already seen,
// so a cycle already in the data cannot loop, and EXISTS stops at the first hit.
const reachesQuery = `
WITH RECURSIVE reach(id, depth) AS (
	SELECT d.depends_on_task_id, 1
	FROM task_dependencies d
	JOIN tasks t ON t.id = d.task_id AND t.tenant_id = $3
	WHERE d.task_id = $1
	UNION
	SELECT d.depends_on_task_id, r.depth + 1
	FROM reach r
	JOIN task_dependencies d ON d.task_id = r.id
	JOIN tasks t ON t.id = d.task_id AND t.tenant_id = $3
	WHERE r.depth < $4
)
SELECT EXISTS (SELECT 1 FROM reach WHERE id = $2)`

// reachesInDB reports whether target is reachable from start along dependency edges.
func reachesInDB(ctx context.Context, tx *ent.Tx, tenantID, start, target uuid.UUID) (bool, error) {
	rows, err := tx.QueryContext(ctx, reachesQuery, start, target, tenantID, maxDependencyDepth)
	if err != nil {
		return false, fmt.Errorf("cycle check: %w", err)
	}
	defer rows.Close()
	var found bool
	if rows.Next() {
		if err := rows.Scan(&found); err != nil {
			return false, fmt.Errorf("cycle check: %w", err)
		}
	}
	return found, rows.Err()
}

// RemoveDependency removes a dependency of a task in the project.
func (s *Service) RemoveDependency(ctx context.Context, tenantID, projectID, taskID, dependsOnID uuid.UUID) error {
	return txn.Run(ctx, s.client, func(tx *ent.Tx) error {
		n, err := tx.TaskDependency.Delete().
			Where(enttaskdep.TaskID(taskID), enttaskdep.DependsOnTaskID(dependsOnID),
				enttaskdep.HasTaskWith(enttask.TenantID(tenantID), enttask.ProjectID(projectID))).Exec(ctx)
		if err != nil {
			return fmt.Errorf("remove dependency: %w", err)
		}
		if n == 0 {
			return ErrNotFound
		}
		return activity.Record(ctx, tx, activity.Entry{
			TenantID: tenantID, ProjectID: activity.Ref(projectID), TaskID: activity.Ref(taskID), EntityType: "task",
			EntityID: taskID, Action: activity.ActionUpdated, Changes: map[string]any{"dependency_removed": dependsOnID.String()},
		})
	})
}

// GanttData is the project's plan for the Gantt chart. Truncated is set when the project has more
// than ganttLimit tasks and only the first ganttLimit (oldest first) are returned.
type GanttData struct {
	Tasks     []*ent.Task `json:"data"`
	Truncated bool        `json:"truncated"`
	Limit     int         `json:"limit"`
}

// GetGanttData returns tasks with their dependencies for Gantt chart rendering, capped.
func (s *Service) GetGanttData(ctx context.Context, tenantID, projectID uuid.UUID) (GanttData, error) {
	tasks, err := s.client.Task.Query().
		Where(enttask.TenantID(tenantID), enttask.ProjectID(projectID)).
		WithDependencies().
		Order(ent.Asc(enttask.FieldCreatedAt), ent.Asc(enttask.FieldID)).
		Limit(ganttLimit + 1).
		All(ctx)
	if err != nil {
		return GanttData{}, fmt.Errorf("get gantt data: %w", err)
	}
	out := GanttData{Tasks: tasks, Limit: ganttLimit}
	if len(tasks) > ganttLimit {
		out.Tasks, out.Truncated = tasks[:ganttLimit], true
	}
	return out, nil
}

// clampPct keeps a progress percentage within 0-100.
func clampPct(p int) int {
	if p < 0 {
		return 0
	}
	if p > 100 {
		return 100
	}
	return p
}
