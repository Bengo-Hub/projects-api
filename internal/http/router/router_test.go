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
		nil, []string{"*"},
	)
	// With no tenant header the handler answers 400 (invalid tenant): the route exists.
	for _, path := range []string{
		"/api/v1/acme/financials/portfolio",
		"/api/v1/acme/financials/projects/00000000-0000-0000-0000-000000000001",
	} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code == http.StatusNotFound || w.Code == http.StatusMethodNotAllowed {
			t.Errorf("%s: route missing (%d)", path, w.Code)
		}
	}
}
