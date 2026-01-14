package rbac

import (
	"context"
	"testing"

	"github.com/bengobox/projects-service/internal/testutil"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestService_RoleOperations(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	client, cleanup := testutil.SetupTestDB(t)
	defer cleanup()

	logger := zap.NewNop()
	svc := NewService(logger, client)
	ctx := context.Background()

	// Seed a test role
	testRole, err := client.Role.Create().
		SetID("test-role").
		SetName("Test Role").
		SetDescription("A test role").
		SetPermissions([]string{"projects:read", "projects:write"}).
		Save(ctx)
	require.NoError(t, err)

	t.Run("GetRole", func(t *testing.T) {
		tests := []struct {
			name      string
			roleID    string
			wantErr   bool
			wantName  string
		}{
			{
				name:     "existing role",
				roleID:   "test-role",
				wantErr:  false,
				wantName: "Test Role",
			},
			{
				name:    "non-existent role",
				roleID:  "does-not-exist",
				wantErr: true,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				role, err := svc.GetRole(ctx, tt.roleID)
				if tt.wantErr {
					assert.Error(t, err)
					assert.Nil(t, role)
				} else {
					assert.NoError(t, err)
					assert.Equal(t, tt.wantName, role.Name)
				}
			})
		}
	})

	t.Run("ListRoles", func(t *testing.T) {
		roles, err := svc.ListRoles(ctx)
		require.NoError(t, err)
		assert.Len(t, roles, 1)
		assert.Equal(t, testRole.ID, roles[0].ID)
	})
}

func TestService_UserRoleOperations(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	client, cleanup := testutil.SetupTestDB(t)
	defer cleanup()

	logger := zap.NewNop()
	svc := NewService(logger, client)
	ctx := context.Background()

	// Seed roles
	_, err := client.Role.Create().
		SetID("admin").
		SetName("Admin").
		SetPermissions([]string{"projects:read", "projects:write", "projects:delete"}).
		Save(ctx)
	require.NoError(t, err)

	_, err = client.Role.Create().
		SetID("viewer").
		SetName("Viewer").
		SetPermissions([]string{"projects:read"}).
		Save(ctx)
	require.NoError(t, err)

	userID := uuid.New()
	tenantID := uuid.New()

	t.Run("AssignRole", func(t *testing.T) {
		tests := []struct {
			name     string
			roleCode string
			wantErr  bool
		}{
			{
				name:     "assign existing role",
				roleCode: "admin",
				wantErr:  false,
			},
			{
				name:     "assign same role again (idempotent)",
				roleCode: "admin",
				wantErr:  false,
			},
			{
				name:     "assign non-existent role",
				roleCode: "super-admin",
				wantErr:  true,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				ur, err := svc.AssignRole(ctx, userID, tenantID, tt.roleCode, nil)
				if tt.wantErr {
					assert.Error(t, err)
					assert.Nil(t, ur)
				} else {
					assert.NoError(t, err)
					assert.NotNil(t, ur)
					assert.Equal(t, tt.roleCode, ur.RoleCode)
				}
			})
		}
	})

	t.Run("GetUserRoles", func(t *testing.T) {
		roles, err := svc.GetUserRoles(ctx, userID, tenantID)
		require.NoError(t, err)
		assert.Len(t, roles, 1)
		assert.Equal(t, "admin", roles[0].ID)
	})

	t.Run("HasPermission", func(t *testing.T) {
		tests := []struct {
			name       string
			permission string
			want       bool
		}{
			{
				name:       "has read permission",
				permission: "projects:read",
				want:       true,
			},
			{
				name:       "has write permission",
				permission: "projects:write",
				want:       true,
			},
			{
				name:       "has delete permission",
				permission: "projects:delete",
				want:       true,
			},
			{
				name:       "does not have manage permission",
				permission: "projects:manage",
				want:       false,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				has, err := svc.HasPermission(ctx, userID, tenantID, tt.permission)
				require.NoError(t, err)
				assert.Equal(t, tt.want, has)
			})
		}
	})

	t.Run("RevokeRole", func(t *testing.T) {
		tests := []struct {
			name     string
			roleCode string
			wantErr  bool
		}{
			{
				name:     "revoke assigned role",
				roleCode: "admin",
				wantErr:  false,
			},
			{
				name:     "revoke already revoked role",
				roleCode: "admin",
				wantErr:  true,
			},
			{
				name:     "revoke never assigned role",
				roleCode: "viewer",
				wantErr:  true,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				err := svc.RevokeRole(ctx, userID, tenantID, tt.roleCode)
				if tt.wantErr {
					assert.Error(t, err)
				} else {
					assert.NoError(t, err)
				}
			})
		}
	})

	t.Run("HasPermission after revoke", func(t *testing.T) {
		has, err := svc.HasPermission(ctx, userID, tenantID, "projects:read")
		require.NoError(t, err)
		assert.False(t, has, "should not have permission after role revoked")
	})
}
