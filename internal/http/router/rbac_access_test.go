package router

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/bengobox/projects-service/internal/modules/rbac"
)

// codesRBAC resolves every user to a fixed set of codes and role codes, so a test can hold
// projects.rbac.manage without projects_admin. err makes Resolve fail.
type codesRBAC struct {
	perms   []string
	roles   []string
	err     error
	ensured []rbac.Identity
}

func (f *codesRBAC) EnsureUserFromToken(_ context.Context, id rbac.Identity) error {
	f.ensured = append(f.ensured, id)
	return nil
}

func (f *codesRBAC) Resolve(context.Context, uuid.UUID, uuid.UUID) (rbac.Resolved, error) {
	if f.err != nil {
		return rbac.Resolved{Roles: []string{}, Perms: []string{}}, f.err
	}
	return rbac.Resolved{Roles: f.roles, Perms: f.perms}, nil
}

func codesRouter(t *testing.T, f *codesRBAC) http.Handler {
	t.Helper()
	d := testDeps(t, &fakeRBAC{roles: map[uuid.UUID]string{}}, fakeMembers{})
	d.Access = f
	return New(d)
}

func codeOf(t *testing.T, body []byte) string {
	t.Helper()
	var m map[string]any
	_ = json.Unmarshal(body, &m)
	s, _ := m["code"].(string)
	return s
}

// Role edits, revokes, status changes and the invite need admin tier on top of
// projects.rbac.manage; reads and giving a role need only the permission.
func TestRoleManagementNeedsAdminTier(t *testing.T) {
	manager := &codesRBAC{perms: []string{rbac.PermRBACManage, rbac.PermProjectsView}, roles: []string{"team_lead"}}
	h := codesRouter(t, manager)
	staff := userClaims("staff")
	adminOnly := []struct{ method, path string }{
		{http.MethodPost, "/api/v1/acme/rbac/roles"},
		{http.MethodPost, "/api/v1/acme/rbac/roles/" + someID + "/clone"},
		{http.MethodPatch, "/api/v1/acme/rbac/roles/" + someID},
		{http.MethodPut, "/api/v1/acme/rbac/roles/" + someID + "/permissions"},
		{http.MethodDelete, "/api/v1/acme/rbac/roles/" + someID},
		{http.MethodDelete, "/api/v1/acme/rbac/users/" + someID + "/roles/member"},
		{http.MethodPatch, "/api/v1/acme/rbac/users/" + someID},
		{http.MethodPost, "/api/v1/acme/users"},
	}
	for _, c := range adminOnly {
		w := call(h, staff, c.method, c.path)
		if w.Code != http.StatusForbidden || codeOf(t, w.Body.Bytes()) != "admin_required" {
			t.Errorf("%s %s with rbac.manage only: %d %s, want 403 admin_required", c.method, c.path, w.Code, w.Body.String())
		}
	}
	// Reads and giving a role pass the gate (the handler has no service here: 503).
	for _, c := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/acme/rbac/roles"},
		{http.MethodGet, "/api/v1/acme/rbac/users"},
		{http.MethodGet, "/api/v1/acme/rbac/users/" + someID},
		{http.MethodGet, "/api/v1/acme/rbac/audit-logs"},
		{http.MethodGet, "/api/v1/acme/rbac/roles/" + someID + "/permissions"},
		{http.MethodPost, "/api/v1/acme/rbac/users/" + someID + "/roles"},
	} {
		if w := call(h, staff, c.method, c.path); w.Code == http.StatusForbidden || w.Code == http.StatusNotFound || w.Code == http.StatusMethodNotAllowed {
			t.Errorf("%s %s with rbac.manage: %d, want past the gate", c.method, c.path, w.Code)
		}
	}

	// projects_admin is admin tier without any admin global role.
	projectsAdmin := codesRouter(t, &codesRBAC{perms: rbac.AllCodes(), roles: []string{rbac.RoleAdmin}})
	for _, c := range adminOnly {
		if w := call(projectsAdmin, staff, c.method, c.path); w.Code == http.StatusForbidden {
			t.Errorf("%s %s as projects_admin: 403 %s", c.method, c.path, w.Body.String())
		}
	}
	// So is an organisation admin (global role).
	orgAdmin := codesRouter(t, &codesRBAC{perms: []string{rbac.PermRBACManage}, roles: []string{"team_lead"}})
	if w := call(orgAdmin, userClaims("admin"), http.MethodPost, "/api/v1/acme/rbac/roles"); w.Code == http.StatusForbidden {
		t.Errorf("an organisation admin creating a role: 403 %s", w.Body.String())
	}
	// Without rbac.manage even reads are refused.
	member := codesRouter(t, &codesRBAC{perms: []string{rbac.PermProjectsView}, roles: []string{rbac.RoleMember}})
	if w := call(member, staff, http.MethodGet, "/api/v1/acme/rbac/users"); w.Code != http.StatusForbidden || codeOf(t, w.Body.Bytes()) != "permission_denied" {
		t.Errorf("listing people without rbac.manage: %d", w.Code)
	}
}

// GET /people (pickers) needs projects.members.manage, projects.rbac.manage or
// projects.tenders.manage (the tender committee picker).
func TestPeoplePickerGate(t *testing.T) {
	staff := userClaims("staff")
	for _, perms := range [][]string{{rbac.PermMembersManage}, {rbac.PermRBACManage}, {rbac.PermTendersManage}} {
		h := codesRouter(t, &codesRBAC{perms: perms, roles: []string{"x"}})
		if w := call(h, staff, http.MethodGet, "/api/v1/acme/people?search=an"); w.Code == http.StatusForbidden || w.Code == http.StatusNotFound {
			t.Errorf("people with %v: %d", perms, w.Code)
		}
	}
	// Viewing a team or a tender is not enough.
	h := codesRouter(t, &codesRBAC{perms: []string{rbac.PermMembersView, rbac.PermTimeView, rbac.PermTendersView}, roles: []string{rbac.RoleMember}})
	if w := call(h, staff, http.MethodGet, "/api/v1/acme/people"); w.Code != http.StatusForbidden {
		t.Errorf("people with members.view only: %d, want 403", w.Code)
	}
}

// auth/me answers 503 when the permissions could not be read, never an empty profile.
func TestAuthMeUnavailableOnResolveError(t *testing.T) {
	h := codesRouter(t, &codesRBAC{err: errors.New("db down")})
	w := call(h, userClaims("staff"), http.MethodGet, "/api/v1/acme/auth/me")
	if w.Code != http.StatusServiceUnavailable || codeOf(t, w.Body.Bytes()) != "permissions_unavailable" {
		t.Fatalf("auth/me on a resolve error: %d %s", w.Code, w.Body.String())
	}
	// Every other route fails closed: no permissions.
	if w := call(h, userClaims("staff"), http.MethodGet, "/api/v1/acme/projects/not-a-uuid"); w.Code != http.StatusForbidden {
		t.Errorf("a gated route on a resolve error: %d, want 403", w.Code)
	}
}

// JIT provisions only when the token lets the user into projects; no user_services claim means
// every service.
func TestJITRespectsUserServices(t *testing.T) {
	f := &codesRBAC{perms: []string{rbac.PermProjectsView}, roles: []string{rbac.RoleViewer}}
	h := codesRouter(t, f)
	c := userClaims("staff")
	c.UserServices = []string{"pos", "treasury"}
	call(h, c, http.MethodGet, "/api/v1/acme/auth/me")
	if len(f.ensured) != 0 {
		t.Fatalf("provisioned a user auth-api keeps out of projects: %+v", f.ensured)
	}
	c.UserServices = []string{"projects"}
	call(h, c, http.MethodGet, "/api/v1/acme/auth/me")
	c.UserServices = nil
	call(h, c, http.MethodGet, "/api/v1/acme/auth/me")
	if len(f.ensured) != 2 {
		t.Fatalf("provisioning with projects allowed (or no claim): %d calls, want 2", len(f.ensured))
	}
}
