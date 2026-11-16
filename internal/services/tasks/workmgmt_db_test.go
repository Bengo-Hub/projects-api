package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/projects-service/internal/ent"
	entoutbox "github.com/bengobox/projects-service/internal/ent/outboxevent"
	"github.com/bengobox/projects-service/internal/platform/realtime"
	"github.com/bengobox/projects-service/internal/services/activity"
	"github.com/bengobox/projects-service/internal/testutil"
)

// fixture is a tenant with one project owned by owner and two members, ana and ben.
type fixture struct {
	client          *ent.Client
	svc             *Service
	live            *recorder
	tenant          uuid.UUID
	owner, ana, ben uuid.UUID
	project         *ent.Project
	ctx             context.Context // acting as owner
}

type recorder struct{ events []realtime.Event }

func (r *recorder) Publish(_ uuid.UUID, ev realtime.Event) { r.events = append(r.events, ev) }

func newFixture(t *testing.T) *fixture {
	t.Helper()
	client := testutil.Client(t)
	f := &fixture{client: client, live: &recorder{}, tenant: uuid.New(), owner: uuid.New(), ana: uuid.New(), ben: uuid.New()}
	f.svc = NewService(client, nil, zap.NewNop()).WithRealtime(f.live)
	ctx := context.Background()
	f.project = client.Project.Create().SetTenantID(f.tenant).SetName("Bridge").SetOwnerID(f.owner).SaveX(ctx)
	for _, u := range []uuid.UUID{f.ana, f.ben} {
		client.ProjectMember.Create().SetTenantID(f.tenant).SetProjectID(f.project.ID).SetUserID(u).SetRoleCode("member").SaveX(ctx)
	}
	client.ProjectsUser.Create().SetTenantID(f.tenant).SetAuthServiceUserID(f.ana).SetEmail("ana@acme.test").SetName("Ana").SaveX(ctx)
	client.ProjectsUser.Create().SetTenantID(f.tenant).SetAuthServiceUserID(f.owner).SetEmail("own@acme.test").SetName("Olive").SaveX(ctx)
	f.ctx = activity.WithActor(ctx, f.owner)
	return f
}

func (f *fixture) outbox(t *testing.T, eventType string) []map[string]any {
	t.Helper()
	rows := f.client.OutboxEvent.Query().Where(entoutbox.EventType(eventType)).AllX(context.Background())
	out := make([]map[string]any, 0, len(rows))
	for _, r := range rows {
		var env struct {
			Payload map[string]any `json:"payload"`
		}
		if err := json.Unmarshal(r.Payload, &env); err != nil {
			t.Fatal(err)
		}
		out = append(out, env.Payload)
	}
	return out
}

func day(s string) *Date {
	v, _ := time.Parse(time.DateOnly, s)
	return &Date{Time: v}
}

// Two people on one task both see it in My Work and through ?assignee; only the new assignee
// other than the author is notified; a non-member cannot be assigned.
func TestAssigneesAndMyWork(t *testing.T) {
	f := newFixture(t)
	task, err := f.svc.CreateTask(f.ctx, f.tenant, f.project.ID, CreateTaskInput{
		Title: "Pour", AssigneeIDs: []uuid.UUID{f.ana, f.owner, f.ana}, DueDate: day("2026-11-16"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(task.Edges.Assignees) != 2 || task.Edges.Assignees[0].UserID != f.ana || !task.Edges.Assignees[0].IsPrimary {
		t.Fatalf("assignees = %+v, want ana (primary) and owner", task.Edges.Assignees)
	}
	assigned := f.outbox(t, "task.assigned")
	if len(assigned) != 1 || assigned[0]["recipient_user_id"] != f.ana.String() || assigned[0]["recipient_email"] != "ana@acme.test" ||
		assigned[0]["actor_name"] != "Olive" || assigned[0]["project_name"] != "Bridge" {
		t.Fatalf("assigned events = %v, want one for ana", assigned)
	}
	now := time.Date(2026, 11, 16, 9, 0, 0, 0, time.UTC)
	for _, u := range []uuid.UUID{f.ana, f.owner} {
		items, total, err := f.svc.MyWork(context.Background(), f.tenant, u, MyWorkFilter{Bucket: BucketToday, Now: now})
		if err != nil || total != 1 || len(items) != 1 || items[0].ID != task.ID {
			t.Errorf("my work today for %s: %d %v", u, total, err)
		}
	}
	counts, err := f.svc.MyWorkCounts(context.Background(), f.tenant, f.ana, nil, now, nil)
	if err != nil || counts[BucketToday] != 1 || counts[BucketOverdue] != 0 || counts[BucketLater] != 0 {
		t.Errorf("counts = %v %v", counts, err)
	}
	// The next day it is overdue.
	if counts, _ := f.svc.MyWorkCounts(context.Background(), f.tenant, f.ana, nil, now.AddDate(0, 0, 1), nil); counts[BucketOverdue] != 1 {
		t.Errorf("next day counts = %v, want overdue 1", counts)
	}
	_, total, err := f.svc.ListTasks(context.Background(), f.tenant, f.project.ID, ListTasksFilter{Assignee: &f.owner})
	if err != nil || total != 1 {
		t.Errorf("assignee filter on a secondary assignee: %d %v", total, err)
	}
	if ok, _ := f.svc.IsAssignee(context.Background(), f.tenant, f.project.ID, task.ID, f.owner); !ok {
		t.Error("a secondary assignee must pass the _own check")
	}
	stranger := uuid.New()
	_, err = f.svc.UpdateTask(f.ctx, f.tenant, f.project.ID, task.ID, UpdateTaskInput{AssigneeIDs: &[]uuid.UUID{stranger}})
	var verr ValidationError
	if !errors.As(err, &verr) {
		t.Errorf("assigning a non-member: %v, want a validation error", err)
	}
	// A member who left no longer sees the task through VisibleTo.
	if _, total, _ := f.svc.MyWork(context.Background(), f.tenant, f.ben, MyWorkFilter{VisibleTo: &f.ben}); total != 0 {
		t.Errorf("ben has no tasks, got %d", total)
	}
}

// Statuses: the default set, validation on write, board counts in one query, and a status still
// in use cannot be removed.
func TestStatusesAndBoardCounts(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	list, err := f.svc.GetStatuses(ctx, f.tenant, f.project.ID)
	if err != nil || len(list) != 4 || list[3].Key != StatusDone {
		t.Fatalf("default statuses = %v %v", list, err)
	}
	if _, err := f.svc.CreateTask(f.ctx, f.tenant, f.project.ID, CreateTaskInput{Title: "x", Status: "nope"}); err == nil {
		t.Error("an unknown status must be refused")
	}
	for _, st := range []string{"todo", "todo", "review"} {
		if _, err := f.svc.CreateTask(f.ctx, f.tenant, f.project.ID, CreateTaskInput{Title: "t", Status: st}); err != nil {
			t.Fatal(err)
		}
	}
	counts, err := f.svc.BoardCounts(ctx, f.tenant, f.project.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]int{"todo": 2, "in_progress": 0, "review": 1, "done": 0}
	if len(counts) != 4 {
		t.Fatalf("counts = %v", counts)
	}
	for _, c := range counts {
		if want[c.Status] != c.Count {
			t.Errorf("%s = %d, want %d", c.Status, c.Count, want[c.Status])
		}
	}
	// Removing "review" while a task holds it is refused; without "done" too.
	if _, err := f.svc.SetStatuses(ctx, f.tenant, f.project.ID, []Status{{Key: "todo", Label: "To do", Category: CategoryTodo}, {Key: "done", Label: "Done", Category: CategoryDone}}); err == nil {
		t.Error("removing a status in use must be refused")
	}
	if _, err := ValidateStatuses([]Status{{Key: "todo", Label: "To do", Category: CategoryTodo}}); err == nil {
		t.Error(`a set without "done" must be refused`)
	}
	custom := []Status{
		{Key: "todo", Label: "Backlog", Category: CategoryTodo, Order: 0},
		{Key: "review", Label: "Review", Category: CategoryInProgress, Order: 2},
		{Key: "blocked", Label: "Blocked", Category: CategoryInProgress, Order: 1},
		{Key: "done", Label: "Done", Category: CategoryDone, Order: 3},
	}
	saved, err := f.svc.SetStatuses(ctx, f.tenant, f.project.ID, custom)
	if err != nil || saved[1].Key != "blocked" {
		t.Fatalf("set statuses = %v %v", saved, err)
	}
	if got, _ := f.svc.GetStatuses(ctx, f.tenant, f.project.ID); len(got) != 4 || got[1].Key != "blocked" {
		t.Errorf("stored statuses = %v", got)
	}
}

// A replayed status change with the same Idempotency-Key changes nothing the second time.
func TestChangeStatusIdempotent(t *testing.T) {
	f := newFixture(t)
	task, err := f.svc.CreateTask(f.ctx, f.tenant, f.project.ID, CreateTaskInput{Title: "t", AssigneeIDs: []uuid.UUID{f.ana}})
	if err != nil {
		t.Fatal(err)
	}
	got, replayed, err := f.svc.ChangeStatus(f.ctx, f.tenant, f.project.ID, task.ID, "done", "k-1")
	if err != nil || replayed || got.Status != "done" || got.CompletedAt.IsZero() {
		t.Fatalf("first change: %+v replayed=%v %v", got, replayed, err)
	}
	// Someone moves it back; the offline replay of k-1 must not move it to done again.
	if _, _, err := f.svc.ChangeStatus(f.ctx, f.tenant, f.project.ID, task.ID, "todo", ""); err != nil {
		t.Fatal(err)
	}
	got, replayed, err = f.svc.ChangeStatus(f.ctx, f.tenant, f.project.ID, task.ID, "done", "k-1")
	if err != nil || !replayed || got.Status != "todo" {
		t.Fatalf("replay: status %s replayed=%v %v", got.Status, replayed, err)
	}
	if n := len(f.outbox(t, "task.status_changed")); n != 2 {
		t.Errorf("status_changed events = %d, want 2 (the replay writes none)", n)
	}
	moved := 0
	for _, ev := range f.live.events {
		if ev.Type == realtime.TaskMoved {
			moved++
		}
	}
	if moved != 2 {
		t.Errorf("task.moved hints = %d, want 2", moved)
	}
	// A metadata update by a client keeps the server-owned keys.
	if _, err := f.svc.UpdateTask(f.ctx, f.tenant, f.project.ID, task.ID, UpdateTaskInput{Metadata: map[string]any{"checklist": []any{}, metaStatusMutations: "x"}}); err != nil {
		t.Fatal(err)
	}
	if _, replayed, _ := f.svc.ChangeStatus(f.ctx, f.tenant, f.project.ID, task.ID, "done", "k-1"); !replayed {
		t.Error("a client metadata update must not drop the idempotency keys")
	}
}

// Rescheduling cannot start a task before its finish-to-start predecessor finishes, nor push it
// past a successor's start; the conflict names the blocking tasks.
func TestRescheduleConflicts(t *testing.T) {
	f := newFixture(t)
	mk := func(title, start, due string) *ent.Task {
		task, err := f.svc.CreateTask(f.ctx, f.tenant, f.project.ID, CreateTaskInput{Title: title, StartDate: day(start), DueDate: day(due)})
		if err != nil {
			t.Fatal(err)
		}
		return task
	}
	pred := mk("dig", "2026-11-02", "2026-11-06")
	task := mk("pour", "2026-11-09", "2026-11-11")
	succ := mk("cure", "2026-11-12", "2026-11-20")
	if err := f.svc.AddDependency(f.ctx, f.tenant, f.project.ID, task.ID, pred.ID, "FS"); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.AddDependency(f.ctx, f.tenant, f.project.ID, succ.ID, task.ID, ""); err != nil {
		t.Fatal(err)
	}
	_, err := f.svc.Reschedule(f.ctx, f.tenant, f.project.ID, task.ID, RescheduleInput{StartDate: day("2026-11-05")})
	var conflict *ScheduleConflict
	if !errors.As(err, &conflict) || len(conflict.BlockingTaskIDs) != 1 || conflict.BlockingTaskIDs[0] != pred.ID {
		t.Fatalf("start before the predecessor finishes: %v", err)
	}
	_, err = f.svc.Reschedule(f.ctx, f.tenant, f.project.ID, task.ID, RescheduleInput{DueDate: day("2026-11-14")})
	if !errors.As(err, &conflict) || conflict.BlockingTaskIDs[0] != succ.ID {
		t.Fatalf("finish after the successor starts: %v", err)
	}
	// A same-day hand-over is allowed.
	got, err := f.svc.Reschedule(f.ctx, f.tenant, f.project.ID, task.ID, RescheduleInput{StartDate: day("2026-11-06"), DueDate: day("2026-11-12")})
	if err != nil || got.DueDate.Format(time.DateOnly) != "2026-11-12" {
		t.Fatalf("valid move: %v", err)
	}
	if _, err := f.svc.Reschedule(f.ctx, f.tenant, f.project.ID, task.ID, RescheduleInput{StartDate: day("2026-11-10"), DueDate: day("2026-11-09")}); err == nil {
		t.Error("due before start must be refused")
	}
	gantt, err := f.svc.GetGanttData(context.Background(), f.tenant, f.project.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range gantt.Tasks {
		if !g.Critical {
			t.Errorf("%s is on the only chain and must be critical", g.Title)
		}
	}
}
