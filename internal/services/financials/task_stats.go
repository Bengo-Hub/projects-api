package financials

import (
	"context"
	"fmt"
	"time"

	entsql "entgo.io/ent/dialect/sql"
	"github.com/google/uuid"

	entproject "github.com/bengobox/projects-service/internal/ent/project"
	enttask "github.com/bengobox/projects-service/internal/ent/task"
)

// taskStats is everything the financials need from a project's tasks, summed in SQL: one row
// per project however many tasks it has, so the portfolio's cost stays flat as projects grow.
// The rules are TaskWeights' (parity checked by TestTaskStats_MatchesGoWeights).
type taskStats struct {
	ProjectID uuid.UUID `json:"project_id"`
	Total     int       `json:"total"`
	Done      int       `json:"done"`
	Overdue   int       `json:"overdue"`
	EstSum    float64   `json:"est_sum"` // estimated hours, for utilisation
	// Weights with every task weighing the same, and weighted by estimate (tasks with one).
	AllTotal   float64 `json:"all_total"`
	AllEarned  float64 `json:"all_earned"`
	AllPlanned float64 `json:"all_planned"`
	EstTotal   float64 `json:"est_total"`
	EstEarned  float64 `json:"est_earned"`
	EstPlanned float64 `json:"est_planned"`
}

// weights picks estimate weighting when any task has an estimate, as TaskWeights does.
func (t taskStats) weights() Weights {
	if t.EstTotal > 0 {
		return Weights{Total: t.EstTotal, Earned: t.EstEarned, Planned: t.EstPlanned}
	}
	return Weights{Total: t.AllTotal, Earned: t.AllEarned, Planned: t.AllPlanned}
}

// loadTaskStats sums the tasks of the given projects as of asOf, in one grouped query.
func (s *Service) loadTaskStats(ctx context.Context, tenantID uuid.UUID, projectIDs []uuid.UUID, asOf time.Time) (map[uuid.UUID]taskStats, error) {
	out := make(map[uuid.UUID]taskStats, len(projectIDs))
	if len(projectIDs) == 0 {
		return out, nil
	}
	// asOf is the server clock, never user input, so it is inlined as a timestamp literal.
	ts := fmt.Sprintf("'%s'::timestamptz", asOf.UTC().Format(time.RFC3339Nano))
	p := entsql.Table(entproject.Table)
	exprs := func(s *entsql.Selector) (progress, planned, estW string) {
		status, pct := s.C(enttask.FieldStatus), s.C(enttask.FieldProgressPct)
		progress = fmt.Sprintf("LEAST(1, GREATEST(0, CASE WHEN %s = 'done' THEN 1 ELSE COALESCE(%s, 0) / 100.0 END))", status, pct)
		start := fmt.Sprintf("COALESCE(%s, %s)", s.C(enttask.FieldStartDate), p.C(entproject.FieldStartDate))
		end := fmt.Sprintf("COALESCE(%s, %s)", s.C(enttask.FieldDueDate), p.C(entproject.FieldEndDate))
		planned = fmt.Sprintf(`CASE
			WHEN %[2]s IS NOT NULL AND %[3]s >= %[2]s THEN 1
			WHEN %[1]s IS NOT NULL AND %[3]s <= %[1]s THEN 0
			WHEN %[1]s IS NULL OR %[2]s IS NULL OR %[2]s <= %[1]s THEN %[4]s
			ELSE LEAST(1, GREATEST(0, EXTRACT(EPOCH FROM (%[3]s - %[1]s)) / EXTRACT(EPOCH FROM (%[2]s - %[1]s))))
		END`, start, end, ts, progress)
		estW = fmt.Sprintf("CASE WHEN COALESCE(%[1]s, 0) > 0 THEN %[1]s ELSE 0 END", s.C(enttask.FieldEstimatedHours))
		return
	}
	sum := func(alias string, build func(s *entsql.Selector) string) func(*entsql.Selector) string {
		return func(s *entsql.Selector) string {
			return entsql.As(fmt.Sprintf("COALESCE(SUM(%s), 0)::float8", build(s)), alias)
		}
	}
	var rows []taskStats
	err := s.client.Task.Query().
		Where(enttask.TenantID(tenantID), enttask.ProjectIDIn(projectIDs...)).
		Where(func(s *entsql.Selector) {
			s.LeftJoin(p).On(s.C(enttask.FieldProjectID), p.C(entproject.FieldID))
		}).
		GroupBy(enttask.FieldProjectID).
		Aggregate(
			func(s *entsql.Selector) string { return entsql.As("COUNT(*)", "total") },
			sum("done", func(s *entsql.Selector) string {
				return fmt.Sprintf("CASE WHEN %s = 'done' THEN 1 ELSE 0 END", s.C(enttask.FieldStatus))
			}),
			sum("overdue", func(s *entsql.Selector) string {
				return fmt.Sprintf("CASE WHEN %s <> 'done' AND %s < %s THEN 1 ELSE 0 END", s.C(enttask.FieldStatus), s.C(enttask.FieldDueDate), ts)
			}),
			sum("est_sum", func(s *entsql.Selector) string { return fmt.Sprintf("COALESCE(%s, 0)", s.C(enttask.FieldEstimatedHours)) }),
			sum("all_total", func(*entsql.Selector) string { return "1" }),
			sum("all_earned", func(s *entsql.Selector) string { pr, _, _ := exprs(s); return pr }),
			sum("all_planned", func(s *entsql.Selector) string { _, pl, _ := exprs(s); return pl }),
			sum("est_total", func(s *entsql.Selector) string { _, _, w := exprs(s); return w }),
			sum("est_earned", func(s *entsql.Selector) string { pr, _, w := exprs(s); return w + " * " + pr }),
			sum("est_planned", func(s *entsql.Selector) string { _, pl, w := exprs(s); return w + " * (" + pl + ")" }),
		).
		Scan(ctx, &rows)
	if err != nil {
		return nil, fmt.Errorf("task stats: %w", err)
	}
	for _, r := range rows {
		out[r.ProjectID] = r
	}
	return out, nil
}
