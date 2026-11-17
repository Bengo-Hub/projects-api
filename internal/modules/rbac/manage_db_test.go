package rbac

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	sharedcache "github.com/Bengo-Hub/cache"
	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	"github.com/bengobox/projects-service/internal/ent"
	entprojectsuser "github.com/bengobox/projects-service/internal/ent/projectsuser"
	entrole "github.com/bengobox/projects-service/internal/ent/role"
	entassign "github.com/bengobox/projects-service/internal/ent/userroleassignment"
	"github.com/bengobox/projects-service/internal/testutil"
)

func signIn(t *testing.T, svc *Service, tenant uuid.UUID, global ...string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	if err := svc.EnsureUserFromToken(context.Background(), Identity{TenantID: tenant, AuthUserID: id, Email: id.String()[:8] + "@acme.test", GlobalRoles: global}); err != nil {
		t.Fatal(err)
	}
	return id
}

func globalRole(t *testing.T, svc *Service, code string) *ent.Role {
	t.Helper()
	r, err := svc.client.Role.Query().Where(entrole.RoleCode(code), entrole.TenantIDIsNil()).Only(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func without(codes []string, drop string) []string {
	out := make([]string, 0, len(codes))
	for _, c := range codes {
		if c != drop {
			out = append(out, c)
		}
	}
	return out
}

func memberCodes() []string {
	for _, r := range Roles {
		if r.Code == RoleMember {
			return r.Permissions
		}
	}
	return nil
}

// The first edit of a default role makes this tenant's copy, moves this tenant's holders to it in
// the same transaction and leaves every other tenant on the default.
func TestCopyOnWriteMovesAssignments(t *testing.T) {
	svc := seeded(t)
	ctx := context.Background()
	tenant, other := uuid.New(), uuid.New()
	ana, ben := signIn(t, svc, tenant, "staff"), signIn(t, svc, tenant, "staff")
	elsewhere := signIn(t, svc, other, "staff")
	member := globalRole(t, svc, RoleMember)

	role, before, copied, err := svc.SetRolePermissions(ctx, tenant, nil, member.ID, without(memberCodes(), PermTasksAdd))
	if err != nil {
		t.Fatal(err)
	}
	if !copied || role.TenantID == nil || *role.TenantID != tenant || role.RoleCode != RoleMember || role.ID == member.ID {
		t.Fatalf("copy = %+v copied=%v, want a tenant copy of member", role, copied)
	}
	if !slices.Contains(before, PermTasksAdd) {
		t.Fatalf("before = %v, want the default's codes", before)
	}
	for _, u := range []uuid.UUID{ana, ben} {
		if has(t, svc, tenant, u, PermTasksAdd) {
			t.Fatal("a holder in this tenant still adds tasks")
		}
		if !has(t, svc, tenant, u, PermTasksView) {
			t.Fatal("a holder lost a code the copy kept")
		}
	}
	if !has(t, svc, other, elsewhere, PermTasksAdd) {
		t.Fatal("another tenant's member changed")
	}
	onDefault, err := svc.client.UserRoleAssignment.Query().Where(entassign.TenantID(tenant), entassign.RoleID(member.ID)).Count(ctx)
	if err != nil || onDefault != 0 {
		t.Fatalf("assignments left on the default: %d %v", onDefault, err)
	}
	roles, err := svc.ListRoles(ctx, tenant)
	if err != nil {
		t.Fatal(err)
	}
	var seen int
	for _, r := range roles {
		if r.Code != RoleMember {
			continue
		}
		seen++
		if r.Scope != "tenant" || !r.IsCustomized || r.Holders != 2 || r.ID != role.ID {
			t.Errorf("member in the list = %+v", r)
		}
	}
	if seen != 1 {
		t.Errorf("member listed %d times, want once (the copy replaces the default)", seen)
	}
	// A second edit changes the copy in place.
	again, _, copiedAgain, err := svc.SetRolePermissions(ctx, tenant, nil, role.ID, memberCodes())
	if err != nil || copiedAgain || again.ID != role.ID {
		t.Fatalf("second edit = %v copied=%v err=%v", again, copiedAgain, err)
	}
	// Editing by the default's id after the copy exists also lands on the copy.
	third, _, copiedThird, err := svc.SetRolePermissions(ctx, tenant, nil, member.ID, memberCodes())
	if err != nil || copiedThird || third.ID != role.ID {
		t.Fatalf("edit by the default id = %v copied=%v err=%v", third, copiedThird, err)
	}
}

// After a default role is customised, sign-in resolves the tenant copy first: a new user gets the
// copy, and an existing holder never gets the default back beside it.
func TestJITKeepsCustomisedDefaultRole(t *testing.T) {
	svc := seeded(t)
	ctx := context.Background()
	tenant := uuid.New()
	ana := signIn(t, svc, tenant, "staff")
	tenantCopy, _, _, err := svc.SetRolePermissions(ctx, tenant, nil, globalRole(t, svc, RoleMember).ID, without(memberCodes(), PermTasksAdd))
	if err != nil {
		t.Fatal(err)
	}
	// A fresh service has no JIT throttle, so sign-in really runs again.
	fresh := NewService(svc.client, nil, zap.NewNop())
	if err := fresh.EnsureUserFromToken(ctx, Identity{TenantID: tenant, AuthUserID: ana, GlobalRoles: []string{"staff"}}); err != nil {
		t.Fatal(err)
	}
	held, err := fresh.client.UserRoleAssignment.Query().
		Where(entassign.TenantID(tenant), entassign.HasUserWith(entprojectsuser.AuthServiceUserID(ana))).All(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(held) != 1 || held[0].RoleID != tenantCopy.ID {
		t.Fatalf("ana holds %+v, want the copy only", held)
	}
	ben := signIn(t, fresh, tenant, "manager")
	res, err := fresh.Resolve(ctx, tenant, ben)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Roles) != 1 || res.Roles[0] != RoleMember || slices.Contains(res.Perms, PermTasksAdd) {
		t.Fatalf("a new member resolves %+v, want the customised member", res)
	}
}

// The grant ceiling: nobody gives the client role; projects_admin and projects.projects.manage
// need admin tier; anyone else gives only roles inside their own permissions.
func TestCheckGrantCeiling(t *testing.T) {
	svc := seeded(t)
	ctx := context.Background()
	tenant := uuid.New()
	member := Expand(memberCodes())

	if err := svc.CheckGrant(ctx, tenant, RoleViewer, member, false); err != nil {
		t.Errorf("a member giving viewer: %v", err)
	}
	if err := svc.CheckGrant(ctx, tenant, RoleProjectManager, member, false); !errors.Is(err, ErrGrantAbove) {
		t.Errorf("a member giving project manager: %v, want ErrGrantAbove", err)
	}
	if err := svc.CheckGrant(ctx, tenant, RoleAdmin, AllCodes(), false); !errors.Is(err, ErrAdminOnly) {
		t.Errorf("projects_admin without admin tier: %v", err)
	}
	if err := svc.CheckGrant(ctx, tenant, RoleAdmin, nil, true); err != nil {
		t.Errorf("projects_admin by admin tier: %v", err)
	}
	if err := svc.CheckGrant(ctx, tenant, RoleClient, AllCodes(), true); !errors.Is(err, ErrReservedRole) {
		t.Errorf("the client role: %v, want ErrReservedRole", err)
	}
	if err := svc.CheckGrant(ctx, tenant, "nope", AllCodes(), true); !errors.Is(err, ErrRoleNotFound) {
		t.Errorf("unknown role: %v", err)
	}
	wide, err := svc.CreateCustomRole(ctx, tenant, RoleInput{Name: "Portfolio lead", PermissionCodes: []string{PermProjectsManage}})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.CheckGrant(ctx, tenant, wide.RoleCode, AllCodes(), false); !errors.Is(err, ErrAdminOnly) {
		t.Errorf("a role lifting membership scope without admin tier: %v", err)
	}
	// The assignment itself refuses the client role and a past end date.
	admin := uuid.New()
	if _, err := svc.AssignRoleUntil(ctx, tenant, uuid.New(), RoleClient, admin, nil); !errors.Is(err, ErrReservedRole) {
		t.Errorf("assigning client: %v", err)
	}
	past := time.Now().Add(-time.Hour)
	if _, err := svc.AssignRoleUntil(ctx, tenant, uuid.New(), RoleViewer, admin, &past); !errors.Is(err, ErrInvalidExpiry) {
		t.Errorf("a past end date: %v", err)
	}
}

// Nobody removes their own projects.rbac.manage through a role edit; nobody deactivates or
// reactivates themselves; the last active projects administrator can be neither deactivated nor
// have the role removed.
func TestSelfLockoutAndLastAdmin(t *testing.T) {
	svc := seeded(t)
	ctx := context.Background()
	tenant := uuid.New()
	boss := signIn(t, svc, tenant, "admin")
	lead := signIn(t, svc, tenant, "staff")
	lead2 := signIn(t, svc, tenant, "staff")
	teamLead, err := svc.CreateCustomRole(ctx, tenant, RoleInput{Name: "Team lead", PermissionCodes: []string{PermRBACManage, PermProjectsView}})
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range []uuid.UUID{lead, lead2} {
		if err := svc.AssignRole(ctx, tenant, u, teamLead.RoleCode, boss); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, _, err := svc.SetRolePermissions(ctx, tenant, &lead, teamLead.ID, []string{PermProjectsView}); !errors.Is(err, ErrSelfLockout) {
		t.Fatalf("removing one's own rbac.manage: %v, want ErrSelfLockout", err)
	}
	if _, _, _, err := svc.SetRolePermissions(ctx, tenant, &boss, teamLead.ID, []string{PermProjectsView}); err != nil {
		t.Fatalf("someone who does not hold the role may change it: %v", err)
	}

	if _, err := svc.SetUserStatus(ctx, tenant, boss, StatusInactive, boss); !errors.Is(err, ErrSelfStatus) {
		t.Errorf("self deactivation: %v", err)
	}
	if _, err := svc.SetUserStatus(ctx, tenant, boss, StatusInactive, lead); !errors.Is(err, ErrLastAdmin) {
		t.Errorf("deactivating the last admin: %v, want ErrLastAdmin", err)
	}
	if err := svc.RevokeRole(ctx, tenant, boss, RoleAdmin, lead); !errors.Is(err, ErrLastAdmin) {
		t.Errorf("revoking the last admin: %v, want ErrLastAdmin", err)
	}
	if err := svc.AssignRole(ctx, tenant, lead2, RoleAdmin, boss); err != nil {
		t.Fatal(err)
	}
	if changed, err := svc.SetUserStatus(ctx, tenant, boss, StatusInactive, lead2); err != nil || !changed {
		t.Fatalf("with a second admin, deactivation goes through: %v %v", changed, err)
	}
	if changed, err := svc.SetUserStatus(ctx, tenant, boss, StatusInactive, lead2); err != nil || changed {
		t.Errorf("deactivating twice changes nothing: %v %v", changed, err)
	}
	if _, err := svc.SetUserStatus(ctx, tenant, boss, "paused", lead2); !errors.Is(err, ErrInvalidStatus) {
		t.Errorf("unknown status: %v", err)
	}
	if _, err := svc.SetUserStatus(ctx, tenant, uuid.New(), StatusInactive, lead2); !errors.Is(err, ErrUserNotFound) {
		t.Errorf("unknown user: %v", err)
	}
}

func cachedService(t *testing.T) *Service {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	svc := NewService(testutil.Client(t), sharedcache.New(rdb, zap.NewNop()), zap.NewNop())
	if err := svc.Seed(context.Background()); err != nil {
		t.Fatal(err)
	}
	return svc
}

// With the Redis permission cache on: deactivation denies access at once and survives sign-in,
// reactivation restores it, and a role permission change reaches every holder at once.
func TestDeactivationAndCacheInvalidation(t *testing.T) {
	svc := cachedService(t)
	ctx := context.Background()
	tenant := uuid.New()
	boss := signIn(t, svc, tenant, "admin")
	ana := signIn(t, svc, tenant, "staff")
	ben := signIn(t, svc, tenant, "staff")
	if !has(t, svc, tenant, ana, PermTasksAdd) || !has(t, svc, tenant, ben, PermTasksAdd) {
		t.Fatal("members add tasks") // now cached
	}
	if _, err := svc.SetUserStatus(ctx, tenant, ana, StatusInactive, boss); err != nil {
		t.Fatal(err)
	}
	if has(t, svc, tenant, ana, PermTasksAdd) {
		t.Fatal("a deactivated user still holds permissions (stale cache)")
	}
	u, _ := svc.User(ctx, tenant, ana)
	if u.Status != StatusInactive || u.DeactivatedAt == nil || u.DeactivatedBy == nil || *u.DeactivatedBy != boss {
		t.Fatalf("deactivated row = %+v", u)
	}
	// Sign-in (a fresh pod, no throttle) never reactivates.
	fresh := NewService(svc.client, svc.cache, zap.NewNop())
	if err := fresh.EnsureUserFromToken(ctx, Identity{TenantID: tenant, AuthUserID: ana, Email: "ana@acme.test", GlobalRoles: []string{"staff"}}); err != nil {
		t.Fatal(err)
	}
	if has(t, fresh, tenant, ana, PermProjectsView) {
		t.Fatal("sign-in reactivated a deactivated user")
	}
	if _, err := svc.SetUserStatus(ctx, tenant, ana, StatusActive, boss); err != nil {
		t.Fatal(err)
	}
	if !has(t, svc, tenant, ana, PermTasksAdd) {
		t.Fatal("reactivation did not restore access")
	}
	u, _ = svc.User(ctx, tenant, ana)
	if u.DeactivatedAt != nil || u.DeactivatedBy != nil {
		t.Fatal("reactivation kept the deactivation stamp")
	}
	// Ben's answer is cached; the role edit clears the whole tenant.
	if _, _, _, err := svc.SetRolePermissions(ctx, tenant, nil, globalRole(t, svc, RoleMember).ID, without(memberCodes(), PermTasksAdd)); err != nil {
		t.Fatal(err)
	}
	if has(t, svc, tenant, ben, PermTasksAdd) {
		t.Fatal("a role edit did not reach a holder with a cached answer")
	}
}

// Custom roles: validation, reserved codes, copy, rename, delete rules; assignments with end dates.
func TestCustomRolesAndAssignments(t *testing.T) {
	svc := seeded(t)
	ctx := context.Background()
	tenant := uuid.New()
	boss := signIn(t, svc, tenant, "admin")
	ana := signIn(t, svc, tenant, "staff")

	for name, c := range map[string]struct {
		in   RoleInput
		want error
	}{
		"bad code":      {RoleInput{Name: "Lead", Code: "9lead"}, ErrInvalidRole},
		"no name":       {RoleInput{Name: " "}, ErrInvalidRole},
		"reserved":      {RoleInput{Name: "Client"}, ErrReservedRole},
		"admin code":    {RoleInput{Name: "x", Code: RoleAdmin}, ErrReservedRole},
		"taken":         {RoleInput{Name: "Member"}, ErrRoleCodeTaken},
		"unknown code":  {RoleInput{Name: "Lead", PermissionCodes: []string{"projects.nope.view"}}, ErrUnknownPermission},
		"portal mixing": {RoleInput{Name: "Lead", PermissionCodes: []string{PermPortalView, PermTasksView}}, ErrPortalMix},
	} {
		if _, err := svc.CreateCustomRole(ctx, tenant, c.in); !errors.Is(err, c.want) {
			t.Errorf("%s: %v, want %v", name, err, c.want)
		}
	}
	lead, err := svc.CreateCustomRole(ctx, tenant, RoleInput{Name: "Site lead", PermissionCodes: []string{PermProjectsView, PermProjectsView, PermTasksManage}})
	if err != nil {
		t.Fatal(err)
	}
	if lead.RoleCode != "site_lead" {
		t.Errorf("code from the name = %q", lead.RoleCode)
	}
	if codes, _ := svc.RolePermissionCodes(ctx, tenant, lead.ID); !slices.Equal(codes, []string{PermProjectsView, PermTasksManage}) {
		t.Errorf("codes = %v", codes)
	}
	pm := globalRole(t, svc, RoleProjectManager)
	clone, err := svc.CloneRole(ctx, tenant, pm.ID, RoleInput{Name: "Senior PM"})
	if err != nil {
		t.Fatal(err)
	}
	pmCodes, _ := svc.RolePermissionCodes(ctx, tenant, pm.ID)
	cloneCodes, _ := svc.RolePermissionCodes(ctx, tenant, clone.ID)
	if !slices.Equal(pmCodes, cloneCodes) || clone.Description != pm.Description {
		t.Errorf("clone codes %v, want %v", cloneCodes, pmCodes)
	}
	// Another tenant never sees this tenant's roles.
	if _, err := svc.RolePermissionCodes(ctx, uuid.New(), lead.ID); !errors.Is(err, ErrRoleNotFound) {
		t.Errorf("another tenant reading the role: %v", err)
	}
	// Reserved roles are read only.
	if _, _, err := svc.UpdateRoleDetails(ctx, tenant, globalRole(t, svc, RoleAdmin).ID, ptr("Boss"), nil); !errors.Is(err, ErrReservedRole) {
		t.Errorf("renaming projects_admin: %v", err)
	}
	if _, _, _, err := svc.SetRolePermissions(ctx, tenant, nil, globalRole(t, svc, RoleClient).ID, []string{PermPortalView}); !errors.Is(err, ErrReservedRole) {
		t.Errorf("editing client: %v", err)
	}
	// Renaming a default role copies it for the tenant.
	renamed, copied, err := svc.UpdateRoleDetails(ctx, tenant, globalRole(t, svc, RoleViewer).ID, ptr("Observer"), nil)
	if err != nil || !copied || renamed.Name != "Observer" || renamed.TenantID == nil {
		t.Fatalf("rename = %+v copied=%v err=%v", renamed, copied, err)
	}
	if globalRole(t, svc, RoleViewer).Name != "Viewer" {
		t.Error("the default viewer was renamed for everyone")
	}

	// An end date in the future grants until then; an ended one shows as expired and grants nothing.
	until := time.Now().Add(48 * time.Hour)
	if _, err := svc.AssignRoleUntil(ctx, tenant, ana, lead.RoleCode, boss, &until); err != nil {
		t.Fatal(err)
	}
	if !has(t, svc, tenant, ana, PermTasksDelete) {
		t.Fatal("the role with an end date does not grant")
	}
	if _, err := svc.DeleteRole(ctx, tenant, lead.ID); !errors.Is(err, ErrRoleInUse) {
		t.Errorf("deleting a held role: %v", err)
	}
	if _, err := svc.DeleteRole(ctx, tenant, pm.ID); !errors.Is(err, ErrSystemRole) {
		t.Errorf("deleting a default role: %v", err)
	}
	svc.client.UserRoleAssignment.Update().Where(entassign.RoleID(lead.ID)).SetExpiresAt(time.Now().Add(-time.Minute)).ExecX(ctx)
	if has(t, svc, tenant, ana, PermTasksDelete) {
		t.Fatal("an ended role still grants")
	}
	page, total, err := svc.ListUsersPaged(ctx, tenant, "", 50, 0)
	if err != nil || total != 2 {
		t.Fatalf("users = %d %v", total, err)
	}
	var anaView *UserView
	for i := range page {
		if page[i].AuthUserID == ana {
			anaView = &page[i]
		}
	}
	if anaView == nil || slices.Contains(anaView.Roles, lead.RoleCode) {
		t.Fatalf("ana's roles = %+v, the ended role must not be listed as held", anaView)
	}
	var ended bool
	for _, a := range anaView.Assignments {
		if a.RoleCode == lead.RoleCode && a.Expired && a.ExpiresAt != nil && !a.Automatic {
			ended = true
		}
	}
	if !ended {
		t.Errorf("ana's assignments = %+v, want the ended lead role", anaView.Assignments)
	}
	if found, n, _ := svc.ListUsersPaged(ctx, tenant, anaView.Email[:4], 50, 0); n != 1 || found[0].AuthUserID != ana {
		t.Errorf("search by email = %+v (%d)", found, n)
	}
	// Nobody actively holds it now: it can go, with its ended assignment.
	if _, err := svc.DeleteRole(ctx, tenant, lead.ID); err != nil {
		t.Fatalf("deleting an unheld role: %v", err)
	}
	if _, err := svc.RolePermissionCodes(ctx, tenant, lead.ID); !errors.Is(err, ErrRoleNotFound) {
		t.Error("the deleted role is still there")
	}
}

func ptr(s string) *string { return &s }

// Audit rows are per tenant and listed newest first.
func TestAuditLogs(t *testing.T) {
	svc := seeded(t)
	ctx := context.Background()
	tenant, actor := uuid.New(), uuid.New()
	svc.Audit(ctx, AuditEntry{TenantID: tenant, ActorID: actor, ActorEmail: "boss@acme.test", Action: "role.created", ResourceType: "role", ResourceID: "r1", Changes: map[string]any{"name": "Lead"}, IP: "10.0.0.1"})
	time.Sleep(5 * time.Millisecond)
	svc.Audit(ctx, AuditEntry{TenantID: tenant, ActorID: actor, Action: "user.deactivated", ResourceType: "user", ResourceID: "u1"})
	svc.Audit(ctx, AuditEntry{TenantID: uuid.New(), ActorID: actor, Action: "role.deleted", ResourceType: "role", ResourceID: "r2"})
	// No actor (a service call): nothing to attribute, nothing written.
	svc.Audit(ctx, AuditEntry{TenantID: tenant, Action: "role.deleted", ResourceType: "role", ResourceID: "r3"})

	rows, total, err := svc.AuditLogs(ctx, tenant, 20, 0)
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 || rows[0].Action != "user.deactivated" || rows[1].Action != "role.created" {
		t.Fatalf("audit = %d %+v", total, rows)
	}
	if rows[1].Changes["name"] != "Lead" || rows[1].IPAddress != "10.0.0.1" || rows[1].ActorEmail != "boss@acme.test" {
		t.Errorf("row = %+v", rows[1])
	}
}
