package timetracking

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/projects-service/internal/testutil"
)

// Against Postgres: timers live in task metadata, one per user, starting another stops the first
// (logging its time), and billable time is refused on a project that does not bill hours.
func TestTimersAndEntriesDB(t *testing.T) {
	client := testutil.Client(t)
	ctx := context.Background()
	tenant, owner, ana, ben := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	tm := client.Project.Create().SetTenantID(tenant).SetName("Retainer").SetOwnerID(owner).
		SetMetadata(map[string]any{"billing_type": "time_and_materials", "hourly_rate": "2500"}).SaveX(ctx)
	fixed := client.Project.Create().SetTenantID(tenant).SetName("Fixed").SetOwnerID(owner).
		SetMetadata(map[string]any{"billing_type": "fixed", "notes": "kept"}).SaveX(ctx)
	design := client.Task.Create().SetTenantID(tenant).SetProjectID(tm.ID).SetTitle("Design").
		SetMetadata(map[string]any{"colour": "blue"}).SaveX(ctx)
	build := client.Task.Create().SetTenantID(tenant).SetProjectID(fixed.ID).SetTitle("Build").SaveX(ctx)

	f := &fakeERP{}
	svc := NewService(client, f, zap.NewNop())
	now := time.Date(2026, 11, 18, 9, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }

	// Ana and Ben time the same task; each sees only their own timer.
	if _, err := svc.StartTimer(ctx, tenant, ana, design.ID, time.UTC); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.StartTimer(ctx, tenant, ben, design.ID, time.UTC); err != nil {
		t.Fatal(err)
	}
	if tmr, err := svc.TaskTimer(ctx, tenant, ana, design.ID); err != nil || tmr == nil || !tmr.StartedAt.Equal(now) || tmr.TaskTitle != "Design" {
		t.Fatalf("ana's timer: %+v %v", tmr, err)
	}
	if got := client.Task.GetX(ctx, design.ID).Metadata; got["colour"] != "blue" {
		t.Fatalf("a timer start must keep the task's other metadata: %v", got)
	}

	// Ana starts on Build two hours later: the Design timer stops and logs 2 billable hours.
	now = now.Add(2 * time.Hour)
	started, err := svc.StartTimer(ctx, tenant, ana, build.ID, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if len(started.Stopped) != 1 || len(f.logged) != 1 || f.logged[0].Hours != 2 || !f.logged[0].Billable || !f.logged[0].Add ||
		f.logged[0].ProjectID != tm.ID || *f.logged[0].TaskID != design.ID || f.logged[0].AuthUserID != ana {
		t.Fatalf("auto stop: %+v logged %+v", started.Stopped, f.logged)
	}
	if running, _ := svc.RunningTimer(ctx, tenant, ana); running == nil || running.TaskID != build.ID {
		t.Fatalf("ana runs %+v, want Build", running)
	}
	if tmr, _ := svc.TaskTimer(ctx, tenant, ben, design.ID); tmr == nil {
		t.Fatal("ben's timer on Design must survive ana's stop")
	}
	// Starting the running timer again keeps its start.
	now = now.Add(time.Minute)
	again, err := svc.StartTimer(ctx, tenant, ana, build.ID, time.UTC)
	if err != nil || !again.Timer.StartedAt.Equal(now.Add(-time.Minute)) || len(again.Stopped) != 0 {
		t.Fatalf("restart: %+v %v", again, err)
	}

	// A stop clears the timer; a discard clears without logging.
	if _, err := svc.StopTimer(ctx, tenant, ana, StopInput{ProjectID: fixed.ID, TaskID: &build.ID, StartedAt: now.Add(-time.Hour), StoppedAt: now}, time.UTC); err != nil {
		t.Fatal(err)
	}
	if f.logged[1].Billable {
		t.Fatal("time on a fixed price project defaults to not billable")
	}
	if running, _ := svc.RunningTimer(ctx, tenant, ana); running != nil {
		t.Fatalf("after stop ana still runs %+v", running)
	}
	if err := svc.ClearTimer(ctx, tenant, ben, design.ID); err != nil {
		t.Fatal(err)
	}
	if running, _ := svc.RunningTimer(ctx, tenant, ben); running != nil || len(f.logged) != 2 {
		t.Fatalf("discard: running %+v, logged %d", running, len(f.logged))
	}

	// Billable validation and task ownership on manual entries.
	yes := true
	_, err = svc.SetEntry(ctx, tenant, ana, EntryInput{ProjectID: fixed.ID, Date: "2026-11-18", Hours: 2, Billable: &yes})
	var verr ValidationError
	if !errors.As(err, &verr) {
		t.Fatalf("billable on a fixed project: %v, want a validation error", err)
	}
	if _, err := svc.SetEntry(ctx, tenant, ana, EntryInput{ProjectID: tm.ID, TaskID: &build.ID, Date: "2026-11-18", Hours: 2}); !errors.As(err, &verr) {
		t.Fatalf("a task of another project: %v", err)
	}
	if _, err := svc.SetEntry(ctx, tenant, ana, EntryInput{ProjectID: uuid.New(), Date: "2026-11-18", Hours: 2}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown project: %v", err)
	}
	if _, err := svc.SetEntry(ctx, tenant, ana, EntryInput{ProjectID: tm.ID, Date: "2026-11-18", Hours: 25}); !errors.As(err, &verr) {
		t.Fatalf("25 hours: %v", err)
	}
	if _, err := svc.SetEntry(ctx, tenant, ana, EntryInput{ProjectID: tm.ID, TaskID: &design.ID, Date: "2026-11-22", Hours: 3.333, Note: " review "}); err != nil {
		t.Fatal(err)
	}
	last := f.logged[len(f.logged)-1]
	if last.Add || !last.Billable || last.Hours != 3.33 || last.Note != "review" || last.PeriodStart.Format(dateLayout) != "2026-11-16" {
		t.Fatalf("manual entry sent %+v", last)
	}
}
