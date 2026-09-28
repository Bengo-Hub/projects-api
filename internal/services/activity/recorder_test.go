package activity

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/projects-service/internal/ent"
	entactivity "github.com/bengobox/projects-service/internal/ent/activity"
	"github.com/bengobox/projects-service/internal/testutil"
)

func TestTaskChanges(t *testing.T) {
	d1 := time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)
	d1Later := time.Date(2026, 9, 1, 17, 0, 0, 0, time.UTC) // same day
	d2 := time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC)
	est := 8.0
	before := &ent.Task{Title: "A", Status: "todo", Priority: "low", ProgressPct: 0, DueDate: d1}
	after := &ent.Task{Title: "A", Status: "done", Priority: "low", ProgressPct: 100, DueDate: d1Later, StartDate: &d2, EstimatedHours: &est, Description: "x"}
	got := TaskChanges(before, after)
	for _, k := range []string{"status", "progress_pct", "start_date", "estimated_hours", "description"} {
		if _, ok := got[k]; !ok {
			t.Errorf("missing change %s in %v", k, got)
		}
	}
	for _, k := range []string{"title", "priority", "due_date", "assignee_id"} {
		if _, ok := got[k]; ok {
			t.Errorf("unexpected change %s", k)
		}
	}
	if got["status"].From != "todo" || got["status"].To != "done" {
		t.Errorf("status change = %+v", got["status"])
	}
	if len(TaskChanges(before, before)) != 0 || len(TaskChanges(nil, after)) != 0 {
		t.Error("no changes expected")
	}
}

func TestNilRecorderIsSafe(t *testing.T) {
	var r *Recorder
	r.Record(context.Background(), Entry{Type: "x"})
	if r.TaskTitle(context.Background(), uuid.New(), uuid.New()) != "" {
		t.Fatal("nil recorder has no titles")
	}
}

func TestRecord_DB(t *testing.T) {
	client := testutil.PGClient(t)
	ctx := context.Background()
	tenant := uuid.New()
	p := client.Project.Create().SetTenantID(tenant).SetName("P").SetOwnerID(uuid.New()).SaveX(ctx)
	task := client.Task.Create().SetTenantID(tenant).SetProjectID(p.ID).SetTitle("Survey").SaveX(ctx)
	r := NewRecorder(client, zap.NewNop())
	r.Record(ctx, Entry{TenantID: tenant, ProjectID: p.ID, TaskID: &task.ID, UserID: uuid.New(), Type: "task.updated",
		Payload: map[string]any{"changes": map[string]Change{"status": {From: "todo", To: "done"}}}})
	if got := r.TaskTitle(ctx, tenant, task.ID); got != "Survey" {
		t.Fatalf("title = %q", got)
	}
	if got := r.TaskTitle(ctx, uuid.New(), task.ID); got != "" {
		t.Fatal("another tenant's task title leaked")
	}
	a := client.Activity.Query().Where(entactivity.TaskID(task.ID)).OnlyX(ctx)
	changes := a.Payload["changes"].(map[string]any)["status"].(map[string]any)
	if a.ActivityType != "task.updated" || changes["from"] != "todo" || changes["to"] != "done" {
		t.Fatalf("stored activity = %+v", a)
	}
	// Deleting the task keeps the row as project-level history.
	client.Task.DeleteOne(task).ExecX(ctx)
	if n := client.Activity.Query().Where(entactivity.ProjectID(p.ID)).CountX(ctx); n != 1 {
		t.Fatalf("activity rows after task delete = %d", n)
	}
}
