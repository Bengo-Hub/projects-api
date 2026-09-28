// Package activity writes the project activity feed: one row per change to a project's tasks,
// comments, attachments, milestones and members. projects-ui reads it through the activities
// routes.
package activity

import (
	"context"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/projects-service/internal/ent"
	enttask "github.com/bengobox/projects-service/internal/ent/task"
)

// Entry is one feed row. TaskID is set for task-level activity.
type Entry struct {
	TenantID  uuid.UUID
	ProjectID uuid.UUID
	TaskID    *uuid.UUID
	UserID    uuid.UUID
	Type      string
	Payload   map[string]any
}

// Recorder writes feed rows. A nil Recorder records nothing, so callers need no checks.
type Recorder struct {
	client *ent.Client
	log    *zap.Logger
}

// NewRecorder builds a recorder.
func NewRecorder(client *ent.Client, log *zap.Logger) *Recorder {
	return &Recorder{client: client, log: log.Named("activity")}
}

// Record writes an entry. It is best-effort: the change it describes is already saved, so a
// failure is logged and never fails the request.
func (r *Recorder) Record(ctx context.Context, e Entry) {
	if r == nil || r.client == nil {
		return
	}
	c := r.client.Activity.Create().
		SetTenantID(e.TenantID).
		SetProjectID(e.ProjectID).
		SetNillableTaskID(e.TaskID).
		SetUserID(e.UserID).
		SetActivityType(e.Type).
		SetOccurredAt(time.Now())
	if e.Payload != nil {
		c = c.SetPayload(e.Payload)
	}
	if err := c.Exec(ctx); err != nil {
		r.log.Warn("activity not recorded", zap.String("type", e.Type), zap.Error(err))
	}
}

// TaskTitle looks up a task's title for a payload; empty when not found.
func (r *Recorder) TaskTitle(ctx context.Context, tenantID, taskID uuid.UUID) string {
	if r == nil || r.client == nil {
		return ""
	}
	t, err := r.client.Task.Query().Where(enttask.ID(taskID), enttask.TenantID(tenantID)).
		Select(enttask.FieldTitle).Only(ctx)
	if err != nil {
		return ""
	}
	return t.Title
}

// Change is one field's before and after value.
type Change struct {
	From any `json:"from"`
	To   any `json:"to"`
}

// TaskChanges lists the fields that differ between two versions of a task. Dates compare by
// day and text fields report only that they changed (no content), to keep payloads small.
func TaskChanges(before, after *ent.Task) map[string]Change {
	out := map[string]Change{}
	if before == nil || after == nil {
		return out
	}
	if before.Title != after.Title {
		out["title"] = Change{From: before.Title, To: after.Title}
	}
	if before.Description != after.Description {
		out["description"] = Change{}
	}
	if before.Status != after.Status {
		out["status"] = Change{From: before.Status, To: after.Status}
	}
	if before.Priority != after.Priority {
		out["priority"] = Change{From: before.Priority, To: after.Priority}
	}
	if before.ProgressPct != after.ProgressPct {
		out["progress_pct"] = Change{From: before.ProgressPct, To: after.ProgressPct}
	}
	if before.AssigneeID != after.AssigneeID {
		out["assignee_id"] = Change{From: idOrNil(before.AssigneeID), To: idOrNil(after.AssigneeID)}
	}
	if day(before.DueDate) != day(after.DueDate) {
		out["due_date"] = Change{From: day(before.DueDate), To: day(after.DueDate)}
	}
	if dayPtr(before.StartDate) != dayPtr(after.StartDate) {
		out["start_date"] = Change{From: dayPtr(before.StartDate), To: dayPtr(after.StartDate)}
	}
	if f64(before.EstimatedHours) != f64(after.EstimatedHours) {
		out["estimated_hours"] = Change{From: before.EstimatedHours, To: after.EstimatedHours}
	}
	return out
}

func idOrNil(id uuid.UUID) any {
	if id == uuid.Nil {
		return nil
	}
	return id.String()
}

func day(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format("2006-01-02")
}

func dayPtr(t *time.Time) string {
	if t == nil {
		return ""
	}
	return day(*t)
}

func f64(p *float64) float64 {
	if p == nil {
		return -1
	}
	return *p
}
