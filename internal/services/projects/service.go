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
	enttask "github.com/bengobox/projects-service/internal/ent/task"
	"github.com/bengobox/projects-service/internal/services/activity"
	"github.com/bengobox/projects-service/internal/services/membership"
	"github.com/bengobox/projects-service/internal/services/txn"
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
	Status string
	Limit  int
	Offset int
	// VisibleTo limits the list to the projects this user owns or belongs to (nil: every project).
	VisibleTo *uuid.UUID
}

func scoped(q *ent.ProjectQuery, visibleTo *uuid.UUID) *ent.ProjectQuery {
	if visibleTo != nil {
		q = q.Where(membership.Visible(*visibleTo))
	}
	return q
}

// ListProjects returns a paginated list of projects for the given tenant.
func (s *Service) ListProjects(ctx context.Context, tenantID uuid.UUID, filter ListProjectsFilter) ([]*ent.Project, int, error) {
	if filter.Limit <= 0 {
		filter.Limit = 20
	}

	q := scoped(s.client.Project.Query().Where(entproject.TenantID(tenantID)), filter.VisibleTo)
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

// GetProject fetches a single project with its members and milestones. Tasks come from the paged
// task list, not from here.
func (s *Service) GetProject(ctx context.Context, tenantID, id uuid.UUID) (*ent.Project, error) {
	p, err := s.client.Project.Query().
		Where(entproject.ID(id), entproject.TenantID(tenantID)).
		WithMembers().WithMilestones().
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
	if input.Metadata != nil {
		if err := normalizeCommercial(input.Metadata); err != nil {
			return nil, err
		}
	}
	var p *ent.Project
	err := txn.Run(ctx, s.client, func(tx *ent.Tx) error {
		c := tx.Project.Create().
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
			c = c.SetMetadata(mergeMetadata(nil, input.Metadata))
		}
		var err error
		if p, err = c.Save(ctx); err != nil {
			return fmt.Errorf("create project: %w", err)
		}
		if err := activity.Record(ctx, tx, activity.Entry{
			TenantID: tenantID, ProjectID: activity.Ref(p.ID), EntityType: "project", EntityID: p.ID,
			Action: activity.ActionCreated, Changes: map[string]any{"name": p.Name, "status": p.Status},
		}); err != nil {
			return err
		}
		return emit(ctx, tx, tenantID, p.ID, EventCreated, projectPayload(p))
	})
	if err != nil {
		return nil, err
	}
	s.log.Info("project created", zap.String("id", p.ID.String()), zap.String("tenant", tenantID.String()))
	return p, nil
}

// UpdateProject updates an existing project.
func (s *Service) UpdateProject(ctx context.Context, tenantID, id uuid.UUID, input UpdateProjectInput) (*ent.Project, error) {
	if input.Metadata != nil {
		// A partial update: keys sent as null are removed, keys not sent are kept.
		if err := normalizeCommercial(input.Metadata); err != nil {
			return nil, err
		}
	}
	var updated *ent.Project
	err := txn.Run(ctx, s.client, func(tx *ent.Tx) error {
		p, err := tx.Project.Query().Where(entproject.ID(id), entproject.TenantID(tenantID)).Only(ctx)
		if err != nil {
			if ent.IsNotFound(err) {
				return ErrNotFound
			}
			return fmt.Errorf("fetch project: %w", err)
		}
		changes := map[string]any{}
		u := p.Update()
		if input.Name != nil {
			u = u.SetName(*input.Name)
			changes["name"] = *input.Name
		}
		if input.Description != nil {
			u = u.SetDescription(*input.Description)
			changes["description"] = "changed"
		}
		if input.Status != nil {
			u = u.SetStatus(*input.Status)
			changes["status"] = map[string]any{"from": p.Status, "to": *input.Status}
		}
		if input.Currency != nil {
			u = u.SetCurrency(*input.Currency)
			changes["currency"] = *input.Currency
		}
		if input.StartDate != nil {
			u = u.SetStartDate(*input.StartDate)
			changes["start_date"] = input.StartDate.Format(time.DateOnly)
		}
		if input.EndDate != nil {
			u = u.SetEndDate(*input.EndDate)
			changes["end_date"] = input.EndDate.Format(time.DateOnly)
		}
		if input.Budget != nil {
			u = u.SetBudget(*input.Budget)
			changes["budget"] = *input.Budget
		}
		if input.Metadata != nil {
			u = u.SetMetadata(mergeMetadata(p.Metadata, input.Metadata))
			changes["metadata"] = "changed"
		}
		prevStatus := p.Status
		if updated, err = u.Save(ctx); err != nil {
			return fmt.Errorf("update project: %w", err)
		}
		if err := activity.Record(ctx, tx, activity.Entry{
			TenantID: tenantID, ProjectID: activity.Ref(id), EntityType: "project", EntityID: id,
			Action: activity.ActionUpdated, Changes: changes,
		}); err != nil {
			return err
		}
		payload := projectPayload(updated)
		payload["previous_status"] = prevStatus
		if err := emit(ctx, tx, tenantID, id, EventUpdated, payload); err != nil {
			return err
		}
		if closesProject(prevStatus, updated.Status) {
			return emit(ctx, tx, tenantID, id, EventClosed, payload)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return updated, nil
}

// DeleteProject deletes a project by ID. Its feed entries stay (their project link is cleared by
// the foreign key), with one entry recording the deletion.
func (s *Service) DeleteProject(ctx context.Context, tenantID, id uuid.UUID) error {
	return txn.Run(ctx, s.client, func(tx *ent.Tx) error {
		p, err := tx.Project.Query().Where(entproject.ID(id), entproject.TenantID(tenantID)).Only(ctx)
		if ent.IsNotFound(err) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("fetch project: %w", err)
		}
		if err := tx.Project.DeleteOne(p).Exec(ctx); err != nil {
			return fmt.Errorf("delete project: %w", err)
		}
		if err := activity.Record(ctx, tx, activity.Entry{
			TenantID: tenantID, EntityType: "project", EntityID: id,
			Action: activity.ActionDeleted, Changes: map[string]any{"name": p.Name},
		}); err != nil {
			return err
		}
		return emit(ctx, tx, tenantID, id, EventDeleted, map[string]any{"id": id.String(), "tenant_id": tenantID.String()})
	})
}

// GetProjectSummary returns aggregated stats for a project, counted in SQL (tasks grouped by
// status, overdue tasks, members, milestones) rather than by loading every row.
func (s *Service) GetProjectSummary(ctx context.Context, tenantID, id uuid.UUID) (map[string]any, error) {
	p, err := s.client.Project.Query().
		Where(entproject.ID(id), entproject.TenantID(tenantID)).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get project: %w", err)
	}
	var byStatus []struct {
		Status string `json:"status"`
		Count  int    `json:"count"`
	}
	if err := s.client.Task.Query().
		Where(enttask.ProjectID(id), enttask.TenantID(tenantID)).
		GroupBy(enttask.FieldStatus).
		Aggregate(ent.Count()).
		Scan(ctx, &byStatus); err != nil {
		return nil, fmt.Errorf("task counts: %w", err)
	}
	statusCounts := map[string]int{}
	totalTasks, completedTasks := 0, 0
	for _, r := range byStatus {
		statusCounts[r.Status] = r.Count
		totalTasks += r.Count
		if r.Status == "done" {
			completedTasks = r.Count
		}
	}
	overdue, err := s.client.Task.Query().
		Where(enttask.ProjectID(id), enttask.TenantID(tenantID), enttask.StatusNEQ("done"),
			enttask.DueDateNotNil(), enttask.DueDateLT(time.Now())).
		Count(ctx)
	if err != nil {
		return nil, fmt.Errorf("overdue tasks: %w", err)
	}
	members, err := p.QueryMembers().Count(ctx)
	if err != nil {
		return nil, fmt.Errorf("member count: %w", err)
	}
	milestones, err := p.QueryMilestones().Count(ctx)
	if err != nil {
		return nil, fmt.Errorf("milestone count: %w", err)
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
		"overdue_tasks":    overdue,
		"tasks_by_status":  statusCounts,
		"progress":         progress,
		"total_members":    members,
		"total_milestones": milestones,
	}, nil
}

// ProjectMetrics counts the tenant's projects by status in one grouped query, so dashboards
// report every project rather than whatever fits on one list page.
type ProjectMetrics struct {
	Total    int            `json:"total"`
	ByStatus map[string]int `json:"by_status"`
}

// GetProjectMetrics returns project counts by status for the tenant, limited to the projects
// visibleTo owns or belongs to when it is set.
func (s *Service) GetProjectMetrics(ctx context.Context, tenantID uuid.UUID, visibleTo *uuid.UUID) (ProjectMetrics, error) {
	var rows []struct {
		Status string `json:"status"`
		Count  int    `json:"count"`
	}
	if err := scoped(s.client.Project.Query().Where(entproject.TenantID(tenantID)), visibleTo).
		GroupBy(entproject.FieldStatus).
		Aggregate(ent.Count()).
		Scan(ctx, &rows); err != nil {
		return ProjectMetrics{}, fmt.Errorf("project metrics: %w", err)
	}
	m := ProjectMetrics{ByStatus: make(map[string]int, len(rows))}
	for _, r := range rows {
		m.ByStatus[r.Status] = r.Count
		m.Total += r.Count
	}
	return m, nil
}
