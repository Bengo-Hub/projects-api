package tasks

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/projects-service/internal/testutil"
)

func TestTrend(t *testing.T) {
	client := testutil.Client(t)
	ctx := context.Background()
	tenant := uuid.New()
	now := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	p := client.Project.Create().SetTenantID(tenant).SetName("P").SetOwnerID(uuid.New()).SaveX(ctx)
	other := client.Project.Create().SetTenantID(tenant).SetName("Q").SetOwnerID(uuid.New()).SaveX(ctx)
	day := func(s string) time.Time { v, _ := time.Parse(time.DateOnly, s); return v }
	task := func(projectID uuid.UUID, created, due, completed string, status string) {
		c := client.Task.Create().SetTenantID(tenant).SetProjectID(projectID).SetTitle("t").SetStatus(status).SetCreatedAt(day(created))
		if due != "" {
			c = c.SetDueDate(day(due))
		}
		if completed != "" {
			c = c.SetCompletedAt(day(completed))
		}
		c.SaveX(ctx)
	}
	// Window: Jul, Aug, Sep 2026 (Sep as of the 15th).
	task(p.ID, "2026-05-01", "2026-06-10", "", "todo")           // overdue since June: all three months
	task(p.ID, "2026-07-05", "2026-07-20", "2026-08-10", "done") // overdue end of July, done in August
	task(p.ID, "2026-08-01", "2026-08-25", "2026-08-20", "done") // done before due: never overdue
	task(p.ID, "2026-09-01", "2026-09-10", "", "in_progress")    // overdue as of now (Sep 15)
	task(p.ID, "2026-09-02", "2026-09-28", "", "todo")           // due later this month: not overdue yet
	task(p.ID, "2026-04-01", "2026-04-10", "", "done")           // done, undated: never overdue
	task(other.ID, "2026-08-03", "2026-08-05", "", "todo")       // other project

	svc := NewService(client, nil, zap.NewNop())
	rows, err := svc.Trend(ctx, tenant, &p.ID, 3, now)
	if err != nil {
		t.Fatal(err)
	}
	type want struct {
		month                       string
		created, completed, overdue int
	}
	exp := []want{{"2026-07", 1, 0, 2}, {"2026-08", 1, 2, 1}, {"2026-09", 2, 0, 2}}
	if len(rows) != len(exp) {
		t.Fatalf("rows = %+v", rows)
	}
	for i, e := range exp {
		r := rows[i]
		if r.Month != e.month || r.Created != e.created || r.Completed != e.completed || r.Overdue != e.overdue {
			t.Errorf("%s: got created %d completed %d overdue %d, want %d %d %d", r.Month, r.Created, r.Completed, r.Overdue, e.created, e.completed, e.overdue)
		}
	}
	all, err := svc.Trend(ctx, tenant, nil, 3, now)
	if err != nil {
		t.Fatal(err)
	}
	if all[1].Overdue != 2 || all[1].Created != 2 { // August adds the other project's overdue task
		t.Errorf("tenant-wide August = %+v", all[1])
	}
}
