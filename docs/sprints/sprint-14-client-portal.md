# Sprint 14: Client portal

**Status**: Server side built (2026-11-17), portal screens built in projects-ui (2026-11-17, waiting on its type check and Playwright run). Planned 2026-11-16. Gated on the existing `client_portal` code, which every Projects tier has (decided 2026-11-16). Decision: clients sign in through auth-api, and their contact record stays in marketflow.

**Sprint goal**: let a firm's client follow and approve the work without emails back and forth. Zoho charges extra for client users; ours comes with every Projects plan.

## Stories
- **US-14.1 Invite a client.** From the project, pick the client's marketflow contact. The client gets an email and a WhatsApp invite with a sign-in button. auth-api creates the account with the client role (an OAuth client with a `self_signup_role` of client, like the Digitika parent portal). projects-api stores only `crm_contact_id` and the auth user id on a project member row with the role `client`.
- **US-14.2 What a client sees.** Branded sign-in and pages showing project progress, milestones and their status, tasks marked as shared, shared files and invoices from treasury. Never internal comments, costs or margins.
- **US-14.3 Approve a milestone.** The client approves or asks for changes, with a comment. Approval publishes `project.milestone.approved`, which sprint 15 can turn into an invoice.
- **US-14.4 Comments with the client.** A separate client thread per project, so internal talk never leaks.
- **US-14.5 Updates.** WhatsApp and email notices when a milestone is ready for review or a status report is shared.

## Progress (2026-11-17)

Server side built; the portal screens in projects-ui come next, from the contract in `docs/api.md`
(Client portal).

| Story | State | Where |
|---|---|---|
| US-14.1 | API done | `POST /projects/{id}/clients`; auth-api OAuth client `projects-portal` (cmd/seed); invite event `project.client.invited`; activation on the first portal call |
| US-14.2 | API done except invoices | `/portal/projects...`; tasks and files shared through the sharing routes; invoices answer `[]` until treasury lists a project's invoices over S2S |
| US-14.3 | API done | `POST /portal/projects/{id}/milestones/{mid}/decision`; statuses `ready_for_review`, `approved`, `changes_requested` |
| US-14.4 | API done | client thread on the comments table (`metadata.thread = client`) |
| US-14.5 | API and notices done | email and WhatsApp `_btn` templates in notifications-api; the four WhatsApp templates wait for the Meta sync. A shared status report notice is not built (no status report feature yet) |

### UI (projects-ui, 2026-11-17)

- [x] US-14.1: portal sign-in with the `projects-portal` client at `/{org}/portal`, a session kept
  apart from the staff one; Clients tab to invite from a marketflow contact, see resent invites
  and remove a client.
- [x] US-14.2: branded portal landing with progress cards and the `email_not_verified` banner;
  project page with summary, milestones, shared tasks, files with signed links and the invoices
  placeholder; "Visible to client" switches in the task drawer and on file rows.
- [x] US-14.3: approve or request changes with a comment, the 409 `milestone_not_ready` message;
  Ready for review on the team's milestones page.
- [x] US-14.4: the client thread in the portal and as a Client thread tab for the team.

### API follow-ups for the screens (2026-11-17)

- [x] `X-Client-Invites` in the CORS exposed headers, so the portal reads `email_not_verified`
  from another origin (router test `TestCORSExposesClientInvites`).
- [x] `GET /lookups/contacts` also opens to `portal.manage`, so someone who only manages clients
  can pick the contact to invite; the cost centre lookup keeps its gate
  (`TestContactLookupOpensToPortalManage`).
- [x] `last_decision` on the team's milestone list and on one milestone, the same reader as the
  portal's, in one query for the list. The milestones page shows the client's change request in
  place.
- [x] `author_kind` (client or team) and `author_name` on every client thread entry, both routes,
  one query per page.
- [x] `progress` on each `GET /portal/projects` card in two grouped queries for the page, counted
  the same way as the project page (the summary now reads the same counts), so the landing no
  longer asks for a summary per card. I added `awaiting_review` to it so the card still says when
  a milestone waits for the client.
- Indexes: none added. The progress counts read `tasks (tenant_id, project_id, status)` and
  `milestones (tenant_id, project_id, target_date)`, the decisions `activities (tenant_id,
  project_id, occurred_at)` and the thread authors `project_members (tenant_id, project_id,
  user_id)`.

Schema: none. Client rows, sharing flags and the thread all live in existing metadata columns;
the review lives in the milestone status and its activity entry.

## Security
- Client users pass the same permission checks, with only `projects.portal.*` view codes.
- Every client query is scoped to projects where they are a client member.
- Tests: a client cannot read another project, internal comments or financials.
  - `internal/services/portal/portal_db_test.go`: invite and activation (never across tenants,
    never another project), a client row is not team membership and cannot be mentioned, only
    shared tasks show, a task update cannot share a task, the client thread and the internal
    thread never mix, milestone review and decision.
  - `internal/http/router/portal_test.go`: a client is refused every team route (tasks, comments,
    financials, the client list), a member without `portal.manage` cannot invite, the feature gate.
  - `internal/services/crmsync/clients_test.go`: CRM erasure and delete follow the client rows.
- Known risk: activation trusts the SSO email (as the QA invitee SSO hand-over does). auth-api's
  email verification policy decides how far that goes.
