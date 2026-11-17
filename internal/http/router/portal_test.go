package router

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"go.uber.org/zap"

	handlers "github.com/bengobox/projects-service/internal/http/handlers"
	"github.com/bengobox/projects-service/internal/modules/rbac"
)

// Client portal gates (sprint 14). Malformed ids stop a request that passed its gates in the
// handler (400) before any service is touched.
func TestPortalRouteGates(t *testing.T) {
	user := uuid.MustParse(testUser)
	withPortal := func(role string) http.Handler {
		r := &fakeRBAC{roles: map[uuid.UUID]string{}, assigned: map[uuid.UUID]string{user: role}}
		d := testDeps(t, r, fakeMembers{})
		d.Handlers.Portal = handlers.NewPortalHandler(zap.NewNop(), nil, nil, nil, nil, nil)
		return New(d)
	}
	client, member, manager := withPortal(rbac.RoleClient), withPortal(rbac.RoleMember), withPortal(rbac.RoleProjectManager)
	claims := userClaims("client")
	claims.SubscriptionFeatures = append(claims.SubscriptionFeatures, "client_portal")

	// A client reaches the portal routes (stopped by the malformed id) and nothing of the team's.
	for _, path := range []string{
		"/api/v1/acme/portal/projects/not-a-uuid",
		"/api/v1/acme/portal/projects/not-a-uuid/milestones",
		"/api/v1/acme/portal/projects/not-a-uuid/tasks",
		"/api/v1/acme/portal/projects/not-a-uuid/files",
		"/api/v1/acme/portal/projects/not-a-uuid/invoices",
		"/api/v1/acme/portal/projects/not-a-uuid/thread",
	} {
		if w := call(client, claims, http.MethodGet, path); w.Code != http.StatusBadRequest {
			t.Errorf("client GET %s: %d, want 400 from the handler", path, w.Code)
		}
	}
	for _, path := range []string{
		"/api/v1/acme/projects/not-a-uuid/clients",
		"/api/v1/acme/projects/not-a-uuid/client-thread",
		"/api/v1/acme/projects/not-a-uuid/tasks",
		"/api/v1/acme/projects/not-a-uuid/comments",
		"/api/v1/acme/financials/projects/not-a-uuid",
	} {
		if w := call(client, claims, http.MethodGet, path); w.Code != http.StatusForbidden {
			t.Errorf("client GET %s: %d, want 403", path, w.Code)
		}
	}
	// A team member without portal.manage cannot invite or share; a project manager can.
	if w := call(member, claims, http.MethodPost, "/api/v1/acme/projects/not-a-uuid/clients"); w.Code != http.StatusForbidden {
		t.Errorf("member invite: %d, want 403", w.Code)
	}
	if w := call(member, claims, http.MethodGet, "/api/v1/acme/portal/projects/not-a-uuid"); w.Code != http.StatusForbidden {
		t.Errorf("member on the portal: %d, want 403 (no portal.view)", w.Code)
	}
	for _, path := range []string{
		"/api/v1/acme/projects/not-a-uuid/clients",
		"/api/v1/acme/projects/not-a-uuid/tasks/not-a-uuid/sharing",
	} {
		method := http.MethodPost
		if path != "/api/v1/acme/projects/not-a-uuid/clients" {
			method = http.MethodPatch
		}
		if w := call(manager, claims, method, path); w.Code != http.StatusBadRequest {
			t.Errorf("manager %s %s: %d, want 400 from the handler", method, path, w.Code)
		}
	}
	// Without the client_portal feature every portal route answers feature_not_available.
	noFeature := userClaims("client")
	if w := call(client, noFeature, http.MethodGet, "/api/v1/acme/portal/projects/not-a-uuid"); w.Code != http.StatusForbidden {
		t.Errorf("portal without client_portal: %d, want 403", w.Code)
	}
}
