package projects

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	sharedcache "github.com/Bengo-Hub/cache"
	"github.com/bengobox/projects-service/internal/ent"
	entproject "github.com/bengobox/projects-service/internal/ent/project"
)

// Service handles project CRUD operations.
type Service struct {
	client *ent.Client
	cache  *sharedcache.Aside
	log    *zap.Logger
}

// NewService creates a new projects service.
func NewService(client *ent.Client, cache *sharedcache.Aside, log *zap.Logger) *Service {
	return &Service{client: client, cache: cache, log: log.Named("projects.svc")}
}

// CreateProjectInput holds the data needed to create a project.
type CreateProjectInput struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Status      string         `json:"status"`
	StartDate   *time.Time     `json:"start_date"`
	EndDate     *time.Time     `json:"end_date"`
	Budget      *float64       `json:"budget"`
	Currency    string         `json:"currency"`
	OwnerID     uuid.UUID      `json:"owner_id"`
	Metadata    map[string]any `json:"metadata"`
}

// UpdateProjectInput holds the data needed to update a project.
type UpdateProjectInput struct {
	Name        *string        `json:"name"`
	Description *string        `json:"description"`
	Status      *string        `json:"status"`
	StartDate   *time.Time     `json:"start_date"`
	EndDate     *time.Time     `json:"end_date"`
	Budget      *float64       `json:"budget"`
	Currency    *string        `json:"currency"`
	Metadata    map[string]any `json:"metadata"`
}

// ListProjectsFilter holds filter options for listing projects.
type ListProjectsFilter struct {
	Status string `json:"status"`
	Limit  int    `json:"limit"`
	Offset int    `json:"offset"`
}

// ListProjects returns a paginated list of projects for the given tenant.
func (s *Service) ListProjects(ctx context.Context, tenantID uuid.UUID, filter ListProjectsFilter) ([]*ent.Project, int, error) {
	if filter.Limit <= 0 {
		filter.Limit = 20
	}

	q := s.client.Project.Query().Where(entproject.TenantID(tenantID))
	if filter.Status != "" {
		q = q.Where(entproject.Status(filter.Status))
	}
	total, err := q.Clone().Count(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("count projects: %w", err)
	}
	items, err := q.Order(ent.Desc(entproject.FieldCreatedAt)).
		Offset(filter.Offset).Limit(filter.Limit).All(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("list projects: %w", err)
	}
	return items, total, nil
}

// GetProject fetches a single project with its edges.
func (s *Service) GetProject(ctx context.Context, tenantID, id uuid.UUID) (*ent.Project, error) {
	p, err := s.client.Project.Query().
		Where(entproject.ID(id), entproject.TenantID(tenantID)).
		WithTasks().WithMembers().WithMilestones().
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get project: %w", err)
	}
	return p, nil
}

// CreateProject creates a new project for the tenant.
func (s *Service) CreateProject(ctx context.Context, tenantID uuid.UUID, input CreateProjectInput) (*ent.Project, error) {
	if input.Name == "" {
		return nil, ErrValidation("name is required")
	}
	currency := input.Currency
	if currency == "" {
		currency = "KES"
	}
	status := input.Status
	if status == "" {
		status = "active"
	}
	c := s.client.Project.Create().
		SetTenantID(tenantID).
		SetName(input.Name).
		SetDescription(input.Description).
		SetStatus(status).
		SetCurrency(currency).
		SetOwnerID(input.OwnerID)
	if input.StartDate != nil {
		c = c.SetStartDate(*input.StartDate)
	}
	if input.EndDate != nil {
		c = c.SetEndDate(*input.EndDate)
	}
	if input.Budget != nil {
		c = c.SetBudget(*input.Budget)
	}
	if input.Metadata != nil {
		c = c.SetMetadata(input.Metadata)
	}
	p, err := c.Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("create project: %w", err)
	}
	s.log.Info("project created", zap.String("id", p.ID.String()), zap.String("tenant", tenantID.String()))
	return p, nil
}

// UpdateProject updates an existing project.
func (s *Service) UpdateProject(ctx context.Context, tenantID, id uuid.UUID, input UpdateProjectInput) (*ent.Project, error) {
	p, err := s.client.Project.Query().
		Where(entproject.ID(id), entproject.TenantID(tenantID)).Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("fetch project: %w", err)
	}
	u := p.Update()
	if input.Name != nil {
		u = u.SetName(*input.Name)
	}
	if input.Description != nil {
		u = u.SetDescription(*input.Description)
	}
	if input.Status != nil {
		u = u.SetStatus(*input.Status)
	}
	if input.Currency != nil {
		u = u.SetCurrency(*input.Currency)
	}
	if input.StartDate != nil {
		u = u.SetStartDate(*input.StartDate)
	}
	if input.EndDate != nil {
		u = u.SetEndDate(*input.EndDate)
	}
	if input.Budget != nil {
		u = u.SetBudget(*input.Budget)
	}
	if input.Metadata != nil {
		u = u.SetMetadata(input.Metadata)
	}
	updated, err := u.Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("update project: %w", err)
	}
	return updated, nil
}

// DeleteProject deletes a project by ID.
func (s *Service) DeleteProject(ctx context.Context, tenantID, id uuid.UUID) error {
	n, err := s.client.Project.Delete().
		Where(entproject.ID(id), entproject.TenantID(tenantID)).Exec(ctx)
	if err != nil {
		return fmt.Errorf("delete project: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// GetProjectSummary returns aggregated stats for a project.
func (s *Service) GetProjectSummary(ctx context.Context, tenantID, id uuid.UUID) (map[string]any, error) {
	p, err := s.GetProject(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	totalTasks := len(p.Edges.Tasks)
	completedTasks := 0
	for _, t := range p.Edges.Tasks {
		if t.Status == "done" {
			completedTasks++
		}
	}
	progress := 0
	if totalTasks > 0 {
		progress = completedTasks * 100 / totalTasks
	}
	return map[string]any{
		"project_id":       p.ID,
		"name":             p.Name,
		"status":           p.Status,
		"total_tasks":      totalTasks,
		"completed_tasks":  completedTasks,
		"progress":         progress,
		"total_members":    len(p.Edges.Members),
		"total_milestones": len(p.Edges.Milestones),
	}, nil
}
