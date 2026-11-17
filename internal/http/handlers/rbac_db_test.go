package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"

	authclient "github.com/Bengo-Hub/shared-auth-client"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/projects-service/internal/http/authz"
	"github.com/bengobox/projects-service/internal/modules/rbac"
	"github.com/bengobox/projects-service/internal/services/usersync"
	"github.com/bengobox/projects-service/internal/testutil"
)

// fakeAuthMembers stands in for auth-api's S2S tenant member route and records what it was sent.
type fakeAuthMembers struct {
	mu     sync.Mutex
	calls  int
	emails []string
	userID uuid.UUID
}

func (f *fakeAuthMembers) seen() (int, []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls, append([]string(nil), f.emails...)
}

func (f *fakeAuthMembers) server(t *testing.T, tenant uuid.UUID) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-Key") != "test-key" || !strings.HasSuffix(r.URL.Path, "/s2s/tenants/"+tenant.String()+"/members") {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		var body struct {
			Email string `json:"email"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		f.calls++
		f.emails = append(f.emails, body.Email)
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{"user_id": f.userID.String(), "tenant_id": tenant.String(), "temp_password": "not-a-real-password"}) // secret-scan:allow (fake auth-api answer)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// rbacFixture is the RBAC handler on a real database, behind a router that hands every request
// the access of a projects administrator of tenant (as authz.Resolve would).
type rbacFixture struct {
	svc    *rbac.Service
	h      http.Handler
	tenant uuid.UUID
	admin  uuid.UUID
	auth   *fakeAuthMembers
}

func newRBACFixture(t *testing.T) *rbacFixture {
	t.Helper()
	ctx := context.Background()
	client := testutil.Client(t)
	svc := rbac.NewService(client, nil, zap.NewNop())
	if err := svc.Seed(ctx); err != nil {
		t.Fatal(err)
	}
	f := &rbacFixture{svc: svc, tenant: uuid.New(), admin: uuid.New(), auth: &fakeAuthMembers{userID: uuid.New()}}
	if err := svc.EnsureUserFromToken(ctx, rbac.Identity{TenantID: f.tenant, AuthUserID: f.admin, Email: "boss@acme.test", GlobalRoles: []string{"admin"}}); err != nil {
		t.Fatal(err)
	}
	srv := f.auth.server(t, f.tenant)
	handler := NewRBACHandler(zap.NewNop(), svc, usersync.NewService(srv.URL, "test-key", zap.NewNop()))
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			res, err := svc.Resolve(req.Context(), f.tenant, f.admin)
			if err != nil {
				t.Error(err)
			}
			claims := &authclient.Claims{TenantID: f.tenant.String(), Email: "boss@acme.test", Roles: []string{"staff"}}
			claims.Subject = f.admin.String()
			a := &authz.Access{TenantID: f.tenant, AuthUserID: f.admin, Email: "boss@acme.test", Roles: res.Roles, Perms: res.Perms, Claims: claims}
			next.ServeHTTP(w, req.WithContext(authz.WithAccess(req.Context(), a)))
		})
	})
	handler.RegisterRoutes(r)
	f.h = r
	return f
}

func (f *rbacFixture) do(t *testing.T, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	f.h.ServeHTTP(rec, req)
	return rec
}

func (f *rbacFixture) actions(t *testing.T) []string {
	t.Helper()
	logs, _, err := f.svc.AuditLogs(context.Background(), f.tenant, 100, 0)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]string, 0, len(logs))
	for i := len(logs) - 1; i >= 0; i-- {
		out = append(out, logs[i].Action)
	}
	return out
}

// Inviting by email goes through auth-api once, records the person locally with the role and an
// audit row; inviting someone already here gives the role without calling auth-api again.
func TestInviteThroughAuthAPI(t *testing.T) {
	f := newRBACFixture(t)
	w := f.do(t, http.MethodPost, "/users", `{"email":" New.Person@Acme.test ","role_code":"viewer"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("invite: %d %s", w.Code, w.Body.String())
	}
	var out struct {
		UserID   uuid.UUID `json:"user_id"`
		Email    string    `json:"email"`
		Created  bool      `json:"created"`
		RoleCode string    `json:"role_code"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	if out.UserID != f.auth.userID || out.Email != "new.person@acme.test" || !out.Created || out.RoleCode != rbac.RoleViewer {
		t.Fatalf("invite answer = %+v", out)
	}
	if calls, emails := f.auth.seen(); calls != 1 || emails[0] != "new.person@acme.test" {
		t.Fatalf("auth-api calls = %d %v", calls, emails)
	}
	detail, err := f.svc.UserDetail(context.Background(), f.tenant, f.auth.userID)
	if err != nil || detail.Email != "new.person@acme.test" || len(detail.Roles) != 1 || detail.Roles[0] != rbac.RoleViewer {
		t.Fatalf("local user = %+v %v", detail, err)
	}

	// Again, as a member: the person is known here, so auth-api is not called (its member route
	// would replace their global roles).
	w = f.do(t, http.MethodPost, "/users", `{"email":"new.person@acme.test","role_code":"member"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("second invite: %d %s", w.Code, w.Body.String())
	}
	if calls, _ := f.auth.seen(); calls != 1 {
		t.Fatalf("auth-api called again: %d", calls)
	}
	// Refusals never reach auth-api.
	for _, body := range []string{`{"email":"x@acme.test","role_code":"client"}`, `{"email":"x@acme.test","roles":["admin"]}`, `{"email":"not-an-email"}`} {
		if w := f.do(t, http.MethodPost, "/users", body); w.Code < 400 || w.Code >= 500 {
			t.Errorf("%s: %d, want a refusal", body, w.Code)
		}
	}
	if calls, _ := f.auth.seen(); calls != 1 {
		t.Fatalf("a refused invite reached auth-api: %d calls", calls)
	}
	got := f.actions(t)
	if len(got) != 2 || got[0] != "user.invited" || got[1] != "user.invited" {
		t.Fatalf("audit = %v", got)
	}
}

// Every role and access change writes its audit row; the copy on write of a default role writes
// role.customized before role.permissions_updated, and the answer carries the copy.
func TestRoleChangesAreAudited(t *testing.T) {
	f := newRBACFixture(t)
	ctx := context.Background()
	ana := uuid.New()
	if err := f.svc.EnsureUserFromToken(ctx, rbac.Identity{TenantID: f.tenant, AuthUserID: ana, Email: "ana@acme.test", GlobalRoles: []string{"staff"}}); err != nil {
		t.Fatal(err)
	}

	w := f.do(t, http.MethodPost, "/rbac/roles", `{"name":"Site lead","permission_codes":["projects.projects.view","projects.tasks.manage"]}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	var lead rbac.RoleView
	_ = json.Unmarshal(w.Body.Bytes(), &lead)
	if lead.Code != "site_lead" || lead.Scope != "tenant" || len(lead.Permissions) != 2 {
		t.Fatalf("created = %+v", lead)
	}
	if w := f.do(t, http.MethodPost, "/rbac/roles/"+lead.ID.String()+"/clone", `{"name":"Site lead copy"}`); w.Code != http.StatusCreated {
		t.Fatalf("clone: %d %s", w.Code, w.Body.String())
	}
	if w := f.do(t, http.MethodPost, "/rbac/users/"+ana.String()+"/roles", `{"role_code":"site_lead","expires_at":"2099-01-31T23:59:59Z"}`); w.Code != http.StatusNoContent {
		t.Fatalf("assign: %d %s", w.Code, w.Body.String())
	}
	if w := f.do(t, http.MethodPost, "/rbac/users/"+ana.String()+"/roles", `{"role_code":"viewer","expires_at":"2001-01-01T00:00:00Z"}`); w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("a past end date: %d, want 422", w.Code)
	}
	if w := f.do(t, http.MethodDelete, "/rbac/roles/"+lead.ID.String(), ``); w.Code != http.StatusConflict {
		t.Fatalf("deleting a held role: %d, want 409", w.Code)
	}

	var roles struct {
		Data []rbac.RoleView `json:"data"`
	}
	_ = json.Unmarshal(f.do(t, http.MethodGet, "/rbac/roles", ``).Body.Bytes(), &roles)
	var viewer rbac.RoleView
	for _, r := range roles.Data {
		if r.Code == rbac.RoleViewer {
			viewer = r
		}
	}
	if viewer.Scope != "global" {
		t.Fatalf("viewer = %+v", viewer)
	}
	w = f.do(t, http.MethodPut, "/rbac/roles/"+viewer.ID.String()+"/permissions", `{"permission_codes":["projects.projects.view"]}`)
	if w.Code != http.StatusOK {
		t.Fatalf("customise viewer: %d %s", w.Code, w.Body.String())
	}
	var customised rbac.RoleView
	_ = json.Unmarshal(w.Body.Bytes(), &customised)
	if customised.ID == viewer.ID || !customised.IsCustomized || customised.Code != rbac.RoleViewer {
		t.Fatalf("customised = %+v", customised)
	}
	if w := f.do(t, http.MethodPut, "/rbac/roles/"+viewer.ID.String()+"/permissions", `{}`); w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("no permission_codes: %d, want 422", w.Code)
	}
	if w := f.do(t, http.MethodPatch, "/rbac/users/"+ana.String(), `{"status":"inactive"}`); w.Code != http.StatusOK {
		t.Fatalf("deactivate: %d %s", w.Code, w.Body.String())
	}
	if w := f.do(t, http.MethodPatch, "/rbac/users/"+f.admin.String(), `{"status":"inactive"}`); w.Code != http.StatusConflict {
		t.Fatalf("deactivating oneself: %d, want 409", w.Code)
	}
	if w := f.do(t, http.MethodPatch, "/rbac/users/"+ana.String(), `{"status":"active"}`); w.Code != http.StatusOK {
		t.Fatalf("reactivate: %d", w.Code)
	}
	if w := f.do(t, http.MethodDelete, "/rbac/users/"+ana.String()+"/roles/site_lead", ``); w.Code != http.StatusNoContent {
		t.Fatalf("revoke: %d %s", w.Code, w.Body.String())
	}
	if w := f.do(t, http.MethodDelete, "/rbac/roles/"+lead.ID.String(), ``); w.Code != http.StatusNoContent {
		t.Fatalf("delete: %d %s", w.Code, w.Body.String())
	}

	want := []string{"role.created", "role.cloned", "role.assigned", "role.customized", "role.permissions_updated",
		"user.deactivated", "user.reactivated", "role.revoked", "role.deleted"}
	// Two rows of one request can share a timestamp on a coarse clock, so compare as sets.
	got := f.actions(t)
	sortedWant := append([]string(nil), want...)
	sort.Strings(got)
	sort.Strings(sortedWant)
	if strings.Join(got, ",") != strings.Join(sortedWant, ",") {
		t.Fatalf("audit = %v\nwant    %v", got, want)
	}
	logs, _, _ := f.svc.AuditLogs(ctx, f.tenant, 100, 0)
	for _, l := range logs {
		if l.Action == "role.permissions_updated" {
			if l.ActorUserID != f.admin || l.ResourceType != "role" || l.Changes["before"] == nil || l.Changes["after"] == nil {
				t.Errorf("permissions row = %+v", l)
			}
		}
	}
	// The audit list route pages newest first.
	var page struct {
		Data  []map[string]any `json:"data"`
		Total int              `json:"total"`
	}
	_ = json.Unmarshal(f.do(t, http.MethodGet, "/rbac/audit-logs?limit=2", ``).Body.Bytes(), &page)
	if page.Total != len(want) || len(page.Data) != 2 || page.Data[0]["action"] != "role.deleted" || page.Data[1]["action"] != "role.revoked" {
		t.Fatalf("audit page = %+v", page)
	}
}
