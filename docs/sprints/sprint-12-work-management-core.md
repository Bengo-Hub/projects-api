# Sprint 12: Work management core

**Status**: API built, awaiting the main session's generate, build and test run (2026-11-16); UI not started. Works on every Projects plan (`task_tracking`, `team_collaboration`); only the Gantt chart needs `gantt_chart` (tier 3). Closes the open parts of sprints 3 and 4.

## Progress (2026-11-16, API)
- [x] US-12.2 `task_assignees` with a primary; `tasks.assignee_id` dropped by the migrator after copying it. Filters, `_own` checks and My Work use any assignee.
- [x] US-12.3 Mentions parsed from sanitised comment HTML (Tiptap mention spans), members only, never the author; `project.task.mentioned` and `project.task.assigned` through the outbox.
- [x] US-12.4 Attachments: direct upload to the shared object storage, confirm with a byte check, list per task and project, delete own or with `comments.manage`.
- [x] US-12.5 Board statuses in project metadata, column pages through `?status=`, board counts in one GROUP BY.
- [x] US-12.6 Calendar date filters (`due_from`, `due_to`) and the reschedule route.
- [x] US-12.7 Critical path on the Gantt route (2000 cap kept); reschedule refuses a broken finish-to-start link with 409 and the blocking tasks.
- [x] US-12.8 My Work list and counts across visible projects.
- [x] US-12.9 SSE stream per project on the shared Broadcaster.
- [x] US-12.10 (API side) idempotent status change with `Idempotency-Key`.
- [ ] US-12.1 drawer, checklist UI, and every UI story: projects-ui.
- [ ] notifications-api consumer and templates (below).

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

## Notifications
Work needed in notifications-api (pattern: `cmd/worker/projects_consumer.go`), bound to the projects stream `project.>`:
- **Subjects:** `project.task.assigned`, `project.task.mentioned`, `project.task.status_changed`. Each event is one notice for one person (projects-api already fans out and never addresses the actor), so the consumer sends exactly what it receives, deduplicated by event id.
- **Common payload:** `project_id`, `project_name`, `task_id` (empty for a mention in a project comment), `task_title`, `actor_id`, `actor_name`, `recipient_user_id`, `recipient_email`, `recipient_name`, `comment_excerpt`, `action_path`.
- **Extra fields:** mentioned adds `comment_id` (and fills `comment_excerpt`, plain text, at most 200 characters); status_changed adds `from_status`, `to_status`, `to_status_label`.
- **Recipient:** `recipient_email` and `recipient_name` can be empty for someone who never opened projects; resolve them from auth-api by `recipient_user_id` then, and the phone for WhatsApp always.
- **Link:** `action_path` is `/{org}/projects/{project}/tasks?task={task}`; prefix the projects UI base URL. In WhatsApp it goes on the button.
- **Templates:** email and a WhatsApp `_btn` template for each of the three (`project_task_assigned_btn`, `project_task_mentioned_btn`, `project_task_status_changed_btn`), with the Meta template sync noted. Status changes can be noisy: respect the user's notification preferences, and consider a per-task digest.
