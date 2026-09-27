# Projects API Backlog

**Last updated:** 2026-09-27. Built by checking `plan.md`, `docs/sprints/*.md`, `docs/erd.md` and `docs/integrations.md` against the code. Each item names the doc it came from. Items marked **In progress (plan budgets-planning-projects-bi-2026-09-27)** are being built now under `.claude/plans/budgets-planning-projects-bi-2026-09-27.md`; do not start them separately. UI gaps live in `projects-service/projects-ui/docs/backlog.md`.

## Correctness and security

All In progress (plan budgets-planning-projects-bi-2026-09-27, Phase 0):

- Tenant comes only from the `X-Tenant-ID` header and is not checked against the JWT tenant claim. Source: sprint-11.
- Tender metrics run seven COUNT queries and the response shape does not match projects-ui (Awarded and Evaluating show 0, Est. Value shows "NaNK"). Source: sprint-1.
- Task and tender page sizes are uncapped, and the UI sends `page_size` while `pagination.Parse` reads `limit`. Source: sprint-1, sprint-3.
- Project summary eager-loads tasks and milestones instead of grouping in SQL. Source: sprint-7.
- Dependency cycle check is an N+1 walk and not tenant scoped. Source: sprint-3.
- Comments, members, milestones and tender sub-resources are unpaginated. Source: sprint-4.

## Project finance

All In progress (plan budgets-planning-projects-bi-2026-09-27, Phases 3 and 5). The financials, budget proxy and portfolio endpoints gate on the existing projects feature code `budget_tracking` (T3), decided 2026-09-27.

- Drop the dead `Budget`, `Expense` and `TimeLog` schemas and tables (treasury owns budgets). Source: sprint-5, erd.md.
- Task `estimated_hours` and `progress_pct`; project `billing_type`, `crm_contact_id`, `cost_center_id`, `contract_value` in metadata. Source: sprint-3, sprint-5.
- Treasury client and `GET /projects/{id}/financials` with EVM (BAC, AC, PV, EV, CPI, SPI, EAC, ETC, VAC). Source: sprint-5, integrations.md.
- Budget proxy `GET/PUT /projects/{id}/budget` to treasury project budgets. Source: sprint-5.
- `GET /portfolio` with RAG health. Source: sprint-7.
- Publish `project.created`, `project.updated`, `project.closed`. Source: plan.md Event Architecture.
- Utilisation from ERP timesheet hours. Source: sprint-6.
- Task throughput, overdue trend and tender pipeline value and win rate reports. Source: sprint-7.

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
