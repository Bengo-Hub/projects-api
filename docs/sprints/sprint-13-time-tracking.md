# Sprint 13: Time tracking

**Status**: API built (2026-11-16), waiting for build, tests, the erp-api deploy and the UI. Works on every Projects plan that has `time_tracking`. Replaces the time part of sprint 5.

**Sprint goal**: log time where the work happens, without a second timesheet system.

## Ownership
ERP owns timesheets. Its attendance module has a weekly timesheet per employee (`POST /hrm/attendance/timesheets`), entries per date carrying a `project_id` (`PUT /hrm/attendance/timesheets/{id}/entries`), and submit, approve and reject transitions. projects-api already reads the approved and submitted hours through `GET /hrm/attendance/timesheets/project-hours`. projects-api never stores hours of its own.

## Stories
- **US-13.1 Timer.** Start and stop a timer on a task. The running timer lives in the user's browser and in task metadata, so it survives a reload. Stopping it adds the time to that day's entry for the project in ERP.
- **US-13.2 Manual entry and week view.** A week grid of my hours per project and day. Edits go to the ERP entry; submitting sends the ERP submit transition. Approval stays in ERP.
- **US-13.3 Billable time.** Each project has a billable flag and a rate in its metadata. Billable hours feed sprint 15 invoicing.
- **US-13.4 Figures.** The existing EVM and utilisation figures read the same ERP hours, so the numbers agree everywhere.
- **US-13.5 Offline.** Timer stops and week edits queue while offline.

## Integration work
- projects-api maps the signed-in user to their ERP employee and finds or creates the week's timesheet over S2S.
- If ERP has no S2S route for that lookup, or entries cannot carry the task and billable flag, I add them in erp-api in the same sprint and deploy erp first.

## Tests
- A contract test against ERP's request and response shapes.
- A timer stop that crosses midnight splits into two dates.
- A user with no ERP employee record gets a clear message, never a silent failure.

## Progress

### 2026-11-16: API side (projects-api and erp-api), not yet built, tested or deployed
- **erp-api.** ERP had per-employee timesheets but entries were unique per date, so a second project on the same day overwrote the first, and no route let a service act for one employee. Added:
  - `TimesheetEntry.task_id` (optional) and `billable` (default false); entries are now unique per (date, project, task). `cmd/migrate` drops the old per-date unique index before the online migration. ERP's own entry upsert takes `task_id` and `billable` too.
  - `GET /hrm/attendance/timesheets/for-user`, `POST .../for-user/entries` (mode set or add) and `POST .../for-user/submit`, keyed by the auth user id through the ESS employee resolver. S2S with X-API-Key and X-Tenant-ID, or the employee's own sign-in (checked for the `attendance` feature). 404 `no_employee_record`, 409 `timesheet_locked` and `nothing_to_submit`.
  - Handler tests: caller rules and feature lock, and an end to end flow against Postgres (set, add, a second project the same day, the 24 hour cap, submit, lock, and the submitted hours in `project-hours`).
- **projects-api.** `internal/services/timetracking` and `handlers/time.go`: week, entries, submit, timer start, stop, discard and the running timer (see `docs/api.md`, Time). Gated on `time_tracking` and ERP's `attendance` on the user's token, with `projects.time.*`. Billable defaults from `billing_type` (`time_and_materials` bills hours); the rate is project `metadata.hourly_rate`, validated with the other commercial terms. Running timers sit in `tasks.metadata.timers`, server owned.
- **Plans.** ERP gates timesheets on `attendance`. Every plan that grants `time_tracking` also grants it: standalone ERP tiers 2 to 4 (`erpFeatures`), and PowerSuite tier 3 (`psSuiteProjectsBlock` and `psSuiteERPBlock` both open at tier 3).
- **US-13.4.** EVM and utilisation already read `project-hours`; the new entries carry `project_id`, so they count once submitted or approved. No change there.
- **Open.** US-13.5 (offline queue) is UI work: the stop route takes the browser's own start and stop for it. Deploy erp-api first. Sprint 15 invoicing reads the billable entries.

### 2026-11-17: UI side (projects-ui), built, waiting on its checks
- [x] US-13.1 timer: start and stop in the task drawer and on board cards, discard after a confirm, and a header pill on every page that reads `GET /time/timer` and keeps a browser copy for offline.
- [x] US-13.2 week: `/{org}/time` grid by project, task and day (a day list on phones), notes, add a row, week navigation, status, submit after a confirm, and another person's week with `time.view`.
- [x] US-13.3 billable: the switch shows only on time and materials projects.
- [x] US-13.5 offline: week edits and timer stops queue offline. The stop route has no Idempotency-Key, so the UI checks the task's timer before replaying a stop and uses `logged_segments` to send only the rest of a partial one.
- [x] `no_employee_record` shows an empty state telling the user to ask HR.
- Waiting on the main session: type check, lint, `pnpm test` and Playwright in projects-ui. Details in projects-ui `docs/plan.md`, Sprint 13.
