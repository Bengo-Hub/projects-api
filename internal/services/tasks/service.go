package tasks

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	sharedcache "github.com/Bengo-Hub/cache"
	"github.com/bengobox/projects-service/internal/ent"
	entproject "github.com/bengobox/projects-service/internal/ent/project"
	enttask "github.com/bengobox/projects-service/internal/ent/task"
	enttaskdep "github.com/bengobox/projects-service/internal/ent/taskdependency"
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
	Title       string         `json:"title"`
	Description string         `json:"description"`
	Status      string         `json:"status"`
	Priority    string         `json:"priority"`
	AssigneeID  *uuid.UUID     `json:"assignee_id"`
	DueDate     *time.Time     `json:"due_date"`
	ParentID    *uuid.UUID     `json:"parent_id"`
	WbsCode     string         `json:"wbs_code"`
	Metadata    map[string]any `json:"metadata"`
}

// UpdateTaskInput holds the data needed to update a task.
type UpdateTaskInput struct {
	Title       *string        `json:"title"`
	Description *string        `json:"description"`
	Status      *string        `json:"status"`
	Priority    *string        `json:"priority"`
	AssigneeID  *uuid.UUID     `json:"assignee_id"`
	DueDate     *time.Time     `json:"due_date"`
	ParentID    *uuid.UUID     `json:"parent_id"`
	WbsCode     *string        `json:"wbs_code"`
	Metadata    map[string]any `json:"metadata"`
}

// ListTasksFilter holds filter options for listing tasks.
type ListTasksFilter struct {
	Status     string     `json:"status"`
	Priority   string     `json:"priority"`
	AssigneeID *uuid.UUID `json:"assignee_id"`
	Page       int        `json:"page"`
	PageSize   int        `json:"page_size"`
}

// AddDependencyInput holds data to add a task dependency.
type AddDependencyInput struct {
	DependsOnTaskID uuid.UUID `json:"depends_on_task_id"`
	DependencyType  string    `json:"dependency_type"`
}

// ListTasks returns paginated tasks for the given project.
func (s *Service) ListTasks(ctx context.Context, tenantID, projectID uuid.UUID, filter ListTasksFilter) ([]*ent.Task, int, error) {
	if filter.PageSize <= 0 {
		filter.PageSize = 50
	}
	if filter.Page <= 0 {
		filter.Page = 1
	}
	offset := (filter.Page - 1) * filter.PageSize

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
	total, err := q.Count(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("count tasks: %w", err)
	}
	items, err := q.Order(ent.Asc(enttask.FieldCreatedAt)).
		Offset(offset).Limit(filter.PageSize).All(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("list tasks: %w", err)
	}
	return items, total, nil
}

// GetTask fetches a single task with its edges.
func (s *Service) GetTask(ctx context.Context, tenantID, projectID, taskID uuid.UUID) (*ent.Task, error) {
	t, err := s.client.Task.Query().
		Where(enttask.ID(taskID), enttask.TenantID(tenantID), enttask.ProjectID(projectID)).
		WithDependencies().WithComments().
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get task: %w", err)
	}
	return t, nil
}

// CreateTask creates a new task, validating the project belongs to the tenant first.
func (s *Service) CreateTask(ctx context.Context, tenantID, projectID uuid.UUID, input CreateTaskInput) (*ent.Task, error) {
	if input.Title == "" {
		return nil, ErrValidation("title is required")
	}
	// Validate project belongs to tenant
	exists, err := s.client.Project.Query().
		Where(entproject.ID(projectID), entproject.TenantID(tenantID)).
		Exist(ctx)
	if err != nil {
		return nil, fmt.Errorf("validate project: %w", err)
	}
	if !exists {
		return nil, ErrNotFound
	}

	status := input.Status
	if status == "" {
		status = "todo"
	}
	priority := input.Priority
	if priority == "" {
		priority = "medium"
	}

	c := s.client.Task.Create().
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
		c = c.SetDueDate(*input.DueDate)
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

	task, err := c.Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("create task: %w", err)
	}
	return task, nil
}

// UpdateTask updates an existing task.
func (s *Service) UpdateTask(ctx context.Context, tenantID, projectID, taskID uuid.UUID, input UpdateTaskInput) (*ent.Task, error) {
	t, err := s.client.Task.Query().
		Where(enttask.ID(taskID), enttask.TenantID(tenantID), enttask.ProjectID(projectID)).Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("fetch task: %w", err)
	}
	u := t.Update()
	if input.Title != nil {
		u = u.SetTitle(*input.Title)
	}
	if input.Description != nil {
		u = u.SetDescription(*input.Description)
	}
	if input.Status != nil {
		u = u.SetStatus(*input.Status)
		if *input.Status == "done" {
			now := time.Now()
			u = u.SetCompletedAt(now)
		}
	}
	if input.Priority != nil {
		u = u.SetPriority(*input.Priority)
	}
	if input.AssigneeID != nil {
		u = u.SetAssigneeID(*input.AssigneeID)
	}
	if input.DueDate != nil {
		u = u.SetDueDate(*input.DueDate)
	}
	if input.ParentID != nil {
		u = u.SetParentID(*input.ParentID)
	}
	if input.WbsCode != nil {
		u = u.SetWbsCode(*input.WbsCode)
	}
	if input.Metadata != nil {
		u = u.SetMetadata(input.Metadata)
	}
	updated, err := u.Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("update task: %w", err)
	}
	return updated, nil
}

// DeleteTask deletes a task by ID.
func (s *Service) DeleteTask(ctx context.Context, tenantID, projectID, taskID uuid.UUID) error {
	n, err := s.client.Task.Delete().
		Where(enttask.ID(taskID), enttask.TenantID(tenantID), enttask.ProjectID(projectID)).Exec(ctx)
	if err != nil {
		return fmt.Errorf("delete task: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// AddDependency adds a dependency between two tasks, checking for circular deps via DFS.
func (s *Service) AddDependency(ctx context.Context, tenantID, taskID, dependsOnID uuid.UUID, depType string) error {
	if depType == "" {
		depType = "FS"
	}
	// Circular dependency check: does dependsOnID eventually depend on taskID?
	if err := s.checkCircular(ctx, dependsOnID, taskID); err != nil {
		return err
	}
	_, err := s.client.TaskDependency.Create().
		SetTaskID(taskID).
		SetDependsOnTaskID(dependsOnID).
		SetDependencyType(depType).
		Save(ctx)
	if err != nil {
		return fmt.Errorf("add dependency: %w", err)
	}
	return nil
}

// checkCircular performs DFS to detect if adding taskID→target would create a cycle.
func (s *Service) checkCircular(ctx context.Context, start, target uuid.UUID) error {
	visited := map[uuid.UUID]bool{}
	queue := []uuid.UUID{start}
	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]
		if curr == target {
			return ErrCircularDependency
		}
		if visited[curr] {
			continue
		}
		visited[curr] = true
		deps, err := s.client.TaskDependency.Query().
			Where(enttaskdep.TaskID(curr)).All(ctx)
		if err != nil {
			continue
		}
		for _, d := range deps {
			queue = append(queue, d.DependsOnTaskID)
		}
	}
	return nil
}

// RemoveDependency removes a dependency between two tasks.
func (s *Service) RemoveDependency(ctx context.Context, tenantID, taskID, dependsOnID uuid.UUID) error {
	n, err := s.client.TaskDependency.Delete().
		Where(enttaskdep.TaskID(taskID), enttaskdep.DependsOnTaskID(dependsOnID)).Exec(ctx)
	if err != nil {
		return fmt.Errorf("remove dependency: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// GetGanttData returns tasks with their dependencies for Gantt chart rendering.
func (s *Service) GetGanttData(ctx context.Context, tenantID, projectID uuid.UUID) ([]*ent.Task, error) {
	tasks, err := s.client.Task.Query().
		Where(enttask.TenantID(tenantID), enttask.ProjectID(projectID)).
		WithDependencies().
		Order(ent.Asc(enttask.FieldCreatedAt)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("get gantt data: %w", err)
	}
	return tasks, nil
}
