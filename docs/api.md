# projects-api: API reference

Routes live in `internal/http/router/router.go` and the handlers under `internal/http/handlers`.
Auth follows `shared-docs/docs/architecture/trinity-authorization-pattern.md` and
`sso-integration-guide.md`.

## Access (sprint 0b, 2026-11-16)

Every tenant route runs: JWKS or service key auth, the tenant guard, subscription enforcement on
mutations, just-in-time user provisioning, then the route's permission check.

- **Permissions are local to this service.** The auth-api token carries only `auth.*` codes and
  global roles. The registry is `internal/modules/rbac/permissions.go`: codes are
  `projects.{module}.{action}`, with modules projects, tasks, milestones, members, comments,
  tenders, financials, time, portal, qa and rbac. A module's `manage` implies its other actions,
  `view`/`change`/`delete` imply their `_own` forms, and `qa.run` implies `qa.view`.
- **Roles.** System roles are projects_admin, project_manager, member, viewer and client
  (`roles.go`). A user's first request creates them locally with a role mapped from the global
  role: superuser, admin or owner become projects_admin; manager or staff become member; anyone
  else becomes viewer. The automatic role follows later changes to the global role. A role an
  admin assigns replaces it and sticks. Superuser and platform owner skip permission checks, but
  never plan gating.
- **Membership.** Without `projects.projects.manage`, a user reaches only projects they own or
  belong to; any other project answers 404. With `tasks.view_own` only, a user sees only tasks
  assigned to them.
- **S2S.** Service-key calls take the tenant from the path. `X-Tenant-ID` is ignored.

### Profile and roles

| Method | Path | Permission | Notes |
|---|---|---|---|
| GET | `/api/v1/{tenant}/auth/me` | signed in | `{user_id, email, name, tenant_id, global_roles[], service_role, roles[], permissions[], is_platform_owner}`. The UI calls it after SSO `/auth/me` and gates on `permissions`. |
| GET | `/api/v1/{tenant}/rbac/roles` | `projects.rbac.manage` | System and tenant roles with their codes |
| GET | `/api/v1/{tenant}/rbac/permissions` | `projects.rbac.manage` | The registry |
| GET | `/api/v1/{tenant}/rbac/users` | `projects.rbac.manage` | Users and their roles |
| POST | `/api/v1/{tenant}/rbac/users/{authUserID}/roles` | `projects.rbac.manage` | `{role_code}`. Nobody changes their own roles. |
| DELETE | `/api/v1/{tenant}/rbac/users/{authUserID}/roles/{roleCode}` | `projects.rbac.manage` | Revoking the last assigned role returns the user to the automatic role |

### Errors and paging

- **403 bodies:**
  - Permission: `{"error", "code": "permission_denied", "required_permission"}`.
  - Subscription: `{"code": "subscription_inactive" | "feature_not_available", "upgrade": true}`.
  - Tenant mismatch: `{"code": "tenant_forbidden"}`.
- **5xx bodies:** always `internal server error`; the detail goes to the log only.
- **Lists:** tasks, tenders and comments take `limit` (default 20, max 500) with `page` or
  `offset`, and answer `{data, total, limit, page, hasMore}`. There is no `page_size`.
- **Gantt:** answers `{data, truncated, limit: 2000, critical_days}`, each task with `critical`.
- **GetTask:** returns the task with `edges.dependencies`, `edges.assignees`, its latest 50
  `comments` (oldest first) and `comments_total`.

### Data and events

- **Tender numbers:** come from a per-tenant counter (`document_sequences`, kind `tender`), moved by
  one `INSERT ... ON CONFLICT DO UPDATE ... RETURNING` inside the tender's transaction. They are
  unique per tenant, and a deleted tender never frees its number.
- **Outbox:** events are written in the same transaction as the domain change. Every create,
  update and delete on projects, tasks, milestones, members, comments and tenders also writes an
  `activities` row in that transaction (`services/activity`).
- **`project.milestone.reached`:** fires only when a milestone moves to completed.

## Work management (sprint 12, 2026-11-16)

Plan features gate reads too: `task_tracking` for every task route, the board, My Work, the
calendar and the stream; `gantt_chart` on top of it for the Gantt chart; `team_collaboration` for
comments, mentions and attachments. A missing feature answers 403 `feature_not_available` with
`required_feature` and `upgrade: true`. Every project route goes through membership scoping (404).

### Tasks, assignees and the board

| Method | Path | Permission | Notes |
|---|---|---|---|
| GET | `/projects/{id}/tasks` | `tasks.view` or `view_own` | `?status=` one board column (each pages on its own), `?assignee=me\|<user id>` (any assignee), `?due_from=&due_to=` (inclusive, calendar), `?priority`, `limit`, `page` |
| POST | `/projects/{id}/tasks` | `tasks.add` | `assignee_ids: []` (first is primary, members only); `status` must be a project status |
| PUT | `/projects/{id}/tasks/{taskID}` | `tasks.change` or `change_own` | `assignee_ids` replaces the assignees (`[]` clears); `""` dates clear |
| PATCH | `/projects/{id}/tasks/{taskID}/status` | `tasks.change` or `change_own` | `{status}`. Optional `Idempotency-Key` header (at most 128 characters): a key already applied to this task in 24 h changes nothing and answers the task with `Idempotent-Replayed: true` |
| PATCH | `/projects/{id}/tasks/{taskID}/dates` | `tasks.change` or `change_own` | `{start_date?, due_date?}`. Breaking a finish-to-start dependency (FS or the old `blocks`; a same-day hand-over is fine) answers 409 `{code: "schedule_conflict", blocking_task_ids}` |
| GET | `/projects/{id}/statuses` | `tasks.view` or `view_own` | `{data: [{key, label, category: todo\|in_progress\|done, order}]}`; default To do, In progress, In review, Done |
| PUT | `/projects/{id}/statuses` | `projects.projects.change` | `{statuses: [...]}`. Keys snake case, `done` required (done category), at most 20; a status tasks still hold cannot be removed. Kept in project metadata `statuses`; the project update drops that key |
| GET | `/projects/{id}/board-counts` | `tasks.view` or `view_own` | `{data: [{status, count}]}`, one GROUP BY: configured columns first, then stray statuses |
| GET | `/projects/{id}/gantt` | `tasks.view` + `gantt_chart` | `{data: [task + critical], truncated, limit: 2000, critical_days}`. Critical path over finish-to-start links, durations from start and due dates |
| GET | `/my-work` | `tasks.view` or `view_own` | Open tasks assigned to me across the projects I can see. `?bucket=overdue\|today\|week\|later`, `?tz=` (IANA, default UTC), `?due_from&due_to`, `limit`, `page`. Soonest due first; `edges.project {id, name}` |
| GET | `/my-work/counts` | `tasks.view` or `view_own` | `{data: {overdue, today, week, later}}` in one query |

- **Assignees** live only in `task_assignees`; a task returns them as `edges.assignees`
  (`{user_id, is_primary}`, primary first). There is no `assignee_id` any more.
- **Buckets:** overdue before today, today, week the next six days, later after that or undated.
  `done` tasks are left out.
- **Completion:** moving to `done` sets progress 100 and `completed_at`; leaving `done` clears
  `completed_at`.
- **Server-owned metadata:** `tasks.metadata.status_mutations` (idempotency keys) and
  `comments.metadata.mentions` are never taken from a client.

### Comments, mentions and attachments

| Method | Path | Permission | Notes |
|---|---|---|---|
| POST/PUT | comment routes as before | as before | Body is rich text, sanitised to the editor's tags. A Tiptap mention `<span data-type="mention" data-id="<auth user id>">` of a project member notifies them; the author and non-members never. An edit notifies only people it newly mentions |
| POST | `/projects/{id}/attachments/uploads` | `comments.add` | `{file_name, content_type, size, task_id?}` gives `{key, upload: {url, method: PUT, headers, expires_at}, max_bytes, allowed_types}`. PUT the file to `upload.url` with exactly `upload.headers` |
| POST | `/projects/{id}/attachments` | `comments.add` | `{key, file_name, task_id?}` after the PUT. The stored bytes are checked (size, sniffed type); a bad file is deleted, 400. Confirming twice returns the same row |
| GET | `/projects/{id}/attachments` | `comments.view` | Newest first, `?task_id`, paged; each with a signed `url` (15 minutes) |
| GET | `/projects/{id}/tasks/{taskID}/attachments` | `comments.view` | The task's files |
| DELETE | `/projects/{id}/attachments/{attachmentID}` | `comments.manage` or `delete_own` (uploader) | Removes the row, then the stored file |

- Keys: `projects/{tenant}/attachments/{project}/{task id or "project"}/{uuid}.{ext}`, private bucket.
- Limits: `ATTACHMENTS_MAX_BYTES` (default 25 MB), `ATTACHMENTS_ALLOWED_TYPES` (PDF, text and
  CSV, JPEG, PNG, WebP, Word, Excel), `ATTACHMENTS_UPLOAD_TTL` (15m). Storage is the shared
  `OBJECT_STORAGE_*`; without it the upload routes answer 503 `uploads_unavailable`.

### Live updates

`GET /projects/{id}/stream` (`tasks.view` + `task_tracking`) is SSE. Read it with fetch and an
`Authorization` header, plus `Accept: text/event-stream` (which skips the 30 s request timeout and
the rate limiter). It sends `event: connected`, then `task.created`, `task.updated`,
`task.deleted`, `task.moved`, `comment.created` and `attachment.added` with data
`{type, project_id, task_id?, id?, status?, actor_id?}`, and a heartbeat comment every 15 s. Events
are hints sent after the commit through the shared Broadcaster (`_rt.projects.work.*`, every pod);
refetch through the routes above, and refetch everything after a reconnect.

### Events (outbox, `project.>`)

One event per recipient, never to the actor. Payload: `project_id, project_name, task_id,
task_title, actor_id, actor_name, recipient_user_id, recipient_email, recipient_name,
comment_excerpt, action_path` (`/{org}/projects/{id}/tasks?task={id}`). Recipient email and name
come from `projects_users` and may be empty for someone who never signed in to projects.

- **`project.task.assigned`:** people newly added to a task (create or update).
- **`project.task.mentioned`:** each member mentioned in a comment; plus `comment_id` and the
  comment as plain text (200 characters). `task_id` is empty for a project comment.
- **`project.task.status_changed`:** to the task's assignees; plus `from_status`, `to_status`,
  `to_status_label`.

## QA and acceptance

One place where a tenant or Codevertex staff run acceptance for a service, a project or other work.
A template holds the criteria and the pass rule; a session freezes them, invites the people who
test or sign, records their verdicts, opens tickets for severe failures and produces the signed
report.

### Permissions

The codes come from this service's local RBAC (see Access above), not from the auth-api token.
projects_admin, superuser and the platform owner hold all of them.

| Code | Grants |
|---|---|
| `projects.qa.view` | Read templates, sessions, invites, results, defects, the outcome and the report |
| `projects.qa.run` | Record results as a signed-in team member (includes view) |
| `projects.qa.manage` | Templates, criteria, sessions, invites, open, close, decide and raise defects (includes everything) |

Platform templates (no tenant) are created and changed only by the platform owner; every tenant
reads the active ones.

### Tenant routes (`/api/v1/{tenantID}/qa`, JWT)

| Method | Path | Permission | Notes |
|---|---|---|---|
| GET | `/catalog/features?service=` | view | Catalogue features for the subject picker; `available:false` until the catalogue read is switched on |
| GET | `/templates?service_tag=&status=&page=&limit=` | view | Own templates and active platform templates |
| POST | `/templates` | manage | `{name, kind, service_tag, status, pass_rule, metadata, platform}` |
| GET | `/templates/{templateID}` | view | With criteria in order |
| PATCH | `/templates/{templateID}` | manage | `{name, service_tag, status, pass_rule, metadata, clear_review}` |
| POST | `/templates/{templateID}/criteria` | manage | `{criteria: [...]}`, up to 500 per call (the import path) |
| PUT | `/templates/{templateID}/criteria/{criterionID}` | manage | Replaces one criterion |
| DELETE | `/templates/{templateID}/criteria/{criterionID}` | manage | Sessions keep their snapshot |
| GET | `/sessions?status=&page=&limit=` | view | |
| POST | `/sessions` | manage | `{template_id, title, subject_kind, subject_ref, pass_rule, tenant_name}` |
| GET | `/sessions/{sessionID}` | view | |
| GET | `/sessions/{sessionID}/outcome` | view | The pass rule over current results (frozen after close) |
| POST | `/sessions/{sessionID}/open` | manage | draft to open |
| POST | `/sessions/{sessionID}/close` | manage | open to closed; freezes the outcome and stores the report hash |
| POST | `/sessions/{sessionID}/decision` | manage | `{decision: accepted or rejected, reason}`; acceptance needs the pass rule met |
| GET | `/sessions/{sessionID}/invites` | view | Masked hints only, never addresses |
| POST | `/sessions/{sessionID}/invites` | manage | `{invites: [{email, name, role}]}`, role tester, signer or observer |
| DELETE | `/sessions/{sessionID}/invites/{inviteID}` | manage | Ends that invitee's access at once |
| GET | `/sessions/{sessionID}/results?history=true` | view | Current verdicts, or all of them |
| POST | `/sessions/{sessionID}/results` | run | `{criterion_id, result, severity, notes, device}` recorded as the caller |
| GET | `/sessions/{sessionID}/defects` | view | Most severe first |
| POST | `/sessions/{sessionID}/defects` | manage | `{criterion_id}`: raise a ticket by hand, or retry a failed one |
| GET | `/sessions/{sessionID}/report.pdf` | view | `X-Document-SHA256` carries the hash |

Field values:

- Criterion `subject_kind`: `feature` (catalogue feature code), `milestone` or `task` (an id in this
  tenant), `none`. `weight`: `must`, `should`, `could`.
- Session `subject_kind`: `service` (a catalogue service tag such as `hadia`), `project` (a project
  id), `general`.
- `result`: `pass`, `fail`, `blocked`, `not_run`. A failure needs `severity` 1 (critical) to 4
  (cosmetic).

### Pass rule

```json
{"require_all_must": true, "blocking_severities": [1, 2], "min_score": 0,
 "weights": {"must": 3, "should": 2, "could": 1}}
```

A session is accepted when every must criterion passed, no failure at a blocking severity is open,
and the weighted score (weight of passed criteria over the weight of all) reaches `min_score`.
Each criterion takes the worst current verdict across its testers. A session may only tighten its
template's rule: keep the must rule, keep every blocking severity, raise the score, never change
the weights.

### Invitee routes (`/api/v1/qa`, no tenant JWT)

| Method | Path | Notes |
|---|---|---|
| GET | `/access/{code}` | What the share link reveals: `{tenant_slug, methods}` |
| POST | `/access/{code}/email` | `{otp_token}` from auth-api `POST /api/v1/auth/public-otp/send` then `/verify` (send `tenant_slug` for branding) |
| POST | `/access/{code}/sso` | Bearer token whose email holds an invite |
| GET | `/invitee/session` | Frozen criteria, my current verdicts, my role; signers and observers also see the outcome |
| PUT | `/invitee/results/{criterionID}` | Testers only: `{result, severity, notes, device}` |
| GET | `/invitee/report.pdf` | Print and sign |
| POST | `/invitee/logout` | Clears the cookie |

The access answer is `{access_token, expires_at, session_id, role}` and also sets an HttpOnly
`qa_access` cookie (path `/api/v1/qa`, SameSite=None and Secure in production). Clients that cannot
keep cookies send the token as `X-QA-Access`. The token is bound to one session and invite and
lasts `QA_ACCESS_TTL` (8 hours); revoking the invite ends it at once. The link alone grants nothing,
and every refusal reads the same, so it never tells whether an address was invited.

### Events (outbox, projects stream `project.>`)

| Subject | When | Payload beyond the common fields |
|---|---|---|
| `project.qa.invite.created` | An address is invited or re-invited | `invite_id, email, name, role, invite_link, invited_by_name` |
| `project.qa.result.failed` | The first severity 1 or 2 failure of a criterion (and a by-hand raise of one) | `criterion_id, criterion_title, area_code, requirement_ref, feature_code, severity, result_id, defect_id, ticket_number, ticket_status, tester_hint, raised_by_hand` |
| `project.qa.session.closed` | Close | `accepted, score, document_hash, signers` |
| `project.qa.session.accepted` / `rejected` | Decision | `reason, document_hash, signers` |

`invited_by_name` is the inviting user's display name read from auth-api (its S2S member list,
`GET /api/v1/s2s/{tenant}/users`, cached for five minutes), or the organisation name when the user
has no name or the lookup fails. It is never taken from the request.

`signers` is `[{email, name}]`: the session's live signer invites, their addresses opened from
`qa_invites.email_enc` at publish time. It is empty when `FIELD_ENCRYPTION_KEY` is not set, and a
signer invited before the key was set is left out. The report and the data frozen at close hold
signer names only, never an address.

Common fields: `session_id, tenant_id, tenant_slug, tenant_name, session_title, status, scope,
subject_kind, subject_ref, owner_id, link`. `scope` is `platform` for a service session (route to
Codevertex ops through the auth-api platform alert channels) and `tenant` otherwise (route to the
session owner and the tenant's team by email and in-app, never to ops).

Consumed: `subscriptions.feature.changed` (`code` or `feature_code` in the payload) flags every
template with a criterion on that feature as `needs_review`.

### Configuration

| Variable | Default | Use |
|---|---|---|
| `QA_HASH_KEY` | derived from `INTERNAL_SERVICE_KEY` | Keys the email hashes and signs access tokens; set it from a secret |
| `FIELD_ENCRYPTION_KEY` | empty | Root key for field encryption (AES-256-GCM): it seals the data keys in `field_keys`, and the values themselves until the first rotation. Without it signers stay hash-only and get no close or decision notice. Never change it by hand once data keys exist: they would no longer open |
| `QA_ACCESS_TTL` | `8h` | Invitee access lifetime |
| `QA_COOKIE_SECURE` | `true` | Secure, SameSite=None cookie; off only on plain HTTP locally |
| `QA_CATALOG_ENABLED` | `false` | Read the feature catalogue from subscriptions-api |
| `QA_UI_BASE_URL` | `https://projects.codevertexafrica.com` | Invite and session links |
| `TICKETING_SERVICE_URL` | in-cluster ticketing-api | Defect tickets |
| `SUBSCRIPTIONS_SERVICE_URL` | in-cluster subscriptions-api | Feature catalogue |

## Platform: field keys

These routes back the Security keys page in projects-ui. They sit under `/api/v1/platform`, outside
any tenant, and only the platform owner (`is_platform_owner` in the token) gets through; a tenant
admin or superuser gets 403 `platform_owner_required`.

| Method and path | Body | Answer |
|---|---|---|
| `GET /api/v1/platform/field-keys` | none | 200, the key health card |
| `POST /api/v1/platform/field-keys/backup` | `{"otp_token"}` | 200, a JSON file download (`Content-Disposition: attachment`, `Cache-Control: no-store`) |
| `POST /api/v1/platform/field-keys/rotate` | `{"otp_token"}` | 202, the health card with the rotation started |

Backup and rotation need a fresh email code. I reuse auth-api's signed-in code flow: the page asks
auth-api to email the owner a code (`POST /api/v1/auth/otp/send`), trades the code for an
`otp_token` (`POST /api/v1/auth/otp/verify`, valid 10 minutes), and sends that token here. I check
it with auth-api's S2S `GET /api/v1/s2s/otp/validate` (the `authotp` client QA invitees already
use), which deletes it on first read, and the address it proves must be the owner's own. So each
code allows one backup or one rotation. Backup and rotation are POST so the token never sits in a
URL or an access log.

Errors carry a `code`: `otp_required` (400), `otp_invalid` (403), `otp_unavailable` (503),
`rotation_running` (409, the last rotation is still re-sealing), `not_configured` (409, no
`FIELD_ENCRYPTION_KEY`).

The health card:

```json
{
  "configured": true,
  "root_fingerprint": "1a2b3c4d",
  "current_key_id": "k9f8e7d6c5b",
  "current_fingerprint": "5e6f7a8b",
  "sealed_values": 12, "on_current": 9, "on_older": 3, "on_root": 1,
  "keys": [{"id": "k9f8e7d6c5b", "fingerprint": "5e6f7a8b", "status": "current", "created_at": "...", "values": 9}],
  "last_backup_at": "...", "last_backup_by": "owner@example.com",
  "last_rotated_at": "...",
  "rotation": {"state": "resealing", "key_id": "k9f8e7d6c5b", "started_at": "...", "resealed": 9, "failed": 0, "remaining": 3}
}
```

A fingerprint is the first 8 hex characters of the SHA-256 of the key. `rotation.state` is `idle`
(never rotated, values under the root), `resealing` or `done`.

The backup file holds the current and previous data keys in the clear (base64 of the 32 bytes),
each with its id, status and fingerprint, plus the root fingerprint, the sealed value format and
restore notes. It does not hold `FIELD_ENCRYPTION_KEY`. Every download writes a `field_key_audits`
row (who, when, key and root fingerprints) before the file leaves, and nothing in it is logged.
