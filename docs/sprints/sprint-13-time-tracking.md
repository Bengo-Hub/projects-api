# Sprint 13: Time tracking

**Status**: Planned (2026-11-16). Works on every Projects plan. Replaces the time part of sprint 5.

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
