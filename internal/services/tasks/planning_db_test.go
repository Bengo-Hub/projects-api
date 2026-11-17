package tasks

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	enttask "github.com/bengobox/projects-service/internal/ent/task"
)

// Status rules refuse a move with the missing fields named, on the drag route and the drawer
// update alike; filling the fields (in the same update) lets it through; without the feature the
// rules are dormant.
func TestStatusRulesEnforced(t *testing.T) {
	f := newFixture(t)
	if _, err := f.svc.SetStatusRules(f.ctx, f.tenant, f.project.ID, StatusRules{
		Transitions:  []Transition{{From: "todo", To: []string{"in_progress"}}},
		Requirements: []Requirement{{To: "done", Fields: []string{FieldActualHours, FieldComment}}},
	}); err != nil {
		t.Fatal(err)
	}
	task, err := f.svc.CreateTask(f.ctx, f.tenant, f.project.ID, CreateTaskInput{Title: "Pour"})
	if err != nil {
		t.Fatal(err)
	}
	var blocked *StatusRuleBlocked
	if _, _, err := f.svc.ChangeStatus(f.ctx, f.tenant, f.project.ID, task.ID, "done", ""); !errors.As(err, &blocked) || blocked.Reason != ReasonTransition {
		t.Fatalf("todo to done: %v", err)
	}
	if _, _, err := f.svc.ChangeStatus(f.ctx, f.tenant, f.project.ID, task.ID, "in_progress", ""); err != nil {
		t.Fatal(err)
	}
	checks, err := f.svc.CheckMoves(f.ctx, f.tenant, f.project.ID, task.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range checks {
		if c.Status == "done" && (c.Allowed || !slices.Equal(c.Missing, []string{FieldActualHours, FieldComment})) {
			t.Fatalf("moves check for done: %+v", c)
		}
	}
	done := "done"
	_, err = f.svc.UpdateTask(f.ctx, f.tenant, f.project.ID, task.ID, UpdateTaskInput{Status: &done, Metadata: map[string]any{MetaActualHours: 3.0}})
	if !errors.As(err, &blocked) || !slices.Equal(blocked.Missing, []string{FieldComment}) {
		t.Fatalf("hours given, comment missing: %v", err)
	}
	f.client.Comment.Create().SetTenantID(f.tenant).SetProjectID(f.project.ID).SetTaskID(task.ID).SetUserID(f.owner).SetContent("ok").SaveX(f.ctx)
	got, err := f.svc.UpdateTask(f.ctx, f.tenant, f.project.ID, task.ID, UpdateTaskInput{Status: &done, Metadata: map[string]any{MetaActualHours: 3.0}})
	if err != nil || got.Status != "done" {
		t.Fatalf("all fields given: %v", err)
	}
	// Dormant without project_automation.
	other, _ := f.svc.CreateTask(f.ctx, f.tenant, f.project.ID, CreateTaskInput{Title: "Cure"})
	if _, _, err := f.svc.ChangeStatus(WithPlanningRules(f.ctx, false), f.tenant, f.project.ID, other.ID, "done", ""); err != nil {
		t.Fatalf("rules should be dormant: %v", err)
	}
	// A project update cannot overwrite the rules through metadata.
	rules, _ := f.svc.GetStatusRules(f.ctx, f.tenant, f.project.ID)
	if len(rules.Requirements) != 1 || len(f.outbox(t, EventStatusRulesUpdated)) != 1 {
		t.Fatalf("rules: %+v", rules)
	}
}

// An automation on "done" assigns, sets the priority and notifies in the same transaction as the
// move; its own changes trigger nothing further.
func TestAutomationsRunWithTheMove(t *testing.T) {
	f := newFixture(t)
	if _, err := f.svc.SetAutomations(f.ctx, f.tenant, f.project.ID, []Automation{
		{Name: "Hand to QA", Enabled: true, Trigger: Trigger{To: "review"}, Actions: []AutomationAction{
			{Type: ActionAssign, UserIDs: []uuid.UUID{f.ben}},
			{Type: ActionSetField, Field: "priority", Value: "high"},
			{Type: ActionSetField, Field: "metadata.qa_round", Value: 1.0},
			{Type: ActionNotify, UserIDs: []uuid.UUID{f.ana}, Recipients: []string{"assignees"}, Message: "Ready for QA"},
		}},
		// Would fire if the automation's own change re-triggered rules (it never changes status).
		{Name: "Loop guard", Enabled: true, Trigger: Trigger{To: "done"}, Actions: []AutomationAction{{Type: ActionSetField, Field: "priority", Value: "low"}}},
	}); err != nil {
		t.Fatal(err)
	}
	task, _ := f.svc.CreateTask(f.ctx, f.tenant, f.project.ID, CreateTaskInput{Title: "Pour", AssigneeIDs: []uuid.UUID{f.ana}})
	got, _, err := f.svc.ChangeStatus(f.ctx, f.tenant, f.project.ID, task.ID, "review", "")
	if err != nil {
		t.Fatal(err)
	}
	if got.Priority != "high" || got.Metadata["qa_round"] != 1.0 || len(got.Edges.Assignees) != 2 || got.Edges.Assignees[1].UserID != f.ben {
		t.Fatalf("automation result: priority %s meta %v assignees %d", got.Priority, got.Metadata, len(got.Edges.Assignees))
	}
	notices := f.outbox(t, EventAutomationNotice)
	if len(notices) != 1 || notices[0]["message"] != "Ready for QA" || len(notices[0]["recipient_ids"].([]any)) != 2 {
		t.Fatalf("notice: %v", notices)
	}
	assigned := f.outbox(t, "task.assigned")
	if !slices.ContainsFunc(assigned, func(p map[string]any) bool { return p["recipient_user_id"] == f.ben.String() }) {
		t.Fatal("ben was not told he was assigned")
	}
	// Dormant without the feature: nothing fires.
	other, _ := f.svc.CreateTask(f.ctx, f.tenant, f.project.ID, CreateTaskInput{Title: "Cure"})
	got, _, _ = f.svc.ChangeStatus(WithPlanningRules(f.ctx, false), f.tenant, f.project.ID, other.ID, "review", "")
	if got.Priority != "medium" {
		t.Fatalf("dormant automation ran: %s", got.Priority)
	}
	// A rule cannot name someone off the team.
	if _, err := f.svc.SetAutomations(f.ctx, f.tenant, f.project.ID, []Automation{{Name: "x", Enabled: true, Trigger: Trigger{To: "done"},
		Actions: []AutomationAction{{Type: ActionAssign, UserIDs: []uuid.UUID{uuid.New()}}}}}); err == nil {
		t.Fatal("stranger accepted")
	}
}

// The job creates the next occurrence of a past-due weekly task once (with its assignee), marks
// the source, and a second run does nothing; a repeat rule is validated on write.
func TestRecurringTasksSpawnOnce(t *testing.T) {
	f := newFixture(t)
	if _, err := f.svc.CreateTask(f.ctx, f.tenant, f.project.ID, CreateTaskInput{Title: "Bad", Metadata: map[string]any{MetaRepeat: map[string]any{"freq": "hourly"}}}); err == nil {
		t.Fatal("bad repeat accepted")
	}
	src, err := f.svc.CreateTask(f.ctx, f.tenant, f.project.ID, CreateTaskInput{
		Title: "Weekly report", AssigneeIDs: []uuid.UUID{f.ana}, StartDate: day("2026-11-09"), DueDate: day("2026-11-10"),
		EstimatedHours: hrs(2), Metadata: map[string]any{MetaRepeat: map[string]any{"freq": "weekly"}, "recurrence": "forged"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, forged := src.Metadata["recurrence"]; forged {
		t.Fatal("a client wrote the server-owned recurrence key")
	}
	now := time.Date(2026, 11, 17, 8, 0, 0, 0, time.UTC)
	n, err := f.svc.SpawnRecurring(f.ctx, now)
	if err != nil || n != 1 {
		t.Fatalf("first run: %d %v", n, err)
	}
	if n, _ := f.svc.SpawnRecurring(f.ctx, now); n != 0 {
		t.Fatalf("second run handled %d", n)
	}
	next := f.client.Task.Query().Where(enttask.TenantID(f.tenant), enttask.IDNEQ(src.ID)).WithAssignees().OnlyX(f.ctx)
	if next.DueDate.Format(time.DateOnly) != "2026-11-17" || next.StartDate.Format(time.DateOnly) != "2026-11-16" || next.Status != "todo" {
		t.Fatalf("next occurrence dates/status: %v %v %s", next.DueDate, next.StartDate, next.Status)
	}
	if len(next.Edges.Assignees) != 1 || next.Edges.Assignees[0].UserID != f.ana || *next.EstimatedHours != 2 {
		t.Fatal("assignee or estimate not carried")
	}
	rec := next.Metadata["recurrence"].(map[string]any)
	if rec["key"] != src.ID.String()+":2" || rec["previous_task_id"] != src.ID.String() {
		t.Fatalf("recurrence: %v", rec)
	}
	srcNow := f.client.Task.GetX(f.ctx, src.ID)
	if srcNow.Metadata["recurrence"].(map[string]any)["next_task_id"] != next.ID.String() {
		t.Fatal("source not marked")
	}
	if ev := f.outbox(t, EventTaskRecurred); len(ev) != 1 || ev[0]["occurrence"] != 2.0 {
		t.Fatalf("event: %v", ev)
	}
	// Not due yet and not done: the new occurrence waits.
	if n, _ := f.svc.SpawnRecurring(f.ctx, now); n != 0 {
		t.Fatal("future occurrence spawned early")
	}
	// Done early: the next one is created at once.
	if _, _, err := f.svc.ChangeStatus(f.ctx, f.tenant, f.project.ID, next.ID, "done", ""); err != nil {
		t.Fatal(err)
	}
	if n, _ := f.svc.SpawnRecurring(f.ctx, now); n != 1 {
		t.Fatal("done occurrence did not spawn")
	}
}
