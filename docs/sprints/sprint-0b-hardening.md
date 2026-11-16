# Sprint 0b: Hardening

**Status**: API built and verified 2026-11-16 (build, vet, every test, migration generated, migrator dry run twice against a copy of the production schema). It ships together with the matching projects-ui release. Plan: `.claude/plans/projects-service-competitive-revamp-2026-11-16.md`.

**Production facts checked before shipping (read-only, 2026-11-16):**
- **Tenders:** there are none yet, so the new per-tenant unique number index has no duplicates to trip on.
- **Old RBAC tables:** all four are empty, so dropping them loses nothing.
- **Outbox:** `outbox_events` already had `published_at` and `error_message`; the migration now records them in the versioned history too.

**Migrations:** `20261116100000_drop_unused_rbac_tables.sql` and `20261116105639_sprint_0b_local_rbac_sequences_indexes.sql`. Unrelated column-type drift that the generator found on the QA and field key tables was trimmed out. Production runs `cmd/migrate`, the only migration path now:
- the fixups: drop the old RBAC tables while `roles.code` exists, drop the global `tenders_number_key`, backfill the tender counters;
- `Schema.Create`;
- seeding the registry: 40 codes, 5 roles.

**Kept, with reasons:**
- **`shared-auth-client` replace:** stays, because the auth-client repo still declares that module name fleet-wide.
- **authotp:** stays on plain HTTP, because its codes are single use and the shared client retries and logs URLs.
- **QA events:** still written after the change on a best-effort basis, because QA writes are not transactional yet.

**Open:**
- Trend without a `project_id` refuses users who lack `projects.manage` instead of scoping to their projects.
- QA sessions have no project membership scoping yet.
- The ticketing client switches to ticketing's new create contract (`subject`, `X-Tenant-ID`, `source_ref` per QA defect) in the release after ticketing-api deploys.
- Done 2026-11-16: the legacy `projects.>` stream reconcile in `platform/events/nats.go` was removed after a read-only check showed the production `projects` stream binds only `project.>`.

**Sprint goal**: make what already exists correct, safe and ready for growth before any new feature lands.

I found these problems in the 2026-11-16 audit. Each one has a fix and a test.

## 1. Permissions (plan, feature and permission together)
References: `shared-docs/docs/architecture/trinity-authorization-pattern.md`, `sso-integration-guide.md` and `subscription-gating-guide.md`.

- Problem: only QA routes checked permission codes, and they looked for `projects.qa.*` in the JWT. Following `shared-docs/docs/architecture/trinity-authorization-pattern.md`, auth-api puts only `auth.*` codes and global roles in the token, so only admins ever passed. Every other route let any signed-in tenant user read and write. The local `role`, `permission`, `rolepermission` and `userrole` tables existed but were never used.
- Fix: the shared-docs Layer 3 pattern, as in maskani-api and library-api. The service keeps local permissions, roles, role permissions and user role assignments. A user is created just in time on their first request, with a default role mapped from their global JWT role (admin to projects_admin, staff to member, others to viewer). `auth/me` returns the service role and codes for the UI. The permission registry lives in code: `projects.{projects,tasks,milestones,members,comments,tenders,financials,time,portal,qa,rbac}.*`, with the fleet actions (`view`, `view_own`, `add`, `change`, `change_own`, `delete`, `delete_own`, `manage`). System roles are projects_admin, project_manager, member, viewer and client. Every route group checks its code. Users without `projects.projects.manage` see only the projects they are members of. Superuser and platform owner skip permission checks but never plan gating.
- Test: JIT role mapping, `auth/me`, router tests for 401, 403 and 200 on each group (including a member whose token has no service codes), and a member and non-member list.

## 2. Tenant on S2S calls
- Problem: a service-key request with no user claims could pick its tenant through `X-Tenant-ID`.
- Fix: S2S routes take the tenant from the path only.
- Test: an S2S call whose header names another tenant is ignored.

## 3. Queries that will not scale
- The cycle check loaded every dependency edge in the tenant. Now one recursive CTE that walks from the new target only.
- No index on tasks `assignee_id` or `parent_id`. Now `(tenant_id, assignee_id)` and `(tenant_id, parent_id)`.
- Gantt data and task comments had no limit. Gantt is capped with a `truncated` flag; a task returns its latest comments plus a count, and the rest come from the paged comments route.
- Tender numbers used COUNT+1, which repeats after a delete or under load. Now a race-free counter.
- `task.go` and `tender.go` parsed paging by hand. Now the shared pagination lib.
- Some 500 responses returned raw error text. Now a generic message, with the detail in the log.

## 4. Events and activity
- Outbox rows were written in their own transaction on a second database pool, so a domain write could commit without its event. Now the event is written in the domain transaction.
- Nothing wrote `Activity` rows, so the feed was empty. Now one writer records actor, entity, action and a small change summary inside every mutating transaction.

## 5. Migrations and libraries
- `migrations.go` never used its embedded files. Now it tries the local folder, then the embedded files, like treasury and pos.
- Shared libs moved to their current tags: auth-client v0.20.0 (the old `shared-auth-client` replace is gone), httpware v0.20.0, cache v0.5.10, pagination v0.5.0, shared-events v0.11.1 and shared-service-client v0.5.0.
- `GET /` pointed at a Swagger page that was never mounted.

## 6. No legacy code (owner rule, 2026-11-16)
- No shims, aliases or deprecated paths stay behind: the legacy `projects.>` stream reconcile goes once the prod stream subjects are checked, `page_size` stops being an alias of `limit`, and the unused RBAC code and seed stub are deleted.
