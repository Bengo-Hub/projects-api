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
	entproject "github.com/bengobox/projects-service/internal/ent/project"
	"github.com/bengobox/projects-service/internal/platform/events"
	"github.com/bengobox/projects-service/internal/services/activity"
	"github.com/bengobox/projects-service/internal/services/txn"
)

// ErrNotFound is returned when a milestone is not found.
var ErrNotFound = errors.New("not found")

// ValidationError is a refused input, answered 400.
type ValidationError string

func (e ValidationError) Error() string { return string(e) }

// EventMilestoneReached is emitted (project.milestone.reached) when a milestone is completed;
// notifications-api emails the tenant.
const EventMilestoneReached = "milestone.reached"

// Service handles milestone CRUD operations.
type Service struct {
	client *ent.Client
	cache  *sharedcache.Aside
	log    *zap.Logger
}

// NewService creates a new milestones service.
func NewService(client *ent.Client, cache *sharedcache.Aside, log *zap.Logger) *Service {
	return &Service{client: client, cache: cache, log: log.Named("milestones.svc")}
}

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
		Limit(subListLimit).All(ctx)
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

func record(ctx context.Context, tx *ent.Tx, m *ent.Milestone, action string, changes map[string]any) error {
	return activity.Record(ctx, tx, activity.Entry{
		TenantID: m.TenantID, ProjectID: activity.Ref(m.ProjectID), EntityType: "milestone", EntityID: m.ID,
		Action: action, Changes: changes,
	})
}

// CreateMilestone creates a new milestone for a project of the tenant.
func (s *Service) CreateMilestone(ctx context.Context, tenantID, projectID uuid.UUID, input CreateMilestoneInput) (*ent.Milestone, error) {
	if input.Name == "" {
		return nil, ValidationError("name is required")
	}
	status := input.Status
	if status == "" {
		status = "pending"
	}
	var m *ent.Milestone
	err := txn.Run(ctx, s.client, func(tx *ent.Tx) error {
		ok, err := tx.Project.Query().Where(entproject.ID(projectID), entproject.TenantID(tenantID)).Exist(ctx)
		if err != nil {
			return fmt.Errorf("validate project: %w", err)
		}
		if !ok {
			return ErrNotFound
		}
		c := tx.Milestone.Create().
			SetTenantID(tenantID).
			SetProjectID(projectID).
			SetName(input.Name).
			SetDescription(input.Description).
			SetStatus(status)
		if input.TargetDate != nil {
			c = c.SetTargetDate(*input.TargetDate)
		}
		if m, err = c.Save(ctx); err != nil {
			return fmt.Errorf("create milestone: %w", err)
		}
		return record(ctx, tx, m, activity.ActionCreated, map[string]any{"name": m.Name, "status": m.Status})
	})
	if err != nil {
		return nil, err
	}
	return m, nil
}

// UpdateMilestone updates an existing milestone. Completing it writes project.milestone.reached in
// the same transaction.
func (s *Service) UpdateMilestone(ctx context.Context, tenantID, projectID, id uuid.UUID, input UpdateMilestoneInput) (*ent.Milestone, error) {
	var updated *ent.Milestone
	err := txn.Run(ctx, s.client, func(tx *ent.Tx) error {
		m, err := tx.Milestone.Query().
			Where(entmilestone.ID(id), entmilestone.TenantID(tenantID), entmilestone.ProjectID(projectID)).Only(ctx)
		if ent.IsNotFound(err) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("fetch milestone: %w", err)
		}
		changes := map[string]any{}
		u := m.Update()
		if input.Name != nil {
			u = u.SetName(*input.Name)
			changes["name"] = *input.Name
		}
		if input.Description != nil {
			u = u.SetDescription(*input.Description)
			changes["description"] = "changed"
		}
		completed := false
		if input.Status != nil {
			u = u.SetStatus(*input.Status)
			changes["status"] = map[string]any{"from": m.Status, "to": *input.Status}
			if *input.Status == "completed" {
				u = u.SetCompletedAt(time.Now())
				completed = m.Status != "completed"
			}
		}
		if input.TargetDate != nil {
			u = u.SetTargetDate(*input.TargetDate)
			changes["target_date"] = input.TargetDate.Format(time.DateOnly)
		}
		if updated, err = u.Save(ctx); err != nil {
			return fmt.Errorf("update milestone: %w", err)
		}
		if err := record(ctx, tx, updated, activity.ActionUpdated, changes); err != nil {
			return err
		}
		if !completed {
			return nil
		}
		projectName := ""
		if proj, perr := tx.Project.Get(ctx, projectID); perr == nil {
			projectName = proj.Name
		}
		return events.Write(ctx, tx, events.AggregateProject, EventMilestoneReached, updated.ID, tenantID, map[string]any{
			"project_id":      projectID.String(),
			"project_name":    projectName,
			"milestone_name":  updated.Name,
			"completion_date": time.Now().UTC().Format(time.RFC3339),
			"progress":        100,
		})
	})
	if err != nil {
		return nil, err
	}
	return updated, nil
}

// DeleteMilestone deletes a milestone.
func (s *Service) DeleteMilestone(ctx context.Context, tenantID, projectID, id uuid.UUID) error {
	return txn.Run(ctx, s.client, func(tx *ent.Tx) error {
		m, err := tx.Milestone.Query().
			Where(entmilestone.ID(id), entmilestone.TenantID(tenantID), entmilestone.ProjectID(projectID)).Only(ctx)
		if ent.IsNotFound(err) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("fetch milestone: %w", err)
		}
		if err := tx.Milestone.DeleteOne(m).Exec(ctx); err != nil {
			return fmt.Errorf("delete milestone: %w", err)
		}
		return record(ctx, tx, m, activity.ActionDeleted, map[string]any{"name": m.Name})
	})
}
