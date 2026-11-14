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

## Field encryption keys

### Design

I use envelope encryption. `FIELD_ENCRYPTION_KEY` (the `projects-api-secrets` secret) is the root
and the app never writes it anywhere. Data keys are 32 random bytes the app makes on a rotation and
stores in `field_keys`, sealed under the root and bound to their key id. Values sealed under a data
key read `v3:<key id>:base64(nonce|ciphertext)`, AES-256-GCM with the raw data key and the row id
as additional data. Values from before the first rotation (`v2:` bound, or unprefixed legacy) are
under the root and keep opening; the first rotation moves them.

I looked at the two patterns already in the fleet. treasury-api and notifications-api keep their
credential key in a `service_configs` row and let the platform owner replace it, with only a masked
status and a fingerprint shown. hadia-api and maskani-api keep a key ring in env
(`FIELD_ENCRYPTION_KEYS`) with a re-seal job. projects-api cannot change its own env, so I kept
hadia's ring idea (key id in the value, old keys kept for decrypt until a re-seal empties them,
compare and set writes) and moved the ring into the database under the env root, which is closer to
treasury's model but never stores a key in the clear. The fingerprint is treasury's: the first 8
hex characters of the SHA-256. Neither service has a backup download or a step-up code, so those
are new here.

### Code

- `internal/shared/fieldcrypt`: `Box` is the root; `keyring.go` has data key sealing, the `Ring`
  (seals with the current key, opens any loaded key or the root, `Reseal`) and `Fingerprint`.
- `internal/services/fieldkeys`: health, backup, rotation, the re-seal pass and `Run`.
- `internal/http/handlers/field_keys.go`: the platform owner routes.

### Tables

| Table | Holds |
|---|---|
| `field_keys` | One row per data key: id, `key_enc` (sealed under the root, cleared on retirement), fingerprint, the root fingerprint it was sealed under, status `current`, `previous` or `retired` (one current, by a partial unique index), and the re-seal progress of the rotation that made it current (heartbeat, done time, moved and failed counts) |
| `field_key_audits` | Backup downloads, rotations and retirements: action, actor id and email, key id, key and root fingerprints, time |

### Rotation

1. The owner rotates with a fresh code. In one transaction the current key turns `previous` and a
   new key becomes `current`. New values go under it at once on that replica.
2. Every replica reloads the ring every 30 seconds (and at once when it meets a key id it does not
   know), so within a minute everyone seals under the new key while still opening the old one.
3. The re-seal pass moves every value in 200 row batches. It needs no stored cursor: a moved value
   drops out of the next scan, so a pass cut short by a restart is simply continued. A row is only
   written if it still holds the value that was read. One replica runs it, holding a heartbeat on
   the current key; another replica takes over when the heartbeat is two minutes stale.
4. When nothing is left on an older key, nothing failed, and the new key has been current for five
   minutes (so no replica still seals under the old one), the previous keys are retired: status
   `retired`, material cleared, one audit row each. The rotation then shows `done`.

A value that will not open is logged by row id and counted; it holds the old key back until I deal
with it. Nothing rotates on deploy: `Run` only reloads the ring and continues a rotation the owner
started. Every new sealed column must be added to `targets` in `fieldkeys.go`, or a retirement
would strand its values.

### Backup and recovery

The backup is the current and previous data keys in the clear, plus the root fingerprint, the value
format and restore notes. I left the root out on purpose: it already has its own home and backup in
the cluster secrets, and a file holding both would be the whole system in one place. With the file
alone I can open every value sealed under its keys even if `field_keys` or the root is lost. Values
still under the root, and the sealed rows in `field_keys`, need the root. Take a backup after every
rotation finishes, because a retired key is gone for good.

Changing `FIELD_ENCRYPTION_KEY` by hand after the first rotation stops the data keys from opening
(start-up logs which key and which root fingerprints differ). The way back is the old root, or
re-inserting the keys from a backup sealed under the new root.
