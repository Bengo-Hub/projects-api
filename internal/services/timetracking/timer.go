package timetracking

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/bengobox/projects-service/internal/ent"
	enttask "github.com/bengobox/projects-service/internal/ent/task"
)

// Running timers live in tasks.metadata under "timers" as {user id: RFC 3339 start}, one entry
// per user who is timing the task. They are written with single jsonb statements, so a task edit
// and a timer start never overwrite each other's keys (tasks' clientMetadata keeps "timers" too).

// Timer is a user's running timer.
type Timer struct {
	ProjectID uuid.UUID `json:"project_id"`
	TaskID    uuid.UUID `json:"task_id"`
	TaskTitle string    `json:"task_title"`
	StartedAt time.Time `json:"started_at"`
}

// TaskProject returns the project of a task in the tenant, for membership checks.
func (s *Service) TaskProject(ctx context.Context, tenantID, taskID uuid.UUID) (uuid.UUID, error) {
	t, err := s.client.Task.Query().Where(enttask.TenantID(tenantID), enttask.ID(taskID)).
		Select(enttask.FieldID, enttask.FieldProjectID).Only(ctx)
	if ent.IsNotFound(err) {
		return uuid.Nil, ErrNotFound
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("load task: %w", err)
	}
	return t.ProjectID, nil
}

// runningTimers returns every task the user has a timer running on, newest first. Normally there is
// at most one; a second can only come from a race, and starting a timer stops them all.
func (s *Service) runningTimers(ctx context.Context, tenantID, userID uuid.UUID) ([]Timer, error) {
	rows, err := s.client.QueryContext(ctx,
		`SELECT id, project_id, title, metadata->'timers'->>$2::text FROM tasks
		  WHERE tenant_id = $1 AND metadata->'timers'->>$2::text IS NOT NULL`, tenantID, userID.String())
	if err != nil {
		return nil, fmt.Errorf("running timers: %w", err)
	}
	defer rows.Close()
	var out []Timer
	for rows.Next() {
		var t Timer
		var started string
		if err := rows.Scan(&t.TaskID, &t.ProjectID, &t.TaskTitle, &started); err != nil {
			return nil, fmt.Errorf("running timers: %w", err)
		}
		at, perr := time.Parse(time.RFC3339Nano, started)
		if perr != nil {
			continue // not a start this package wrote; ignored rather than failing every read
		}
		t.StartedAt = at
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("running timers: %w", err)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt.After(out[j].StartedAt) })
	return out, nil
}

// RunningTimer returns the user's running timer, nil when none runs.
func (s *Service) RunningTimer(ctx context.Context, tenantID, userID uuid.UUID) (*Timer, error) {
	ts, err := s.runningTimers(ctx, tenantID, userID)
	if err != nil || len(ts) == 0 {
		return nil, err
	}
	return &ts[0], nil
}

// TaskTimer returns the user's running timer on the task, nil when none runs there.
func (s *Service) TaskTimer(ctx context.Context, tenantID, userID, taskID uuid.UUID) (*Timer, error) {
	ts, err := s.runningTimers(ctx, tenantID, userID)
	if err != nil {
		return nil, err
	}
	for i := range ts {
		if ts[i].TaskID == taskID {
			return &ts[i], nil
		}
	}
	return nil, nil
}

// Started is a timer start: the running timer and, when another task's timer was running, what
// stopping it logged.
type Started struct {
	Timer   Timer     `json:"timer"`
	Stopped []Stopped `json:"stopped"`
}

// StartTimer starts the user's timer on the task. A user runs one timer at a time: a timer running
// on another task is stopped first and its time logged to ERP, split at midnight in loc. Starting
// the task's own running timer again keeps its start.
func (s *Service) StartTimer(ctx context.Context, tenantID, userID, taskID uuid.UUID, loc *time.Location) (*Started, error) {
	if _, err := s.TaskProject(ctx, tenantID, taskID); err != nil {
		return nil, err
	}
	running, err := s.runningTimers(ctx, tenantID, userID)
	if err != nil {
		return nil, err
	}
	out := &Started{Stopped: []Stopped{}}
	now := s.now().UTC()
	for _, t := range running {
		if t.TaskID == taskID {
			out.Timer = t
			continue
		}
		stopped, err := s.StopTimer(ctx, tenantID, userID, StopInput{
			ProjectID: t.ProjectID, TaskID: &t.TaskID, StartedAt: t.StartedAt, StoppedAt: now,
		}, loc)
		if err != nil {
			return nil, fmt.Errorf("stop the timer on %q: %w", t.TaskTitle, err)
		}
		out.Stopped = append(out.Stopped, *stopped)
	}
	if out.Timer.TaskID == taskID {
		return out, nil
	}
	_, err = s.client.ExecContext(ctx, `UPDATE tasks SET metadata =
		(CASE WHEN jsonb_typeof(metadata) = 'object' THEN metadata ELSE '{}'::jsonb END) ||
		jsonb_build_object('timers',
			(CASE WHEN jsonb_typeof(metadata->'timers') = 'object' THEN metadata->'timers' ELSE '{}'::jsonb END) ||
			jsonb_build_object($1::text, $2::text))
		WHERE tenant_id = $3 AND id = $4`, userID.String(), now.Format(time.RFC3339Nano), tenantID, taskID)
	if err != nil {
		return nil, fmt.Errorf("start timer: %w", err)
	}
	t, err := s.TaskTimer(ctx, tenantID, userID, taskID)
	if err != nil {
		return nil, err
	}
	if t == nil {
		return nil, ErrNotFound // the task was deleted meanwhile
	}
	out.Timer = *t
	return out, nil
}

// ClearTimer discards the user's running timer on the task without logging any time.
func (s *Service) ClearTimer(ctx context.Context, tenantID, userID, taskID uuid.UUID) error {
	return s.clearTimer(ctx, tenantID, userID, taskID)
}

func (s *Service) clearTimer(ctx context.Context, tenantID, userID, taskID uuid.UUID) error {
	_, err := s.client.ExecContext(ctx, `UPDATE tasks SET metadata = metadata #- ARRAY['timers', $1::text]
		WHERE tenant_id = $2 AND id = $3 AND metadata->'timers'->>$1::text IS NOT NULL`,
		userID.String(), tenantID, taskID)
	if err != nil {
		return fmt.Errorf("clear timer: %w", err)
	}
	return nil
}
