package timetracking

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/projects-service/internal/platform/erp"
)

func mustZone(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatalf("zone %s: %v", name, err)
	}
	return loc
}

func TestWeekOfStartsOnMonday(t *testing.T) {
	for in, want := range map[string]string{
		"2026-11-16": "2026-11-16", // Monday
		"2026-11-19": "2026-11-16",
		"2026-11-22": "2026-11-16", // Sunday belongs to the week before it
		"2026-11-23": "2026-11-23",
		"2027-01-01": "2026-12-28", // across a year end
	} {
		d, _ := ParseDate(in)
		start, end := WeekOf(d)
		if start.Format(dateLayout) != want || end.Sub(start) != 6*24*time.Hour {
			t.Errorf("WeekOf(%s) = %s..%s, want %s..+6d", in, start.Format(dateLayout), end.Format(dateLayout), want)
		}
	}
}

// A timer split happens at midnight in the user's zone, not at UTC midnight.
func TestSplitByDayAtLocalMidnight(t *testing.T) {
	nairobi := mustZone(t, "Africa/Nairobi") // UTC+3, no DST
	// 22:30 to 01:15 Nairobi time = 19:30 to 22:15 UTC: the same UTC day, two local days.
	start := time.Date(2026, 11, 17, 19, 30, 0, 0, time.UTC)
	stop := time.Date(2026, 11, 17, 22, 15, 0, 0, time.UTC)
	got := SplitByDay(start, stop, nairobi)
	want := []Segment{{"2026-11-17", 1.5}, {"2026-11-18", 1.25}}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("Nairobi split = %v, want %v", got, want)
	}
	// The same run in UTC stays on one day.
	if got := SplitByDay(start, stop, time.UTC); len(got) != 1 || got[0] != (Segment{"2026-11-17", 2.75}) {
		t.Fatalf("UTC split = %v", got)
	}
	// Inside one day.
	if got := SplitByDay(start, start.Add(45*time.Minute), nairobi); len(got) != 1 || got[0].Hours != 0.75 {
		t.Fatalf("one day = %v", got)
	}
	// Across a DST change: London's night of 2026-10-25 has 25 hours; 23:00 to 02:00 local is 4h.
	london := mustZone(t, "Europe/London")
	from := time.Date(2026, 10, 24, 23, 0, 0, 0, london)
	to := time.Date(2026, 10, 25, 2, 0, 0, 0, london)
	got = SplitByDay(from, to, london)
	if len(got) != 2 || got[0] != (Segment{"2026-10-24", 1}) || got[1] != (Segment{"2026-10-25", 3}) {
		t.Fatalf("DST split = %v", got)
	}
	if SplitByDay(stop, start, nairobi) != nil {
		t.Fatal("a reversed run has no segments")
	}
}

func TestResolveBillable(t *testing.T) {
	yes, no := true, false
	cases := []struct {
		bills   bool
		req     *bool
		want    bool
		refused bool
	}{
		{true, nil, true, false},   // time and materials defaults to billable
		{false, nil, false, false}, // fixed, non billable or unset default to not
		{true, &no, false, false},  // a non-billable hour on a T&M project is allowed
		{false, &no, false, false},
		{false, &yes, false, true}, // billable time on a project that does not bill hours
		{true, &yes, true, false},
	}
	for _, c := range cases {
		got, err := ResolveBillable(c.bills, c.req)
		var verr ValidationError
		if c.refused != errors.As(err, &verr) || got != c.want {
			t.Errorf("ResolveBillable(%v, %v) = %v, %v", c.bills, c.req, got, err)
		}
	}
}

// fakeERP records calls and answers with what it was given.
type fakeERP struct {
	err    error
	week   *erp.UserTimesheets
	logged []erp.LogEntry
	failAt int // LogUserEntry call (1-based) that fails with err; 0 never
}

func (f *fakeERP) UserTimesheets(_ context.Context, _, _ uuid.UUID, _, _ time.Time) (*erp.UserTimesheets, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.week, nil
}

func (f *fakeERP) LogUserEntry(_ context.Context, _ uuid.UUID, in erp.LogEntry) (*erp.Logged, error) {
	if f.err != nil && (f.failAt == 0 || f.failAt == len(f.logged)+1) {
		return nil, f.err
	}
	f.logged = append(f.logged, in)
	return &erp.Logged{Entry: erp.Entry{Date: in.Date, RegularHours: in.Hours}}, nil
}

func (f *fakeERP) SubmitUserTimesheets(_ context.Context, _, _ uuid.UUID, _, _ time.Time) ([]erp.Timesheet, error) {
	return nil, f.err
}

// A user without an ERP employee record gets ErrNoEmployee from every call, never an empty week.
func TestNoEmployeeRecord(t *testing.T) {
	svc := NewService(nil, &fakeERP{err: erp.ErrNoEmployee}, zap.NewNop())
	d, _ := ParseDate("2026-11-18")
	if _, err := svc.Week(context.Background(), uuid.New(), uuid.New(), d); !errors.Is(err, ErrNoEmployee) {
		t.Fatalf("week: %v, want ErrNoEmployee", err)
	}
	if _, err := svc.SubmitWeek(context.Background(), uuid.New(), uuid.New(), d); !errors.Is(err, ErrNoEmployee) {
		t.Fatalf("submit: %v, want ErrNoEmployee", err)
	}
}

// The week groups ERP entries by project, task and day; status follows the timesheets.
func TestWeekGroupsEntries(t *testing.T) {
	p, task := uuid.New(), uuid.New()
	day := func(s string) time.Time { d, _ := ParseDate(s); return d }
	reason := "missing Friday"
	f := &fakeERP{week: &erp.UserTimesheets{
		Timesheets: []erp.Timesheet{{ID: uuid.New(), PeriodStart: day("2026-11-16"), PeriodEnd: day("2026-11-22"), Status: "rejected", RejectionReason: &reason}},
		Entries: []erp.Entry{
			{ProjectID: &p, TaskID: &task, Date: day("2026-11-16"), RegularHours: 2, Billable: true},
			{ProjectID: &p, TaskID: &task, Date: day("2026-11-17"), RegularHours: 1.5, OvertimeHours: 0.5, Billable: true, TaskDescription: "review"},
			{ProjectID: &p, Date: day("2026-11-17"), RegularHours: 3},
			{Date: day("2026-11-18"), RegularHours: 1},
		},
	}}
	// Names come from the database; the grouping is under test here, so the namer is a stub.
	svc := NewService(nil, f, zap.NewNop())
	w, err := svc.weekFrom(context.Background(), f.week, mustDate("2026-11-16"),
		func(context.Context, []uuid.UUID, []uuid.UUID) (map[uuid.UUID]string, map[uuid.UUID]string, error) {
			return map[uuid.UUID]string{p: "Website"}, map[uuid.UUID]string{task: "Design"}, nil
		})
	if err != nil {
		t.Fatal(err)
	}
	if w.Status != "rejected" || !w.Editable || w.Timesheets[0].RejectionReason != reason {
		t.Fatalf("status %s editable %v sheets %+v", w.Status, w.Editable, w.Timesheets)
	}
	if len(w.Rows) != 3 || w.Total != 8 || w.BillableTotal != 4 || w.DayTotals["2026-11-17"] != 5 || len(w.Days) != 7 {
		t.Fatalf("rows %d total %v billable %v day totals %v", len(w.Rows), w.Total, w.BillableTotal, w.DayTotals)
	}
	var designed bool
	for _, r := range w.Rows {
		if r.TaskTitle == "Design" {
			designed = r.ProjectName == "Website" && r.Billable && r.Total == 4 && r.Hours["2026-11-17"] == 2 && r.Notes["2026-11-17"] == "review"
		}
	}
	if !designed {
		t.Fatalf("task row wrong: %+v", w.Rows)
	}
	// Once the week is submitted it is no longer editable.
	f.week.Timesheets[0].Status = "submitted"
	w, _ = svc.weekFrom(context.Background(), f.week, mustDate("2026-11-16"),
		func(context.Context, []uuid.UUID, []uuid.UUID) (map[uuid.UUID]string, map[uuid.UUID]string, error) {
			return nil, nil, nil
		})
	if w.Editable || w.Status != "submitted" {
		t.Fatalf("submitted week: editable %v status %s", w.Editable, w.Status)
	}
}

func mustDate(s string) time.Time { d, _ := ParseDate(s); return d }

// A stop that crosses midnight logs two adds, one per local date, each in its own week.
func TestStopTimerSplitsAcrossMidnight(t *testing.T) {
	f := &fakeERP{}
	nairobi := mustZone(t, "Africa/Nairobi")
	svc := NewService(nil, f, zap.NewNop())
	svc.now = func() time.Time { return time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC) }
	// Sunday 23:00 to Monday 00:30 Nairobi: the two dates fall in different weeks.
	in := StopInput{StartedAt: time.Date(2026, 11, 22, 20, 0, 0, 0, time.UTC), StoppedAt: time.Date(2026, 11, 22, 21, 30, 0, 0, time.UTC)}
	segs, err := svc.logRun(context.Background(), uuid.New(), uuid.New(), target{projectID: uuid.New(), billsHrs: true}, in, true, "", nairobi)
	if err != nil {
		t.Fatal(err)
	}
	if len(segs.Segments) != 2 || len(f.logged) != 2 {
		t.Fatalf("segments %v logged %d", segs.Segments, len(f.logged))
	}
	a, b := f.logged[0], f.logged[1]
	if a.Date.Format(dateLayout) != "2026-11-22" || a.Hours != 1 || !a.Add || a.PeriodStart.Format(dateLayout) != "2026-11-16" || !a.Billable {
		t.Errorf("first %+v", a)
	}
	if b.Date.Format(dateLayout) != "2026-11-23" || b.Hours != 0.5 || !b.Add || b.PeriodStart.Format(dateLayout) != "2026-11-23" {
		t.Errorf("second %+v", b)
	}

	// The second date failing reports the first as logged, so a retry does not add it twice.
	f2 := &fakeERP{err: errors.New("erp down"), failAt: 2}
	svc2 := NewService(nil, f2, zap.NewNop())
	_, err = svc2.logRun(context.Background(), uuid.New(), uuid.New(), target{projectID: uuid.New()}, in, false, "", nairobi)
	var partial *PartialStop
	if !errors.As(err, &partial) || len(partial.Logged) != 1 || partial.Logged[0].Date != "2026-11-22" {
		t.Fatalf("partial stop: %v", err)
	}
}

func TestStopTimerValidation(t *testing.T) {
	svc := NewService(nil, &fakeERP{}, zap.NewNop())
	now := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }
	for name, in := range map[string]StopInput{
		"reversed":  {StartedAt: now, StoppedAt: now.Add(-time.Hour)},
		"zero":      {},
		"too long":  {StartedAt: now.Add(-25 * time.Hour), StoppedAt: now},
		"in future": {StartedAt: now, StoppedAt: now.Add(time.Hour)},
	} {
		_, err := svc.StopTimer(context.Background(), uuid.New(), uuid.New(), in, time.UTC)
		var verr ValidationError
		if !errors.As(err, &verr) {
			t.Errorf("%s: %v, want a validation error", name, err)
		}
	}
}
