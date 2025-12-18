package rbac

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// Permission represents a permission in the system
type Permission struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Module      string    `json:"module"`
	Action      string    `json:"action"`
	Resource    string    `json:"resource,omitempty"`
	Description string    `json:"description,omitempty"`
}

// Role represents a role in the system
type Role struct {
	ID          string       `json:"id"`
	Name        string       `json:"name"`
	Description string       `json:"description,omitempty"`
	Permissions []Permission `json:"permissions"`
}

// Service handles RBAC operations
type Service struct {
	logger *zap.Logger
	// In a real implementation, this would have database access
	// For now, we'll use in-memory storage as a placeholder
	roles       map[string]*Role
	permissions map[string]*Permission
}

// NewService creates a new RBAC service
func NewService(logger *zap.Logger) *Service {
	s := &Service{
		logger:      logger,
		roles:       make(map[string]*Role),
		permissions: make(map[string]*Permission),
	}
	s.initDefaultRoles()
	return s
}

// initDefaultRoles initializes default roles and permissions
func (s *Service) initDefaultRoles() {
	// Default permissions
	projectRead := &Permission{
		ID:          uuid.New(),
		Name:        "projects:read",
		Module:      "projects",
		Action:      "read",
		Description: "Read projects",
	}
	projectWrite := &Permission{
		ID:          uuid.New(),
		Name:        "projects:write",
		Module:      "projects",
		Action:      "write",
		Description: "Create and update projects",
	}
	projectDelete := &Permission{
		ID:          uuid.New(),
		Name:        "projects:delete",
		Module:      "projects",
		Action:      "delete",
		Description: "Delete projects",
	}
	projectManage := &Permission{
		ID:          uuid.New(),
		Name:        "projects:manage",
		Module:      "projects",
		Action:      "manage",
		Description: "Full management of projects",
	}

	s.permissions[projectRead.Name] = projectRead
	s.permissions[projectWrite.Name] = projectWrite
	s.permissions[projectDelete.Name] = projectDelete
	s.permissions[projectManage.Name] = projectManage

	// Default roles
	s.roles["admin"] = &Role{
		ID:          "admin",
		Name:        "admin",
		Description: "Administrator with full access",
		Permissions: []Permission{*projectRead, *projectWrite, *projectDelete, *projectManage},
	}

	s.roles["member"] = &Role{
		ID:          "member",
		Name:        "member",
		Description: "Regular member with read and write access",
		Permissions: []Permission{*projectRead, *projectWrite},
	}

	s.roles["viewer"] = &Role{
		ID:          "viewer",
		Name:        "viewer",
		Description: "Viewer with read-only access",
		Permissions: []Permission{*projectRead},
	}
}

// HasPermission checks if a user has a specific permission
func (s *Service) HasPermission(ctx context.Context, userID uuid.UUID, tenantID uuid.UUID, module, action, resource string) (bool, error) {
	// In a real implementation, this would:
	// 1. Query the database for user roles
	// 2. Check role permissions
	// 3. Check for user-specific permission overrides
	// For now, we'll return true for admin role as a placeholder
	s.logger.Debug("checking permission",
		zap.String("user_id", userID.String()),
		zap.String("module", module),
		zap.String("action", action),
	)
	return true, nil
}

// GetUserRoles returns the roles for a user
func (s *Service) GetUserRoles(ctx context.Context, userID uuid.UUID, tenantID uuid.UUID) ([]Role, error) {
	// In a real implementation, this would query the database
	// For now, return default member role
	return []Role{*s.roles["member"]}, nil
}

// GetRole returns a role by ID
func (s *Service) GetRole(ctx context.Context, roleID string) (*Role, error) {
	role, ok := s.roles[roleID]
	if !ok {
		return nil, fmt.Errorf("role not found: %s", roleID)
	}
	return role, nil
}

// ListRoles returns all available roles
func (s *Service) ListRoles(ctx context.Context) ([]Role, error) {
	roles := make([]Role, 0, len(s.roles))
	for _, role := range s.roles {
		roles = append(roles, *role)
	}
	return roles, nil
}

