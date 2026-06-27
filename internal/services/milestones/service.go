package milestones

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	sharedcache "github.com/Bengo-Hub/cache"
	"github.com/bengobox/projects-service/internal/ent"
	entmilestone "github.com/bengobox/projects-service/internal/ent/milestone"
)

// ErrNotFound is returned when a milestone is not found.
var ErrNotFound = errors.New("not found")

// Publisher emits milestone domain events to the shared-events outbox. Kept as a narrow
// interface so the service stays decoupled from the events platform package.
type Publisher interface {
	PublishMilestoneReached(ctx context.Context, tenantID, milestoneID uuid.UUID, payload map[string]any) error
}

// Service handles milestone CRUD operations.
type Service struct {
	client    *ent.Client
	cache     *sharedcache.Aside
	log       *zap.Logger
	publisher Publisher
}

// NewService creates a new milestones service.
func NewService(client *ent.Client, cache *sharedcache.Aside, log *zap.Logger) *Service {
	return &Service{client: client, cache: cache, log: log.Named("milestones.svc")}
}

// SetPublisher wires the event publisher used to emit project.milestone.reached.
func (s *Service) SetPublisher(p Publisher) { s.publisher = p }

// CreateMilestoneInput holds data for creating a milestone.
type CreateMilestoneInput struct {
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Status      string     `json:"status"`
	TargetDate  *time.Time `json:"target_date"`
}

// UpdateMilestoneInput holds data for updating a milestone.
type UpdateMilestoneInput struct {
	Name        *string    `json:"name"`
	Description *string    `json:"description"`
	Status      *string    `json:"status"`
	TargetDate  *time.Time `json:"target_date"`
}

// ListMilestones returns all milestones for the given project.
func (s *Service) ListMilestones(ctx context.Context, tenantID, projectID uuid.UUID) ([]*ent.Milestone, error) {
	items, err := s.client.Milestone.Query().
		Where(entmilestone.TenantID(tenantID), entmilestone.ProjectID(projectID)).
		Order(ent.Asc(entmilestone.FieldTargetDate)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list milestones: %w", err)
	}
	return items, nil
}

// GetMilestone returns a single milestone.
func (s *Service) GetMilestone(ctx context.Context, tenantID, projectID, id uuid.UUID) (*ent.Milestone, error) {
	m, err := s.client.Milestone.Query().
		Where(entmilestone.ID(id), entmilestone.TenantID(tenantID), entmilestone.ProjectID(projectID)).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get milestone: %w", err)
	}
	return m, nil
}

// CreateMilestone creates a new milestone for a project.
func (s *Service) CreateMilestone(ctx context.Context, tenantID, projectID uuid.UUID, input CreateMilestoneInput) (*ent.Milestone, error) {
	if input.Name == "" {
		return nil, fmt.Errorf("name is required")
	}
	status := input.Status
	if status == "" {
		status = "pending"
	}
	c := s.client.Milestone.Create().
		SetTenantID(tenantID).
		SetProjectID(projectID).
		SetName(input.Name).
		SetDescription(input.Description).
		SetStatus(status)
	if input.TargetDate != nil {
		c = c.SetTargetDate(*input.TargetDate)
	}
	m, err := c.Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("create milestone: %w", err)
	}
	return m, nil
}

// UpdateMilestone updates an existing milestone.
func (s *Service) UpdateMilestone(ctx context.Context, tenantID, projectID, id uuid.UUID, input UpdateMilestoneInput) (*ent.Milestone, error) {
	m, err := s.GetMilestone(ctx, tenantID, projectID, id)
	if err != nil {
		return nil, err
	}
	u := m.Update()
	if input.Name != nil {
		u = u.SetName(*input.Name)
	}
	if input.Description != nil {
		u = u.SetDescription(*input.Description)
	}
	if input.Status != nil {
		u = u.SetStatus(*input.Status)
		if *input.Status == "completed" {
			now := time.Now()
			u = u.SetCompletedAt(now)
		}
	}
	if input.TargetDate != nil {
		u = u.SetTargetDate(*input.TargetDate)
	}
	updated, err := u.Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("update milestone: %w", err)
	}

	// On completion, emit project.milestone.reached so notifications-api can email the
	// tenant. Best-effort: a publish failure must not fail the milestone update.
	if input.Status != nil && *input.Status == "completed" && s.publisher != nil {
		projectName := ""
		if proj, perr := s.client.Project.Get(ctx, projectID); perr == nil {
			projectName = proj.Name
		}
		if perr := s.publisher.PublishMilestoneReached(ctx, tenantID, updated.ID, map[string]any{
			"project_id":      projectID.String(),
			"project_name":    projectName,
			"milestone_name":  updated.Name,
			"completion_date": time.Now().UTC().Format(time.RFC3339),
			"progress":        100,
		}); perr != nil {
			s.log.Warn("publish milestone.reached failed", zap.Error(perr))
		}
	}
	return updated, nil
}

// DeleteMilestone deletes a milestone.
func (s *Service) DeleteMilestone(ctx context.Context, tenantID, projectID, id uuid.UUID) error {
	n, err := s.client.Milestone.Delete().
		Where(entmilestone.ID(id), entmilestone.TenantID(tenantID), entmilestone.ProjectID(projectID)).Exec(ctx)
	if err != nil {
		return fmt.Errorf("delete milestone: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
