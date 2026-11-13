# projects-api: architecture notes

## QA and acceptance module

### Ownership

| Data | Owner | How projects-api uses it |
|---|---|---|
| Templates, criteria, sessions, invites, results, defects | projects-api (this module) | Tenant scoped by `tenant_id`; platform templates have none |
| Feature catalogue | subscriptions-api | Read through `internal/platform/catalog` (behind `QA_CATALOG_ENABLED`); never copied |
| Users, email codes, stored signatures and consent | auth-api | Email code tokens traded once at `GET /api/v1/s2s/otp/validate` (`internal/platform/authotp`); the inviter's display name from `GET /api/v1/s2s/{tenant}/users`, falling back to the tenant name from the shared cache's tenant details (`internal/platform/authusers`, Redis cached) |
| Tickets | ticketing-api | `POST /api/v1/internal/tickets` (`internal/platform/ticketing`) |
| Alert routing | notifications-api | Fed by the `project.qa.*` events |

### Code

- `internal/qamodel`: vocabularies, the pass rule and the criteria snapshot (JSON column types,
  kept free of ent so the schema can use them).
- `internal/ent/schema/qa*.go`: `qa_templates`, `qa_criteria`, `qa_sessions`, `qa_invites`,
  `qa_results`, `qa_defects`.
- `internal/services/qa`: the service. `passrule.go` is the pure evaluation shared by the board,
  close and the report; `access.go` the invitee door; `defects.go` tickets and the failure event;
  `report.go` the PDF; `feature_events.go` the catalogue change consumer.
- `internal/http/handlers/qa.go` (tenant routes), `qa_invitee.go` (invitee routes),
  `qa_perms.go` (permission codes and gate).

### Tables

| Table | Key points |
|---|---|
| `qa_templates` | Nullable `tenant_id` (platform template), `service_tag`, `version` bumped on every criteria or rule change, `needs_review`, `pass_rule` jsonb |
| `qa_criteria` | `template_id` FK, `subject_kind` and `subject_ref`, `area_code`, `weight`, `sort_order`; index on (`subject_kind`, `subject_ref`) for the catalogue consumer |
| `qa_sessions` | `criteria_snapshot` and `pass_rule` frozen at creation, unique random `share_code`, `status`, `document_hash`; `metadata` holds tenant branding, the frozen report data and the decision |
| `qa_invites` | Keyed `email_hash` (unique per session) and a masked `email_hint`. Signers also keep `email_enc`, the address sealed with `FIELD_ENCRYPTION_KEY`, cleared when the invite stops being a signer; testers and observers are hash-only |
| `qa_results` | Append only; `is_current` with a unique partial index (session, criterion, tester) where current |
| `qa_defects` | One per (session, criterion), unique; the ticket number and status |

### Decisions

- Sessions copy the template's criteria, so a template change never moves an audit trail. A
  catalogue change only flags the template (`needs_review`).
- Testers are identified by the keyed hash of their email, whether they come in through SSO, an
  email code or the tenant's own login, so one person is one tester across all three.
- Signers must hear when a session closes and is decided, so their address is kept, sealed with
  AES-256-GCM (`internal/shared/fieldcrypt`, the same construction as hadia-api's field box, which
  is internal there and cannot be imported) and opened only to build the `signers` list of those
  events. The PDF and the frozen close data carry signer names only.
- Invitee access is a short HMAC-signed token bound to one session and invite, checked against the
  invite on every request.
- A severity 1 or 2 failure opens one ticket and one event per criterion: the unique defect row
  decides which tester's failure gets there first. Severity 3 and 4 are listed and raised by hand.
  A ticketing outage never loses the verdict; the defect is marked `ticket_failed` for a retry.
- Close freezes the outcome, defects and signers into the session and stores the SHA-256 of the
  PDF. The shared `reports` module renders the same input to the same bytes, so the stored hash can
  be checked by rendering again; the decision afterwards does not touch the document.
- Events use the `project.qa.` prefix so they ride the existing projects stream.

### Still to come

- subscriptions-api catalogue fields (`module`, `shipped_at`, `exclusive_tenant_id`), the S2S read
  and `subscriptions.feature.changed`; then `QA_CATALOG_ENABLED=true`.
- auth-api stored signatures with per-service consent, an email code `purpose`, and the `projects.qa.*`
  codes in its permission seed. Signing (a `qa_signatures` row per signer, stored signature through
  `reports.Signature.Image` or an uploaded wet-ink scan) and acceptance on the last signature.
- Evidence uploads through `httpware/objectstore` once scanning lands, into `qa_results.evidence_keys`;
  the stored PDF and a public verify-by-code route.
- notifications-api routing and templates for the `project.qa.*` events; projects-ui screens.
