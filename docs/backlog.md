# Projects API Backlog

**Last updated:** 2026-09-28. Built by checking `plan.md`, `docs/sprints/*.md`, `docs/erd.md` and `docs/integrations.md` against the code. Each item names the doc it came from. The budgets, planning, project costing and BI plan is complete here; its items are under Done. UI gaps live in `projects-service/projects-ui/docs/backlog.md`.

## Project finance, open

- Project `billing_type`, `crm_contact_id`, `cost_center_id` and `contract_value` in metadata (margin against contract value, billing mode on the financials tab). Source: sprint-3, sprint-5.

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

- @mentions in comments. Source: sprint-4.
- Attachment routes (the `attachment` table is unused) and document storage. Source: sprint-4, sprint-1.
- WebSocket real-time updates. Source: sprint-4.
- Task timers and signing sheets. Source: sprint-5.
- Resource pool, allocation and capacity planning. Source: sprint-6.
- Governance hierarchy, decision logs and change control. Source: sprint-7.

## Integrations and later sprints

- Google Meet, Microsoft Teams and Zoom meeting creation. Source: sprint-1, sprint-9.
- Superset views and embedding. Deferred. Source: sprint-8, superset-integration.md.
- pgvector semantic search and AI features. Source: sprint-1, sprint-10.

## Quality

- The repo has no `_test.go` files: unit and integration tests, Testcontainers setup. Source: sprint-1 Testing, sprint-11.
- Swagger or OpenAPI docs (was wrongly ticked in sprint-1). Source: sprint-1.
