package router

import (
	"net/http"
	"slices"
	"testing"

	"github.com/google/uuid"
)

// sprint16Routes are the planning routes, each with a request that stops in the handler with a
// 400 before any service is touched (a malformed id or query), and its plan feature ("" for the
// module alone).
var sprint16Routes = []struct {
	method, path, feature string
}{
	{http.MethodGet, "/api/v1/acme/workload?weeks=x", "workload_planning"},
	{http.MethodGet, "/api/v1/acme/workload/tasks?user_id=x", "workload_planning"},
	{http.MethodGet, "/api/v1/acme/projects/not-a-uuid/baselines", "gantt_chart"},
	{http.MethodPost, "/api/v1/acme/projects/not-a-uuid/baselines", "gantt_chart"},
	{http.MethodGet, "/api/v1/acme/projects/not-a-uuid/baselines/" + someID, "gantt_chart"},
	{http.MethodDelete, "/api/v1/acme/projects/not-a-uuid/baselines/" + someID, "gantt_chart"},
	{http.MethodGet, "/api/v1/acme/projects/not-a-uuid/slips", "gantt_chart"},
	{http.MethodGet, "/api/v1/acme/projects/not-a-uuid/status-rules", "project_automation"},
	{http.MethodPut, "/api/v1/acme/projects/not-a-uuid/status-rules", "project_automation"},
	{http.MethodGet, "/api/v1/acme/projects/not-a-uuid/task-moves/" + someID, "project_automation"},
	{http.MethodGet, "/api/v1/acme/projects/not-a-uuid/automations", "project_automation"},
	{http.MethodPut, "/api/v1/acme/projects/not-a-uuid/automations", "project_automation"},
	{http.MethodGet, "/api/v1/acme/projects/not-a-uuid/risks", ""},
	{http.MethodPost, "/api/v1/acme/projects/not-a-uuid/risks", ""},
	{http.MethodGet, "/api/v1/acme/projects/not-a-uuid/risks/heatmap", ""},
	{http.MethodPut, "/api/v1/acme/projects/not-a-uuid/risks/" + someID, ""},
	{http.MethodDelete, "/api/v1/acme/projects/not-a-uuid/risks/" + someID, ""},
	{http.MethodGet, "/api/v1/acme/projects/not-a-uuid/change-requests", ""},
	{http.MethodPost, "/api/v1/acme/projects/not-a-uuid/change-requests", ""},
	{http.MethodPost, "/api/v1/acme/projects/not-a-uuid/change-requests/" + someID + "/approve", ""},
	{http.MethodPost, "/api/v1/acme/projects/not-a-uuid/change-requests/" + someID + "/reject", ""},
	{http.MethodPost, "/api/v1/acme/projects/not-a-uuid/change-requests/" + someID + "/withdraw", ""},
	{http.MethodPost, "/api/v1/acme/projects/not-a-uuid/change-requests/" + someID + "/push-budget", ""},
	{http.MethodGet, "/api/v1/acme/project-templates/not-a-uuid", ""},
	{http.MethodDelete, "/api/v1/acme/project-templates/not-a-uuid", ""},
	{http.MethodPost, "/api/v1/acme/project-templates/not-a-uuid/projects", ""},
	{http.MethodPost, "/api/v1/acme/projects/not-a-uuid/save-as-template", ""},
}

var planningFeatures = []string{"workload_planning", "project_automation"}

// Every planning route answers 401 without a token, 403 permission_denied to a portal client,
// and lets a projects admin on a plan with the features through to the handler.
func TestPlanningRoutesGate(t *testing.T) {
	h := testRouter(t, &fakeRBAC{roles: map[uuid.UUID]string{}}, fakeMembers{})
	hc := testRouter(t, &fakeRBAC{roles: map[uuid.UUID]string{}, assigned: map[uuid.UUID]string{uuid.MustParse(testUser): "client"}}, fakeMembers{})
	for _, rt := range sprint16Routes {
		if w := call(h, nil, rt.method, rt.path); w.Code != http.StatusUnauthorized {
			t.Errorf("%s %s without a token: %d", rt.method, rt.path, w.Code)
		}
		c := userClaims("customer")
		c.SubscriptionFeatures = append(c.SubscriptionFeatures, planningFeatures...)
		if w := call(hc, c, rt.method, rt.path); w.Code != http.StatusForbidden || bodyOf(t, w.Body.Bytes())["code"] != "permission_denied" {
			t.Errorf("%s %s as a client: %d %s", rt.method, rt.path, w.Code, w.Body.String())
		}
		admin := userClaims("admin")
		admin.SubscriptionFeatures = append(admin.SubscriptionFeatures, planningFeatures...)
		if w := call(h, admin, rt.method, rt.path); w.Code != http.StatusBadRequest {
			t.Errorf("%s %s as an admin: %d %s, want 400 from the handler", rt.method, rt.path, w.Code, w.Body.String())
		}
	}
}

// Without its feature a gated route answers 403 feature_not_available with upgrade.
func TestPlanningFeatureGates(t *testing.T) {
	h := testRouter(t, &fakeRBAC{roles: map[uuid.UUID]string{}}, fakeMembers{})
	for _, rt := range sprint16Routes {
		if rt.feature == "" {
			continue
		}
		c := userClaims("admin")
		c.SubscriptionFeatures = append(slices.DeleteFunc(slices.Clone(c.SubscriptionFeatures), func(f string) bool { return f == rt.feature }),
			slices.DeleteFunc(slices.Clone(planningFeatures), func(f string) bool { return f == rt.feature })...)
		w := call(h, c, rt.method, rt.path)
		b := bodyOf(t, w.Body.Bytes())
		if w.Code != http.StatusForbidden || b["code"] != "feature_not_available" || b["required_feature"] != rt.feature || b["upgrade"] != true {
			t.Errorf("%s %s without %s: %d %v", rt.method, rt.path, rt.feature, w.Code, b)
		}
	}
}

// A member sees only their projects' planning data; members may raise risks and change requests
// but not approve, manage automations or save templates.
func TestPlanningMembershipAndRoles(t *testing.T) {
	mine := uuid.MustParse(someID)
	h := testRouter(t, &fakeRBAC{roles: map[uuid.UUID]string{}}, fakeMembers{mine: true})
	staff := userClaims("staff")
	staff.SubscriptionFeatures = append(staff.SubscriptionFeatures, planningFeatures...)
	other := uuid.New().String()
	for _, path := range []string{"/risks", "/change-requests", "/baselines", "/status-rules", "/automations", "/slips"} {
		if w := call(h, staff, http.MethodGet, "/api/v1/acme/projects/"+other+path); w.Code != http.StatusNotFound {
			t.Errorf("%s of another project: %d, want 404", path, w.Code)
		}
	}
	for _, rt := range []struct{ method, path string }{
		{http.MethodPost, "/api/v1/acme/projects/" + someID + "/change-requests/" + someID + "/approve"},
		{http.MethodPut, "/api/v1/acme/projects/" + someID + "/automations"},
		{http.MethodPost, "/api/v1/acme/projects/" + someID + "/save-as-template"},
		{http.MethodPost, "/api/v1/acme/projects/" + someID + "/baselines"},
		{http.MethodGet, "/api/v1/acme/workload"},
		{http.MethodDelete, "/api/v1/acme/projects/" + someID + "/risks/" + someID},
	} {
		if w := call(h, staff, rt.method, rt.path); w.Code != http.StatusForbidden {
			t.Errorf("a member %s %s: %d, want 403", rt.method, rt.path, w.Code)
		}
	}
	// Members may raise a risk and a change request (past the gate: the body is missing, 400).
	for _, path := range []string{"/risks", "/change-requests"} {
		if w := call(h, staff, http.MethodPost, "/api/v1/acme/projects/"+someID+path); w.Code != http.StatusBadRequest {
			t.Errorf("a member raising %s: %d, want 400 from the handler", path, w.Code)
		}
	}
}
