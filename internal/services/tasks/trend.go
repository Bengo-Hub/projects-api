package tasks

import (
	"context"
	"fmt"
	"time"

	entsql "entgo.io/ent/dialect/sql"
	"github.com/google/uuid"

	"github.com/bengobox/projects-service/internal/ent"
	enttask "github.com/bengobox/projects-service/internal/ent/task"
)

// TrendMonth is one month of task flow: tasks created, tasks completed (throughput) and tasks
// open past their due date at the month's end.
type TrendMonth struct {
	Month     string `json:"month"` // YYYY-MM
	Created   int    `json:"created"`
	Completed int    `json:"completed"`
	Overdue   int    `json:"overdue_at_month_end"`
}

// Trend returns the last months of task flow (1 to 24, current month included), optionally for
// one project, from three grouped queries whose row counts depend on the months asked, never on
// the number of tasks. A done task without a completion date counts as completed before the
// window (it is never overdue).
func (s *Service) Trend(ctx context.Context, tenantID uuid.UUID, projectID *uuid.UUID, months int, now time.Time) ([]TrendMonth, error) {
	if months < 1 || months > 24 {
		months = 6
	}
	now = now.UTC()
	first := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, -(months - 1), 0)
	until := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, 1, 0)
	out := make([]TrendMonth, months)
	index := map[string]int{}
	for i := range out {
		m := first.AddDate(0, i, 0).Format("2006-01")
		out[i].Month = m
		index[m] = i
	}
	scope := func(q *ent.TaskQuery) *ent.TaskQuery {
		q = q.Where(enttask.TenantID(tenantID))
		if projectID != nil {
			q = q.Where(enttask.ProjectID(*projectID))
		}
		return q
	}
	monthOf := func(col string) func(*entsql.Selector) string {
		return func(sel *entsql.Selector) string { return fmt.Sprintf("to_char(%s, 'YYYY-MM')", sel.C(col)) }
	}
	countBy := func(q *ent.TaskQuery, col string) (map[string]int, error) {
		var rows []struct {
			Month string `json:"month"`
			Count int    `json:"count"`
		}
		err := q.Where(func(sel *entsql.Selector) { sel.GroupBy(monthOf(col)(sel)) }).
			Aggregate(
				func(sel *entsql.Selector) string { return entsql.As(monthOf(col)(sel), "month") },
				func(*entsql.Selector) string { return entsql.As("COUNT(*)", "count") },
			).Scan(ctx, &rows)
		m := map[string]int{}
		for _, r := range rows {
			m[r.Month] = r.Count
		}
		return m, err
	}

	created, err := countBy(scope(s.client.Task.Query()).Where(enttask.CreatedAtGTE(first), enttask.CreatedAtLT(until)), enttask.FieldCreatedAt)
	if err != nil {
		return nil, fmt.Errorf("task trend created: %w", err)
	}
	completed, err := countBy(scope(s.client.Task.Query()).Where(enttask.CompletedAtGTE(first), enttask.CompletedAtLT(until)), enttask.FieldCompletedAt)
	if err != nil {
		return nil, fmt.Errorf("task trend completed: %w", err)
	}
	for m, i := range index {
		out[i].Created, out[i].Completed = created[m], completed[m]
	}

	// Overdue at a month end: due before it and not completed by it. Tasks due before the window
	// share the "earlier" due bucket; completion is bucketed by month, "earlier" when before the
	// window (or undated but done), "open" when not done.
	// The literals are server-computed times, never user input. Tasks due after now (later this
	// month) are not overdue yet, so the current month counts as of now.
	fromLit := "'" + first.Format(time.RFC3339) + "'::timestamptz"
	nowLit := "'" + now.Format(time.RFC3339) + "'::timestamptz"
	dueBucket := func(sel *entsql.Selector) string {
		return fmt.Sprintf("CASE WHEN %[1]s >= %[3]s THEN 'future' WHEN %[1]s < %[2]s THEN 'earlier' ELSE to_char(%[1]s, 'YYYY-MM') END",
			sel.C(enttask.FieldDueDate), fromLit, nowLit)
	}
	doneBucket := func(sel *entsql.Selector) string {
		return fmt.Sprintf(`CASE WHEN %[1]s <> 'done' THEN 'open'
			WHEN %[2]s IS NULL OR %[2]s < %[3]s THEN 'earlier'
			ELSE to_char(%[2]s, 'YYYY-MM') END`, sel.C(enttask.FieldStatus), sel.C(enttask.FieldCompletedAt), fromLit)
	}
	var grid []struct {
		Due   string `json:"due"`
		Done  string `json:"done"`
		Count int    `json:"count"`
	}
	err = scope(s.client.Task.Query()).
		Where(enttask.DueDateNotNil(), enttask.DueDateLT(until)).
		Where(func(sel *entsql.Selector) { sel.GroupBy(dueBucket(sel), doneBucket(sel)) }).
		Aggregate(
			func(sel *entsql.Selector) string { return entsql.As(dueBucket(sel), "due") },
			func(sel *entsql.Selector) string { return entsql.As(doneBucket(sel), "done") },
			func(*entsql.Selector) string { return entsql.As("COUNT(*)", "count") },
		).Scan(ctx, &grid)
	if err != nil {
		return nil, fmt.Errorf("task trend overdue: %w", err)
	}
	for i := range out {
		m := out[i].Month
		for _, g := range grid {
			// Due in or before month m (its end is past the due date), not yet completed by then.
			dueBefore := g.Due != "future" && (g.Due == "earlier" || g.Due <= m)
			stillOpen := g.Done == "open" || (g.Done != "earlier" && g.Done > m)
			if dueBefore && stillOpen {
				out[i].Overdue += g.Count
			}
		}
	}
	return out, nil
}
