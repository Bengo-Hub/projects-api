package router

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	authclient "github.com/Bengo-Hub/shared-auth-client"
	"github.com/google/uuid"
	"go.uber.org/zap"

	handlers "github.com/bengobox/projects-service/internal/http/handlers"
	"github.com/bengobox/projects-service/internal/modules/rbac"
)

const (
	testTenant = "00000000-0000-0000-0000-00000000000a"
	testUser   = "00000000-0000-0000-0000-0000000000a1"
	someID     = "00000000-0000-0000-0000-000000000001"
)

// fakeRBAC hands out a fixed role set per user and records JIT calls.
// fakeRBAC mirrors rbac.Service: a role an admin assigned (assigned) sticks, while the automatic
// role (roles) follows the user's current global role on every request.
type fakeRBAC struct {
	roles    map[uuid.UUID]string
	assigned map[uuid.UUID]string
	ensured  []rbac.Identity
}

func (f *fakeRBAC) EnsureUserFromToken(_ context.Context, id rbac.Identity) error {
	f.ensured = append(f.ensured, id)
	if _, ok := f.assigned[id.AuthUserID]; !ok {
		f.roles[id.AuthUserID] = rbac.MapGlobalRoles(id.GlobalRoles)
	}
	return nil
}

func (f *fakeRBAC) Resolve(_ context.Context, _, user uuid.UUID) (rbac.Resolved, error) {
	code, ok := f.assigned[user]
	if !ok {
		code, ok = f.roles[user]
	}
	if !ok {
		return rbac.Resolved{Roles: []string{}, Perms: []string{}}, nil
	}
	for _, r := range rbac.Roles {
		if r.Code == code {
			return rbac.Resolved{Roles: []string{code}, Perms: rbac.Expand(r.Permissions)}, nil
		}
	}
	return rbac.Resolved{}, nil
}

type fakeMembers map[uuid.UUID]bool

func (m fakeMembers) IsMember(_ context.Context, _, projectID, _ uuid.UUID) (bool, error) {
	return m[projectID], nil
}

// claimsKey lets a test hand its claims to the fake authenticator.
type claimsKey struct{}

func testRouter(t *testing.T, r *fakeRBAC, members fakeMembers) http.Handler {
	t.Helper()
	log := zap.NewNop()
	return New(Deps{
		Log: log,
		Handlers: Handlers{
			Health:      handlers.NewHealthHandler(log, nil, nil, nil),
			RBAC:        handlers.NewRBACHandler(log, nil, nil),
			Projects:    handlers.NewProjectHandler(log, nil),
			Tasks:       handlers.NewTaskHandler(log, nil),
			Milestones:  handlers.NewMilestoneHandler(log, nil),
			Members:     handlers.NewMemberHandler(log, nil),
			Comments:    handlers.NewCommentHandler(log, nil),
			Attachments: handlers.NewAttachmentHandler(log, nil),
			Stream:      handlers.NewStreamHandler(log, nil),
			Activity:    handlers.NewActivityHandler(log, nil),
			Tenders:     handlers.NewTenderHandler(log, nil),
			Financials:  handlers.NewFinancialsHandler(log, nil),
			Time:        handlers.NewTimeHandler(log, nil),
		},
		Authenticate: func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				c, ok := req.Context().Value(claimsKey{}).(*authclient.Claims)
				if !ok {
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				next.ServeHTTP(w, req.WithContext(authclient.ContextWithClaims(req.Context(), c)))
			})
		},
		AllowedOrigins: []string{"*"},
		Access:         r,
		Members:        members,
		CRMLinks:       http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) }),
		QA:             QARoutes{Tenant: handlers.NewQAHandler(log, nil), Invitee: handlers.NewQAInviteeHandler(log, nil, true)},
		Platform:       PlatformRoutes{FieldKeys: handlers.NewFieldKeyHandler(nil, log)},
	})
}

func userClaims(roles ...string) *authclient.Claims {
	c := &authclient.Claims{TenantID: testTenant, TenantSlug: "acme", Email: "u@acme.test", Roles: roles,
		SubscriptionStatus: "ACTIVE", SubscriptionFeatures: []string{"project_management", "budget_tracking", "task_tracking", "team_collaboration", "gantt_chart"},
		ActiveServiceTags: []string{"projects"}}
	c.Subject = testUser
	return c
}

func call(h http.Handler, c *authclient.Claims, method, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	if c != nil {
		req = req.WithContext(context.WithValue(req.Context(), claimsKey{}, c))
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

// groupRoutes is one read route per group, with a malformed id so a request that passes its
// permission gate stops in the handler (400) before touching a service.
var groupRoutes = []struct {
	group, path, perm string
}{
	{"projects", "/api/v1/acme/projects/not-a-uuid", rbac.PermProjectsView},
	{"tasks", "/api/v1/acme/projects/not-a-uuid/tasks", rbac.PermTasksView},
	{"milestones", "/api/v1/acme/projects/not-a-uuid/milestones", rbac.PermMilestonesView},
	{"members", "/api/v1/acme/projects/not-a-uuid/members", rbac.PermMembersView},
	{"comments", "/api/v1/acme/projects/not-a-uuid/comments", rbac.PermCommentsView},
	{"activity", "/api/v1/acme/projects/not-a-uuid/activities", rbac.PermProjectsView},
	{"tenders", "/api/v1/acme/tenders/not-a-uuid", rbac.PermTendersView},
	{"financials", "/api/v1/acme/financials/projects/not-a-uuid", rbac.PermFinancialsView},
	{"qa", "/api/v1/acme/qa/templates/not-a-uuid", rbac.PermQAView},
	{"rbac", "/api/v1/acme/rbac/permissions", rbac.PermRBACManage},
}

// Every group answers 401 without a token, 403 permission_denied for a user without its code
// (a client: portal only), and lets a holder through.
func TestRouteGroupsGate(t *testing.T) {
	h := testRouter(t, &fakeRBAC{roles: map[uuid.UUID]string{}}, fakeMembers{})
	client := userClaims("customer")
	clientRBAC := &fakeRBAC{roles: map[uuid.UUID]string{}, assigned: map[uuid.UUID]string{uuid.MustParse(testUser): rbac.RoleClient}}
	hc := testRouter(t, clientRBAC, fakeMembers{})
	admin := userClaims("admin")
	for _, g := range groupRoutes {
		if w := call(h, nil, http.MethodGet, g.path); w.Code != http.StatusUnauthorized {
			t.Errorf("%s without a token: %d, want 401", g.group, w.Code)
		}
		w := call(hc, client, http.MethodGet, g.path)
		if w.Code != http.StatusForbidden {
			t.Errorf("%s as a client: %d, want 403", g.group, w.Code)
			continue
		}
		var body map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &body)
		if body["code"] != "permission_denied" || body["upgrade"] != nil {
			t.Errorf("%s 403 body = %v, want code permission_denied without upgrade", g.group, body)
		}
		if w := call(h, admin, http.MethodGet, g.path); w.Code == http.StatusUnauthorized || w.Code == http.StatusForbidden {
			t.Errorf("%s as a mapped projects_admin: %d, want past the gate", g.group, w.Code)
		}
	}
}

// A staff member's token carries no projects.* codes (auth-api never issues them); local RBAC
// still lets them in through the JIT default role (member), and keeps them out of admin routes.
func TestMemberWithoutServiceCodesInToken(t *testing.T) {
	r := &fakeRBAC{roles: map[uuid.UUID]string{}}
	h := testRouter(t, r, fakeMembers{})
	staff := userClaims("staff")
	if len(staff.Permissions) != 0 {
		t.Fatal("the token must carry no service codes")
	}
	for _, path := range []string{"/api/v1/acme/projects/not-a-uuid", "/api/v1/acme/projects/not-a-uuid/tasks", "/api/v1/acme/qa/templates/not-a-uuid"} {
		if w := call(h, staff, http.MethodGet, path); w.Code != http.StatusBadRequest {
			t.Errorf("%s as a staff member: %d, want 400 from the handler", path, w.Code)
		}
	}
	if w := call(h, staff, http.MethodGet, "/api/v1/acme/rbac/permissions"); w.Code != http.StatusForbidden {
		t.Errorf("role management as a member: %d, want 403", w.Code)
	}
	if w := call(h, staff, http.MethodPost, "/api/v1/acme/tenders"); w.Code != http.StatusForbidden {
		t.Errorf("creating a tender as a member: %d, want 403", w.Code)
	}
	if len(r.ensured) == 0 || r.ensured[0].TenantID.String() != testTenant {
		t.Errorf("JIT provisioning not run in the token's tenant: %+v", r.ensured)
	}
}

// Without projects.projects.manage a user reaches only projects they belong to; another project
// answers 404 like a missing one.
func TestMembershipScopingOnProjectRoutes(t *testing.T) {
	mine := uuid.MustParse(someID)
	h := testRouter(t, &fakeRBAC{roles: map[uuid.UUID]string{}}, fakeMembers{mine: true})
	staff := userClaims("staff")
	other := uuid.New().String()
	for _, path := range []string{"/api/v1/acme/projects/" + other, "/api/v1/acme/projects/" + other + "/tasks/not-a-uuid"} {
		if w := call(h, staff, http.MethodGet, path); w.Code != http.StatusNotFound {
			t.Errorf("%s for a non-member: %d, want 404", path, w.Code)
		}
	}
	if w := call(h, staff, http.MethodGet, "/api/v1/acme/projects/"+someID+"/tasks/not-a-uuid"); w.Code != http.StatusBadRequest {
		t.Errorf("a member's own project: %d, want 400 from the task id", w.Code)
	}
	admin := userClaims("admin")
	if w := call(h, admin, http.MethodGet, "/api/v1/acme/projects/"+other+"/tasks/not-a-uuid"); w.Code != http.StatusBadRequest {
		t.Errorf("projects.manage sees every project: %d, want 400 from the task id", w.Code)
	}
}

// GET auth/me returns the projects layer of the profile.
func TestAuthMe(t *testing.T) {
	h := testRouter(t, &fakeRBAC{roles: map[uuid.UUID]string{}}, fakeMembers{})
	w := call(h, userClaims("manager"), http.MethodGet, "/api/v1/acme/auth/me")
	if w.Code != http.StatusOK {
		t.Fatalf("auth/me: %d %s", w.Code, w.Body.String())
	}
	var me handlers.MeResponse
	if err := json.Unmarshal(w.Body.Bytes(), &me); err != nil {
		t.Fatal(err)
	}
	if me.UserID != testUser || me.TenantID != testTenant || me.Email != "u@acme.test" {
		t.Errorf("identity = %+v", me)
	}
	if me.ServiceRole != rbac.RoleMember || len(me.GlobalRoles) != 1 || me.GlobalRoles[0] != "manager" {
		t.Errorf("roles = service %q global %v, want member from manager", me.ServiceRole, me.GlobalRoles)
	}
	has := map[string]bool{}
	for _, p := range me.Permissions {
		has[p] = true
	}
	if !has[rbac.PermTasksView] || !has[rbac.PermQAView] || has[rbac.PermRBACManage] {
		t.Errorf("member permissions = %v", me.Permissions)
	}
	if w := call(h, nil, http.MethodGet, "/api/v1/acme/auth/me"); w.Code != http.StatusUnauthorized {
		t.Errorf("auth/me without a token: %d, want 401", w.Code)
	}
}

// A subscription refusal is told apart from a permission refusal by upgrade:true.
func TestSubscriptionRefusalCarriesUpgrade(t *testing.T) {
	h := testRouter(t, &fakeRBAC{roles: map[uuid.UUID]string{}}, fakeMembers{})
	c := userClaims("admin")
	c.SubscriptionStatus = "EXPIRED"
	w := call(h, c, http.MethodPost, "/api/v1/acme/tenders")
	var body map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if w.Code != http.StatusForbidden || body["upgrade"] != true || body["code"] != "subscription_inactive" {
		t.Errorf("expired subscription: %d %v", w.Code, body)
	}
}

// Routes outside the tenant group keep their own gates.
func TestRoutesOutsideTheTenantGroup(t *testing.T) {
	h := testRouter(t, &fakeRBAC{roles: map[uuid.UUID]string{}}, fakeMembers{})
	// Platform field keys: a static segment that must win over /{tenantID}; a non-owner gets 403.
	for _, path := range []string{"/api/v1/platform/field-keys"} {
		if w := call(h, userClaims("admin"), http.MethodGet, path); w.Code != http.StatusForbidden {
			t.Errorf("%s: want 403 from the platform owner gate, got %d", path, w.Code)
		}
	}
	for _, path := range []string{"/api/v1/qa/invitee/session", "/api/v1/qa/invitee/report.pdf"} {
		if w := call(h, nil, http.MethodGet, path); w.Code != http.StatusUnauthorized {
			t.Errorf("%s: want 401 from the invitee handler, got %d", path, w.Code)
		}
	}
	if w := call(h, nil, http.MethodGet, "/api/v1/s2s/acme/crm-links/"+someID); w.Code != http.StatusTeapot {
		t.Errorf("crm-links route not mounted: %d", w.Code)
	}
	if w := call(h, nil, http.MethodGet, "/"); w.Code != http.StatusOK {
		t.Errorf("GET / = %d, want service info", w.Code)
	}
	// Every tenant route exists (never 404 or 405) behind its gate.
	admin := userClaims("admin")
	for _, path := range []string{
		"/api/v1/acme/financials/portfolio", "/api/v1/acme/projects/metrics",
		"/api/v1/acme/projects/" + someID + "/summary", "/api/v1/acme/projects/" + someID + "/gantt",
		"/api/v1/acme/projects/" + someID + "/tasks/" + someID + "/comments", "/api/v1/acme/tasks/trend",
		"/api/v1/acme/rbac/roles", "/api/v1/acme/rbac/users",
	} {
		if w := call(h, admin, http.MethodGet, path); w.Code == http.StatusNotFound || w.Code == http.StatusMethodNotAllowed {
			t.Errorf("%s: route missing (%d)", path, w.Code)
		}
	}
}
