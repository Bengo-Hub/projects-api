# Sprint 15: Billing and profitability

**Status**: Planned (2026-11-16). Gated on the existing `milestone_billing` code (tier 3).

**Sprint goal**: go from finished work to a sent invoice in three clicks or fewer, and show the margin per project and per client. This is what Teamwork sells to agencies, joined here to M-Pesa, PayHero and eTIMS through treasury.

## Ownership
Treasury owns invoices, payments and eTIMS. projects-api creates draft invoices over S2S (`POST /api/v1/s2s/{tenant}/invoices`) with `reference_type` set to `project_milestone` or `project_time` and the source id as `reference_id`, so `GET /invoices/by-reference` makes every create idempotent. projects-api keeps only the invoice id in milestone or project metadata.

## Stories
- **US-15.1 Bill a milestone.** A milestone carries an amount or a share of the contract value. "Create invoice" opens a prefilled draft in treasury.
- **US-15.2 Bill time.** Pick a period. Approved billable ERP hours times the project rate become invoice lines, and the hours are marked billed in the entry metadata so they are never billed twice.
- **US-15.3 Contract types.** Fixed fee, time and materials, retainer (a monthly amount with a hours allowance and a rollover rule) and non-billable, in project metadata. This finishes the `billing_type` and `contract_value` work from the budgets plan.
- **US-15.4 Profitability.** Margin per project and per client from invoiced revenue (treasury) minus cost (treasury actuals plus ERP labour cost), worked out in SQL or batched S2S calls, never one call per project.
- **US-15.5 Payment status.** Paid and overdue status shows on the milestone, read from treasury by reference.

## Tests
- A second create returns the same invoice, by reference.
- Billed hours are excluded from the next run.
- A tenant without `milestone_billing` gets the plan lock message.
