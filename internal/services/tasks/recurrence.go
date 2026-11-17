package tasks

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"entgo.io/ent/dialect/sql"
	"github.com/bengobox/projects-service/internal/ent/predicate"

	"github.com/bengobox/projects-service/internal/ent"
	enttask "github.com/bengobox/projects-service/internal/ent/task"
	entassignee "github.com/bengobox/projects-service/internal/ent/taskassignee"
	"github.com/bengobox/projects-service/internal/platform/events"
	"github.com/bengobox/projects-service/internal/platform/realtime"
	"github.com/bengobox/projects-service/internal/services/activity"
	"github.com/bengobox/projects-service/internal/services/notify"
	"github.com/bengobox/projects-service/internal/services/txn"
)

// Recurring tasks (sprint 16). A task repeats when its metadata holds a repeat rule:
//
//	repeat      {"freq": "daily"|"weekly"|"monthly", "interval": 1-365, "until": "YYYY-MM-DD"?}
//	recurrence  server-owned: {"series_id", "occurrence", "key", "previous_task_id", "anchor_day",
//	            "spawned", "next_task_id", "ended"}
//
// The next copy is created once the current task is done or its due date has passed, whichever
// comes first, so a recurring chore keeps appearing even when one round is missed. The next due
// date steps from the current due date (or from the completion day when the task has none) and
// skips forward past any rounds already missed, so a task overdue for weeks spawns one copy due
// today or later, not a backlog. Each occurrence has a key (series id and occurrence number);
// the source task's row is locked and marked spawned in the same transaction that creates the
// copy, and the key is checked before creating, so a retry or a second pod never makes two.
const (
	MetaRepeat     = "repeat"
	metaRecurrence = "recurrence"
)

// Repeat frequencies.
const (
	FreqDaily   = "daily"
	FreqWeekly  = "weekly"
	FreqMonthly = "monthly"
)

// EventTaskRecurred is written when the job creates the next occurrence.
const EventTaskRecurred = "task.recurred"

// recurringBatch bounds one scan; maxSkip bounds the skip over missed rounds.
const (
	recurringBatch = 200
	maxSkip        = 5000
)

// RepeatRule is a task's repeat rule.
type RepeatRule struct {
	Freq     string `json:"freq"`
	Interval int    `json:"interval"`
	Until    string `json:"until,omitempty"`
}

// UntilDate is the last day an occurrence may be due (nil: no end).
func (r RepeatRule) UntilDate() *time.Time {
	if r.Until == "" {
		return nil
	}
	t, err := time.Parse(time.DateOnly, r.Until)
	if err != nil {
		return nil
	}
	return &t
}

// ParseRepeat validates a repeat rule as stored in metadata (a JSON object).
func ParseRepeat(v any) (RepeatRule, error) {
	var r RepeatRule
	b, err := json.Marshal(v)
	if err != nil || json.Unmarshal(b, &r) != nil {
		return r, ErrValidation("repeat must be an object: {freq, interval, until?}")
	}
	switch r.Freq {
	case FreqDaily, FreqWeekly, FreqMonthly:
	default:
		return r, ErrValidation("repeat.freq must be daily, weekly or monthly")
	}
	if r.Interval == 0 {
		r.Interval = 1
	}
	if r.Interval < 1 || r.Interval > 365 {
		return r, ErrValidation("repeat.interval must be between 1 and 365")
	}
	if r.Until != "" {
		if _, err := time.Parse(time.DateOnly, r.Until); err != nil {
			return r, ErrValidation("repeat.until must be a date (YYYY-MM-DD)")
		}
	}
	return r, nil
}

// addMonths moves t by n months keeping the anchor day, clamped to the month's last day (the
// 31st repeats as 28 or 29 February, then 31 March again).
func addMonths(t time.Time, n, anchorDay int) time.Time {
	y, m, _ := t.Date()
	first := time.Date(y, m+time.Month(n), 1, t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), t.Location())
	last := first.AddDate(0, 1, -1).Day()
	d := anchorDay
	if d > last {
		d = last
	}
	return first.AddDate(0, 0, d-1)
}

// NextOccurrence is the occurrence after `from` under r. anchorDay is the series' day of the
// month for monthly rules (0 takes from's day).
func NextOccurrence(from time.Time, r RepeatRule, anchorDay int) time.Time {
	interval := r.Interval
	if interval < 1 {
		interval = 1
	}
	switch r.Freq {
	case FreqWeekly:
		return from.AddDate(0, 0, 7*interval)
	case FreqMonthly:
		if anchorDay < 1 || anchorDay > 31 {
			anchorDay = from.Day()
		}
		return addMonths(from, interval, anchorDay)
	}
	return from.AddDate(0, 0, interval)
}

// NextDue is the due date of the occurrence after a task due `due`: the first occurrence on or
// after today (missed rounds are skipped). ok is false when the rule's end date is passed.
func NextDue(due, today time.Time, r RepeatRule, anchorDay int) (time.Time, bool) {
	next := NextOccurrence(due, r, anchorDay)
	for i := 0; i < maxSkip && dayOf(next).Before(dayOf(today)); i++ {
		next = NextOccurrence(next, r, anchorDay)
	}
	if until := r.UntilDate(); until != nil && dayOf(next).After(*until) {
		return time.Time{}, false
	}
	return next, true
}

// normalizeRepeat validates and canonicalises the repeat key of client metadata in place.
func normalizeRepeat(meta map[string]any) error {
	v, ok := meta[MetaRepeat]
	if !ok {
		return nil
	}
	if v == nil {
		delete(meta, MetaRepeat)
		return nil
	}
	r, err := ParseRepeat(v)
	if err != nil {
		return err
	}
	out := map[string]any{"freq": r.Freq, "interval": r.Interval}
	if r.Until != "" {
		out["until"] = r.Until
	}
	meta[MetaRepeat] = out
	return nil
}

// recurringPendingWhere matches the task_recurring_pending partial index (see the Task schema).
const recurringPendingWhere = `(metadata -> 'repeat') IS NOT NULL AND (metadata #>> '{recurrence,spawned}') IS NULL`

// SpawnRecurring creates the next occurrence of every recurring task that is done or past due,
// at most recurringBatch per call; it returns how many it handled. Run it from one pod at a time
// (the app's ticker claims each period through the shared run-once helper); the row locks and
// the spawned mark keep it safe even when two runs overlap.
func (s *Service) SpawnRecurring(ctx context.Context, now time.Time) (int, error) {
	today := dayOf(now)
	rows, err := s.client.QueryContext(ctx, `SELECT id FROM tasks WHERE `+recurringPendingWhere+
		` AND (status = $1 OR due_date < $2) ORDER BY due_date NULLS LAST LIMIT $3`, StatusDone, today, recurringBatch)
	if err != nil {
		return 0, fmt.Errorf("scan recurring tasks: %w", err)
	}
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return 0, fmt.Errorf("scan recurring tasks: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}
	handled := 0
	for _, id := range ids {
		created, err := s.spawnOne(ctx, id, now)
		if err != nil {
			s.log.Warn("recurring task not spawned", zap.String("task_id", id.String()), zap.Error(err))
			continue
		}
		handled++
		if created != nil {
			s.emit(ctx, created.TenantID, realtime.TaskCreated, created)
		}
	}
	return handled, nil
}

// spawnOne handles one source task: creates its next occurrence (nil when the series has ended
// or another run got there first) and marks it spawned.
func (s *Service) spawnOne(ctx context.Context, sourceID uuid.UUID, now time.Time) (*ent.Task, error) {
	today := dayOf(now)
	var created *ent.Task
	err := txn.Run(ctx, s.client, func(tx *ent.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT id FROM tasks WHERE id = $1 AND `+recurringPendingWhere+
			` AND (status = $2 OR due_date < $3) FOR UPDATE SKIP LOCKED`, sourceID, StatusDone, today)
		if err != nil {
			return fmt.Errorf("lock recurring task: %w", err)
		}
		found := rows.Next()
		if err := rows.Close(); err != nil {
			return err
		}
		if !found {
			return nil // handled meanwhile, or locked by another run
		}
		src, err := tx.Task.Query().Where(enttask.ID(sourceID)).Only(ctx)
		if err != nil {
			return fmt.Errorf("load recurring task: %w", err)
		}
		rec, _ := src.Metadata[metaRecurrence].(map[string]any)
		mark := func(extra map[string]any) error {
			next := map[string]any{}
			for k, v := range rec {
				next[k] = v
			}
			next["spawned"] = true
			for k, v := range extra {
				next[k] = v
			}
			meta := make(map[string]any, len(src.Metadata)+1)
			for k, v := range src.Metadata {
				meta[k] = v
			}
			meta[metaRecurrence] = next
			return tx.Task.UpdateOneID(src.ID).SetMetadata(meta).Exec(ctx)
		}
		rule, err := ParseRepeat(src.Metadata[MetaRepeat])
		if err != nil {
			return mark(map[string]any{"ended": true, "error": err.Error()})
		}
		anchor := src.DueDate
		if anchor.IsZero() {
			anchor = src.CompletedAt
			if anchor.IsZero() {
				anchor = now
			}
		}
		anchorDay, _ := toFloat(rec["anchor_day"])
		if anchorDay == 0 {
			anchorDay = float64(anchor.Day())
		}
		nextDue, ok := NextDue(anchor, today, rule, int(anchorDay))
		if !ok {
			return mark(map[string]any{"ended": true})
		}
		seriesID := src.ID.String()
		if v, ok := rec["series_id"].(string); ok && v != "" {
			seriesID = v
		}
		occ, _ := toFloat(rec["occurrence"])
		if occ < 1 {
			occ = 1
		}
		key := fmt.Sprintf("%s:%d", seriesID, int(occ)+1)
		exists, err := tx.Task.Query().Where(enttask.TenantID(src.TenantID), enttask.ProjectID(src.ProjectID),
			metadataContains(map[string]any{metaRecurrence: map[string]any{"key": key}})).Exist(ctx)
		if err != nil {
			return fmt.Errorf("check occurrence: %w", err)
		}
		if exists {
			return mark(nil)
		}
		p, err := projectForTask(ctx, tx, src.TenantID, src.ProjectID)
		if err != nil {
			return err
		}
		status := statusesOf(p.Metadata)[0].Key
		meta := clientMetadata(nil, src.Metadata)
		meta[metaRecurrence] = map[string]any{
			"series_id": seriesID, "occurrence": int(occ) + 1, "key": key,
			"previous_task_id": src.ID.String(), "anchor_day": int(anchorDay),
		}
		c := tx.Task.Create().SetTenantID(src.TenantID).SetProjectID(src.ProjectID).SetTitle(src.Title).
			SetDescription(src.Description).SetStatus(status).SetPriority(src.Priority).
			SetDueDate(nextDue).SetMetadata(meta)
		if src.StartDate != nil && !src.StartDate.IsZero() && !src.DueDate.IsZero() {
			c.SetStartDate(nextDue.Add(-src.DueDate.Sub(*src.StartDate)))
		}
		if src.EstimatedHours != nil {
			c.SetEstimatedHours(*src.EstimatedHours)
		}
		if src.ParentID != uuid.Nil {
			c.SetParentID(src.ParentID)
		}
		if src.WbsCode != "" {
			c.SetWbsCode(src.WbsCode)
		}
		if created, err = c.Save(ctx); err != nil {
			return fmt.Errorf("create occurrence: %w", err)
		}
		// The assignees carry over (anyone who has left the project is dropped), without a fresh
		// "assigned to you" notice each round.
		prev, err := tx.TaskAssignee.Query().Where(entassignee.TenantID(src.TenantID), entassignee.TaskID(src.ID)).
			Order(assigneeOrderBy()...).All(ctx)
		if err != nil {
			return fmt.Errorf("load assignees: %w", err)
		}
		ids := assigneeIDs(prev)
		members, err := notify.Members(ctx, tx, src.TenantID, src.ProjectID, ids)
		if err != nil {
			return err
		}
		kept := ids[:0:0]
		for _, id := range ids {
			if members[id] {
				kept = append(kept, id)
			}
		}
		if created.Edges.Assignees, _, err = setAssignees(ctx, tx, src.TenantID, src.ProjectID, created.ID, kept); err != nil {
			return err
		}
		if err := mark(map[string]any{"next_task_id": created.ID.String()}); err != nil {
			return fmt.Errorf("mark spawned: %w", err)
		}
		if err := activity.Record(ctx, tx, activity.Entry{
			TenantID: src.TenantID, ProjectID: activity.Ref(src.ProjectID), TaskID: activity.Ref(created.ID),
			EntityType: "task", EntityID: created.ID, Action: activity.ActionCreated,
			Changes: map[string]any{"title": created.Title, "status": created.Status, "recurred_from": src.ID.String(), "occurrence": int(occ) + 1},
		}); err != nil {
			return err
		}
		return events.Write(ctx, tx, events.AggregateProject, EventTaskRecurred, created.ID, src.TenantID, map[string]any{
			"project_id": src.ProjectID.String(), "task_id": created.ID.String(), "task_title": created.Title,
			"previous_task_id": src.ID.String(), "series_id": seriesID, "occurrence": int(occ) + 1,
			"due_date": nextDue.Format(time.DateOnly), "assignee_ids": idStrings(kept),
		})
	})
	return created, err
}

// metadataContains matches tasks whose metadata contains v (jsonb @>).
func metadataContains(v map[string]any) predicate.Task {
	raw, _ := json.Marshal(v)
	return predicate.Task(func(s *sql.Selector) {
		s.Where(sql.P(func(b *sql.Builder) {
			b.WriteString(s.C(enttask.FieldMetadata)).WriteString(" @> ").Arg(string(raw)).WriteString("::jsonb")
		}))
	})
}
