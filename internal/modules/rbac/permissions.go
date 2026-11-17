// Package rbac is projects-api's Layer 3 authorization (shared-docs trinity-authorization-pattern):
// the permission registry, the system roles, JIT provisioning of local users with a default role
// mapped from their global auth-api roles, and permission resolution. auth-api's token carries
// only auth.* codes and global roles, never projects.* codes; those live here.
package rbac

import "strings"

// Permission codes, {service}.{module}.{action}, with the fleet's action vocabulary plus QA's run.
const (
	PermProjectsView   = "projects.projects.view"
	PermProjectsAdd    = "projects.projects.add"
	PermProjectsChange = "projects.projects.change"
	PermProjectsDelete = "projects.projects.delete"
	// PermProjectsManage also lifts membership scoping: without it a user sees and acts only on the
	// projects they own or are a member of.
	PermProjectsManage = "projects.projects.manage"

	PermTasksView      = "projects.tasks.view"
	PermTasksViewOwn   = "projects.tasks.view_own"
	PermTasksAdd       = "projects.tasks.add"
	PermTasksChange    = "projects.tasks.change"
	PermTasksChangeOwn = "projects.tasks.change_own"
	PermTasksDelete    = "projects.tasks.delete"
	PermTasksManage    = "projects.tasks.manage"

	PermMilestonesView   = "projects.milestones.view"
	PermMilestonesAdd    = "projects.milestones.add"
	PermMilestonesChange = "projects.milestones.change"
	PermMilestonesDelete = "projects.milestones.delete"

	PermMembersView   = "projects.members.view"
	PermMembersManage = "projects.members.manage"

	PermCommentsView      = "projects.comments.view"
	PermCommentsAdd       = "projects.comments.add"
	PermCommentsChangeOwn = "projects.comments.change_own"
	PermCommentsDeleteOwn = "projects.comments.delete_own"
	PermCommentsManage    = "projects.comments.manage"

	PermTendersView   = "projects.tenders.view"
	PermTendersAdd    = "projects.tenders.add"
	PermTendersChange = "projects.tenders.change"
	PermTendersDelete = "projects.tenders.delete"
	PermTendersManage = "projects.tenders.manage"

	PermFinancialsView   = "projects.financials.view"
	PermFinancialsManage = "projects.financials.manage"

	// Billing (sprint 15): view sees a project's billing terms, milestone amounts and invoices;
	// manage sets amounts and creates invoices in treasury.
	PermBillingView   = "projects.billing.view"
	PermBillingManage = "projects.billing.manage"

	PermTimeView    = "projects.time.view"
	PermTimeViewOwn = "projects.time.view_own"
	PermTimeAdd     = "projects.time.add"
	PermTimeManage  = "projects.time.manage"

	PermPortalView   = "projects.portal.view"
	PermPortalManage = "projects.portal.manage"

	PermQAView   = "projects.qa.view"   // templates, sessions, results, defects and reports
	PermQARun    = "projects.qa.run"    // record results as a signed-in team member (implies view)
	PermQAManage = "projects.qa.manage" // templates, sessions, invites, close and decide (implies all)

	PermRBACManage = "projects.rbac.manage"

	// Planning depth (sprint 16). Workload: view sees people's load; manage sets capacity.
	PermWorkloadView   = "projects.workload.view"
	PermWorkloadManage = "projects.workload.manage"
	// Baselines: view reads them and the slips report; add saves one; delete removes one.
	PermBaselinesView   = "projects.baselines.view"
	PermBaselinesAdd    = "projects.baselines.add"
	PermBaselinesDelete = "projects.baselines.delete"
	// Risk register.
	PermRisksView   = "projects.risks.view"
	PermRisksAdd    = "projects.risks.add"
	PermRisksChange = "projects.risks.change"
	PermRisksDelete = "projects.risks.delete"
	// Change requests: add raises one (and lets the requester edit or withdraw it while pending);
	// manage edits or withdraws anyone's. Deciding is a separate module so it can be granted on its
	// own: projects.change_approvals.manage approves or rejects (never one's own request).
	PermChangeRequestsView   = "projects.change_requests.view"
	PermChangeRequestsAdd    = "projects.change_requests.add"
	PermChangeRequestsManage = "projects.change_requests.manage"
	PermChangeApprovals      = "projects.change_approvals.manage"
	// Project templates: view lists them (and, with projects.add, starts a project from one); add
	// saves a project as a template; delete removes a tenant template (system ones never).
	PermTemplatesView   = "projects.templates.view"
	PermTemplatesAdd    = "projects.templates.add"
	PermTemplatesDelete = "projects.templates.delete"
	// Status rules and automations: view reads the automation rules; manage edits status rules and
	// automations. The board reads status rules with tasks.view.
	PermAutomationView   = "projects.automation.view"
	PermAutomationManage = "projects.automation.manage"
)

// PermissionDef is one registry entry.
type PermissionDef struct {
	Code   string `json:"code"`
	Name   string `json:"name"`
	Module string `json:"module"`
	Action string `json:"action"`
}

func def(code, name string) PermissionDef {
	parts := strings.SplitN(code, ".", 3)
	return PermissionDef{Code: code, Name: name, Module: parts[1], Action: parts[2]}
}

// Catalogue is the single source of truth for projects permission codes. Seeded on every start.
var Catalogue = []PermissionDef{
	def(PermProjectsView, "View projects"),
	def(PermProjectsAdd, "Create projects"),
	def(PermProjectsChange, "Edit projects"),
	def(PermProjectsDelete, "Delete projects"),
	def(PermProjectsManage, "Manage every project in the organisation"),
	def(PermTasksView, "View tasks"),
	def(PermTasksViewOwn, "View tasks assigned to me"),
	def(PermTasksAdd, "Create tasks"),
	def(PermTasksChange, "Edit tasks"),
	def(PermTasksChangeOwn, "Edit tasks assigned to me"),
	def(PermTasksDelete, "Delete tasks"),
	def(PermTasksManage, "Manage tasks"),
	def(PermMilestonesView, "View milestones"),
	def(PermMilestonesAdd, "Create milestones"),
	def(PermMilestonesChange, "Edit milestones"),
	def(PermMilestonesDelete, "Delete milestones"),
	def(PermMembersView, "View project members"),
	def(PermMembersManage, "Add and remove project members"),
	def(PermCommentsView, "View comments"),
	def(PermCommentsAdd, "Write comments"),
	def(PermCommentsChangeOwn, "Edit my comments"),
	def(PermCommentsDeleteOwn, "Delete my comments"),
	def(PermCommentsManage, "Moderate every comment"),
	def(PermTendersView, "View tenders"),
	def(PermTendersAdd, "Create tenders"),
	def(PermTendersChange, "Edit tenders"),
	def(PermTendersDelete, "Delete tenders"),
	def(PermTendersManage, "Manage tenders, committees and evaluations"),
	def(PermFinancialsView, "View project financials"),
	def(PermFinancialsManage, "Manage project budgets"),
	def(PermBillingView, "View project billing and invoices"),
	def(PermBillingManage, "Create project invoices"),
	def(PermTimeView, "View all time entries"),
	def(PermTimeViewOwn, "View my time entries"),
	def(PermTimeAdd, "Log time"),
	def(PermTimeManage, "Manage and approve time entries"),
	def(PermPortalView, "Use the client portal"),
	def(PermPortalManage, "Manage client portal access"),
	def(PermQAView, "View QA templates, sessions and reports"),
	def(PermQARun, "Record QA results"),
	def(PermQAManage, "Manage QA templates, sessions and decisions"),
	def(PermRBACManage, "Manage roles and role assignments"),
	def(PermWorkloadView, "View team workload"),
	def(PermWorkloadManage, "Set team capacity"),
	def(PermBaselinesView, "View baselines and slips"),
	def(PermBaselinesAdd, "Save baselines"),
	def(PermBaselinesDelete, "Delete baselines"),
	def(PermRisksView, "View the risk register"),
	def(PermRisksAdd, "Raise risks"),
	def(PermRisksChange, "Edit risks"),
	def(PermRisksDelete, "Delete risks"),
	def(PermChangeRequestsView, "View change requests"),
	def(PermChangeRequestsAdd, "Raise change requests"),
	def(PermChangeRequestsManage, "Manage every change request"),
	def(PermChangeApprovals, "Approve and reject change requests"),
	def(PermTemplatesView, "View project templates"),
	def(PermTemplatesAdd, "Save projects as templates"),
	def(PermTemplatesDelete, "Delete project templates"),
	def(PermAutomationView, "View automation rules"),
	def(PermAutomationManage, "Manage status rules and automations"),
}

// AllCodes returns every registry code.
func AllCodes() []string {
	out := make([]string, 0, len(Catalogue))
	for _, p := range Catalogue {
		out = append(out, p.Code)
	}
	return out
}

// impliers returns the codes that also grant code: the module's manage grants every action of the
// module, view, change and delete grant their _own form, and QA's run grants view.
func impliers(code string) []string {
	parts := strings.SplitN(code, ".", 3)
	if len(parts) != 3 {
		return nil
	}
	prefix, action := parts[0]+"."+parts[1]+".", parts[2]
	var out []string
	if action != "manage" {
		out = append(out, prefix+"manage")
	}
	switch action {
	case "view_own":
		out = append(out, prefix+"view")
	case "change_own":
		out = append(out, prefix+"change")
	case "delete_own":
		out = append(out, prefix+"delete")
	}
	if code == PermQAView {
		out = append(out, PermQARun)
	}
	return out
}

// Expand returns every registry code granted by held, directly or through an implying code.
func Expand(held []string) []string {
	set := make(map[string]bool, len(held))
	for _, h := range held {
		set[h] = true
	}
	out := make([]string, 0, len(held))
	for _, p := range Catalogue {
		if set[p.Code] {
			out = append(out, p.Code)
			continue
		}
		for _, i := range impliers(p.Code) {
			if set[i] {
				out = append(out, p.Code)
				break
			}
		}
	}
	return out
}
