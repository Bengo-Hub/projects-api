# Sprint 14: Client portal

**Status**: Planned (2026-11-16). Gated on the existing `client_portal` code, which every Projects tier has (decided 2026-11-16). Decision: clients sign in through auth-api, and their contact record stays in marketflow.

**Sprint goal**: let a firm's client follow and approve the work without emails back and forth. Zoho charges extra for client users; ours comes with every Projects plan.

## Stories
- **US-14.1 Invite a client.** From the project, pick the client's marketflow contact. The client gets an email and a WhatsApp invite with a sign-in button. auth-api creates the account with the client role (an OAuth client with a `self_signup_role` of client, like the Digitika parent portal). projects-api stores only `crm_contact_id` and the auth user id on a project member row with the role `client`.
- **US-14.2 What a client sees.** Branded sign-in and pages showing project progress, milestones and their status, tasks marked as shared, shared files and invoices from treasury. Never internal comments, costs or margins.
- **US-14.3 Approve a milestone.** The client approves or asks for changes, with a comment. Approval publishes `project.milestone.approved`, which sprint 15 can turn into an invoice.
- **US-14.4 Comments with the client.** A separate client thread per project, so internal talk never leaks.
- **US-14.5 Updates.** WhatsApp and email notices when a milestone is ready for review or a status report is shared.

## Security
- Client users pass the same permission checks, with only `projects.portal.*` view codes.
- Every client query is scoped to projects where they are a client member.
- Tests: a client cannot read another project, internal comments or financials.
