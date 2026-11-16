package members

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"go.uber.org/zap"

	sharedcache "github.com/Bengo-Hub/cache"
	"github.com/bengobox/projects-service/internal/ent"
	entproject "github.com/bengobox/projects-service/internal/ent/project"
	entmember "github.com/bengobox/projects-service/internal/ent/projectmember"
	"github.com/bengobox/projects-service/internal/services/activity"
	"github.com/bengobox/projects-service/internal/services/txn"
)

// ErrNotFound is returned when a member is not found.
var ErrNotFound = errors.New("not found")

// ErrAlreadyMember is returned when the user is already a member of the project.
var ErrAlreadyMember = errors.New("user is already a member of this project")

// ValidationError is a refused input, answered 400.
type ValidationError string

func (e ValidationError) Error() string { return string(e) }

// Service handles project member operations.
type Service struct {
	client  *ent.Client
	cache   *sharedcache.Aside
	log     *zap.Logger
	changed func()
}

// NewService creates a new members service.
func NewService(client *ent.Client, cache *sharedcache.Aside, log *zap.Logger) *Service {
	return &Service{client: client, cache: cache, log: log.Named("members.svc"), changed: func() {}}
}

// OnChange registers a callback run after every committed membership change (the access layer
// drops its cached membership answers).
func (s *Service) OnChange(fn func()) { s.changed = fn }

// AddMemberInput holds data for adding a project member.
type AddMemberInput struct {
	UserID   uuid.UUID `json:"user_id"`
	RoleCode string    `json:"role_code"`
}

// UpdateMemberInput holds data for updating a member's role.
type UpdateMemberInput struct {
	RoleCode string `json:"role_code"`
}

// ListMembers returns all active members of a project.
func (s *Service) ListMembers(ctx context.Context, tenantID, projectID uuid.UUID) ([]*ent.ProjectMember, error) {
	items, err := s.client.ProjectMember.Query().
		Where(entmember.TenantID(tenantID), entmember.ProjectID(projectID)).
		Order(ent.Asc(entmember.FieldJoinedAt)).
		Limit(subListLimit).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list members: %w", err)
	}
	return items, nil
}

func record(ctx context.Context, tx *ent.Tx, tenantID, projectID, userID uuid.UUID, action string, changes map[string]any) error {
	return activity.Record(ctx, tx, activity.Entry{
		TenantID: tenantID, ProjectID: activity.Ref(projectID), EntityType: "member", EntityID: userID,
		Action: action, Changes: changes,
	})
}

// AddMember adds a user to a project of the tenant.
func (s *Service) AddMember(ctx context.Context, tenantID, projectID uuid.UUID, input AddMemberInput) (*ent.ProjectMember, error) {
	if input.UserID == uuid.Nil {
		return nil, ValidationError("user_id is required")
	}
	roleCode := input.RoleCode
	if roleCode == "" {
		roleCode = "member"
	}
	var m *ent.ProjectMember
	err := txn.Run(ctx, s.client, func(tx *ent.Tx) error {
		ok, err := tx.Project.Query().Where(entproject.ID(projectID), entproject.TenantID(tenantID)).Exist(ctx)
		if err != nil {
			return fmt.Errorf("validate project: %w", err)
		}
		if !ok {
			return ErrNotFound
		}
		exists, err := tx.ProjectMember.Query().
			Where(entmember.TenantID(tenantID), entmember.ProjectID(projectID), entmember.UserID(input.UserID)).
			Exist(ctx)
		if err != nil {
			return fmt.Errorf("check existing member: %w", err)
		}
		if exists {
			return ErrAlreadyMember
		}
		if m, err = tx.ProjectMember.Create().
			SetTenantID(tenantID).SetProjectID(projectID).SetUserID(input.UserID).SetRoleCode(roleCode).
			Save(ctx); err != nil {
			return fmt.Errorf("add member: %w", err)
		}
		return record(ctx, tx, tenantID, projectID, input.UserID, activity.ActionCreated, map[string]any{"role_code": roleCode})
	})
	if err != nil {
		return nil, err
	}
	s.changed()
	s.log.Info("member added", zap.String("project", projectID.String()), zap.String("user", input.UserID.String()))
	return m, nil
}

// UpdateMemberRole changes a member's role in the project.
func (s *Service) UpdateMemberRole(ctx context.Context, tenantID, projectID, userID uuid.UUID, input UpdateMemberInput) (*ent.ProjectMember, error) {
	if input.RoleCode == "" {
		return nil, ValidationError("role_code is required")
	}
	var updated *ent.ProjectMember
	err := txn.Run(ctx, s.client, func(tx *ent.Tx) error {
		m, err := tx.ProjectMember.Query().
			Where(entmember.TenantID(tenantID), entmember.ProjectID(projectID), entmember.UserID(userID)).
			Only(ctx)
		if err != nil {
			if ent.IsNotFound(err) {
				return ErrNotFound
			}
			return fmt.Errorf("fetch member: %w", err)
		}
		if updated, err = m.Update().SetRoleCode(input.RoleCode).Save(ctx); err != nil {
			return fmt.Errorf("update member: %w", err)
		}
		return record(ctx, tx, tenantID, projectID, userID, activity.ActionUpdated,
			map[string]any{"role_code": map[string]any{"from": m.RoleCode, "to": input.RoleCode}})
	})
	if err != nil {
		return nil, err
	}
	return updated, nil
}

// RemoveMember removes a user from a project.
func (s *Service) RemoveMember(ctx context.Context, tenantID, projectID, userID uuid.UUID) error {
	err := txn.Run(ctx, s.client, func(tx *ent.Tx) error {
		n, err := tx.ProjectMember.Delete().
			Where(entmember.TenantID(tenantID), entmember.ProjectID(projectID), entmember.UserID(userID)).Exec(ctx)
		if err != nil {
			return fmt.Errorf("remove member: %w", err)
		}
		if n == 0 {
			return ErrNotFound
		}
		return record(ctx, tx, tenantID, projectID, userID, activity.ActionDeleted, nil)
	})
	if err != nil {
		return err
	}
	s.changed()
	return nil
}
