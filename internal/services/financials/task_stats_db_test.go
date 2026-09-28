package financials

import (
	"context"
	"database/sql"
	"math"
	"os"
	"strings"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"go.uber.org/zap"

	"github.com/bengobox/projects-service/internal/ent"
)

// TestTaskStats_MatchesGoWeights checks the SQL task stats against TaskWeights over the same
// tasks, on a throwaway schema of the local Postgres (PROJECTS_TEST_DATABASE_URL, or the local
// default; skipped when unreachable).
func TestTaskStats_MatchesGoWeights(t *testing.T) {
	url := os.Getenv("PROJECTS_TEST_DATABASE_URL")
	if url == "" {
		url = "postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable"
	}
	admin, err := sql.Open("pgx", url)
	if err != nil || admin.Ping() != nil {
		t.Skip("local postgres unavailable")
	}
	schema := "proj_it_" + strings.ReplaceAll(uuid.NewString()[:8], "-", "")
	if _, err := admin.Exec("CREATE SCHEMA " + schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = admin.Exec("DROP SCHEMA " + schema + " CASCADE"); _ = admin.Close() })
	sep := "?"
	if strings.Contains(url, "?") {
		sep = "&"
	}
	db, err := sql.Open("pgx", url+sep+"search_path="+schema)
	if err != nil {
		t.Fatal(err)
	}
	client := ent.NewClient(ent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	t.Cleanup(func() { _ = client.Close() })
	ctx := context.Background()
	if err := client.Schema.Create(ctx); err != nil {
		t.Fatalf("schema: %v", err)
	}

	tenant := uuid.New()
	asOf := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	day := func(s string) *time.Time { v, _ := time.Parse(time.DateOnly, s); return &v }
	est := func(v float64) *float64 { return &v }
	mkProject := func(start, end *time.Time) *ent.Project {
		c := client.Project.Create().SetTenantID(tenant).SetName("P").SetOwnerID(uuid.New())
		if start != nil {
			c = c.SetStartDate(*start)
		}
		if end != nil {
			c = c.SetEndDate(*end)
		}
		p, err := c.Save(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	type spec struct {
		status     string
		progress   int
		start, due *time.Time
		estimate   *float64
	}
	mkTasks := func(p *ent.Project, specs []spec) []TaskWork {
		var work []TaskWork
		for _, sp := range specs {
			c := client.Task.Create().SetTenantID(tenant).SetProjectID(p.ID).SetTitle("t").SetStatus(sp.status).SetProgressPct(sp.progress)
			if sp.start != nil {
				c = c.SetStartDate(*sp.start)
			}
			if sp.due != nil {
				c = c.SetDueDate(*sp.due)
			}
			if sp.estimate != nil {
				c = c.SetEstimatedHours(*sp.estimate)
			}
			if _, err := c.Save(ctx); err != nil {
				t.Fatal(err)
			}
			work = append(work, TaskWork{Start: sp.start, Due: sp.due, EstimatedHours: sp.estimate, ProgressPct: sp.progress, Done: sp.status == "done"})
		}
		return work
	}
	window := func(p *ent.Project) (*time.Time, *time.Time) {
		var s, e *time.Time
		if !p.StartDate.IsZero() {
			v := p.StartDate
			s = &v
		}
		if !p.EndDate.IsZero() {
			v := p.EndDate
			e = &v
		}
		return s, e
	}

	withEst := mkProject(day("2026-01-01"), day("2026-12-31"))
	withEstWork := mkTasks(withEst, []spec{
		{"done", 0, day("2026-01-10"), day("2026-02-10"), est(10)},
		{"in_progress", 40, day("2026-05-01"), day("2026-07-01"), est(20)},
		{"todo", 0, nil, day("2026-06-01"), est(5)},                       // overdue, falls back to project start
		{"todo", 150, nil, nil, est(8)},                                   // progress clamped, project window
		{"todo", 10, day("2026-07-01"), nil, nil},                         // no estimate: ignored when others have one
		{"in_progress", 30, day("2026-03-01"), day("2026-03-01"), est(4)}, // zero-length window, past due
	})
	noEst := mkProject(nil, nil)
	noEstWork := mkTasks(noEst, []spec{
		{"todo", 50, nil, nil, nil}, // no schedule: planned = earned
		{"in_progress", 20, day("2026-06-01"), day("2026-06-30"), nil},
		{"done", 100, nil, day("2026-06-20"), nil},
	})
	empty := mkProject(day("2026-01-01"), nil)
	other := client.Project.Create().SetTenantID(uuid.New()).SetName("X").SetOwnerID(uuid.New()).SaveX(ctx)
	client.Task.Create().SetTenantID(other.TenantID).SetProjectID(other.ID).SetTitle("t").SetStatus("todo").SaveX(ctx)

	svc := NewService(client, nil, zap.NewNop())
	stats, err := svc.loadTaskStats(ctx, tenant, []uuid.UUID{withEst.ID, noEst.ID, empty.ID, other.ID}, asOf)
	if err != nil {
		t.Fatal(err)
	}
	close := func(a, b float64) bool { return math.Abs(a-b) < 1e-6 }
	for _, c := range []struct {
		p    *ent.Project
		work []TaskWork
	}{{withEst, withEstWork}, {noEst, noEstWork}} {
		s, e := window(c.p)
		want := TaskWeights(c.work, s, e, asOf)
		got := stats[c.p.ID].weights()
		if !close(got.Total, want.Total) || !close(got.Earned, want.Earned) || !close(got.Planned, want.Planned) {
			t.Errorf("project %s weights sql=%+v go=%+v", c.p.ID, got, want)
		}
	}
	w := stats[withEst.ID]
	if w.Total != 6 || w.Done != 1 || w.Overdue != 2 || !close(w.EstSum, 47) {
		t.Errorf("counts total/done/overdue/est = %d/%d/%d/%v, want 6/1/2/47", w.Total, w.Done, w.Overdue, w.EstSum)
	}
	if _, ok := stats[empty.ID]; ok {
		t.Error("a project without tasks has no row")
	}
	if _, ok := stats[other.ID]; ok {
		t.Error("another tenant's project must not be read")
	}
}
