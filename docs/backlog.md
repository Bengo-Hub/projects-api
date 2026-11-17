# Projects API Backlog

**Last updated:** 2026-11-16. Built by checking `plan.md`, `docs/sprints/*.md`, `docs/erd.md` and `docs/integrations.md` against the code. Each item names the doc it came from. The budgets, planning, project costing and BI plan is complete here; its items are under Done. UI gaps live in `projects-service/projects-ui/docs/backlog.md`. Work order and gating: `plan.md` section 10.

## Hardening, sprint 0b (found in the 2026-11-16 audit)

- Only QA routes checked permission codes, and they read them from the JWT, which never carries service codes (shared-docs trinity pattern), so only admins passed. Every other route let any signed-in tenant user read and write. Fix: real local RBAC (registry, system roles, JIT default role, `auth/me`) checked on every route group, plus project membership scoping.
- The activity feed had read routes but no writer, so it was always empty. Fix: one activity writer called inside every mutating transaction.
- Outbox rows were written in their own transaction on a second database pool. Fix: write them in the domain transaction.
- `checkCircular` loaded the whole tenant dependency graph on every new dependency. Fix: recursive CTE.
- Tender numbers used COUNT+1 and could repeat. Fix: race-free counter.
- Tasks had no index on `assignee_id` or `parent_id`; Gantt data and task comments were unbounded.
- `task.go` and `tender.go` parsed paging by hand; some 500 responses returned raw error text.
- S2S requests could set the tenant through a header. Fix: path tenant only.
- `migrations.go` never used its embedded files, so a container without the folder fell back to auto-create.
- Shared libs behind: auth-client (old replace), httpware, cache, pagination, shared-events, shared-service-client.
- `GET /` redirected to Swagger, which is not mounted.

## Access management, sprint 17 (built 2026-11-17)

Done: team and role management inside projects (`docs/api.md`, "Team, roles and access";
`docs/sprints/sprint-17-access-management.md`). Still open:

- The client portal callback's PKCE `state` check (projects-ui; the portal sign-in stayed as is).
- Tender committees still take a raw user id; they should use `GET /people`.
- No screen yet for one person's effective permissions (`GET /rbac/users/{id}` exists).

## New sprints 12 to 16 (2026-11-16)

See `docs/sprints/sprint-12-work-management-core.md` to `sprint-16-planning-depth.md`.

### Planning depth, sprint 16 (API built 2026-11-17)

Done: workload against capacity, baselines with the slips report and the Gantt baseline, the risk
register and heat map, change requests with local approval, date shifts and the treasury budget
push, project templates (three system ones seeded), recurring tasks, status rules and automations
(`docs/api.md`, "Planning depth"). Still open:

- Capacity from ERP: erp-api has no per-employee working hours read, so capacity is a tenant
  setting (40 hours by default, per person overrides). Add an ERP S2S read once shifts are assigned
  to employees, and subtract leave.
- `actual_hours` in a status rule reads the task's `metadata.actual_hours`, not the hours logged in
  ERP timesheets. Reading ERP inside a status change would put a network call in the transaction; a
  cached per-task logged hours figure is the follow-up.
- Notices (2026-11-17): notifications-api now sends `project.task.automation_notice` (fanned out
  per recipient id), `project.change_request.review_requested` and `project.change_request.decided`
  by email, and the client thread and invited client review notices of sprint 14. Still open:
  WhatsApp templates for the five new notice types (Meta approval), a consumer for the risk
  events, and the change request page the review notice links to.
- Recurring copies do not carry the checklist; a new occurrence starts without one.
- Holidays are not working days aware: workload spreads over Monday to Friday only.
- The UI for all eight stories (projects-ui).

### Billing and profitability, sprint 15 (built 2026-11-17)

Done: milestone prices and invoices, time invoices for a period, retainer month invoices, the
billing overview, labour cost and margin to date, the per-client rollup and the portal's invoice
list (`docs/api.md`, "Billing and profitability"). Still open:

- Retainer overage and monthly usage: hours over the allowance are not billed automatically, and the
  rollover rule is stored but not applied. Needs an ERP read of a project's hours by month.
- A notice to the client when a project invoice is sent belongs to treasury's own send flow; check
  that treasury's invoice email names the project once the first tenant bills from projects.

## Project finance, done 2026-09-29

- Commercial terms in project metadata: `billing_type` (fixed, time_and_materials, non_billable), `contract_value`, `crm_contact_id`/`crm_contact_name` (a marketflow CRM contact) and `cost_center_id`/`cost_center_name` (a treasury cost centre), validated on create and update. Metadata updates merge (null removes a key). The financials response has a `commercial` block: fixed price margin at completion (contract value less EAC) and unbilled; time and materials margin to date (invoiced less cost). `GET /lookups/contacts?q=` (marketflow S2S search) and `GET /lookups/cost-centers` (treasury S2S list) feed the pickers. Commits `4c876f7`, projects-ui `4d46e68`, treasury `08cc2a8`. Source: sprint-3, sprint-5.

## Done (plan budgets-planning-projects-bi-2026-09-27, updated 2026-09-28)

Correctness and security:

- Tenant guard: `X-Tenant-ID` or the path tenant must match the JWT tenant claim (fills a missing header). Commit `881c693`.
- Tender metrics in one `GROUP BY status` with values, win rate and pipeline value. Commit `881c693`.
- Page sizes capped at 100 (`page_size` accepted as an alias of `limit`); sub-lists bounded at 500. Commit `881c693`.
- Project summary and financials task stats counted in SQL. Commits `881c693`, `9aa3f7f`.
- Dependency cycle check in one tenant-scoped query. Commit `881c693`.
- Indexes on every tenant-scoped read path (projects, tasks incl. the trend windows, milestones, members, comments, activities, attachments, dependencies, tender sub-resources, user roles, outbox pending poll); only tenders had any before. Migration `20260928163438_add_query_indexes`, commit `8a87dd5`.

Project finance (gated on `budget_tracking`, T3):

- Dead `Budget`, `Expense` and `TimeLog` tables dropped; treasury owns budgets. Commit `ef19f50`.
- Task `start_date`, `estimated_hours`, `progress_pct`. Commit `ef19f50`.
- Treasury client; `GET /financials/projects/{id}` with EVM (BAC, AC, PV, EV, CPI, SPI, EAC, ETC, VAC) and `GET /financials/portfolio` with RAG health, one batched treasury call per page. Commits `ef19f50`, `9aa3f7f`.
- Budget proxy under `/financials/projects/{id}/budgets` (list, create, update, submit) to treasury project budgets. Commit `ef19f50`.
- `project.created|updated|closed|deleted` outbox events; the `projects` stream is ensured at startup. Commits `070b5cc`, `f37ef5c`.
- `Project.budget` follows `treasury.budget.approved` (`planned_cost`). Commit `ff9338b`.
- Utilisation from ERP timesheet hours. Commit `b9fff12`.
- `GET /tasks/trend` (created, completed, overdue at month end) and `GET /projects/metrics` (counts by status). Commits `ff9338b`, `8a87dd5`.

## Tenders

- Tender sections and section review. Source: sprint-1 US-1.7 to US-1.9.
- Tender submissions (email and physical). Source: sprint-1 US-1.10, US-1.11.
- Go/no-go decision and outcome recording. Source: sprint-1 US-1.6, US-1.13.
- Tender events (`projects.tender.*`) and notifications. Source: sprint-1, plan.md Event Architecture.
- Tender to project conversion with document and team carry-over, charter and stakeholder register. Source: sprint-2.

## Planning and collaboration

- @mentions in comments. Source: sprint-4. Now sprint 12.
- Attachment routes (the `attachment` table is unused) and document storage. Source: sprint-4, sprint-1. Now sprint 12.
- Real-time updates, as SSE through the shared events fan-out hub rather than raw WebSocket (multi-pod rule). Source: sprint-4. Now sprint 12.
- Task timers and signing sheets. Source: sprint-5. Now sprint 13, posting to ERP timesheets.
- Resource pool, allocation and capacity planning. Source: sprint-6. Now sprint 16 (workload).
- Governance hierarchy, decision logs and change control. Source: sprint-7. Risks and change requests now sprint 16.

## Integrations and later sprints

- Google Meet, Microsoft Teams and Zoom meeting creation. Source: sprint-1, sprint-9.
- Superset views and embedding. Deferred. Source: sprint-8, superset-integration.md.
- pgvector semantic search and AI features. Source: sprint-1, sprint-10.

## Quality

- Tests: 23 `_test.go` files exist (handlers, router, financials, projects, QA, tasks, fieldcrypt, platform clients); the old "no tests" note was wrong. Still open: coverage on new code and DB-backed integration tests. Source: sprint-1 Testing, sprint-11.
- Swagger or OpenAPI docs (was wrongly ticked in sprint-1). Source: sprint-1.
- On 2026-11-16 GitHub reported 1 moderate Dependabot alert on the default branch (alert 24). It goes in the sprint 11 security pass with a dependency audit.
- Files over 400 lines to split: `services/tenders/service.go`, `handlers/qa.go`, `handlers/tender.go`. Source: sprint-11.
