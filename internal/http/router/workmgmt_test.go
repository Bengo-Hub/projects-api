package router

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/bengobox/projects-service/internal/modules/rbac"
)

// sprint12Routes are the work management routes with a malformed project id, so a request past
// its gates stops in projectScope (400) before touching a service.
var sprint12Routes = []struct {
	method, path, feature string
}{
	{http.MethodPatch, "/api/v1/acme/projects/not-a-uuid/tasks/" + someID + "/status", "task_tracking"},
	{http.MethodPatch, "/api/v1/acme/projects/not-a-uuid/tasks/" + someID + "/dates", "task_tracking"},
	{http.MethodPatch, "/api/v1/acme/projects/not-a-uuid/tasks/" + someID + "/checklist", "task_tracking"},
	{http.MethodGet, "/api/v1/acme/projects/not-a-uuid/statuses", "task_tracking"},
	{http.MethodPut, "/api/v1/acme/projects/not-a-uuid/statuses", "task_tracking"},
	{http.MethodGet, "/api/v1/acme/projects/not-a-uuid/board-counts", "task_tracking"},
	{http.MethodGet, "/api/v1/acme/projects/not-a-uuid/stream", "task_tracking"},
	{http.MethodGet, "/api/v1/acme/projects/not-a-uuid/gantt", "gantt_chart"},
	{http.MethodGet, "/api/v1/acme/projects/not-a-uuid/attachments", "team_collaboration"},
	{http.MethodGet, "/api/v1/acme/projects/not-a-uuid/tasks/" + someID + "/attachments", "team_collaboration"},
	{http.MethodPost, "/api/v1/acme/projects/not-a-uuid/attachments/uploads", "team_collaboration"},
	{http.MethodPost, "/api/v1/acme/projects/not-a-uuid/attachments", "team_collaboration"},
	{http.MethodDelete, "/api/v1/acme/projects/not-a-uuid/attachments/" + someID, "team_collaboration"},
	{http.MethodGet, "/api/v1/acme/projects/not-a-uuid/comments", "team_collaboration"},
}

func bodyOf(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	return m
}

// Every new route answers 401 without a token, 403 permission_denied to a client (portal role),
// and lets a projects admin through to the handler.
func TestWorkManagementRoutesGate(t *testing.T) {
	h := testRouter(t, &fakeRBAC{roles: map[uuid.UUID]string{}}, fakeMembers{})
	hc := testRouter(t, &fakeRBAC{roles: map[uuid.UUID]string{}, assigned: map[uuid.UUID]string{uuid.MustParse(testUser): rbac.RoleClient}}, fakeMembers{})
	for _, rt := range sprint12Routes {
		if w := call(h, nil, rt.method, rt.path); w.Code != http.StatusUnauthorized {
			t.Errorf("%s %s without a token: %d, want 401", rt.method, rt.path, w.Code)
		}
		w := call(hc, userClaims("customer"), rt.method, rt.path)
		if b := bodyOf(t, w.Body.Bytes()); w.Code != http.StatusForbidden || b["code"] != "permission_denied" {
			t.Errorf("%s %s as a client: %d %v, want 403 permission_denied", rt.method, rt.path, w.Code, b)
		}
		if w := call(h, userClaims("admin"), rt.method, rt.path); w.Code != http.StatusBadRequest {
			t.Errorf("%s %s as an admin: %d, want 400 from the project id", rt.method, rt.path, w.Code)
		}
	}
	// My Work has no project in the path: past the gates it reaches the handler.
	for _, path := range []string{"/api/v1/acme/my-work", "/api/v1/acme/my-work/counts"} {
		if w := call(hc, userClaims("customer"), http.MethodGet, path); w.Code != http.StatusForbidden {
			t.Errorf("%s as a client: %d, want 403", path, w.Code)
		}
		if w := call(h, userClaims("admin"), http.MethodGet, path+"?bucket=x&tz=Not/AZone"); w.Code != http.StatusBadRequest {
			t.Errorf("%s with a bad tz as an admin: %d, want 400 from the handler", path, w.Code)
		}
	}
}

// A plan without the route's feature answers 403 feature_not_available with upgrade, reads
// included; the Gantt chart needs gantt_chart on top of task_tracking.
func TestWorkManagementFeatureGates(t *testing.T) {
	h := testRouter(t, &fakeRBAC{roles: map[uuid.UUID]string{}}, fakeMembers{})
	for _, rt := range sprint12Routes {
		c := userClaims("admin")
		kept := c.SubscriptionFeatures[:0:0]
		for _, f := range c.SubscriptionFeatures {
			if f != rt.feature {
				kept = append(kept, f)
			}
		}
		c.SubscriptionFeatures = kept
		w := call(h, c, rt.method, rt.path)
		b := bodyOf(t, w.Body.Bytes())
		if w.Code != http.StatusForbidden || b["code"] != "feature_not_available" || b["required_feature"] != rt.feature || b["upgrade"] != true {
			t.Errorf("%s %s without %s: %d %v", rt.method, rt.path, rt.feature, w.Code, b)
		}
	}
	c := userClaims("admin")
	c.SubscriptionFeatures = []string{"project_management", "task_tracking"}
	if w := call(h, c, http.MethodGet, "/api/v1/acme/projects/not-a-uuid/tasks"); w.Code != http.StatusBadRequest {
		t.Errorf("task list without gantt_chart: %d, want 400 (only the chart needs it)", w.Code)
	}
	if w := call(h, c, http.MethodGet, "/api/v1/acme/my-work?tz=Not/AZone"); w.Code != http.StatusBadRequest {
		t.Errorf("my work on task_tracking: %d, want 400 from the handler", w.Code)
	}
}

// Without projects.projects.manage, a project the caller does not belong to answers 404 on every
// new project route, like a project that does not exist.
func TestWorkManagementMembershipScoping(t *testing.T) {
	mine := uuid.MustParse(someID)
	h := testRouter(t, &fakeRBAC{roles: map[uuid.UUID]string{}}, fakeMembers{mine: true})
	staff := userClaims("staff")
	other := uuid.New().String()
	for _, path := range []string{
		"/api/v1/acme/projects/" + other + "/statuses",
		"/api/v1/acme/projects/" + other + "/board-counts",
		"/api/v1/acme/projects/" + other + "/stream",
		"/api/v1/acme/projects/" + other + "/gantt",
		"/api/v1/acme/projects/" + other + "/attachments",
		"/api/v1/acme/projects/" + other + "/tasks/" + someID + "/attachments",
	} {
		if w := call(h, staff, http.MethodGet, path); w.Code != http.StatusNotFound {
			t.Errorf("%s for a non-member: %d, want 404", path, w.Code)
		}
	}
	for _, path := range []string{"/tasks/" + someID + "/status", "/tasks/" + someID + "/dates", "/tasks/" + someID + "/checklist"} {
		if w := call(h, staff, http.MethodPatch, "/api/v1/acme/projects/"+other+path); w.Code != http.StatusNotFound {
			t.Errorf("PATCH %s on another project: %d, want 404", path, w.Code)
		}
	}
	// Board statuses are managed with projects.projects.change, which a member lacks.
	if w := call(h, staff, http.MethodPut, "/api/v1/acme/projects/"+someID+"/statuses"); w.Code != http.StatusForbidden {
		t.Errorf("a member changing statuses: %d, want 403", w.Code)
	}
	// The member list (with names) is open to anyone who may view members, not only rbac.manage,
	// and stays scoped to the caller's projects.
	viewer := userClaims("customer")
	if w := call(h, viewer, http.MethodGet, "/api/v1/acme/projects/not-a-uuid/members?q=ana"); w.Code != http.StatusBadRequest {
		t.Errorf("a viewer searching members: %d, want 400 from the project id (past the gate)", w.Code)
	}
	if w := call(h, viewer, http.MethodGet, "/api/v1/acme/projects/"+other+"/members"); w.Code != http.StatusNotFound {
		t.Errorf("members of another project: %d, want 404", w.Code)
	}
	// ?parent_id must be a task id; the handler refuses anything else before reading.
	if w := call(h, staff, http.MethodGet, "/api/v1/acme/projects/"+someID+"/tasks?parent_id=nope"); w.Code != http.StatusBadRequest {
		t.Errorf("a malformed parent_id: %d, want 400", w.Code)
	}
}
