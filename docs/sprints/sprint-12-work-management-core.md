# Sprint 12: Work management core

**Status**: Planned (2026-11-16). Works on every Projects plan (`task_tracking`, `team_collaboration`); only the Gantt chart needs `gantt_chart` (tier 3). Closes the open parts of sprints 3 and 4.

**Sprint goal**: the everyday work screens people compare us with (Zoho, Asana, ClickUp), done well on a phone.

## Why
Reviewers mark Zoho down on its mobile app (54% negative on Capterra), Asana for one assignee per task, and ClickUp for lag past 500 tasks. Each story below answers one of those.

## Stories
- **US-12.1 Task drawer.** Open a task from any view in a side drawer (full screen on phones) showing description in rich text, subtasks, a checklist (kept in task metadata), comments, attachments and the activity history.
  - Test: open, edit and close without leaving the board.
- **US-12.2 Several assignees.** A `task_assignees` table, with the current assignee kept as the primary. "My Work" and filters use any assignee.
  - Test: two people see the same task in My Work.
- **US-12.3 Mentions.** `@name` in a comment notifies that person by email and WhatsApp through notifications-api (`_btn` templates, link as a button).
  - Test: a mention creates one notification per person, never for the author.
- **US-12.4 Attachments.** Upload to the shared object storage, listed on the task and the project. Size and type limits are enforced on the server.
- **US-12.5 Board.** Drag and drop between columns. Columns come from the project's statuses, kept in project metadata with a default set. Each column loads in pages.
  - Test: a 1,000-task project board stays responsive.
- **US-12.6 List and calendar.** The list view uses the shared data table with saved filters. The calendar shows tasks by due date, and dragging a task changes its date.
- **US-12.7 Gantt chart.** Bars, dependency lines, drag to move or resize, and the critical path, from the bounded Gantt route.
- **US-12.8 My Work.** One page for everything assigned to me across projects, grouped by overdue, today, this week and later.
- **US-12.9 Live updates.** Board, drawer and feed update through SSE on the shared events fan-out hub (works across pods), with a reconnect that refetches.
- **US-12.10 Offline-tolerant edits.** Status changes, comments and checklist ticks queue while offline and send on reconnect.

## Data
- New table `task_assignees(tenant_id, task_id, user_id, is_primary)`, unique per task and user, indexed by `(tenant_id, user_id)`.
- Attachments reuse the existing `attachment` table.
- Statuses and the checklist live in metadata, with no new columns.

## Events
- `project.task.assigned`, `project.task.mentioned` and `project.task.status_changed` on the `project` stream, consumed by notifications-api.
