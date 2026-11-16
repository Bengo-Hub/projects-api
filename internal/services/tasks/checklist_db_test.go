package tasks

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/bengobox/projects-service/internal/services/activity"
)

// Ten people ticking ten different items at the same moment all keep their tick (the row lock
// serialises the read-modify-write); a replayed key applies once; a client metadata update can
// neither overwrite nor drop the checklist.
func TestChecklistConcurrentTicks(t *testing.T) {
	f := newFixture(t)
	task, err := f.svc.CreateTask(f.ctx, f.tenant, f.project.ID, CreateTaskInput{Title: "Pour"})
	if err != nil {
		t.Fatal(err)
	}
	const n = 10
	for i := 0; i < n; i++ {
		if _, _, err := f.svc.ApplyChecklist(f.ctx, f.tenant, f.project.ID, task.ID,
			ChecklistOp{Op: ChecklistAdd, ItemID: fmt.Sprintf("i%d", i), Text: ptr(fmt.Sprintf("Step %d", i))}, ""); err != nil {
			t.Fatal(err)
		}
	}
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, _, errs[i] = f.svc.ApplyChecklist(f.ctx, f.tenant, f.project.ID, task.ID,
				ChecklistOp{Op: ChecklistToggle, ItemID: fmt.Sprintf("i%d", i), Done: ptr(true)}, fmt.Sprintf("tick-%d", i))
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("tick %d: %v", i, err)
		}
	}
	got := f.client.Task.GetX(context.Background(), task.ID)
	items := readChecklist(got.Metadata)
	if len(items) != n {
		t.Fatalf("items = %d, want %d", len(items), n)
	}
	for _, it := range items {
		if !it.Done {
			t.Errorf("%s lost its tick", it.ID)
		}
	}

	// Someone unticks i0; the offline replay of tick-0 must not tick it again.
	if _, _, err := f.svc.ApplyChecklist(f.ctx, f.tenant, f.project.ID, task.ID, ChecklistOp{Op: ChecklistToggle, ItemID: "i0", Done: ptr(false)}, ""); err != nil {
		t.Fatal(err)
	}
	after, replayed, err := f.svc.ApplyChecklist(f.ctx, f.tenant, f.project.ID, task.ID, ChecklistOp{Op: ChecklistToggle, ItemID: "i0", Done: ptr(true)}, "tick-0")
	if err != nil || !replayed || readChecklist(after.Metadata)[0].Done {
		t.Fatalf("replay: replayed=%v %v %+v", replayed, err, readChecklist(after.Metadata)[0])
	}

	// A whole-metadata update from a client keeps the stored checklist.
	if _, err := f.svc.UpdateTask(f.ctx, f.tenant, f.project.ID, task.ID, UpdateTaskInput{Metadata: map[string]any{MetaChecklist: []any{}, "colour": "red"}}); err != nil {
		t.Fatal(err)
	}
	got = f.client.Task.GetX(context.Background(), task.ID)
	if len(readChecklist(got.Metadata)) != n || got.Metadata["colour"] != "red" {
		t.Errorf("metadata after a client update = %v", got.Metadata)
	}
	if _, _, err := f.svc.ApplyChecklist(f.ctx, f.tenant, f.project.ID, task.ID, ChecklistOp{Op: ChecklistRename, ItemID: "gone", Text: ptr("x")}, ""); err != ErrChecklistItemNotFound {
		t.Errorf("rename a missing item: %v", err)
	}

	// The task feed names the actor (Olive has a projects_users row) in one batched read.
	feed, err := activity.NewService(f.client).ListByTask(context.Background(), f.tenant, f.project.ID, task.ID)
	if err != nil || len(feed) == 0 {
		t.Fatalf("feed: %v", err)
	}
	for _, e := range feed {
		if e.ActorName != "Olive" {
			t.Errorf("actor_name = %q on %s, want Olive", e.ActorName, e.ActivityType)
		}
	}
}

// ?parent_id lists one task's subtasks only.
func TestListSubtasks(t *testing.T) {
	f := newFixture(t)
	parent, err := f.svc.CreateTask(f.ctx, f.tenant, f.project.ID, CreateTaskInput{Title: "Foundation"})
	if err != nil {
		t.Fatal(err)
	}
	for _, title := range []string{"Dig", "Pour"} {
		if _, err := f.svc.CreateTask(f.ctx, f.tenant, f.project.ID, CreateTaskInput{Title: title, ParentID: &parent.ID}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.svc.CreateTask(f.ctx, f.tenant, f.project.ID, CreateTaskInput{Title: "Roof"}); err != nil {
		t.Fatal(err)
	}
	subs, total, err := f.svc.ListTasks(context.Background(), f.tenant, f.project.ID, ListTasksFilter{Parent: &parent.ID, Limit: 50})
	if err != nil || total != 2 || len(subs) != 2 {
		t.Fatalf("subtasks = %d of %d, %v", len(subs), total, err)
	}
	for _, s := range subs {
		if s.ParentID != parent.ID {
			t.Errorf("%s is not a subtask of the parent", s.Title)
		}
	}
	none := uuid.New()
	if _, total, _ := f.svc.ListTasks(context.Background(), f.tenant, f.project.ID, ListTasksFilter{Parent: &none}); total != 0 {
		t.Errorf("unknown parent: %d tasks", total)
	}
}
