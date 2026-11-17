# Sprint 14: Client portal

**Status**: Server side built (2026-11-17), portal screens pending. Planned 2026-11-16. Gated on the existing `client_portal` code, which every Projects tier has (decided 2026-11-16). Decision: clients sign in through auth-api, and their contact record stays in marketflow.

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
