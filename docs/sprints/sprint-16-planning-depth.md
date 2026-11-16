# Sprint 16: Planning depth

**Status**: Planned (2026-11-16). Workload is gated on the new `workload_planning` code, baselines on the existing `gantt_chart`, and status rules and automations on the new `project_automation` (both new codes are tier 3 in subscriptions-api `psProjectsBlock`). Templates, recurring tasks, risks and change requests sit under `project_management`, on every plan.

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
