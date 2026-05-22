package rbac

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	sharedcache "github.com/Bengo-Hub/cache"
	"github.com/bengobox/projects-service/internal/ent"
	entroleperm "github.com/bengobox/projects-service/internal/ent/rolepermission"
	entuserrole "github.com/bengobox/projects-service/internal/ent/userrole"
)

// Service handles RBAC operations backed by the database.
type Service struct {
	client *ent.Client
	cache  *sharedcache.Aside
	log    *zap.Logger
}

// NewService creates a new RBAC service.
func NewService(client *ent.Client, cache *sharedcache.Aside, log *zap.Logger) *Service {
	return &Service{client: client, cache: cache, log: log.Named("rbac.svc")}
}

// GetUserRoles returns the role codes assigned to a user within a tenant.
// Results are cached for 5 minutes.
func (s *Service) GetUserRoles(ctx context.Context, userID, tenantID uuid.UUID) ([]string, error) {
	cacheKey := fmt.Sprintf("rbac:roles:%s:%s", tenantID, userID)
	codes, err := sharedcache.GetOrSet(ctx, s.cache, cacheKey, 5*time.Minute, func(ctx context.Context) ([]string, error) {
		userRoles, err := s.client.UserRole.Query().
			Where(entuserrole.UserID(userID), entuserrole.TenantID(tenantID)).
			All(ctx)
		if err != nil {
			return nil, fmt.Errorf("query user roles: %w", err)
		}
		result := make([]string, len(userRoles))
		for i, ur := range userRoles {
			result[i] = ur.RoleCode
		}
		return result, nil
	})
	if err != nil {
		return nil, err
	}
	return codes, nil
}

// HasPermission checks if a user has a given permission via their assigned roles.
func (s *Service) HasPermission(ctx context.Context, userID, tenantID uuid.UUID, permission string) (bool, error) {
	roles, err := s.GetUserRoles(ctx, userID, tenantID)
	if err != nil {
		return false, err
	}
	if len(roles) == 0 {
		return false, nil
	}
	rolePerms, err := s.client.RolePermission.Query().
		Where(entroleperm.RoleCodeIn(roles...)).
		WithPermission().All(ctx)
	if err != nil {
		return false, fmt.Errorf("query role permissions: %w", err)
	}
	for _, rp := range rolePerms {
		if rp.Edges.Permission != nil && rp.Edges.Permission.Name == permission {
			return true, nil
		}
	}
	return false, nil
}

// AssignRole assigns a role to a user in the given tenant.
func (s *Service) AssignRole(ctx context.Context, tenantID, userID, assignedBy uuid.UUID, roleCode string) error {
	exists, err := s.client.UserRole.Query().
		Where(entuserrole.UserID(userID), entuserrole.TenantID(tenantID), entuserrole.RoleCode(roleCode)).
		Exist(ctx)
	if err != nil {
		return fmt.Errorf("check existing role: %w", err)
	}
	if exists {
		return errors.New("role already assigned")
	}
	_, err = s.client.UserRole.Create().
		SetTenantID(tenantID).
		SetUserID(userID).
		SetRoleCode(roleCode).
		SetAssignedBy(assignedBy).
		Save(ctx)
	if err != nil {
		return fmt.Errorf("assign role: %w", err)
	}
	s.InvalidateUserCache(ctx, userID, tenantID)
	return nil
}

// ListRoles returns all available roles from the database.
func (s *Service) ListRoles(ctx context.Context) ([]any, error) {
	roles, err := s.client.Role.Query().All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list roles: %w", err)
	}
	result := make([]any, len(roles))
	for i, r := range roles {
		result[i] = r
	}
	return result, nil
}

// InvalidateUserCache clears the cached roles for a user.
func (s *Service) InvalidateUserCache(ctx context.Context, userID, tenantID uuid.UUID) {
	s.cache.Invalidate(ctx, fmt.Sprintf("rbac:roles:%s:%s", tenantID, userID))
}
