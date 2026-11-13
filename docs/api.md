# projects-api: API reference

This file covers the QA and acceptance module. The older project, task, tender and financials
routes are listed in `internal/http/router/router.go` and the handlers under `internal/http/handlers`.

## QA and acceptance

One place where a tenant or Codevertex staff run acceptance for a service, a project or other work.
A template holds the criteria and the pass rule; a session freezes them, invites the people who
test or sign, records their verdicts, opens tickets for severe failures and produces the signed
report.

### Permissions

The codes travel in the auth-api token. Tenant admins (`admin`, `superuser`) and the platform owner
pass without them.

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
| `FIELD_ENCRYPTION_KEY` | empty | Seals signer addresses at rest (AES-256-GCM); without it signers stay hash-only and get no close or decision notice |
| `QA_ACCESS_TTL` | `8h` | Invitee access lifetime |
| `QA_COOKIE_SECURE` | `true` | Secure, SameSite=None cookie; off only on plain HTTP locally |
| `QA_CATALOG_ENABLED` | `false` | Read the feature catalogue from subscriptions-api |
| `QA_UI_BASE_URL` | `https://projects.codevertexafrica.com` | Invite and session links |
| `TICKETING_SERVICE_URL` | in-cluster ticketing-api | Defect tickets |
| `SUBSCRIPTIONS_SERVICE_URL` | in-cluster subscriptions-api | Feature catalogue |
