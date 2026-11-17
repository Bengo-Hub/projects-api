# Sprint 16: Planning depth

**Status**: API built 2026-11-17 (projects-api; the UI is next). Contract in `docs/api.md`, "Planning depth". Planned 2026-11-16. Workload is gated on the new `workload_planning` code, baselines on the existing `gantt_chart`, and status rules and automations on the new `project_automation` (both new codes are tier 3 in subscriptions-api `psProjectsBlock`). Templates, recurring tasks, risks and change requests sit under `project_management`, on every plan.

**Sprint goal**: the planning tools that keep firms from outgrowing us.

## Stories
- **US-16.1 Workload.** For each person and week: capacity (from ERP employees and working hours) against demand (task estimates spread over the task dates, for every assignee). Overloaded weeks are marked, and a task can be reassigned from the view. Worked out in SQL.
- **US-16.2 Baselines.** Save a snapshot of task dates and estimates. The Gantt chart shows the baseline under the live bars, and a report lists the slips. Real table: `project_baselines` with one JSON snapshot per save.
- **US-16.3 Risk register.** Risks with likelihood, impact, owner, mitigation and status, and a heat map. Real table `project_risks`, indexed by tenant and project.
- **US-16.4 Change requests.** A scope, cost or date change goes through the central approvals pattern (as treasury and inventory do), and once approved it can update the budget in treasury or shift the dates.
- **US-16.5 Templates.** Save a project as a template (phases, tasks, durations, roles), and start a project from one. Starter templates for an IT rollout, a website build and a consulting engagement.
- **US-16.6 Recurring tasks.** A repeat rule in task metadata. One job, run once across pods through the shared run-once helper, creates the next copy.
- **US-16.7 Status rules.** Per project: allowed moves between statuses and required fields for a move (for example, actual hours before Done). The server enforces them, and the board shows why a move is blocked.
- **US-16.8 Simple automations.** When the status changes to X, assign Y, notify Z or set a field. A small rule list per project, run in the same transaction as the change.

## What I built (projects-api)
- Workload (`GET /workload`, `/workload/tasks`, `/workload/settings`): demand spread over working
  days and split between assignees in one SQL statement, per person and ISO week, against capacity
  from the tenant's settings. Reassigning uses the normal task update.
- Baselines in `project_baselines` (one JSON snapshot per save), the slips report and the baseline
  on the Gantt response.
- Risk register in `project_risks` with a 5x5 heat map from one grouped query.
- Change requests in `project_change_requests`: raised by anyone with `change_requests.add`,
  approved or rejected with a comment by `change_approvals.manage` (never the requester). Approval
  shifts task dates in the same transaction and checks the dependency chain; a cost change is
  created or updated and submitted in treasury as a project budget after commit, with the result on
  the request and a retry route.
- Templates in `project_templates`: save a project as a template, start a project from one, and
  three system templates (IT rollout, website build, consulting engagement) seeded on every start.
- Recurring tasks: `repeat` in task metadata; a job every 10 minutes (one pod per period) creates
  the next copy once a task is done or past due, once per occurrence.
- Status rules and automations in project metadata, enforced in the one status change path the
  board, the drawer and the idempotent status route share. Automations run in the same transaction
  and never re-trigger rules.
- New permission codes (workload, baselines, risks, change requests, change approvals, templates,
  automation) with role grants; tenant settings table; migration
  `20261117065829_sprint_16_planning_depth.sql`.

## Decisions
- Capacity: erp-api has no per-employee working hours read (shifts are not assigned to employees),
  so capacity is a tenant setting (40 hours a week by default, per person overrides) rather than a
  new ERP route built on data ERP does not hold yet.
- Approving is its own permission module (`projects.change_approvals.manage`) because the fleet
  action vocabulary has no `approve`.
- A recurring task's next copy appears when the task is done or its due date passes, whichever is
  first, and skips missed rounds.
- Without `project_automation` stored rules and automations stay dormant instead of being deleted.

## Still open
- An ERP capacity read (working hours and leave per employee).
- `actual_hours` requirements read the task's `metadata.actual_hours`, not ERP logged hours.
- Notices done 2026-11-17: `project.change_request.review_requested` (one per approver candidate
  holding change approvals, never the requester) and `project.change_request.decided` (to the
  requester) beside the recipient-free events, and notifications-api now consumes them and
  `project.task.automation_notice` (email only). Left: the WhatsApp templates for these three
  (a Meta approval, so mine to submit), the risk events, and the
  `/{org}/projects/{id}/change-requests?request={id}` page the notice links to, which comes with
  the UI below.
- Recurring copies start without the checklist; workload ignores public holidays.
- The UI for all eight stories.
