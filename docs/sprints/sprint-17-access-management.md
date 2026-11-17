# Sprint 17: Access management (team, roles, sign in)

**Status**: Built and tested 2026-11-17 (API and UI). Planned the same day, from the owner's
request to adopt the treasury and pos auth flow,
user management and role based access. Gap report: treasury (`finance-service/treasury-api`, `-ui`)
is the model for role management (the trinity pattern names it as the reference), pos
(`pos-service/pos-api`, `-ui`) for UI permission refresh, the `<Can>` guard and the callback `state`
check. Projects keeps what it already does better: the Redis permission cache, implied permissions
(`manage` grants every action, view/change/delete grant `_own`), membership scoping and
`useServicePermissions` as the one UI resolver.

**Sprint goal**: a firm manages its own team and roles inside projects, safely, with an audit trail.

## Stories
- **US-17.1 Team page.** List people (search, paging) with their roles, status and role end dates;
  invite by email (through auth-api's tenant members route, then a local user and role); give or
  remove a role with an optional end date; deactivate and reactivate (never yourself or the last
  active admin).
- **US-17.2 Custom roles.** Create a role from a permission set, copy a role, rename, delete (tenant
  roles only, nobody holding it). Editing a default role makes a tenant copy (copy on write) and moves
  this tenant's holders to it in the same transaction. `projects_admin` and `client` stay read only.
- **US-17.3 Permission editor.** Grouped by module, with codes implied by a held `manage` shown as
  checked and locked ("via manage"); "Manage every project" (`projects.projects.manage`, which lifts
  membership scoping) labelled clearly.
- **US-17.4 Guard rails.** Role edits, deletes, revokes and status changes need admin tier (platform
  owner, superuser, admin, or `projects_admin`). A grant above the caller's own permissions is
  refused (treasury `checkGrant`). Nobody can remove their own `projects.rbac.manage`. A custom role
  never mixes `projects.portal.view` with staff codes.
- **US-17.5 Audit.** `rbac_audit_logs` for role created, cloned, customised, updated, deleted,
  permissions updated (before and after), assigned, revoked, user invited, deactivated, reactivated;
  a "Role changes" list.
- **US-17.6 Sign in and sign out.** The callback checks the PKCE `state`; the SSO error screen comes
  from shared-ui-lib (`SSOCallbackError`); sign out revokes the SSO session server side
  (`revokeServerSession`) and returns to the firm's own page. Service permissions refresh every 60
  seconds and on focus; a `<Can>` component replaces scattered checks.
- **US-17.7 Correctness fixes.** JIT resolves roles by code with the tenant copy first (so a
  customised default role is not re-added), provisions only when the token allows the projects
  service, and `/auth/me` answers 503 when permissions cannot be resolved. Tenant wide permission
  cache invalidation after role edits. The project team's "add member" picks people from a search
  instead of a raw user id, with role options that match the API.

## Migrations
- `rbac_audit_logs` (tenant_id, actor_user_id, actor_email, action, resource_type, resource_id,
  changes jsonb, ip_address, created_at; index tenant_id, created_at).
- `projects_users.deactivated_at`, `deactivated_by`.

## Built

- **API** (`internal/modules/rbac/manage.go`, `service.go`, `internal/http/handlers/rbac.go`,
  `internal/http/authz/authz.go`): every route in `docs/api.md`, "Team, roles and access". Copy on
  write runs in one transaction (copy, its permissions, this tenant's assignments moved), retried
  once when two first edits race. `roleByCode` resolves the tenant copy first for JIT, assign and
  revoke. Admin tier is a router gate (`authz.RequireAdminTier`, 403 `admin_required`); the grant
  ceiling is `rbac.CheckGrant` (403 `grant_refused`). Deactivation stamps `deactivated_at` and
  `deactivated_by` and clears the person's cache key; role edits clear the tenant's keys. JIT runs
  only when `AllowsUserService("projects")`; `/auth/me` answers 503 on a resolve error. The invite
  skips auth-api for a person already known in the tenant (its member route replaces global
  roles). Project team member roles are now checked: owner, manager, member or viewer.
- **Migration** `20261117130303_sprint_17_access_management.sql` (generated, LF hashed, the
  generator's unrelated `ALTER ... TYPE character varying` drift lines trimmed): `rbac_audit_logs` with
  `(tenant_id, created_at)`, and `projects_users.deactivated_at`, `deactivated_by`.
- **UI** (projects-ui `docs/plan.md`, Sprint 17): Team page, roles page and permission editor,
  callback state check with `SSOCallbackError`, sign out revoking the SSO session, 60 second
  permission refresh, `<Can>`, project team people picker.
- **Tests**: DB tests for copy on write, JIT and a customised default, the grant ceiling, self
  lockout and the last admin, deactivation with the Redis cache (miniredis), custom roles and end
  dates, audit rows, the invite against a fake auth-api and the audit trail of every change;
  router tests for the admin tier and people gates, `/auth/me` 503 and JIT with `user_services`.

## Decisions

- An assignment that ends leaves the person without that role; sign-in does not hand back the
  automatic role while an admin decided their roles.
- The invite does not reactivate a deactivated person; Reactivate does.
- Deleting a customised default role (once nobody holds it) returns the firm to the default.
- `projects_admin` can be given only by admin tier; the last active holder keeps it.

## Still open

- The client portal callback's `state` check (the portal sign-in stayed untouched).
- Committees still take a raw user id.
- A person detail screen with effective permissions.

## Not in scope
- Per user permission overrides (neither treasury nor pos has them).
- Idle logout (no app has it; ask first).
- In app organisation switching.
