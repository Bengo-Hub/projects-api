package rbac

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/projects-service/internal/testutil"
)

func seeded(t *testing.T) *Service {
	t.Helper()
	svc := NewService(testutil.Client(t), nil, zap.NewNop())
	ctx := context.Background()
	if err := svc.Seed(ctx); err != nil {
		t.Fatal(err)
	}
	// A second run changes nothing and does not fail.
	if err := svc.Seed(ctx); err != nil {
		t.Fatalf("second seed: %v", err)
	}
	return svc
}

func has(t *testing.T, svc *Service, tenant, user uuid.UUID, code string) bool {
	t.Helper()
	ok, err := svc.HasPermission(context.Background(), tenant, user, code)
	if err != nil {
		t.Fatal(err)
	}
	return ok
}

// JIT gives a new user the role their global roles map to, and keeps it in step while nobody
// assigned them a role by hand.
func TestEnsureUserFromTokenDefaultRole(t *testing.T) {
	svc := seeded(t)
	ctx := context.Background()
	tenant := uuid.New()
	for _, c := range []struct {
		global []string
		want   string
	}{
		{[]string{"admin"}, RoleAdmin},
		{[]string{"superuser"}, RoleAdmin},
		{[]string{"staff"}, RoleMember},
		{[]string{"manager"}, RoleMember},
		{[]string{"cashier"}, RoleViewer},
	} {
		user := uuid.New()
		if err := svc.EnsureUserFromToken(ctx, Identity{TenantID: tenant, AuthUserID: user, Email: "x@y.z", GlobalRoles: c.global}); err != nil {
			t.Fatal(err)
		}
		res, err := svc.Resolve(ctx, tenant, user)
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Roles) != 1 || res.Roles[0] != c.want {
			t.Errorf("%v -> %v, want [%s]", c.global, res.Roles, c.want)
		}
	}

	// A viewer promoted to staff in auth-api becomes a member on the next request.
	user := uuid.New()
	_ = svc.EnsureUserFromToken(ctx, Identity{TenantID: tenant, AuthUserID: user, GlobalRoles: []string{"member"}})
	if has(t, svc, tenant, user, PermTasksAdd) {
		t.Fatal("a viewer cannot add tasks")
	}
	_ = svc.EnsureUserFromToken(ctx, Identity{TenantID: tenant, AuthUserID: user, GlobalRoles: []string{"staff"}})
	if !has(t, svc, tenant, user, PermTasksAdd) || !has(t, svc, tenant, user, PermQAView) {
		t.Fatal("a member adds tasks and views QA (run implies view)")
	}
	if has(t, svc, tenant, user, PermProjectsManage) || has(t, svc, tenant, user, PermQAManage) {
		t.Fatal("a member holds no manage codes")
	}
}

// An admin's assignment replaces the automatic role and survives later sign-ins; revoking it hands
// back the default; nobody changes their own roles.
func TestAssignAndRevokeRole(t *testing.T) {
	svc := seeded(t)
	ctx := context.Background()
	tenant, admin, user := uuid.New(), uuid.New(), uuid.New()
	_ = svc.EnsureUserFromToken(ctx, Identity{TenantID: tenant, AuthUserID: user, GlobalRoles: []string{"staff"}})

	if err := svc.AssignRole(ctx, tenant, user, RoleProjectManager, admin); err != nil {
		t.Fatal(err)
	}
	_ = svc.EnsureUserFromToken(ctx, Identity{TenantID: tenant, AuthUserID: user, GlobalRoles: []string{"member"}})
	res, _ := svc.Resolve(ctx, tenant, user)
	if len(res.Roles) != 1 || res.Roles[0] != RoleProjectManager {
		t.Fatalf("roles after assignment = %v, want [project_manager] only", res.Roles)
	}
	if !has(t, svc, tenant, user, PermMembersManage) || !has(t, svc, tenant, user, PermQARun) {
		t.Fatal("a project manager manages members and runs QA")
	}
	if err := svc.AssignRole(ctx, tenant, user, "nope", admin); err != ErrRoleNotFound {
		t.Errorf("unknown role: %v", err)
	}
	if err := svc.AssignRole(ctx, tenant, admin, RoleAdmin, admin); err != ErrSelfChange {
		t.Errorf("self assignment: %v", err)
	}
	if err := svc.RevokeRole(ctx, tenant, user, RoleProjectManager, admin); err != nil {
		t.Fatal(err)
	}
	if err := svc.RevokeRole(ctx, tenant, user, RoleProjectManager, admin); err != ErrNotAssigned {
		t.Errorf("second revoke: %v", err)
	}
	if has(t, svc, tenant, user, PermMembersManage) {
		t.Error("revoked role still grants")
	}
	// Assigning someone who never signed in creates their local user.
	stranger := uuid.New()
	if err := svc.AssignRole(ctx, tenant, stranger, RoleViewer, admin); err != nil {
		t.Fatal(err)
	}
	if !has(t, svc, tenant, stranger, PermProjectsView) {
		t.Error("assigned viewer cannot view")
	}
	// Roles are per tenant.
	if has(t, svc, uuid.New(), stranger, PermProjectsView) {
		t.Error("a role leaked into another tenant")
	}
}
