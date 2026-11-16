package tasks

import (
	"context"
	"fmt"
	"time"

	entsql "entgo.io/ent/dialect/sql"
	"github.com/google/uuid"

	"github.com/bengobox/projects-service/internal/ent"
	"github.com/bengobox/projects-service/internal/ent/predicate"
	entproject "github.com/bengobox/projects-service/internal/ent/project"
	enttask "github.com/bengobox/projects-service/internal/ent/task"
	"github.com/bengobox/projects-service/internal/services/membership"
)

// My Work buckets, by due date against the caller's today (in the time zone they send):
// overdue before today, today, week the next six days, later after that or without a due date.
// Done tasks are left out.
const (
	BucketOverdue = "overdue"
	BucketToday   = "today"
	BucketWeek    = "week"
	BucketLater   = "later"
)

// Buckets lists the My Work buckets in display order.
var Buckets = []string{BucketOverdue, BucketToday, BucketWeek, BucketLater}

// MyWorkFilter selects one page of the caller's open tasks. Bucket is optional (all open tasks
// when empty). VisibleTo, when set, also limits the tasks to projects that user may see (a user
// without projects.projects.manage who has since left a project no longer sees its tasks).
type MyWorkFilter struct {
	Bucket    string
	DueFrom   *time.Time
	DueTo     *time.Time
	VisibleTo *uuid.UUID
	Now       time.Time
	Location  *time.Location
	Limit     int
	Offset    int
}

// dayBounds returns the start of the caller's today, tomorrow and a week from today, as the UTC
// midnights task due dates are stored at.
func dayBounds(now time.Time, loc *time.Location) (today, tomorrow, weekEnd time.Time) {
	if loc == nil {
		loc = time.UTC
	}
	y, m, d := now.In(loc).Date()
	today = time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	return today, today.AddDate(0, 0, 1), today.AddDate(0, 0, 7)
}

// minDueDate separates real due dates from the zero timestamps (0001-01-01) older clients saved
// for an empty date field; those count as undated.
var minDueDate = time.Date(1900, 1, 1, 0, 0, 0, 0, time.UTC)

// bucketWhere is the due date condition of a bucket.
func bucketWhere(bucket string, today, tomorrow, weekEnd time.Time) (predicate.Task, error) {
	switch bucket {
	case BucketOverdue:
		return enttask.And(enttask.DueDateGTE(minDueDate), enttask.DueDateLT(today)), nil
	case BucketToday:
		return enttask.And(enttask.DueDateGTE(today), enttask.DueDateLT(tomorrow)), nil
	case BucketWeek:
		return enttask.And(enttask.DueDateGTE(tomorrow), enttask.DueDateLT(weekEnd)), nil
	case BucketLater:
		return enttask.Or(enttask.DueDateGTE(weekEnd), enttask.DueDateIsNil(), enttask.DueDateLT(minDueDate)), nil
	}
	return nil, ErrValidation("bucket must be overdue, today, week or later")
}

// myWorkBase is every open task of the tenant assigned to user, within the projects visibleTo may
// see.
func myWorkBase(s *Service, tenantID, userID uuid.UUID, visibleTo *uuid.UUID) *ent.TaskQuery {
	q := s.client.Task.Query().Where(enttask.TenantID(tenantID), assignedTo(tenantID, userID), enttask.StatusNEQ(StatusDone))
	if visibleTo != nil {
		q = q.Where(enttask.HasProjectWith(entproject.TenantID(tenantID), membership.Visible(*visibleTo)))
	}
	return q
}

// MyWork returns one page of the user's open tasks across projects, soonest due first (undated
// last), each with its assignees and its project's id and name.
func (s *Service) MyWork(ctx context.Context, tenantID, userID uuid.UUID, f MyWorkFilter) ([]*ent.Task, int, error) {
	if f.Limit <= 0 {
		f.Limit = 20
	}
	if f.Now.IsZero() {
		f.Now = time.Now()
	}
	q := myWorkBase(s, tenantID, userID, f.VisibleTo).Where(dueRange(f.DueFrom, f.DueTo)...)
	if f.Bucket != "" {
		today, tomorrow, weekEnd := dayBounds(f.Now, f.Location)
		p, err := bucketWhere(f.Bucket, today, tomorrow, weekEnd)
		if err != nil {
			return nil, 0, err
		}
		q = q.Where(p)
	}
	total, err := q.Clone().Count(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("count my work: %w", err)
	}
	items, err := q.
		Order(enttask.ByDueDate(entsql.OrderNullsLast()), enttask.ByID()).
		WithAssignees(assigneeOrder).
		WithProject(func(pq *ent.ProjectQuery) { pq.Select(entproject.FieldID, entproject.FieldName) }).
		Offset(f.Offset).Limit(f.Limit).All(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("list my work: %w", err)
	}
	return items, total, nil
}

// MyWorkCounts counts the user's open tasks per bucket in one query (COUNT ... FILTER per
// bucket over the same assigned-to-me set).
func (s *Service) MyWorkCounts(ctx context.Context, tenantID, userID uuid.UUID, visibleTo *uuid.UUID, now time.Time, loc *time.Location) (map[string]int, error) {
	if now.IsZero() {
		now = time.Now()
	}
	today, tomorrow, weekEnd := dayBounds(now, loc)
	// The bounds are timestamps this function made, never caller text, so they go in as literals.
	lit := func(t time.Time) string { return "'" + t.Format(time.RFC3339) + "'::timestamptz" }
	count := func(alias, cond string) ent.AggregateFunc {
		return func(sel *entsql.Selector) string {
			return entsql.As(fmt.Sprintf("COUNT(*) FILTER (WHERE "+cond+")", sel.C(enttask.FieldDueDate)), alias)
		}
	}
	var rows []struct {
		Overdue int `json:"overdue"`
		Today   int `json:"today"`
		Week    int `json:"week"`
		Later   int `json:"later"`
	}
	err := myWorkBase(s, tenantID, userID, visibleTo).Aggregate(
		count(BucketOverdue, "%[1]s >= "+lit(minDueDate)+" AND %[1]s < "+lit(today)),
		count(BucketToday, "%[1]s >= "+lit(today)+" AND %[1]s < "+lit(tomorrow)),
		count(BucketWeek, "%[1]s >= "+lit(tomorrow)+" AND %[1]s < "+lit(weekEnd)),
		count(BucketLater, "%[1]s >= "+lit(weekEnd)+" OR %[1]s IS NULL OR %[1]s < "+lit(minDueDate)),
	).Scan(ctx, &rows)
	if err != nil {
		return nil, fmt.Errorf("count my work buckets: %w", err)
	}
	out := map[string]int{BucketOverdue: 0, BucketToday: 0, BucketWeek: 0, BucketLater: 0}
	if len(rows) == 1 {
		r := rows[0]
		out[BucketOverdue], out[BucketToday], out[BucketWeek], out[BucketLater] = r.Overdue, r.Today, r.Week, r.Later
	}
	return out, nil
}
