package router

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/bengobox/projects-service/internal/modules/rbac"
)

// permsRBAC resolves every user to exactly the given permissions, like a tenant's custom role.
type permsRBAC []string

func (permsRBAC) EnsureUserFromToken(context.Context, rbac.Identity) error { return nil }

func (p permsRBAC) Resolve(context.Context, uuid.UUID, uuid.UUID) (rbac.Resolved, error) {
	return rbac.Resolved{Roles: []string{"custom"}, Perms: append([]string(nil), p...)}, nil
}

// The contact picker serves the client invite and the tender client, so projects.portal.manage,
// projects.tenders.add or projects.tenders.change alone opens it; the cost
// centre picker still needs the right to create or edit a project, and a user with neither is
// refused both.
func TestContactLookupOpensToPortalManage(t *testing.T) {
	route := func(perms ...string) http.Handler {
		d := testDeps(t, &fakeRBAC{}, fakeMembers{})
		d.Access = permsRBAC(perms)
		return New(d)
	}
	claims := userClaims("member")
	inviter := route(rbac.PermPortalManage)
	if w := call(inviter, claims, http.MethodGet, "/api/v1/acme/lookups/contacts?q=jo"); w.Code != http.StatusOK {
		t.Errorf("portal.manage on the contact lookup: %d, want 200", w.Code)
	}
	if w := call(inviter, claims, http.MethodGet, "/api/v1/acme/lookups/cost-centers"); w.Code != http.StatusForbidden {
		t.Errorf("portal.manage on the cost centre lookup: %d, want 403", w.Code)
	}
	for _, perm := range []string{rbac.PermProjectsAdd, rbac.PermProjectsChange, rbac.PermTendersAdd, rbac.PermTendersChange} {
		if w := call(route(perm), claims, http.MethodGet, "/api/v1/acme/lookups/contacts"); w.Code != http.StatusOK {
			t.Errorf("%s on the contact lookup: %d, want 200", perm, w.Code)
		}
	}
	viewer := route(rbac.PermProjectsView, rbac.PermPortalView)
	for _, path := range []string{"/api/v1/acme/lookups/contacts", "/api/v1/acme/lookups/cost-centers"} {
		if w := call(viewer, claims, http.MethodGet, path); w.Code != http.StatusForbidden {
			t.Errorf("viewer on %s: %d, want 403", path, w.Code)
		}
	}
}

// The portal reads X-Client-Invites from another origin, so the CORS answer to an allowed origin
// exposes it next to the standard headers; an origin that is not allowed gets no CORS headers.
func TestCORSExposesClientInvites(t *testing.T) {
	d := testDeps(t, &fakeRBAC{}, fakeMembers{})
	d.AllowedOrigins = []string{"https://projects.example.test"}
	h := New(d)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "https://projects.example.test")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "https://projects.example.test" {
		t.Fatalf("Access-Control-Allow-Origin = %q", got)
	}
	exposed := strings.Split(w.Header().Get("Access-Control-Expose-Headers"), ",")
	found := false
	for _, e := range exposed {
		if strings.EqualFold(strings.TrimSpace(e), "X-Client-Invites") {
			found = true
		}
	}
	if !found {
		t.Errorf("Access-Control-Expose-Headers = %q, want X-Client-Invites in it", w.Header().Get("Access-Control-Expose-Headers"))
	}

	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "https://elsewhere.example.test")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if got := w.Header().Get("Access-Control-Expose-Headers"); got != "" {
		t.Errorf("a refused origin got exposed headers %q", got)
	}
}
