package router

import (
	"net/http"
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/bengobox/projects-service/internal/modules/rbac"
)

// tenderRoutes are the sprint 1 and 2 tender routes, each with a malformed tender id so a request
// that passes its gates stops in the handler (400) before touching a service, and the permission
// codes the route needs (all of them).
var tenderRoutes = []struct {
	method, path string
	needs        []string
}{
	{http.MethodPost, "/api/v1/acme/tenders/not-a-uuid/status", []string{rbac.PermTendersChange}},
	{http.MethodGet, "/api/v1/acme/tenders/not-a-uuid/evaluation-summary", []string{rbac.PermTendersView}},
	{http.MethodPost, "/api/v1/acme/tenders/not-a-uuid/decision", []string{rbac.PermTendersManage}},
	{http.MethodPost, "/api/v1/acme/tenders/not-a-uuid/outcome", []string{rbac.PermTendersManage}},
	{http.MethodPost, "/api/v1/acme/tenders/not-a-uuid/convert", []string{rbac.PermTendersManage, rbac.PermProjectsAdd}},
	{http.MethodPost, "/api/v1/acme/tenders/not-a-uuid/conversion/push-budget", []string{rbac.PermTendersManage, rbac.PermFinancialsManage}},
	{http.MethodGet, "/api/v1/acme/tenders/not-a-uuid/sections", []string{rbac.PermTendersView}},
	{http.MethodPost, "/api/v1/acme/tenders/not-a-uuid/sections", []string{rbac.PermTendersManage}},
	{http.MethodPut, "/api/v1/acme/tenders/not-a-uuid/sections/" + someID, []string{rbac.PermTendersView}},
	{http.MethodDelete, "/api/v1/acme/tenders/not-a-uuid/sections/" + someID, []string{rbac.PermTendersManage}},
	{http.MethodPost, "/api/v1/acme/tenders/not-a-uuid/sections/" + someID + "/submit", []string{rbac.PermTendersView}},
	{http.MethodPost, "/api/v1/acme/tenders/not-a-uuid/sections/" + someID + "/approve", []string{rbac.PermTendersView}},
	{http.MethodPost, "/api/v1/acme/tenders/not-a-uuid/sections/" + someID + "/request-changes", []string{rbac.PermTendersView}},
	{http.MethodGet, "/api/v1/acme/tenders/not-a-uuid/submissions", []string{rbac.PermTendersView}},
	{http.MethodPost, "/api/v1/acme/tenders/not-a-uuid/submissions", []string{rbac.PermTendersManage}},
	{http.MethodPost, "/api/v1/acme/tenders/not-a-uuid/documents/uploads", []string{rbac.PermTendersManage}},
}

// Each route answers 401 without a token, 403 permission_denied when the caller lacks any one of
// its codes, and reaches the handler with all of them (and the plan features).
func TestTenderRoutesGate(t *testing.T) {
	h := testRouter(t, &fakeRBAC{roles: map[uuid.UUID]string{}}, fakeMembers{})
	for _, rt := range tenderRoutes {
		if w := call(h, nil, rt.method, rt.path); w.Code != http.StatusUnauthorized {
			t.Errorf("%s %s without a token: %d", rt.method, rt.path, w.Code)
		}
		for i := range rt.needs {
			missing := slices.Delete(slices.Clone(rt.needs), i, i+1)
			d := testDeps(t, &fakeRBAC{}, fakeMembers{})
			d.Access = permsRBAC(missing)
			if w := call(New(d), userClaims("staff"), rt.method, rt.path); w.Code != http.StatusForbidden || codeOf(t, w.Body.Bytes()) != "permission_denied" {
				t.Errorf("%s %s without %s: %d %s", rt.method, rt.path, rt.needs[i], w.Code, w.Body.String())
			}
		}
		d := testDeps(t, &fakeRBAC{}, fakeMembers{})
		d.Access = permsRBAC(rt.needs)
		if w := call(New(d), userClaims("staff"), rt.method, rt.path); w.Code != http.StatusBadRequest {
			t.Errorf("%s %s with %v: %d %s, want 400 from the handler", rt.method, rt.path, rt.needs, w.Code, w.Body.String())
		}
	}
}

// The budget retry also needs the budget_tracking feature; without it the answer is the upgrade
// refusal. Tender mutations need project_management like the rest of the module.
func TestTenderFeatureGates(t *testing.T) {
	d := testDeps(t, &fakeRBAC{}, fakeMembers{})
	d.Access = permsRBAC([]string{rbac.PermTendersManage, rbac.PermFinancialsManage, rbac.PermTendersView})
	h := New(d)
	noBudget := userClaims("staff")
	noBudget.SubscriptionFeatures = slices.DeleteFunc(slices.Clone(noBudget.SubscriptionFeatures), func(f string) bool { return f == "budget_tracking" })
	w := call(h, noBudget, http.MethodPost, "/api/v1/acme/tenders/not-a-uuid/conversion/push-budget")
	if w.Code != http.StatusForbidden || bodyOf(t, w.Body.Bytes())["upgrade"] != true {
		t.Errorf("budget retry without budget_tracking: %d %s", w.Code, w.Body.String())
	}
	noPM := userClaims("staff")
	noPM.SubscriptionFeatures = []string{"task_tracking"}
	w = call(h, noPM, http.MethodPost, "/api/v1/acme/tenders/not-a-uuid/decision")
	if w.Code != http.StatusForbidden || codeOf(t, w.Body.Bytes()) != "feature_not_available" {
		t.Errorf("decision without project_management: %d %s", w.Code, w.Body.String())
	}
	// Reads stay open on the module alone.
	if w := call(h, noPM, http.MethodGet, "/api/v1/acme/tenders/not-a-uuid/sections"); w.Code != http.StatusBadRequest {
		t.Errorf("sections read without project_management: %d", w.Code)
	}
}
