package router

import (
	"net/http"
	"testing"

	authclient "github.com/Bengo-Hub/shared-auth-client"
	"github.com/google/uuid"

	"github.com/bengobox/projects-service/internal/modules/rbac"
)

// timeRoutes are the time tracking routes with input that stops in the handler (400) before the
// service, so a request past the gates never touches ERP.
var timeRoutes = []struct{ method, path string }{
	{http.MethodGet, "/api/v1/acme/time/week?week_start=not-a-date"},
	{http.MethodPut, "/api/v1/acme/time/entries"},
	{http.MethodPost, "/api/v1/acme/time/week/submit"},
	{http.MethodPost, "/api/v1/acme/time/timer/stop"},
	{http.MethodGet, "/api/v1/acme/tasks/not-a-uuid/timer"},
	{http.MethodPost, "/api/v1/acme/tasks/not-a-uuid/timer"},
	{http.MethodDelete, "/api/v1/acme/tasks/not-a-uuid/timer"},
}

// timeClaims are userClaims on a plan with time tracking and ERP attendance.
func timeClaims(roles ...string) *authclient.Claims {
	c := userClaims(roles...)
	c.SubscriptionFeatures = append(append([]string{}, c.SubscriptionFeatures...), "time_tracking", "attendance")
	return c
}

// Every time route answers 401 without a token, 403 permission_denied to a client and to a viewer
// (no projects.time code), and lets an admin and a member through to the handler.
func TestTimeRoutesGate(t *testing.T) {
	h := testRouter(t, &fakeRBAC{roles: map[uuid.UUID]string{}}, fakeMembers{})
	client := testRouter(t, &fakeRBAC{roles: map[uuid.UUID]string{}, assigned: map[uuid.UUID]string{uuid.MustParse(testUser): rbac.RoleClient}}, fakeMembers{})
	viewer := testRouter(t, &fakeRBAC{roles: map[uuid.UUID]string{}, assigned: map[uuid.UUID]string{uuid.MustParse(testUser): rbac.RoleViewer}}, fakeMembers{})
	member := testRouter(t, &fakeRBAC{roles: map[uuid.UUID]string{}, assigned: map[uuid.UUID]string{uuid.MustParse(testUser): rbac.RoleMember}}, fakeMembers{})
	for _, rt := range timeRoutes {
		if w := call(h, nil, rt.method, rt.path); w.Code != http.StatusUnauthorized {
			t.Errorf("%s %s without a token: %d, want 401", rt.method, rt.path, w.Code)
		}
		for name, hh := range map[string]http.Handler{"client": client, "viewer": viewer} {
			w := call(hh, timeClaims("staff"), rt.method, rt.path)
			if b := bodyOf(t, w.Body.Bytes()); w.Code != http.StatusForbidden || b["code"] != "permission_denied" {
				t.Errorf("%s %s as a %s: %d %v, want 403 permission_denied", rt.method, rt.path, name, w.Code, b)
			}
		}
		if w := call(h, timeClaims("admin"), rt.method, rt.path); w.Code != http.StatusBadRequest {
			t.Errorf("%s %s as an admin: %d, want 400 from the handler", rt.method, rt.path, w.Code)
		}
		if w := call(member, timeClaims("staff"), rt.method, rt.path); w.Code != http.StatusBadRequest {
			t.Errorf("%s %s as a member: %d, want 400 from the handler", rt.method, rt.path, w.Code)
		}
	}
	// A member (view_own) may not read someone else's week; that needs projects.time.view.
	w := call(member, timeClaims("staff"), http.MethodGet, "/api/v1/acme/time/week?user_id="+uuid.NewString())
	if b := bodyOf(t, w.Body.Bytes()); w.Code != http.StatusForbidden || b["required_permission"] != rbac.PermTimeView {
		t.Errorf("a member reading another user's week: %d %v", w.Code, b)
	}
	// A bad zone is refused before ERP is called.
	if w := call(h, timeClaims("admin"), http.MethodGet, "/api/v1/acme/time/week?tz=Not/AZone"); w.Code != http.StatusBadRequest {
		t.Errorf("bad tz: %d, want 400", w.Code)
	}
}

// Without time_tracking, or without ERP's attendance feature (the hours go to ERP timesheets),
// every time route answers 403 feature_not_available naming the missing feature, reads included.
func TestTimeRoutesFeatureGates(t *testing.T) {
	h := testRouter(t, &fakeRBAC{roles: map[uuid.UUID]string{}}, fakeMembers{})
	for _, missing := range []string{"time_tracking", "attendance"} {
		for _, rt := range timeRoutes {
			c := timeClaims("admin")
			kept := c.SubscriptionFeatures[:0:0]
			for _, f := range c.SubscriptionFeatures {
				if f != missing {
					kept = append(kept, f)
				}
			}
			c.SubscriptionFeatures = kept
			w := call(h, c, rt.method, rt.path)
			b := bodyOf(t, w.Body.Bytes())
			if w.Code != http.StatusForbidden || b["code"] != "feature_not_available" || b["required_feature"] != missing || b["upgrade"] != true {
				t.Errorf("%s %s without %s: %d %v", rt.method, rt.path, missing, w.Code, b)
			}
		}
	}
}
