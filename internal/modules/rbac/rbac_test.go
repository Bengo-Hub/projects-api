package rbac

import (
	"slices"
	"strings"
	"testing"
)

var actions = map[string]bool{
	"add": true, "view": true, "view_own": true, "change": true, "change_own": true,
	"delete": true, "delete_own": true, "manage": true, "manage_own": true, "run": true,
}

// Every code is projects.{module}.{action} with the fleet vocabulary (plus QA's run), once.
func TestRegistryCodes(t *testing.T) {
	seen := map[string]bool{}
	for _, p := range Catalogue {
		parts := strings.Split(p.Code, ".")
		if len(parts) != 3 || parts[0] != "projects" || parts[1] != p.Module || parts[2] != p.Action || !actions[p.Action] {
			t.Errorf("bad code %+v", p)
		}
		if p.Action == "run" && p.Module != "qa" {
			t.Errorf("run is QA's action only: %s", p.Code)
		}
		if seen[p.Code] {
			t.Errorf("duplicate %s", p.Code)
		}
		seen[p.Code] = true
	}
	for _, r := range Roles {
		for _, c := range r.Permissions {
			if !seen[c] {
				t.Errorf("role %s grants unknown %s", r.Code, c)
			}
		}
	}
}

func TestMapGlobalRoles(t *testing.T) {
	cases := map[string][]string{
		RoleAdmin:  {"superuser"},
		RoleMember: {"staff"},
		RoleViewer: {"cashier"},
	}
	for want, roles := range cases {
		if got := MapGlobalRoles(roles); got != want {
			t.Errorf("%v -> %s, want %s", roles, got, want)
		}
	}
	for _, c := range []struct {
		roles []string
		want  string
	}{
		{[]string{"admin"}, RoleAdmin},
		{[]string{"staff", "admin"}, RoleAdmin},
		{[]string{"manager"}, RoleMember},
		{[]string{"member"}, RoleViewer},
		{nil, RoleViewer},
		// The portal sign-in role maps to client only; a staff role beside it wins.
		{[]string{"client"}, RoleClient},
		{[]string{"client", "staff"}, RoleMember},
		{[]string{"staff", "client"}, RoleMember},
		{[]string{"client", "admin"}, RoleAdmin},
	} {
		if got := MapGlobalRoles(c.roles); got != c.want {
			t.Errorf("%v -> %s, want %s", c.roles, got, c.want)
		}
	}
}

// QA semantics: run implies view, manage implies every QA action; nothing implies upwards.
func TestExpandQASemantics(t *testing.T) {
	run := Expand([]string{PermQARun})
	if !slices.Contains(run, PermQAView) || slices.Contains(run, PermQAManage) {
		t.Errorf("run expands to %v", run)
	}
	manage := Expand([]string{PermQAManage})
	for _, c := range []string{PermQAView, PermQARun, PermQAManage} {
		if !slices.Contains(manage, c) {
			t.Errorf("manage lacks %s: %v", c, manage)
		}
	}
	view := Expand([]string{PermQAView})
	if len(view) != 1 {
		t.Errorf("view expands to %v", view)
	}
	tasks := Expand([]string{PermTasksChange})
	if !slices.Contains(tasks, PermTasksChangeOwn) || slices.Contains(tasks, PermTasksDelete) {
		t.Errorf("tasks.change expands to %v", tasks)
	}
}

// The client role reaches the portal only: no staff module code, directly or by implication.
func TestClientRoleIsPortalOnly(t *testing.T) {
	var client RoleDef
	for _, r := range Roles {
		if r.Code == RoleClient {
			client = r
		}
	}
	for _, code := range Expand(client.Permissions) {
		if code != PermPortalView {
			t.Errorf("client holds %s", code)
		}
	}
}

func TestRoleRankOrdersAdminFirst(t *testing.T) {
	if RoleRank(RoleAdmin) >= RoleRank(RoleMember) || RoleRank(RoleMember) >= RoleRank(RoleViewer) {
		t.Error("rank must be admin, manager, member, viewer")
	}
}
