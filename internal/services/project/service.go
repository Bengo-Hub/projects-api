package project

import (
	"context"
	"fmt"
	"time"

	"github.com/bengobox/projects-service/internal/ent"
	"github.com/bengobox/projects-service/internal/ent/project"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// Service handles project operations with database persistence.
type Service struct {
	logger *zap.Logger
	db     *ent.Client
}

// NewService creates a new project service.
func NewService(logger *zap.Logger, db *ent.Client) *Service {
	return &Service{
		logger: logger,
		db:     db,
	}
}

// CreateParams holds parameters for creating a project.
type CreateParams struct {
	TenantID    uuid.UUID
	Name        string
	Description string
	Status      string
	StartDate   *time.Time
	EndDate     *time.Time
	Budget      *float64
	Currency    string
	OwnerID     uuid.UUID
	Metadata    map[string]any
}

// UpdateParams holds parameters for updating a project.
type UpdateParams struct {
	Name        *string
	Description *string
	Status      *string
	StartDate   *time.Time
	EndDate     *time.Time
	Budget      *float64
	Currency    *string
	Metadata    map[string]any
}

// ListParams holds parameters for listing projects.
type ListParams struct {
	TenantID uuid.UUID
	Status   string
	OwnerID  *uuid.UUID
	Limit    int
	Offset   int
}

// Create creates a new project.
func (s *Service) Create(ctx context.Context, params CreateParams) (*ent.Project, error) {
	builder := s.db.Project.Create().
		SetTenantID(params.TenantID).
		SetName(params.Name).
		SetOwnerID(params.OwnerID)

	if params.Description != "" {
		builder.SetDescription(params.Description)
	}
	if params.Status != "" {
		builder.SetStatus(params.Status)
	}
	if params.StartDate != nil {
		builder.SetStartDate(*params.StartDate)
	}
	if params.EndDate != nil {
		builder.SetEndDate(*params.EndDate)
	}
	if params.Budget != nil {
		builder.SetBudget(*params.Budget)
	}
	if params.Currency != "" {
		builder.SetCurrency(params.Currency)
	}
	if params.Metadata != nil {
		builder.SetMetadata(params.Metadata)
	}

	p, err := builder.Save(ctx)
	if err != nil {
		s.logger.Error("failed to create project",
			zap.String("tenant_id", params.TenantID.String()),
			zap.String("name", params.Name),
			zap.Error(err),
		)
		return nil, fmt.Errorf("failed to create project: %w", err)
	}

	s.logger.Info("project created",
		zap.String("project_id", p.ID.String()),
		zap.String("tenant_id", params.TenantID.String()),
	)

	return p, nil
}

// Get retrieves a project by ID within a tenant.
func (s *Service) Get(ctx context.Context, tenantID, projectID uuid.UUID) (*ent.Project, error) {
	p, err := s.db.Project.Query().
		Where(
			project.ID(projectID),
			project.TenantID(tenantID),
		).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to get project: %w", err)
	}
	return p, nil
}

// List retrieves projects with optional filtering.
func (s *Service) List(ctx context.Context, params ListParams) ([]*ent.Project, int, error) {
	query := s.db.Project.Query().
		Where(project.TenantID(params.TenantID))

	if params.Status != "" {
		query = query.Where(project.Status(params.Status))
	}
	if params.OwnerID != nil {
		query = query.Where(project.OwnerID(*params.OwnerID))
	}

	// Get total count before pagination
	total, err := query.Clone().Count(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to count projects: %w", err)
	}

	// Apply pagination
	if params.Limit > 0 {
		query = query.Limit(params.Limit)
	}
	if params.Offset > 0 {
		query = query.Offset(params.Offset)
	}

	// Order by created_at descending (newest first)
	query = query.Order(ent.Desc(project.FieldCreatedAt))

	projects, err := query.All(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to list projects: %w", err)
	}

	return projects, total, nil
}

// Update updates an existing project.
func (s *Service) Update(ctx context.Context, tenantID, projectID uuid.UUID, params UpdateParams) (*ent.Project, error) {
	// Verify project exists and belongs to tenant
	_, err := s.Get(ctx, tenantID, projectID)
	if err != nil {
		return nil, err
	}

	builder := s.db.Project.UpdateOneID(projectID)

	if params.Name != nil {
		builder.SetName(*params.Name)
	}
	if params.Description != nil {
		builder.SetDescription(*params.Description)
	}
	if params.Status != nil {
		builder.SetStatus(*params.Status)
	}
	if params.StartDate != nil {
		builder.SetStartDate(*params.StartDate)
	}
	if params.EndDate != nil {
		builder.SetEndDate(*params.EndDate)
	}
	if params.Budget != nil {
		builder.SetBudget(*params.Budget)
	}
	if params.Currency != nil {
		builder.SetCurrency(*params.Currency)
	}
	if params.Metadata != nil {
		builder.SetMetadata(params.Metadata)
	}

	p, err := builder.Save(ctx)
	if err != nil {
		s.logger.Error("failed to update project",
			zap.String("project_id", projectID.String()),
			zap.Error(err),
		)
		return nil, fmt.Errorf("failed to update project: %w", err)
	}

	s.logger.Info("project updated",
		zap.String("project_id", p.ID.String()),
	)

	return p, nil
}

// Delete removes a project by ID.
func (s *Service) Delete(ctx context.Context, tenantID, projectID uuid.UUID) error {
	// Verify project exists and belongs to tenant
	_, err := s.Get(ctx, tenantID, projectID)
	if err != nil {
		return err
	}

	err = s.db.Project.DeleteOneID(projectID).Exec(ctx)
	if err != nil {
		s.logger.Error("failed to delete project",
			zap.String("project_id", projectID.String()),
			zap.Error(err),
		)
		return fmt.Errorf("failed to delete project: %w", err)
	}

	s.logger.Info("project deleted",
		zap.String("project_id", projectID.String()),
		zap.String("tenant_id", tenantID.String()),
	)

	return nil
}
