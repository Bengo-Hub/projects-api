package router

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"

	"github.com/bengobox/projects-service/internal/http/apidocs"
	handlers "github.com/bengobox/projects-service/internal/http/handlers"
)

// specOps reads "path -> methods" from the OpenAPI YAML (two-space paths, four-space methods).
func specOps(t *testing.T) map[string]map[string]bool {
	t.Helper()
	pathRe := regexp.MustCompile(`^  (/[^:]*):\s*$`)
	opRe := regexp.MustCompile(`^    (get|post|put|patch|delete):`)
	ops := map[string]map[string]bool{}
	cur := ""
	inPaths := false
	for _, line := range strings.Split(string(apidocs.Spec()), "\n") {
		switch {
		case line == "paths:":
			inPaths = true
		case inPaths && len(line) > 0 && line[0] != ' ' && line[0] != '#':
			inPaths = false
		case inPaths && pathRe.MatchString(line):
			cur = pathRe.FindStringSubmatch(line)[1]
			ops[cur] = map[string]bool{}
		case inPaths && cur != "" && opRe.MatchString(line):
			ops[cur][strings.ToUpper(opRe.FindStringSubmatch(line)[1])] = true
		}
	}
	return ops
}

// TestOpenAPICoversRoutes fails when a tenant route is added without documenting it.
func TestOpenAPICoversRoutes(t *testing.T) {
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
	ops := specOps(t)
	if len(ops) < 40 {
		t.Fatalf("parsed only %d spec paths; is the YAML layout unchanged?", len(ops))
	}
	const prefix = "/api/v1/{tenantID}"
	seen := 0
	err := chi.Walk(h.(chi.Routes), func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if !strings.HasPrefix(route, prefix) {
			return nil
		}
		path := strings.TrimSuffix(strings.TrimPrefix(route, prefix), "/")
		path = strings.ReplaceAll(path, "/*/", "/")
		seen++
		if !ops[path][method] {
			t.Errorf("%s %s is not in openapi.yaml", method, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if seen < 60 {
		t.Fatalf("walked only %d routes", seen)
	}
}

func TestDocsServed(t *testing.T) {
	log := zap.NewNop()
	h := New(log, handlers.NewHealthHandler(log, nil, nil, nil), handlers.NewUserHandler(log, nil, nil),
		handlers.NewProjectHandler(log, nil), handlers.NewTaskHandler(log, nil), handlers.NewMilestoneHandler(log, nil),
		handlers.NewMemberHandler(log, nil), handlers.NewCommentHandler(log, nil), handlers.NewActivityHandler(log, nil),
		handlers.NewAttachmentHandler(log, nil, nil), handlers.NewTenderHandler(log, nil), handlers.NewFinancialsHandler(log, nil),
		nil, []string{"*"})
	for path, want := range map[string]string{"/v1/docs/": "swagger-ui", "/v1/docs/openapi.yaml": "openapi: 3.0.3"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), want) {
			t.Errorf("%s: %d, body lacks %q", path, w.Code, want)
		}
	}
}
