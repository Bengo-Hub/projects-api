package members

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"go.uber.org/zap"

	sharedcache "github.com/Bengo-Hub/cache"
	"github.com/bengobox/projects-service/internal/ent"
	entmember "github.com/bengobox/projects-service/internal/ent/projectmember"
)

// ErrNotFound is returned when a member is not found.
var ErrNotFound = errors.New("not found")

// ErrAlreadyMember is returned when the user is already a member of the project.
var ErrAlreadyMember = errors.New("user is already a member of this project")

// Service handles project member operations.
type Service struct {
	client *ent.Client
	cache  *sharedcache.Aside
	log    *zap.Logger
}

// NewService creates a new members service.
func NewService(client *ent.Client, cache *sharedcache.Aside, log *zap.Logger) *Service {
	return &Service{client: client, cache: cache, log: log.Named("members.svc")}
}

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
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list members: %w", err)
	}
	return items, nil
}

// AddMember adds a user to a project.
func (s *Service) AddMember(ctx context.Context, tenantID, projectID uuid.UUID, input AddMemberInput) (*ent.ProjectMember, error) {
	// Check not already a member
	exists, err := s.client.ProjectMember.Query().
		Where(entmember.TenantID(tenantID), entmember.ProjectID(projectID), entmember.UserID(input.UserID)).
		Exist(ctx)
	if err != nil {
		return nil, fmt.Errorf("check existing member: %w", err)
	}
	if exists {
		return nil, ErrAlreadyMember
	}
	roleCode := input.RoleCode
	if roleCode == "" {
		roleCode = "member"
	}
	m, err := s.client.ProjectMember.Create().
		SetTenantID(tenantID).
		SetProjectID(projectID).
		SetUserID(input.UserID).
		SetRoleCode(roleCode).
		Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("add member: %w", err)
	}
	s.log.Info("member added", zap.String("project", projectID.String()), zap.String("user", input.UserID.String()))
	return m, nil
}

// UpdateMemberRole changes a member's role in the project.
func (s *Service) UpdateMemberRole(ctx context.Context, tenantID, projectID, userID uuid.UUID, input UpdateMemberInput) (*ent.ProjectMember, error) {
	m, err := s.client.ProjectMember.Query().
		Where(entmember.TenantID(tenantID), entmember.ProjectID(projectID), entmember.UserID(userID)).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("fetch member: %w", err)
	}
	updated, err := m.Update().SetRoleCode(input.RoleCode).Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("update member: %w", err)
	}
	return updated, nil
}

// RemoveMember removes a user from a project.
func (s *Service) RemoveMember(ctx context.Context, tenantID, projectID, userID uuid.UUID) error {
	n, err := s.client.ProjectMember.Delete().
		Where(entmember.TenantID(tenantID), entmember.ProjectID(projectID), entmember.UserID(userID)).Exec(ctx)
	if err != nil {
		return fmt.Errorf("remove member: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
