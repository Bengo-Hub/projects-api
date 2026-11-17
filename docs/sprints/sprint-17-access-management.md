# Sprint 17: Access management (team, roles, sign in)

**Status**: Planned 2026-11-17, from the owner's request to adopt the treasury and pos auth flow,
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

## Not in scope
- Per user permission overrides (neither treasury nor pos has them).
- Idle logout (no app has it; ask first).
- In app organisation switching.
