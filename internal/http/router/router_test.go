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
// financials, attachment and tender workflow routes resolve.
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
		handlers.NewAttachmentHandler(log, nil, nil),
		handlers.NewTenderHandler(log, nil),
		handlers.NewFinancialsHandler(log, nil),
		nil, []string{"*"},
	)
	// With no tenant header the handler answers 400 (invalid tenant): the route exists.
	const p = "00000000-0000-0000-0000-000000000001"
	for _, rt := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/acme/financials/portfolio"},
		{http.MethodGet, "/api/v1/acme/financials/projects/" + p},
		// Attachments sit beside the task subrouter and the comment routes.
		{http.MethodGet, "/api/v1/acme/projects/" + p + "/attachments"},
		{http.MethodPost, "/api/v1/acme/projects/" + p + "/attachments"},
		{http.MethodDelete, "/api/v1/acme/projects/" + p + "/attachments/" + p},
		{http.MethodGet, "/api/v1/acme/projects/" + p + "/tasks/" + p + "/attachments"},
		{http.MethodPost, "/api/v1/acme/projects/" + p + "/tasks/" + p + "/attachments"},
		{http.MethodGet, "/api/v1/acme/projects/" + p + "/tasks/" + p + "/activities"},
		{http.MethodGet, "/api/v1/acme/projects/" + p + "/tasks/" + p + "/comments"},
		{http.MethodPost, "/api/v1/acme/projects/" + p + "/tasks/" + p + "/comments"},
		{http.MethodGet, "/api/v1/acme/projects/" + p + "/comments"},
		{http.MethodPut, "/api/v1/acme/projects/" + p + "/comments/" + p},
		{http.MethodGet, "/api/v1/acme/projects/" + p + "/activities"},
		{http.MethodGet, "/api/v1/acme/projects/" + p + "/tasks/" + p},
		{http.MethodGet, "/api/v1/acme/projects/" + p + "/milestones"},
		{http.MethodGet, "/api/v1/acme/projects/" + p + "/members"},
		{http.MethodGet, "/api/v1/acme/projects/" + p + "/summary"},
		// Tender workflow (sprint 1).
		{http.MethodGet, "/api/v1/acme/tenders/" + p + "/evaluation-summary"},
		{http.MethodPost, "/api/v1/acme/tenders/" + p + "/decision"},
		{http.MethodPost, "/api/v1/acme/tenders/" + p + "/status"},
		{http.MethodPost, "/api/v1/acme/tenders/" + p + "/outcome"},
		{http.MethodGet, "/api/v1/acme/tenders/" + p + "/sections"},
		{http.MethodPost, "/api/v1/acme/tenders/" + p + "/sections"},
		{http.MethodPut, "/api/v1/acme/tenders/" + p + "/sections/" + p},
		{http.MethodDelete, "/api/v1/acme/tenders/" + p + "/sections/" + p},
		{http.MethodPost, "/api/v1/acme/tenders/" + p + "/sections/" + p + "/submit"},
		{http.MethodPost, "/api/v1/acme/tenders/" + p + "/sections/" + p + "/approve"},
		{http.MethodPost, "/api/v1/acme/tenders/" + p + "/sections/" + p + "/request-changes"},
		{http.MethodPost, "/api/v1/acme/tenders/" + p + "/final-document"},
		{http.MethodPost, "/api/v1/acme/tenders/" + p + "/ready"},
		{http.MethodPost, "/api/v1/acme/tenders/" + p + "/submit"},
		{http.MethodGet, "/api/v1/acme/tenders/" + p + "/submissions"},
	} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(rt.method, rt.path, nil))
		if w.Code == http.StatusNotFound || w.Code == http.StatusMethodNotAllowed {
			t.Errorf("%s %s: route missing (%d)", rt.method, rt.path, w.Code)
		}
	}
}
