package router

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"go.uber.org/zap"

	handlers "github.com/bengobox/projects-service/internal/http/handlers"
)

// TestRoutesMount builds the full router (chi panics on conflicting patterns, for example a
// route registered under a path another handler mounted as a subrouter) and checks the
// financials routes resolve.
func TestRoutesMount(t *testing.T) {
	log := zap.NewNop()
	h := New(log,
		handlers.NewHealthHandler(log, nil, nil, nil),
		handlers.NewUserHandler(log, nil, nil),
		handlers.NewProjectHandler(log, nil),
		handlers.NewTaskHandler(log, nil),
		handlers.NewMilestoneHandler(log, nil),
		handlers.NewMemberHandler(log, nil),
		handlers.NewCommentHandler(log, nil),
		handlers.NewActivityHandler(log, nil),
		handlers.NewTenderHandler(log, nil),
		handlers.NewFinancialsHandler(log, nil),
		nil, []string{"*"}, nil, nil,
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusTeapot) }),
	)
	// The S2S CRM link check is mounted outside the authenticated group and reaches its handler.
	{
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/s2s/acme/crm-links/00000000-0000-0000-0000-000000000001", nil))
		if w.Code != http.StatusTeapot {
			t.Errorf("crm-links route not mounted: %d", w.Code)
		}
	}
	// With no tenant header the handler answers 400 (invalid tenant): the route exists.
	const id = "00000000-0000-0000-0000-000000000001"
	for _, path := range []string{
		"/api/v1/acme/financials/portfolio",
		"/api/v1/acme/financials/projects/" + id,
		// Project routes live under two mounts (/projects and /projects/{projectID}); chi prefers
		// the parameter, so these were swallowed by the comments subrouter and answered 404.
		"/api/v1/acme/projects",
		"/api/v1/acme/projects/metrics",
		"/api/v1/acme/projects/" + id,
		"/api/v1/acme/projects/" + id + "/summary",
		"/api/v1/acme/projects/" + id + "/comments",
		"/api/v1/acme/projects/" + id + "/tasks/" + id + "/comments",
		"/api/v1/acme/projects/" + id + "/tasks/" + id,
		"/api/v1/acme/projects/" + id + "/tasks",
		"/api/v1/acme/projects/" + id + "/members",
		"/api/v1/acme/projects/" + id + "/milestones",
		"/api/v1/acme/projects/" + id + "/activities",
		"/api/v1/acme/projects/" + id + "/gantt",
		"/api/v1/acme/tasks/trend",
	} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code == http.StatusNotFound || w.Code == http.StatusMethodNotAllowed {
			t.Errorf("%s: route missing (%d)", path, w.Code)
		}
	}
}
