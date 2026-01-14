package rbac

import (
	"context"
	"fmt"

	"github.com/bengobox/projects-service/internal/ent"
	"github.com/bengobox/projects-service/internal/ent/role"
	"github.com/bengobox/projects-service/internal/ent/userrole"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// Service handles RBAC operations with database persistence.
type Service struct {
	logger *zap.Logger
	db     *ent.Client
}

// NewService creates a new RBAC service with database backing.
func NewService(logger *zap.Logger, db *ent.Client) *Service {
	return &Service{
		logger: logger,
		db:     db,
	}
}

// HasPermission checks if a user has a specific permission.
// It queries the user's roles and checks if any role has the required permission.
func (s *Service) HasPermission(ctx context.Context, userID, tenantID uuid.UUID, permission string) (bool, error) {
	// Get all roles for this user in this tenant
	userRoles, err := s.db.UserRole.Query().
		Where(
			userrole.UserID(userID),
			userrole.TenantID(tenantID),
		).
		WithRole().
		All(ctx)
	if err != nil {
		s.logger.Error("failed to query user roles",
			zap.String("user_id", userID.String()),
			zap.String("tenant_id", tenantID.String()),
			zap.Error(err),
		)
		return false, fmt.Errorf("failed to query user roles: %w", err)
	}

	// Check if any role has the required permission
	for _, ur := range userRoles {
		if ur.Edges.Role == nil {
			continue
		}
		for _, perm := range ur.Edges.Role.Permissions {
			if perm == permission {
				s.logger.Debug("permission granted",
					zap.String("user_id", userID.String()),
					zap.String("permission", permission),
					zap.String("role", ur.Edges.Role.ID),
				)
				return true, nil
			}
		}
	}

	s.logger.Debug("permission denied",
		zap.String("user_id", userID.String()),
		zap.String("permission", permission),
	)
	return false, nil
}

// GetUserRoles returns all roles assigned to a user in a tenant.
func (s *Service) GetUserRoles(ctx context.Context, userID, tenantID uuid.UUID) ([]*ent.Role, error) {
	userRoles, err := s.db.UserRole.Query().
		Where(
			userrole.UserID(userID),
			userrole.TenantID(tenantID),
		).
		WithRole().
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to query user roles: %w", err)
	}

	roles := make([]*ent.Role, 0, len(userRoles))
	for _, ur := range userRoles {
		if ur.Edges.Role != nil {
			roles = append(roles, ur.Edges.Role)
		}
	}

	return roles, nil
}

// GetRole returns a role by its ID (code).
func (s *Service) GetRole(ctx context.Context, roleID string) (*ent.Role, error) {
	r, err := s.db.Role.Get(ctx, roleID)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, fmt.Errorf("role not found: %s", roleID)
		}
		return nil, fmt.Errorf("failed to get role: %w", err)
	}
	return r, nil
}

// ListRoles returns all available roles.
func (s *Service) ListRoles(ctx context.Context) ([]*ent.Role, error) {
	roles, err := s.db.Role.Query().
		Order(ent.Asc(role.FieldName)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list roles: %w", err)
	}
	return roles, nil
}

// AssignRole assigns a role to a user in a tenant.
func (s *Service) AssignRole(ctx context.Context, userID, tenantID uuid.UUID, roleCode string, assignedBy *uuid.UUID) (*ent.UserRole, error) {
	// Verify the role exists
	_, err := s.db.Role.Get(ctx, roleCode)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, fmt.Errorf("role not found: %s", roleCode)
		}
		return nil, fmt.Errorf("failed to verify role: %w", err)
	}

	// Check if user already has this role
	existing, err := s.db.UserRole.Query().
		Where(
			userrole.UserID(userID),
			userrole.TenantID(tenantID),
			userrole.RoleCode(roleCode),
		).
		First(ctx)
	if err != nil && !ent.IsNotFound(err) {
		return nil, fmt.Errorf("failed to check existing role: %w", err)
	}
	if existing != nil {
		return existing, nil // Already assigned
	}

	// Create the user role assignment
	builder := s.db.UserRole.Create().
		SetUserID(userID).
		SetTenantID(tenantID).
		SetRoleCode(roleCode)

	if assignedBy != nil {
		builder.SetAssignedBy(*assignedBy)
	}

	ur, err := builder.Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to assign role: %w", err)
	}

	s.logger.Info("role assigned",
		zap.String("user_id", userID.String()),
		zap.String("tenant_id", tenantID.String()),
		zap.String("role", roleCode),
	)

	return ur, nil
}

// RevokeRole removes a role from a user in a tenant.
func (s *Service) RevokeRole(ctx context.Context, userID, tenantID uuid.UUID, roleCode string) error {
	deleted, err := s.db.UserRole.Delete().
		Where(
			userrole.UserID(userID),
			userrole.TenantID(tenantID),
			userrole.RoleCode(roleCode),
		).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to revoke role: %w", err)
	}

	if deleted == 0 {
		return fmt.Errorf("role assignment not found")
	}

	s.logger.Info("role revoked",
		zap.String("user_id", userID.String()),
		zap.String("tenant_id", tenantID.String()),
		zap.String("role", roleCode),
	)

	return nil
}
