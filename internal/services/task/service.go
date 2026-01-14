package task

import (
	"context"
	"fmt"
	"time"

	"github.com/bengobox/projects-service/internal/ent"
	"github.com/bengobox/projects-service/internal/ent/project"
	"github.com/bengobox/projects-service/internal/ent/task"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// Service handles task operations with database persistence.
type Service struct {
	logger *zap.Logger
	db     *ent.Client
}

// NewService creates a new task service.
func NewService(logger *zap.Logger, db *ent.Client) *Service {
	return &Service{
		logger: logger,
		db:     db,
	}
}

// CreateParams holds parameters for creating a task.
type CreateParams struct {
	TenantID    uuid.UUID
	ProjectID   uuid.UUID
	Title       string
	Description string
	Status      string
	Priority    string
	AssigneeID  *uuid.UUID
	DueDate     *time.Time
	Metadata    map[string]any
}

// UpdateParams holds parameters for updating a task.
type UpdateParams struct {
	Title       *string
	Description *string
	Status      *string
	Priority    *string
	AssigneeID  *uuid.UUID
	DueDate     *time.Time
	Metadata    map[string]any
}

// ListParams holds parameters for listing tasks.
type ListParams struct {
	TenantID   uuid.UUID
	ProjectID  *uuid.UUID
	Status     string
	Priority   string
	AssigneeID *uuid.UUID
	Limit      int
	Offset     int
}

// Create creates a new task.
func (s *Service) Create(ctx context.Context, params CreateParams) (*ent.Task, error) {
	// Verify project exists and belongs to tenant
	exists, err := s.db.Project.Query().
		Where(
			project.ID(params.ProjectID),
			project.TenantID(params.TenantID),
		).
		Exist(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to verify project: %w", err)
	}
	if !exists {
		return nil, ErrProjectNotFound
	}

	builder := s.db.Task.Create().
		SetTenantID(params.TenantID).
		SetProjectID(params.ProjectID).
		SetTitle(params.Title)

	if params.Description != "" {
		builder.SetDescription(params.Description)
	}
	if params.Status != "" {
		builder.SetStatus(params.Status)
	}
	if params.Priority != "" {
		builder.SetPriority(params.Priority)
	}
	if params.AssigneeID != nil {
		builder.SetAssigneeID(*params.AssigneeID)
	}
	if params.DueDate != nil {
		builder.SetDueDate(*params.DueDate)
	}
	if params.Metadata != nil {
		builder.SetMetadata(params.Metadata)
	}

	t, err := builder.Save(ctx)
	if err != nil {
		s.logger.Error("failed to create task",
			zap.String("tenant_id", params.TenantID.String()),
			zap.String("project_id", params.ProjectID.String()),
			zap.String("title", params.Title),
			zap.Error(err),
		)
		return nil, fmt.Errorf("failed to create task: %w", err)
	}

	s.logger.Info("task created",
		zap.String("task_id", t.ID.String()),
		zap.String("project_id", params.ProjectID.String()),
	)

	return t, nil
}

// Get retrieves a task by ID within a tenant.
func (s *Service) Get(ctx context.Context, tenantID, taskID uuid.UUID) (*ent.Task, error) {
	t, err := s.db.Task.Query().
		Where(
			task.ID(taskID),
			task.TenantID(tenantID),
		).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to get task: %w", err)
	}
	return t, nil
}

// List retrieves tasks with optional filtering.
func (s *Service) List(ctx context.Context, params ListParams) ([]*ent.Task, int, error) {
	query := s.db.Task.Query().
		Where(task.TenantID(params.TenantID))

	if params.ProjectID != nil {
		query = query.Where(task.ProjectID(*params.ProjectID))
	}
	if params.Status != "" {
		query = query.Where(task.Status(params.Status))
	}
	if params.Priority != "" {
		query = query.Where(task.Priority(params.Priority))
	}
	if params.AssigneeID != nil {
		query = query.Where(task.AssigneeID(*params.AssigneeID))
	}

	// Get total count before pagination
	total, err := query.Clone().Count(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to count tasks: %w", err)
	}

	// Apply pagination
	if params.Limit > 0 {
		query = query.Limit(params.Limit)
	}
	if params.Offset > 0 {
		query = query.Offset(params.Offset)
	}

	// Order by created_at descending (newest first)
	query = query.Order(ent.Desc(task.FieldCreatedAt))

	tasks, err := query.All(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to list tasks: %w", err)
	}

	return tasks, total, nil
}

// ListByProject retrieves all tasks for a specific project.
func (s *Service) ListByProject(ctx context.Context, tenantID, projectID uuid.UUID, limit, offset int) ([]*ent.Task, int, error) {
	return s.List(ctx, ListParams{
		TenantID:  tenantID,
		ProjectID: &projectID,
		Limit:     limit,
		Offset:    offset,
	})
}

// Update updates an existing task.
func (s *Service) Update(ctx context.Context, tenantID, taskID uuid.UUID, params UpdateParams) (*ent.Task, error) {
	// Verify task exists and belongs to tenant
	_, err := s.Get(ctx, tenantID, taskID)
	if err != nil {
		return nil, err
	}

	builder := s.db.Task.UpdateOneID(taskID)

	if params.Title != nil {
		builder.SetTitle(*params.Title)
	}
	if params.Description != nil {
		builder.SetDescription(*params.Description)
	}
	if params.Status != nil {
		builder.SetStatus(*params.Status)
		// Set completed_at when status changes to "done"
		if *params.Status == "done" {
			builder.SetCompletedAt(time.Now())
		} else {
			builder.ClearCompletedAt()
		}
	}
	if params.Priority != nil {
		builder.SetPriority(*params.Priority)
	}
	if params.AssigneeID != nil {
		builder.SetAssigneeID(*params.AssigneeID)
	}
	if params.DueDate != nil {
		builder.SetDueDate(*params.DueDate)
	}
	if params.Metadata != nil {
		builder.SetMetadata(params.Metadata)
	}

	t, err := builder.Save(ctx)
	if err != nil {
		s.logger.Error("failed to update task",
			zap.String("task_id", taskID.String()),
			zap.Error(err),
		)
		return nil, fmt.Errorf("failed to update task: %w", err)
	}

	s.logger.Info("task updated",
		zap.String("task_id", t.ID.String()),
	)

	return t, nil
}

// Delete removes a task by ID.
func (s *Service) Delete(ctx context.Context, tenantID, taskID uuid.UUID) error {
	// Verify task exists and belongs to tenant
	_, err := s.Get(ctx, tenantID, taskID)
	if err != nil {
		return err
	}

	err = s.db.Task.DeleteOneID(taskID).Exec(ctx)
	if err != nil {
		s.logger.Error("failed to delete task",
			zap.String("task_id", taskID.String()),
			zap.Error(err),
		)
		return fmt.Errorf("failed to delete task: %w", err)
	}

	s.logger.Info("task deleted",
		zap.String("task_id", taskID.String()),
		zap.String("tenant_id", tenantID.String()),
	)

	return nil
}

// UpdateStatus is a convenience method to update only the task status.
func (s *Service) UpdateStatus(ctx context.Context, tenantID, taskID uuid.UUID, status string) (*ent.Task, error) {
	return s.Update(ctx, tenantID, taskID, UpdateParams{
		Status: &status,
	})
}

// Assign assigns a task to a user.
func (s *Service) Assign(ctx context.Context, tenantID, taskID, assigneeID uuid.UUID) (*ent.Task, error) {
	return s.Update(ctx, tenantID, taskID, UpdateParams{
		AssigneeID: &assigneeID,
	})
}
