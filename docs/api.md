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
- **Lookups.** `GET /lookups/contacts` (marketflow contact search over S2S, `?q=` and `?limit=` up
  to 50, `{data: [{id, name, detail}]}`) needs `projects.add`, `projects.change` or `portal.manage`,
  since it feeds both the project form and the client invite. `GET /lookups/cost-centers` needs
  `projects.add` or `projects.change`. 502 when the other service cannot be reached.
- **CORS.** Allowed origins can read `X-QA-Access`, `X-Client-Invites` and the shared standard
  exposed headers (httpware `StandardCORS`).

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
- **Lists:** tasks, tenders, comments and project members take `limit` (default 20, max 500) with `page` or
  `offset`, and answer `{data, total, limit, page, hasMore}`. There is no `page_size`.
- **Gantt:** answers `{data, truncated, limit: 2000, critical_days}`, each task with `critical`.
- **GetTask:** returns the task with `edges.dependencies`, `edges.assignees`, its latest 50
  `comments` (oldest first, each with `author_name`) and `comments_total`.

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
| GET | `/projects/{id}/tasks` | `tasks.view` or `view_own` | `?status=` one board column (each pages on its own), `?assignee=me\|<user id>` (any assignee), `?due_from=&due_to=` (inclusive, calendar), `?parent_id=<task id>` (a task's subtasks, served by the `(tenant_id, parent_id)` index; anything but a uuid is 400), `?priority`, `limit`, `page` |
| POST | `/projects/{id}/tasks` | `tasks.add` | `assignee_ids: []` (first is primary, members only); `status` must be a project status |
| PUT | `/projects/{id}/tasks/{taskID}` | `tasks.change` or `change_own` | `assignee_ids` replaces the assignees (`[]` clears); `""` dates clear |
| PATCH | `/projects/{id}/tasks/{taskID}/status` | `tasks.change` or `change_own` | `{status}`. Optional `Idempotency-Key` header (at most 128 characters): a key already applied to this task in 24 h changes nothing and answers the task with `Idempotent-Replayed: true` |
| PATCH | `/projects/{id}/tasks/{taskID}/checklist` | `tasks.change` or `change_own` | One operation: `{op: add\|toggle\|rename\|remove\|reorder, item_id, text?, done?, position?}`, applied under the task row lock (`SELECT ... FOR UPDATE`), so two people ticking at once both keep their tick. add takes `text` (at most 500 characters), an optional client `item_id` (letters, digits, `-`, `_`, at most 64; an id already there is a no-op) and `position` (0-based, default last); toggle takes `done` (absent flips); rename takes `text`; remove of a missing item is a no-op; reorder takes `position`. At most 200 items. An item another edit removed answers 404 `checklist_item_not_found`. `Idempotency-Key` works as on the status route. Answers the task |
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
- **Server-owned metadata:** `tasks.metadata.status_mutations` and `checklist_mutations`
  (idempotency keys), `tasks.metadata.checklist` (`[{id, text, done}]`, changed only through the
  checklist route), `tasks.metadata.timers` (running timers, see Time),
  `comments.metadata.mentions` and `comments.metadata.idempotency_key` are never taken from a
  client. A task PUT locks the task row too, so its metadata write never undoes a tick, a key or a
  timer written meanwhile.

### Members and names

| Method | Path | Permission | Notes |
|---|---|---|---|
| GET | `/projects/{id}/members` | `members.view` | `{data, total, limit, page, hasMore}`, longest standing first, each member with `name` and `email` from `projects_users` (the copy of the SSO identity made on first sign-in; both empty until then). `?q=` searches name or email (case-insensitive substring, wildcards literal), `limit` (default 20, max 500), `page` |

- Names come from one batched `projects_users` read per page (the `(tenant_id,
  auth_service_user_id)` unique index), never one query per row. The search is an `EXISTS` probe
  of that index per member of the project, whose rows come from the `(tenant_id, project_id,
  user_id)` index, so it never scans the tenant's users.
- The same batched read names comment authors (`author_name` on every comment: lists, the task
  detail's latest 50, create and edit answers) and activity actors (`actor_name` on the project
  and task feeds). Empty means the person has not signed in to projects (or a service call); the
  UI shows a fallback. Nobody needs `rbac.manage` to see a teammate's name any more.

### Comments, mentions and attachments

| Method | Path | Permission | Notes |
|---|---|---|---|
| POST/PUT | comment routes as before | as before | Body is rich text, sanitised to the editor's tags. A Tiptap mention `<span data-type="mention" data-id="<auth user id>">` of a project member notifies them; the author and non-members never. An edit notifies only people it newly mentions. Answers carry `author_name` |
| POST | `/projects/{id}/comments`, `/projects/{id}/tasks/{taskID}/comments` | `comments.add` | Optional `Idempotency-Key` (at most 128 characters), unique per tenant and author: a repeat within 24 h answers 200 with the comment the first request made and `Idempotent-Replayed: true` (no second row, notice, activity entry or live hint). The key is kept in `comments.metadata.idempotency_key`, found through the `(tenant_id, user_id, created_at)` index, and a transaction advisory lock on (tenant, author, key) makes racing replays apply once. The same key on another task or the project answers 409 `idempotency_key_reused` |
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

## Time (sprint 13, 2026-11-16)

ERP owns timesheets. These routes read and write the signed-in user's ERP timesheet over S2S
(`internal/platform/erp/timesheets.go`, erp-api `/hrm/attendance/timesheets/for-user`); projects-api
stores no hours. Every route needs the `time_tracking` feature and ERP's `attendance` feature (the
hours land on ERP timesheets, and erp-api lets the service key past its own plan gate, so the
user's token is checked here). Either missing answers 403 `feature_not_available` naming it. Every
route needs a signed-in user (401 for a service key). `?tz=` is an IANA zone, default
Africa/Nairobi; it decides where a day starts.

| Method | Path | Permission | Notes |
|---|---|---|---|
| GET | `/time/week` | `time.view_own` | `?week_start=` any date of the week (default this week), `?tz=`. `?user_id=` reads someone else's week and needs `time.view`. Answers the week below |
| PUT | `/time/entries` | `time.add` | `{project_id, task_id?, date, hours, note?, billable?}` sets the day's hours (0 to 24, two places) for that project and task, opening the Monday to Sunday timesheet on ERP when none covers the date. `user_id` in the body logs for someone else and needs `time.manage`. Answers ERP's `{employee_id, timesheet, entry}` |
| POST | `/time/week/submit` | `time.add` | `{week_start?}` sends ERP's submit for the user's timesheets over that week. `{data: [timesheet]}`. Approval and rejection stay in ERP |
| GET | `/time/timer` | `time.view_own` | The user's running timer anywhere: `{data: {project_id, task_id, task_title, started_at} \| null}` |
| POST | `/time/timer/stop` | `time.add` | `{project_id, task_id?, started_at, stopped_at, note?, billable?}` (RFC 3339 from the browser, so an offline stop logs the real run; at most 24 h; not in the future). Adds the time to the entries, split at midnight in `?tz=` (one ERP add per date, each in its own week), then clears the user's timer on the task. `{segments: [{date, hours}], entries: [...]}`. Send `Idempotency-Key` (projects-ui uses `stop:{task or project}:{started_at}`): see Timer stops below |
| GET | `/time/people` | `time.view` | The tenant's staff who have signed in to projects, never portal clients, for the week page's person picker. `?q=` matches name or email (any case); paged with `limit` and `page`. `{data: [{user_id, name, email}], total, ...}` |
| GET | `/tasks/{taskID}/timer` | `time.view_own` | The user's timer on the task, `{data: timer \| null}` |
| POST | `/tasks/{taskID}/timer` | `time.add` | Starts the user's timer on the task. One timer per user: one running on another task is stopped first (logged as above, `?tz=`), listed in `stopped`. Starting the running timer again keeps its start. `{timer, stopped: [...]}` |
| DELETE | `/tasks/{taskID}/timer` | `time.add` | Discards the user's timer on the task without logging. 204 |

`time.view` and `time.manage` imply `view_own` and `add`. Project and task routes go through
membership scoping (404).

- **Week:** `{week_start, week_end, days[7], status, editable, timesheets: [{id, period_start,
  period_end, status, rejection_reason?, editable}], rows: [{project_id, project_name, task_id?,
  task_title?, billable, hours: {date: h}, notes?: {date: text}, total}], day_totals: {date: h},
  total, billable_total}`. `status` is `none` (nothing logged yet), `draft`, `submitted`,
  `approved`, `rejected`, or `mixed` when ERP has several timesheets over the week. A row with
  `project_id: null` is time logged in ERP without a project. Hours are regular plus overtime.
- **Billable:** defaults from the project's `billing_type` (only `time_and_materials` bills hours);
  `billable: true` on any other project is 422 `validation_error`. The project's rate is
  `metadata.hourly_rate` (decimal string, above zero, two places at most, validated on project
  create and update) in the project's `currency`.
- **Timer state:** `tasks.metadata.timers` is `{auth user id: RFC 3339 start}`, written in one jsonb
  statement and server owned like `status_mutations` (a task update never takes it from a client).
  The browser keeps its own copy for an offline stop.
- **Errors:** 409 `no_employee_record` when the user has no ERP employee record (with a message to
  ask HR); ERP's 409 `timesheet_locked` (the week is submitted or approved) and
  `nothing_to_submit` pass through; 422 `validation_error`; 503 `erp_unavailable` without ERP;
  502 `erp_unavailable` when ERP does not answer. A timer stop across midnight that fails after
  its first date adds `logged_segments` to the error so a retry sends only the rest.
- **Timer stops:** a stop adds hours (it is not a set), so with an `Idempotency-Key` it logs once
  across every pod (`internal/services/timetracking/stopguard.go`, shared Redis). A key already
  done within 24 h answers 200 with `replayed: true`, header `Idempotent-Replayed: true`, and logs
  nothing. While the first request with the key is still logging, a duplicate gets 409
  `stop_in_progress` (retry shortly; it is never told "done" early). The done marker is written only
  after every date logged, and a failed or partial stop leaves none, so a retry with the same key
  goes through. With Redis unreachable a keyed stop is refused with 503 `stop_guard_unavailable`
  rather than risk the same hours twice. Starting a timer that stops another one uses a fixed key
  per run (`auto-stop:{task}:{start}`).
- **Figures:** EVM and utilisation read the same ERP hours (`project-hours`: approved and
  submitted regular plus overtime), so a logged and submitted hour shows there with no change.

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

## Client portal (sprint 14, 2026-11-17)

Every route here needs the `client_portal` plan feature (every Projects tier has it); without it
403 `feature_not_available`. Code: `internal/services/portal`, `internal/http/handlers/portal.go`.

### Who is a client

- A client signs in through auth-api's OAuth client `projects-portal` (public PKCE, callback
  `https://projects.codevertexafrica.com/{orgSlug}/portal/auth/callback`, `tenant={orgSlug}` on the
  authorize call). Its `self_signup_role` is `client`, so signing in joins the firm's tenant with
  the global role client (scope projects). JIT maps it to the projects role `client`, which holds
  `projects.portal.view` only; a staff global role beside it wins.
- A client is a `project_members` row with `role_code` client. Its metadata: `portal_status`
  (invited or active), `crm_contact_id`, `contact_name` (snapshot), `email_hash` (keyed with
  `QA_HASH_KEY`), `email_hint` (`j***@example.com`), `invited_by`, `invited_at`, `activated_at`. No
  address is stored; marketflow owns the contact. An invited row has `user_id` 00000000-...; the
  first `GET /portal/projects` of a signed-in client attaches every pending invite whose hash
  matches their SSO email (any case), in the request's tenant only and only when the token belongs
  to that tenant.
- An invite attaches only to a proven address: when one is waiting, projects-api asks auth-api
  (`GET /api/v1/s2s/users/{id}/email-verification`) and attaches it only if the email is verified,
  not a placeholder and the same as the token's. Otherwise nothing is attached, the call still lists
  the client's current projects, and the answer carries `X-Client-Invites: email_not_verified` so
  the portal asks them to verify their email first. A self-registered account typing a client's
  address therefore cannot claim the invite. auth-api is asked only when an invite is waiting.
- Client rows are never team membership: staff scoping, the team list and its routes, mentions and
  assignees all leave them out. `POST /members` and `PUT /members/{userID}` refuse `role_code`
  client.

### Team routes (membership scoping, 404 outside it)

| Method | Path | Permission | Notes |
|---|---|---|---|
| GET | `/projects/{id}/clients` | `portal.manage` | `{data: [{id, project_id, user_id, crm_contact_id, name, email_hint, status, invited_at, activated_at}]}` |
| POST | `/projects/{id}/clients` | `portal.manage` | `{crm_contact_id}`. Reads the contact from marketflow; 201 `{client, resent: false}`, or 200 `{client, resent: true}` when the contact is still at the invite stage (the invite goes again). 404 `contact_not_found` (unknown or erased), 422 `validation_error` (no or bad email in the CRM), 409 `already_client`, 503 `crm_unavailable` |
| DELETE | `/projects/{id}/clients/{memberID}` | `portal.manage` | Revokes an invite or an active client at once. 204 |
| PATCH | `/projects/{id}/tasks/{taskID}/sharing` | `portal.manage` | `{client_visible: bool}`. `{id, client_visible}`. `tasks.metadata.client_visible` is server owned: a task update never sets it |
| PATCH | `/projects/{id}/attachments/{attachmentID}/sharing` | `portal.manage` | `{client_visible: bool}`. Answers the attachment with its link |
| GET | `/projects/{id}/client-thread` | `comments.view` | The client thread, paged, oldest first, each entry with `author_kind` and `author_name` (see Client thread) |
| POST | `/projects/{id}/client-thread` | `comments.add` | `{content}` (rich text, sanitised), optional `Idempotency-Key` |

project_manager now holds `portal.manage` (it held `portal.view`), so a project manager invites
their own clients. Marking a milestone ready for review is the existing milestone update with
`status: "ready_for_review"`. The team's `GET /projects/{id}/milestones` and
`GET /projects/{id}/milestones/{milestoneID}` answer each milestone with `last_decision` (see
Milestone review), so the team reads the client's change request in place.

### Client routes (`projects.portal.view`)

Every route with a project answers 404 unless the caller is an active client of that project,
whatever else they hold. Nothing here returns budgets, costs, hours, estimates, EVM, margins,
assignees, metadata or internal comments: the answers are built field by field.

| Method | Path | Answer |
|---|---|---|
| GET | `/portal/projects` | Paged `[{id, name, status, start_date, end_date, progress: {tasks_total, tasks_done, percent, milestones_total, milestones_done, awaiting_review}}]`; attaches pending invites first. The progress counts the same way as the project page (all tasks, milestones completed or approved, `awaiting_review` the ones up for the client's decision), computed for the whole page in two grouped queries |
| GET | `/portal/projects/{id}` | `{id, name, status, start_date, end_date, progress, total_tasks, completed_tasks, total_milestones, completed_milestones, awaiting_review, next_milestone: {id, name, status, target_date} \| null}`. Progress is done tasks over all tasks |
| GET | `/portal/projects/{id}/milestones` | `{data: [{id, name, description, status, target_date, completed_at, can_decide, last_decision: {decision: approved \| changes_requested, comment, decided_at, decided_by_name} \| null}]}` |
| POST | `/portal/projects/{id}/milestones/{milestoneID}/decision` | `{decision: approve \| request_changes, comment}` (comment required for request_changes, at most 2000 characters). `{id, status, decision}`. 409 `milestone_not_ready` unless the milestone is `ready_for_review` |
| GET | `/portal/projects/{id}/tasks` | Paged shared tasks `[{id, title, description, status, status_label, start_date, due_date, progress, completed_at}]` |
| GET | `/portal/projects/{id}/files` | Paged shared files `[{id, file_name, file_size, mime_type, uploaded_at, task_id, url}]`, signed links for 15 minutes |
| GET | `/portal/projects/{id}/invoices` | Paged `{data: [Invoice], total, page, limit, hasMore, available: true}` from treasury: sent and paid invoices only (never drafts, voids or cancellations), newest first (see Billing for the Invoice shape). When treasury is not reachable: `{data: [], available: false, note}` |
| GET | `/portal/projects/{id}/thread` | The client thread, paged: `[{id, content, author_id, author_name, author_kind: client \| team, created_at, updated_at}]` |
| POST | `/portal/projects/{id}/thread` | `{content}`, optional `Idempotency-Key` (a key used on an internal comment answers 409 `idempotency_key_reused`) |

### Milestone review

Milestone statuses gain `ready_for_review`, `approved` and `changes_requested`. The team sets
`ready_for_review`; only a client's decision sets `approved` or `changes_requested` (the team's
update answers 400). The decision's who, when and comment are kept in the milestone's activity
entry (`changes.client_decision`, `comment`, `decided_by`, `decided_at`); the milestone metadata
column added in sprint 15 holds billing only. Two clients deciding at once apply one decision (row
lock).

`last_decision` on the team's and the portal's milestone lists is read back from those entries:
`{decision, comment, decided_by_name, decided_at}`, where `decision` is the outcome (`approved` or
`changes_requested`), the newest decision per milestone, `null` when no client decided. One query
serves the whole list (`DISTINCT ON` the milestone over the project's rows of the
`(tenant_id, project_id, occurred_at)` activities index) plus one batched name lookup.

### Client thread

Comments with `task_id` empty and server-owned `metadata.thread = "client"`. The project comment
list leaves them out; the portal shows nothing else. A comment's `thread` key never comes from a
client. Mentions on the thread notify nobody.

Each entry, on the list and on the POST answer of both routes, carries `author_name` and
`author_kind`: `client` for a post made on the portal route, `team` for one made on the team
route. The kind is stored on the comment when it is posted (server-owned `metadata.author_kind`),
so revoking a client later never turns what they said into the team's words, and reading the
thread needs no lookup.

### Events (outbox, `project.>`)

| Subject | To | Payload |
|---|---|---|
| `project.client.invited` | the contact | `project_id, project_name, member_id, tenant_slug, portal_path (/{org}/portal), action_path (/{org}/portal/projects/{id}), actor_id, actor_name, recipient_name, recipient_email, recipient_phone, resent` |
| `project.milestone.ready_for_review` | each signed-in client, one event each | the task notice fields (recipient from `projects_users`) plus `milestone_id, milestone_name, target_date`; `action_path` `/{org}/portal/projects/{id}?milestone={mid}` |
| `project.milestone.approved`, `project.milestone.changes_requested` | the project owner and owner or manager members, one each, never the client | plus `milestone_id, milestone_name, decision, comment_excerpt, decided_at`; `action_path` `/{org}/projects/{id}/milestones?milestone={mid}` |

## Billing and profitability (sprint 15, 2026-11-17)

Treasury owns invoices, payments and eTIMS; ERP owns the hours. projects-api creates customer
invoices in treasury over S2S and keeps only a milestone's invoice id, number and reference.
Treasury approves a service's invoice on create (system origin): it comes back `approved`, not yet
sent and not yet on eTIMS. The firm sends it from treasury, and that is when its revenue posts to
the project (`metadata.project_id` on the revenue journal lines). Code:
`internal/services/billing`, `internal/http/handlers/billing.go`,
`internal/platform/treasury/invoices.go`, `internal/platform/erp/billing.go`.

### Terms

Project metadata, validated on the project update: `billing_type` (`fixed`, `time_and_materials`,
`retainer`, `non_billable`), `contract_value` (number), `hourly_rate`, `retainer_amount` (decimal
strings, above zero, two places), `retainer_hours` (number, zero or more), `retainer_rollover`
(boolean), `tax_code` (`VAT-16`, `VAT-8`, `VAT-0` or `VAT-EXEMPT`, treasury's tax catalogue
codes, sent with its rate on every line; unset sends no VAT, because treasury fills no tax on a line
without an item), `crm_contact_id` (the client in marketflow, who the invoice is for; its name goes
as `customer_name` because treasury keeps the CRM id only when the CRM confirms it) and
`cost_center_id`. Nothing is invoiced for a non-billable project or one without a client (422).

Milestone metadata (new column, read only, never filtered): `amount` (decimal string) or
`amount_pct` (share of the contract value, 0 to 100), and once invoiced `invoice_id`,
`invoice_number` and `invoice_ref`. A milestone with an invoice cannot be deleted (422); void the
invoice in treasury, clear the price, then delete.

### Routes (`milestone_billing` feature, membership scoping)

| Method | Path | Permission | Notes |
|---|---|---|---|
| GET | `/projects/{id}/billing` | `billing.view` | `{terms, milestones: [{id, name, status, amount, amount_pct, bill_amount, invoice_id, invoice_number, invoice}], invoices, invoices_total, unbilled_hours, unbilled_estimate, treasury_unavailable, erp_unavailable}`. One milestone query, one treasury by-references batch, one treasury by-project page (drafts included) and one ERP hours read. Treasury or ERP down sets the flag and leaves that part empty |
| PUT | `/projects/{id}/billing/milestones/{mid}` | `billing.manage` | `{amount}` or `{amount_pct}`, both null clears. 422 while the milestone has a live invoice; once it is voided, cancelled or deleted the price can change and the link is dropped |
| POST | `/projects/{id}/billing/milestones/{mid}/invoice` | `billing.manage` | Draft invoice for the client, one line "{project}: {milestone}" at `bill_amount`. Returns the existing invoice on a repeat |
| POST | `/projects/{id}/billing/time-invoice` | `billing.manage` | `{from, to}` (dates, at most a year). `time_and_materials` with an `hourly_rate` only. Approved, billable, unbilled ERP hours dated in the period, one line per person (hours at the rate, sorted by name). `{invoice, entries, hours, warning}`. 422 `nothing_to_bill` when the period has none |
| POST | `/projects/{id}/billing/retainer-invoice` | `billing.manage` | `{month: YYYY-MM}`. `retainer` with a `retainer_amount` only. One line "Retainer, {Month YYYY} ({n} hours included)" |
| GET | `/financials/clients` | `financials.view` (`budget_tracking`) | Billable projects rolled up by client and currency: `{clients: [{client_id, client_name, currency, projects, contract_value, invoiced, unbilled, labour_cost, cost_to_date, margin_to_date, margin_pct}], truncated, money_unavailable}`, most profitable first. Reads the portfolio 100 projects a page (one project, task, treasury and ERP batch each), at most 2000 projects |

Write errors carry a `code`: `validation_error`, `nothing_to_bill` (422), `invoice_in_progress`
(409, another click is creating it), `billing_unavailable` (503, treasury, ERP or Redis not
configured), `treasury_refused` and `erp_refused` (422 with the upstream reason), `upstream_unavailable`
(502), `not_found` (404).

`Invoice` (from treasury): `{id, invoice_number, status, payment_status, currency, total_amount,
amount_paid, amount_due, balance_due, invoice_date, due_date, reference_type, reference_id,
is_overdue, public_url}`. `amount_due` and `balance_due` are the same figure (what is still owed, net
of credit notes); treasury's full invoice names it one way and its summary the other, so both are
filled. `public_url` is treasury's view and pay link (`/i/{public_token}`).

Permissions: `projects.billing.view` and `projects.billing.manage` (implies view). project_manager
holds manage; projects_admin holds everything.

### Idempotency and voids

Every invoice carries a treasury reference: `project_milestone` with the milestone id,
`project_time` with a UUIDv5 of the tenant, project and period, `project_retainer` with one of the
tenant, project and month. Treasury's create does not dedupe on the reference, so projects-api reads
it first (`GET /invoices/by-reference`) under a Redis lock per reference (two minutes,
`sharedcache.TryLock`): a retry or a second click returns the first invoice and two clicks cannot
both create one. Without Redis (local runs only) there is no lock. A voided or cancelled invoice would block its reference for good, so the next reference is
derived from the dead invoice's id (a chain, the same on every pod and retry, at most 20 voids per
item).

Time billing runs in three steps keyed by the reference: ERP reserves the period's approved,
billable, unbilled entries (`metadata.billing_ref`) and returns every entry carrying it; treasury
creates the invoice; ERP confirms (`metadata.billed_invoice_id`, overwritten so a deleted draft's
hours move to its replacement). A reserved entry is never reserved again, so no hour is billed twice.
If the create fails the entries stay reserved and a retry bills the same set; if the confirm fails
the answer carries a `warning` and running the period again finishes it. When the period's invoice
is voided, its hours are released (`include_confirmed`) before the next reference reserves them.

### Profitability

`commercial` on project financials and the portfolio gains `labour_cost` (ERP: approved hours at
each person's `salary_details.hourly_rate`), `cost_to_date` (treasury actual cost plus labour) and
`margin_to_date` (invoiced minus cost to date, for every billable type). The fixed price margin at
completion subtracts labour to date from the contract value on top of EAC; time and materials and
retainer margins are to date. Payroll is not booked against projects in treasury, so labour is never
counted twice. `hours` gains `unbilled_billable`.

## Planning depth (sprint 16, 2026-11-17)

Workload, baselines and slips, a risk register, change requests, project templates, recurring
tasks, status rules and automations. Code: `internal/services/planning` (workload, baselines,
risks, change requests, templates), `internal/services/tasks` (`rules.go`, `automations.go`,
`recurrence.go`, `planning.go`), `internal/http/handlers/planning.go`. Every route under
`/projects/{id}` goes through membership scoping (404 outside it). Every write records an activity
entry and an outbox event (`project.>` stream) in its own transaction.

### Gating and permissions

| Area | Plan feature | Permissions |
|---|---|---|
| Workload | `workload_planning` | `workload.view`, `workload.manage` (capacity settings) |
| Baselines, slips, Gantt baseline | `task_tracking` + `gantt_chart` | `baselines.view`, `baselines.add`, `baselines.delete` |
| Status rules, task moves, automations | `task_tracking` + `project_automation` | read rules and moves: `tasks.view` or `tasks.view_own`; `automation.view`; `automation.manage` (rules and automations) |
| Risks | module only | `risks.view`, `risks.add`, `risks.change`, `risks.delete` |
| Change requests | module only | `change_requests.view`, `change_requests.add`, `change_requests.manage`; deciding: `change_approvals.manage` |
| Templates | module only | `templates.view`, `templates.add`, `templates.delete`; starting a project also needs `projects.add` |

All codes are `projects.{module}.{action}`; a module's `manage` implies its other actions. The fleet
action vocabulary has no `approve`, so approving is its own module, `projects.change_approvals.manage`,
which can be granted without the right to edit other people's requests.

Roles: projects_admin holds everything. project_manager: `workload.view`, `baselines.*`, `risks.*`,
`change_requests.manage`, `change_approvals.manage`, `templates.view`, `templates.add`,
`automation.manage`. member: `baselines.view`, `risks.view`, `risks.add`, `change_requests.view`,
`change_requests.add`, `templates.view`, `automation.view`. viewer: `baselines.view`, `risks.view`,
`change_requests.view`. Only projects_admin holds `workload.manage` and `templates.delete`.

Errors carry a `code`: `validation_error` (422), `not_found` (404), `status_rule_blocked` (422),
`schedule_conflict` (409, `blocking_task_ids`), `change_request_not_pending`, `budget_push_in_progress`,
`budget_push_done`, `budget_push_not_needed` (409), `own_change_request`, `not_requester`,
`system_template` (403), plus the usual `permission_denied` and `feature_not_available`.

### Workload (`/workload`)

Demand is each open task's estimate spread evenly over its working days (Monday to Friday) from its
start date to its due date, split equally between its assignees, summed per person and ISO week in
one SQL statement. A task with only a due date is a one-day task; a task that spans only a weekend
spreads over those days; tasks without an estimate or a due date add nothing; done tasks count only
with `include_done=true`. Capacity: ERP has no per-employee working hours read (its shifts carry
`total_hours_per_week` but are not assigned to employees), so capacity comes from the tenant's
projects settings: `weekly_hours` (40 when unset) and per person overrides. A week over 100 percent
is `overloaded`; zero capacity with any demand is overloaded too. A caller without
`projects.manage` sees demand from the projects they belong to only. Reassigning from the view is the
existing task update (`PUT /projects/{id}/tasks/{taskId}` with `assignee_ids`).

| Method | Path | Permission | Notes |
|---|---|---|---|
| GET | `/workload` | `workload.view` | `?from=YYYY-MM-DD` (default this week; moved to its Monday), `?weeks=1..26` (default 8), `?project_id` (people limited to that project's owner and staff, demand still across every visible project), `?include_done=true`. `{from, to, weeks: ["YYYY-MM-DD"...], default_weekly_hours, capacity_source: "settings", people: [{user_id, name, email, weekly_capacity, total_demand, overloaded_weeks, weeks: [{week_start, demand_hours, capacity_hours, load_pct, overloaded, task_count}]}], unassigned: [{week_start, demand_hours, task_count}], truncated}` (at most 500 people) |
| GET | `/workload/tasks` | `workload.view` | `?user_id=` a user id or `unassigned`, `&week=YYYY-MM-DD`, optional `&include_done=true`: the tasks behind one cell, largest share first, at most 100. `{data: [{task_id, title, project_id, project_name, status, due_date, hours}]}` |
| GET | `/workload/settings` | `workload.view` | `{weekly_hours, person_hours: {user_id: hours}, capacity_source}` |
| PUT | `/workload/settings` | `workload.manage` | Same body; hours 0 to 168, at most 2000 overrides. Event `project.workload.settings_updated` |

### Baselines and slips

A baseline is a snapshot (one JSON document in `project_baselines`) of the project's dates, every task
(at most 2000, oldest first, `truncated` beyond) with title, status, start, due, estimate and parent,
and every milestone's target date. At most 50 per project.

| Method | Path | Permission | Notes |
|---|---|---|---|
| GET | `/projects/{id}/baselines` | `baselines.view` | `{data: [{id, project_id, name, note, task_count, created_by, created_by_name, created_at}]}` newest first |
| POST | `/projects/{id}/baselines` | `baselines.add` | `{name?, note?}` (name defaults to "Baseline {date time}"). 201 with the summary. Event `project.baseline.saved` |
| GET | `/projects/{id}/baselines/{baselineId}` | `baselines.view` | The row with `snapshot: {project_start, project_end, tasks: [{id, title, status, start_date, due_date, estimated_hours, parent_id}], milestones: [{id, name, target_date}], truncated}` |
| DELETE | `/projects/{id}/baselines/{baselineId}` | `baselines.delete` | 204. Event `project.baseline.deleted` |
| GET | `/projects/{id}/slips` | `baselines.view` | `?baseline_id` (default the latest; 404 when there is none). `{baseline, slips: [{kind (task or milestone), id, title, status, baseline_start, current_start, baseline_finish, current_finish, start_slip_days, finish_slip_days, estimate_change_hours}], tasks_compared, slipped, pulled_in, max_slip_days, removed_since, added_since, milestones_slipped}`. Only items whose finish moved (or gained or lost a due date), latest slip first |
| GET | `/projects/{id}/gantt` | unchanged | Gains `baseline` (`?baseline_id`, default the latest): `{id, name, created_at, tasks: {task_id: {id, title, status, start_date, due_date, estimated_hours, parent_id}}, milestones, truncated}`; `null` when there is none or the caller lacks `baselines.view`; 404 for an unknown `baseline_id` |

### Risk register

Likelihood and impact 1 to 5, `score` = likelihood x impact (stored). Status `open`, `monitoring`,
`mitigated`, `occurred`, `closed`. The owner must be on the project's staff team. At most 1000 per
project.

| Method | Path | Permission | Notes |
|---|---|---|---|
| GET | `/projects/{id}/risks` | `risks.view` | Paged (`?page`, `?limit`), `?status`, `?min_score`, `?owner_id`; highest score first, then soonest due. Rows: `{id, project_id, title, description, category, likelihood, impact, score, owner_id, owner_name, mitigation, status, due_date, created_by, metadata, created_at, updated_at}` |
| POST | `/projects/{id}/risks` | `risks.add` | `{title, likelihood, impact, description?, category?, owner_id?, mitigation?, status?, due_date? (YYYY-MM-DD)}`. 201. Event `project.risk.created` |
| GET | `/projects/{id}/risks/heatmap` | `risks.view` | One grouped query. `{grid: [[n x5] x5] indexed [likelihood-1][impact-1], cells: [{likelihood, impact, count}], total}`; closed and occurred risks are left out unless `?status=all` |
| GET | `/projects/{id}/risks/{riskId}` | `risks.view` | One risk |
| PUT | `/projects/{id}/risks/{riskId}` | `risks.change` | Patch, any field of the create; `owner_id: ""` or `due_date: ""` clears. Rescored on change. Event `project.risk.updated` (with `changes`) |
| DELETE | `/projects/{id}/risks/{riskId}` | `risks.delete` | 204. Event `project.risk.deleted` |

### Change requests

A scope, cost or schedule change raised by a requester and approved or rejected with a comment by
someone holding `change_approvals.manage`, never the requester (403 `own_change_request`). This
sign-off is local to projects; it is not a money decision. Status `pending`, then `approved`,
`rejected` or `withdrawn`.

On approval, in one transaction: the request's `date_shifts` move the listed tasks (each task
locked, activity recorded with `change_request_id`), then every moved task's finish-to-start links
are checked against the new plan as a whole, so moving a chain together is fine and a broken link
refuses the approval with 409 `schedule_conflict` and changes nothing. After commit, a budget change
goes to treasury acting as the approver: with `budget_id` the draft is checked to belong to the
project and updated with `budget` when given; otherwise a project budget is created from `budget`;
then it is submitted, which starts treasury's own budget approval. The budget id is saved before the
submit, so a retry never creates a second budget. Projects never stores a money total of record; the
proposed budget body waits on the request until treasury takes it.

`metadata`: `date_shifts` (`[{task_id, shift_days}]` or `[{task_id, start_date?, due_date?}]`, at
most 200, all in the project), `budget` (a treasury BudgetInput body: `name`, `fiscal_year`,
`period_type`, `start_date`, `end_date`, `currency`, `lines: [{category, name, planned_amount, ...}]`;
`budget_type` and `project_id` are always forced to this project), `budget_id` (an existing treasury
draft), `applied_task_ids` (after approval), `treasury: {status: not_needed|pending|pushing|submitted|failed, budget_id, error, attempts, attempted_at}`.
A `cost` change needs `budget` or `budget_id`; a `schedule` change needs `date_shifts`.

| Method | Path | Permission | Notes |
|---|---|---|---|
| GET | `/change-requests` | `change_requests.view` or `change_approvals.manage` | The tenant's requests across the projects the caller may see (`?status=pending` is the approvals queue), paged, newest first |
| GET | `/projects/{id}/change-requests` | same | Paged, `?status` |
| POST | `/projects/{id}/change-requests` | `change_requests.add` | `{title, description?, kind (scope, cost or schedule; default scope), date_shifts?, budget?, budget_id?}`. 201, status pending. Event `project.change_request.submitted` |
| GET | `/projects/{id}/change-requests/{crId}` | view | One request. Rows: `{id, project_id, title, description, kind, status, requested_by, requested_by_name, decided_by, decided_by_name, decided_at, decision_comment, metadata, created_at, updated_at}` |
| PUT | `/projects/{id}/change-requests/{crId}` | `change_requests.add` (own) or `.manage` | Pending only (409 `change_request_not_pending`). Absent fields stay; `budget: null` removes it, `budget_id: ""` unlinks. 403 `not_requester` for someone else's without manage. Event `project.change_request.updated` |
| POST | `/projects/{id}/change-requests/{crId}/withdraw` | same | Pending only. Event `project.change_request.withdrawn` |
| POST | `/projects/{id}/change-requests/{crId}/approve` | `change_approvals.manage` | `{comment?}`. Applies the shifts, then pushes the budget; the answer carries `metadata.treasury`. Event `project.change_request.approved` |
| POST | `/projects/{id}/change-requests/{crId}/reject` | `change_approvals.manage` | `{comment}` required. Event `project.change_request.rejected` |
| POST | `/projects/{id}/change-requests/{crId}/push-budget` | `change_approvals.manage` | Retries a `failed` (or stuck `pushing` for two minutes) push. 409 `budget_push_done`, `budget_push_in_progress` or `budget_push_not_needed` otherwise. Event `project.change_request.budget_pushed` on every finished attempt |

### Project templates

A template is a plan without dates: phases (become milestones at `offset_days` from the start),
tasks (`ref`, `title`, `offset_days`, `duration_days` at least 1, `estimated_hours`, `phase_ref`,
`parent_ref`, `depends_on` refs as finish-to-start links, `role`, `wbs_code`, `priority`), roles
(`{code, label, count}`) and the board statuses and status rules. At most 500 tasks, 50 phases, 30
roles, 200 templates per tenant. Three system templates (tenant_id NULL, read-only, seeded and
updated on every start and by projects-migrate): IT rollout, Website build, Consulting engagement.

| Method | Path | Permission | Notes |
|---|---|---|---|
| GET | `/project-templates` | `templates.view` | Paged, `?q` on the name; system first, then by name. `{data: [{id, name, description, category, system, task_count, phase_count, duration_days, created_at}]...}` |
| GET | `/project-templates/{templateId}` | `templates.view` | The summary plus `definition: {duration_days, phases, tasks, roles, statuses, status_rules}` |
| DELETE | `/project-templates/{templateId}` | `templates.delete` | 204; 403 `system_template` for a system one. Event `project.template.deleted` |
| POST | `/project-templates/{templateId}/projects` | `projects.add` and `templates.view` | `{start_date (required), name? (defaults to the template's), description?, currency?, metadata?}`. Creates the project owned by the caller (end date = start + duration), milestones, tasks (start + offset, due = start + offset + duration - 1, first board status, `metadata.template_role`), dependencies, statuses and status rules in one transaction. 201 `{project, task_count, milestone_count, roles}`. Events `project.created` and `project.template.applied` |
| POST | `/projects/{id}/save-as-template` | `templates.add` | `{name, description?, category?}`. Offsets from the project start (else its earliest task date); a task's role is its primary assignee's project role; roles count the staff team (owner as project_manager). 201 with the template. Event `project.template.saved` |

### Recurring tasks

A task repeats when its metadata holds `repeat: {freq: daily|weekly|monthly, interval: 1..365,
until?: YYYY-MM-DD}`, set on the normal task create and update (validated, 422 otherwise; `null`
removes it). `recurrence` is server-owned (a client value is dropped): `{series_id, occurrence, key,
previous_task_id, anchor_day, spawned, next_task_id, ended}`.

One job runs every 10 minutes, claimed per period fleet-wide through the shared run-once helper
(`sharedcache.ClaimPeriod`), started from app.Run and stopped with it. It creates the next copy once
the current task is done or its due date has passed, whichever comes first: next due = the next
occurrence after the current due date (or after the completion day when there is none), skipping
rounds already missed so a long-overdue task makes one copy due today or later; the start keeps its
distance from the due date; monthly keeps the series' day of the month (31st becomes 28th or 29th
February, then 31st again). Past `until`, the series ends. The copy takes the title, description,
priority, estimate, parent, WBS code, client metadata, the assignees still on the team (no assigned
notice each round), and the project's first status. The source row is locked and marked spawned in
the same transaction and the occurrence key is checked first, so a retry or a second pod never makes
two. A partial index (`task_recurring_pending`) keeps the scan small. Event `project.task.recurred`.

### Status rules

Stored in project metadata (`status_rules`, written only by its route; a project update drops the
key): `{transitions: [{from, to: [keys]}], requirements: [{to, from?, fields: [...]}]}`. A status
with a transition entry may only move to the listed statuses; a status without one moves anywhere.
Required field codes: `assignee`, `estimated_hours`, `actual_hours` (task `metadata.actual_hours` above
zero), `start_date`, `due_date`, `description`, `comment` (the task has a comment), and
`metadata.<key>` (non-empty). They are checked against the task as it will be after the change, so one
update that fills a field and moves the task passes. Every status change goes through the same
check: the board drag (`PATCH .../status`, idempotent), the drawer (`PUT .../tasks/{taskId}`).
A refused move answers 422 `{error: "Before moving to Done, this task needs actual hours, a comment.",
code: "status_rule_blocked", from, to, reason: transition_not_allowed|missing_fields, missing_fields: [codes]}`.
A task's initial status on create is not a move. Without `project_automation` on the plan the stored
rules and automations stay dormant (kept, not enforced).

| Method | Path | Permission | Notes |
|---|---|---|---|
| GET | `/projects/{id}/status-rules` | `tasks.view` or `tasks.view_own` | `{transitions, requirements}` |
| PUT | `/projects/{id}/status-rules` | `automation.manage` | Same body; keys must be the project's statuses, at most 100 of each. Event `project.status_rules.updated` |
| GET | `/projects/{id}/task-moves/{taskId}` | `tasks.view` or `tasks.view_own` (own tasks only) | Why a column is blocked before the drop: `{data: [{status, allowed, reason, missing_fields, message}]}` for every other status |

### Automations

Stored in project metadata (`automations`, written only by its route), at most 25 per project and 5
actions each: `{id, name, enabled, trigger: {to, from?}, actions: [...]}`. Actions: `assign`
(`user_ids`, `mode: add|replace`, default add), `notify` (`user_ids` and/or `recipients: ["assignees"]`,
`message?`, at most 20 people), `set_field` (`field: priority` with low, medium, high or urgent, or
`metadata.<key>` for any key that is not server-owned and not `repeat`; `value`). People named must be
on the project's staff team when the rules are saved; anyone who has left by the time a rule runs is
skipped. When a status change matches a rule, its actions run in the same transaction as the change
(after the change itself, so an automation wins over a field set in the same request). Actions never
change the status and write the task directly, so an automation's own change never fires rules again.
Newly assigned people get `project.task.assigned`; the task's activity entry lists the rules that ran
under `changes.automations`.

| Method | Path | Permission | Notes |
|---|---|---|---|
| GET | `/projects/{id}/automations` | `automation.view` | `{data: [...], limit: 25}` |
| PUT | `/projects/{id}/automations` | `automation.manage` | `{automations: [...]}` replaces the list; new rules get an id. Event `project.automations.updated` |

### Events (outbox, `project.>`)

Every payload carries the ids named; `actor_id` is the signed-in user ("" for the job).

| Subject | Payload |
|---|---|
| `project.workload.settings_updated` | `{weekly_hours, person_overrides, actor_id}` (aggregate: the tenant) |
| `project.baseline.saved` | `{project_id, baseline_id, name, task_count, milestone_count, truncated, actor_id}` |
| `project.baseline.deleted` | `{project_id, baseline_id, actor_id}` |
| `project.risk.created`, `.updated`, `.deleted` | `{project_id, risk_id, title, likelihood, impact, score, status, owner_id, due_date?}`; updated adds `changes` |
| `project.change_request.submitted`, `.updated`, `.withdrawn`, `.approved`, `.rejected` | `{project_id, change_request_id, title, kind, status, requested_by, date_shift_count, has_budget_change, decided_by?, decision_comment?}`; approved adds `shifted_task_ids` |
| `project.change_request.budget_pushed` | the above plus `{treasury_status (submitted or failed), budget_id, error}` |
| `project.template.saved` | `{template_id, name, source_project_id, task_count, phase_count, actor_id}` |
| `project.template.deleted` | `{template_id, name, actor_id}` |
| `project.template.applied` | `{project_id, template_id, template_name, task_count, milestone_count, start_date, actor_id}` |
| `project.task.recurred` | `{project_id, task_id, task_title, previous_task_id, series_id, occurrence, due_date, assignee_ids}` |
| `project.status_rules.updated` | `{project_id, rules, actor_id}` |
| `project.automations.updated` | `{project_id, count, actor_id}` |
| `project.task.automation_notice` | `{project_id, project_name, task_id, task_title, rule_id, rule_name, from_status, to_status, to_status_label, message, recipient_ids: [...], action_path}`. One event per rule run with every recipient id. notifications-api has no consumer for it yet |

### Tables

`project_baselines`, `project_risks`, `project_change_requests`, `project_templates` and
`tenant_settings` (migration `20261117065829_sprint_16_planning_depth.sql`), each indexed on its
tenant (and project) columns. Change requests are a table rather than project metadata because they
are listed by status per project and across the tenant (the approvals queue). Templates are a table
because they belong to the tenant, not to a project, and system rows have no tenant. Status rules and
automations live in project metadata: they are read only with their project and never queried across
projects. Workload capacity lives in `tenant_settings` because projects had no tenant level row.
Deleting a project deletes its baselines, risks and change requests.

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
