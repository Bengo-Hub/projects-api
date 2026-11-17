package rbac

import "strings"

// System role codes.
const (
	RoleAdmin          = "projects_admin"
	RoleProjectManager = "project_manager"
	RoleMember         = "member"
	RoleViewer         = "viewer"
	// RoleClient is the client portal role (sprint 14): portal only, no staff permissions.
	RoleClient = "client"
)

// RoleDef is a seeded system role.
type RoleDef struct {
	Code        string
	Name        string
	Description string
	Permissions []string
}

// Roles is the seeded system role catalogue, in descending rank (ServiceRole picks the first one a
// user holds).
var Roles = []RoleDef{
	{RoleAdmin, "Projects administrator", "Every project, setting, role and approval", AllCodes()},
	{RoleProjectManager, "Project manager", "Runs the projects they own or belong to: tasks, milestones, team, tenders and QA", []string{
		PermProjectsView, PermProjectsAdd, PermProjectsChange,
		PermTasksManage, PermMilestonesView, PermMilestonesAdd, PermMilestonesChange, PermMilestonesDelete,
		PermMembersView, PermMembersManage, PermCommentsView, PermCommentsAdd, PermCommentsManage,
		PermTendersView, PermTendersAdd, PermTendersChange, PermFinancialsView,
		PermTimeView, PermTimeAdd, PermTimeManage, PermPortalManage, PermQAManage,
	}},
	{RoleMember, "Member", "Works on the projects they belong to: sees every task, edits their own", []string{
		PermProjectsView, PermTasksView, PermTasksAdd, PermTasksChangeOwn,
		PermMilestonesView, PermMembersView, PermCommentsView, PermCommentsAdd, PermCommentsChangeOwn, PermCommentsDeleteOwn,
		PermTendersView, PermTimeViewOwn, PermTimeAdd, PermQARun,
	}},
	{RoleViewer, "Viewer", "Read-only access to the projects they belong to", []string{
		PermProjectsView, PermTasksView, PermMilestonesView, PermMembersView, PermCommentsView, PermTendersView, PermQAView,
	}},
	{RoleClient, "Client", "Client portal only", []string{PermPortalView}},
}

// RoleRank orders role codes by Roles (lower is higher); unknown codes rank last.
func RoleRank(code string) int {
	for i, r := range Roles {
		if r.Code == code {
			return i
		}
	}
	return len(Roles)
}

// MapGlobalRoles maps the global auth-api roles on a token to the default projects role:
// superuser and admin-level roles to projects_admin, manager and staff to member, the client
// portal role (auth-api's projects-portal sign-in) to client, anyone else to viewer. A staff role
// wins over client, so a teammate who also signed in to a portal keeps their staff access.
// Explicit assignments made by a projects admin always win over this default.
func MapGlobalRoles(global []string) string {
	best := RoleViewer
	for _, g := range global {
		switch strings.ToLower(strings.TrimSpace(g)) {
		case "superuser", "super_admin", "admin", "owner", "tenant_admin", "administrator":
			return RoleAdmin
		case "manager", "staff":
			best = RoleMember
		case RoleClient:
			if best == RoleViewer {
				best = RoleClient
			}
		}
	}
	return best
}
