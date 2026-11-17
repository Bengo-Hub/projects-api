# Sprint 15: Billing and profitability

**Status**: Built 2026-11-17 (API, ERP and treasury routes, UI). Gated on the existing `milestone_billing` code (tier 3). Contract in `docs/api.md`, "Billing and profitability".

## What I built
- projects-api `internal/services/billing`: milestone prices (amount or share of the contract
  value) and milestone invoices, time invoices for a period, retainer month invoices, and a billing
  overview per project. Every create is idempotent on a treasury reference, locked per reference in
  Redis, and a voided invoice moves its milestone or period to the next reference in a chain.
- Retainer terms in project metadata (`retainer_amount`, `retainer_hours`, `retainer_rollover`).
- Labour cost from ERP in project margins, `margin_to_date` on every billable project, and a
  per-client rollup (`GET /financials/clients`).
- The portal's invoice list now reads treasury (sent and paid invoices only).
- erp-api: `timesheet_entries.metadata` with reserve, confirm and release routes for billing, and
  `labour_cost` and `unbilled_billable_hours` on project hours.
- treasury-api: S2S `GET /invoices/by-references` (batch payment state) and
  `GET /invoices/by-project` (a project's invoices through the metadata GIN index).

## Still open
- Retainer hours: the allowance and rollover are stored and shown, but hours over the allowance are
  not billed automatically and the monthly usage is not tracked yet (ERP hours have no per-month
  project read). Until then overage is billed by hand in treasury.
- Approving and sending the invoice stay in treasury. Since 2026-11-17 projects-api sends
  `keep_draft: true` (a new treasury create field), so the invoice comes back a draft instead of
  approved; the firm approves it and sends it there, which puts it on eTIMS and posts its revenue to
  the project.
- VAT: lines carry the project's `tax_code` (VAT-16, VAT-8, VAT-0, VAT-EXEMPT). A project without one
  invoices with no VAT, so a VAT registered firm must set it on the commercial terms.

**Sprint goal**: go from finished work to a sent invoice in three clicks or fewer, and show the margin per project and per client. This is what Teamwork sells to agencies, joined here to M-Pesa, PayHero and eTIMS through treasury.

## Ownership
Treasury owns invoices, payments and eTIMS. projects-api creates invoices over S2S (`POST /api/v1/s2s/{tenant}/invoices`) with `reference_type` set to `project_milestone`, `project_time` or `project_retainer` and a reference id. Treasury's create does not dedupe, so projects-api checks `GET /invoices/by-reference` first under a Redis lock per reference, which makes every create idempotent. projects-api keeps only the invoice id, number and reference in milestone metadata.

## Stories
- **US-15.1 Bill a milestone.** A milestone carries an amount or a share of the contract value. "Create invoice" makes the invoice in treasury (a draft the firm approves and sends there) and links to it.
- **US-15.2 Bill time.** Pick a period. Approved billable ERP hours times the project rate become invoice lines, and the hours are marked billed in the entry metadata so they are never billed twice.
- **US-15.3 Contract types.** Fixed fee, time and materials, retainer (a monthly amount with a hours allowance and a rollover rule) and non-billable, in project metadata. This finishes the `billing_type` and `contract_value` work from the budgets plan.
- **US-15.4 Profitability.** Margin per project and per client from invoiced revenue (treasury) minus cost (treasury actuals plus ERP labour cost), worked out in SQL or batched S2S calls, never one call per project.
- **US-15.5 Payment status.** Paid and overdue status shows on the milestone, read from treasury by reference.

## Tests
- A second create returns the same invoice, by reference.
- Billed hours are excluded from the next run.
- A tenant without `milestone_billing` gets the plan lock message.
