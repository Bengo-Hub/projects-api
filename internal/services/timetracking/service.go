// Package timetracking logs the signed-in user's time on their ERP timesheet: the week grid,
// manual entries, the task timer and the weekly submit. ERP owns timesheets and hours; this
// package stores none. The only state kept here is a running timer's start, in the task's
// metadata (key "timers", user id to start time), so a timer survives a reload or another device.
package timetracking

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/projects-service/internal/ent"
	entproject "github.com/bengobox/projects-service/internal/ent/project"
	enttask "github.com/bengobox/projects-service/internal/ent/task"
	"github.com/bengobox/projects-service/internal/platform/erp"
	"github.com/bengobox/projects-service/internal/services/projects"
)

// ERP is the slice of the erp client this package uses.
type ERP interface {
	UserTimesheets(ctx context.Context, tenantID, authUserID uuid.UUID, from, to time.Time) (*erp.UserTimesheets, error)
	LogUserEntry(ctx context.Context, tenantID uuid.UUID, in erp.LogEntry) (*erp.Logged, error)
	SubmitUserTimesheets(ctx context.Context, tenantID, authUserID uuid.UUID, from, to time.Time) ([]erp.Timesheet, error)
}

var (
	// ErrNotFound: the project or task does not exist in the tenant.
	ErrNotFound = errors.New("not found")
	// ErrNoEmployee: the user has no ERP employee record, so there is no timesheet to log on.
	ErrNoEmployee = errors.New("you have no employee record in ERP yet, so your time cannot be logged. Ask HR to add you as an employee")
)

// ValidationError is a request the service refuses.
type ValidationError string

func (e ValidationError) Error() string { return string(e) }

// MaxEntryHours caps one day's entry for a project and task, and MaxTimerSpan one timer run.
const (
	MaxEntryHours = 24
	MaxTimerSpan  = 24 * time.Hour
	maxNoteLen    = 500
	dateLayout    = "2006-01-02"
)

// Service logs time through ERP.
type Service struct {
	client *ent.Client
	erp    ERP
	log    *zap.Logger
	now    func() time.Time
}

// NewService builds the service.
func NewService(client *ent.Client, e ERP, log *zap.Logger) *Service {
	return &Service{client: client, erp: e, log: log.Named("timetracking.svc"), now: time.Now}
}

// erpErr maps the erp client's no-employee answer to ErrNoEmployee; other errors pass through.
func erpErr(err error) error {
	if errors.Is(err, erp.ErrNoEmployee) {
		return ErrNoEmployee
	}
	return err
}

// WeekOf returns the Monday and Sunday (dates at UTC midnight) of the week holding d's date.
func WeekOf(d time.Time) (time.Time, time.Time) {
	day := time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, time.UTC)
	offset := (int(day.Weekday()) + 6) % 7 // Monday 0 .. Sunday 6
	start := day.AddDate(0, 0, -offset)
	return start, start.AddDate(0, 0, 6)
}

// ParseDate reads a YYYY-MM-DD date as UTC midnight.
func ParseDate(s string) (time.Time, error) {
	d, err := time.Parse(dateLayout, strings.TrimSpace(s))
	if err != nil {
		return time.Time{}, ValidationError("dates must be YYYY-MM-DD")
	}
	return d, nil
}

// round2 rounds hours to the two places ERP stores.
func round2(h float64) float64 { return math.Round(h*100) / 100 }

// ---- Week ----

// Sheet is one ERP timesheet the week touches. Editable is false once it is submitted or approved.
type Sheet struct {
	ID              uuid.UUID `json:"id"`
	PeriodStart     string    `json:"period_start"`
	PeriodEnd       string    `json:"period_end"`
	Status          string    `json:"status"`
	RejectionReason string    `json:"rejection_reason,omitempty"`
	Editable        bool      `json:"editable"`
}

// Row is the week's time for one project and task (task nil: the project as a whole; project nil:
// time logged in ERP without a project). Hours and Notes are keyed by date.
type Row struct {
	ProjectID   *uuid.UUID         `json:"project_id"`
	ProjectName string             `json:"project_name,omitempty"`
	TaskID      *uuid.UUID         `json:"task_id,omitempty"`
	TaskTitle   string             `json:"task_title,omitempty"`
	Billable    bool               `json:"billable"`
	Hours       map[string]float64 `json:"hours"`
	Notes       map[string]string  `json:"notes,omitempty"`
	Total       float64            `json:"total"`
}

// Week is a user's week, Monday to Sunday. Status is that of the week's timesheet: none (nothing
// logged yet), draft, submitted, approved, rejected, or mixed when ERP has several timesheets
// over the week with different statuses.
type Week struct {
	WeekStart     string             `json:"week_start"`
	WeekEnd       string             `json:"week_end"`
	Days          []string           `json:"days"`
	Status        string             `json:"status"`
	Editable      bool               `json:"editable"`
	Timesheets    []Sheet            `json:"timesheets"`
	Rows          []Row              `json:"rows"`
	DayTotals     map[string]float64 `json:"day_totals"`
	Total         float64            `json:"total"`
	BillableTotal float64            `json:"billable_total"`
}

// Week returns the user's ERP time for the week holding weekStart, grouped by project, task and day.
func (s *Service) Week(ctx context.Context, tenantID, userID uuid.UUID, weekStart time.Time) (*Week, error) {
	start, end := WeekOf(weekStart)
	got, err := s.erp.UserTimesheets(ctx, tenantID, userID, start, end)
	if err != nil {
		return nil, erpErr(err)
	}
	return s.weekFrom(ctx, got, start, func(ctx context.Context, projectIDs, taskIDs []uuid.UUID) (map[uuid.UUID]string, map[uuid.UUID]string, error) {
		names, err := s.projectNames(ctx, tenantID, projectIDs)
		if err != nil {
			return nil, nil, err
		}
		titles, err := s.taskTitles(ctx, tenantID, taskIDs)
		return names, titles, err
	})
}

// namer looks up project names and task titles by id.
type namer func(ctx context.Context, projectIDs, taskIDs []uuid.UUID) (map[uuid.UUID]string, map[uuid.UUID]string, error)

// weekFrom builds the week view from ERP's timesheets and entries.
func (s *Service) weekFrom(ctx context.Context, got *erp.UserTimesheets, start time.Time, names namer) (*Week, error) {
	start, end := WeekOf(start)
	w := &Week{
		WeekStart: start.Format(dateLayout), WeekEnd: end.Format(dateLayout),
		Status: "none", Editable: true, Timesheets: []Sheet{}, Rows: []Row{}, DayTotals: map[string]float64{},
	}
	for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
		w.Days = append(w.Days, d.Format(dateLayout))
		w.DayTotals[d.Format(dateLayout)] = 0
	}
	for i, ts := range got.Timesheets {
		sh := Sheet{
			ID: ts.ID, PeriodStart: ts.PeriodStart.UTC().Format(dateLayout), PeriodEnd: ts.PeriodEnd.UTC().Format(dateLayout),
			Status: ts.Status, Editable: ts.Status == "draft" || ts.Status == "rejected",
		}
		if ts.RejectionReason != nil {
			sh.RejectionReason = *ts.RejectionReason
		}
		w.Timesheets = append(w.Timesheets, sh)
		w.Editable = w.Editable && sh.Editable
		switch {
		case i == 0:
			w.Status = ts.Status
		case w.Status != ts.Status:
			w.Status = "mixed"
		}
	}

	type key struct{ project, task uuid.UUID }
	rows := map[key]*Row{}
	var order []key
	var projectIDs, taskIDs []uuid.UUID
	for _, e := range got.Entries {
		k := key{}
		if e.ProjectID != nil {
			k.project = *e.ProjectID
		}
		if e.TaskID != nil {
			k.task = *e.TaskID
		}
		row, ok := rows[k]
		if !ok {
			row = &Row{ProjectID: e.ProjectID, TaskID: e.TaskID, Hours: map[string]float64{}}
			rows[k] = row
			order = append(order, k)
			if e.ProjectID != nil {
				projectIDs = append(projectIDs, *e.ProjectID)
			}
			if e.TaskID != nil {
				taskIDs = append(taskIDs, *e.TaskID)
			}
		}
		day := e.Date.UTC().Format(dateLayout)
		h := round2(e.RegularHours + e.OvertimeHours)
		row.Hours[day] = round2(row.Hours[day] + h)
		row.Total = round2(row.Total + h)
		row.Billable = row.Billable || e.Billable
		if note := strings.TrimSpace(e.TaskDescription); note != "" {
			if row.Notes == nil {
				row.Notes = map[string]string{}
			}
			row.Notes[day] = note
		}
		w.DayTotals[day] = round2(w.DayTotals[day] + h)
		w.Total = round2(w.Total + h)
		if e.Billable {
			w.BillableTotal = round2(w.BillableTotal + h)
		}
	}
	projectNames, taskTitles, err := names(ctx, projectIDs, taskIDs)
	if err != nil {
		return nil, err
	}
	for _, k := range order {
		row := rows[k]
		row.ProjectName, row.TaskTitle = projectNames[k.project], taskTitles[k.task]
		w.Rows = append(w.Rows, *row)
	}
	sort.SliceStable(w.Rows, func(i, j int) bool {
		a, b := w.Rows[i], w.Rows[j]
		if a.ProjectName != b.ProjectName {
			return a.ProjectName < b.ProjectName
		}
		return a.TaskTitle < b.TaskTitle
	})
	return w, nil
}

func (s *Service) projectNames(ctx context.Context, tenantID uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]string, error) {
	out := map[uuid.UUID]string{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := s.client.Project.Query().Where(entproject.TenantID(tenantID), entproject.IDIn(ids...)).
		Select(entproject.FieldID, entproject.FieldName).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("project names: %w", err)
	}
	for _, p := range rows {
		out[p.ID] = p.Name
	}
	return out, nil
}

func (s *Service) taskTitles(ctx context.Context, tenantID uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]string, error) {
	out := map[uuid.UUID]string{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := s.client.Task.Query().Where(enttask.TenantID(tenantID), enttask.IDIn(ids...)).
		Select(enttask.FieldID, enttask.FieldTitle).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("task titles: %w", err)
	}
	for _, t := range rows {
		out[t.ID] = t.Title
	}
	return out, nil
}

// ---- Entries ----

// target is a project (and task) time is logged on, with the project's billing default.
type target struct {
	projectID uuid.UUID
	taskID    *uuid.UUID
	billsHrs  bool
}

// resolveTarget checks the project exists in the tenant and the task belongs to it.
func (s *Service) resolveTarget(ctx context.Context, tenantID, projectID uuid.UUID, taskID *uuid.UUID) (target, error) {
	p, err := s.client.Project.Query().Where(entproject.TenantID(tenantID), entproject.ID(projectID)).
		Select(entproject.FieldID, entproject.FieldMetadata).Only(ctx)
	if ent.IsNotFound(err) {
		return target{}, ErrNotFound
	}
	if err != nil {
		return target{}, fmt.Errorf("load project: %w", err)
	}
	if taskID != nil {
		ok, err := s.client.Task.Query().Where(enttask.TenantID(tenantID), enttask.ProjectID(projectID), enttask.ID(*taskID)).Exist(ctx)
		if err != nil {
			return target{}, fmt.Errorf("load task: %w", err)
		}
		if !ok {
			return target{}, ValidationError("the task does not belong to this project")
		}
	}
	return target{projectID: projectID, taskID: taskID, billsHrs: projects.BillsHours(p.Metadata)}, nil
}

// ResolveBillable decides an entry's billable flag: the project's default when the caller does not
// say, and never billable on a project that does not bill hours (only time and materials does).
func ResolveBillable(projectBillsHours bool, requested *bool) (bool, error) {
	if requested == nil {
		return projectBillsHours, nil
	}
	if *requested && !projectBillsHours {
		return false, ValidationError("this project does not bill hours: set its billing type to time and materials to log billable time")
	}
	return *requested, nil
}

// EntryInput sets the hours of one day on a project (and task). Hours replace what was logged.
type EntryInput struct {
	ProjectID uuid.UUID  `json:"project_id"`
	TaskID    *uuid.UUID `json:"task_id"`
	Date      string     `json:"date"`
	Hours     float64    `json:"hours"`
	Note      string     `json:"note"`
	Billable  *bool      `json:"billable"`
}

// SetEntry sets the user's hours for one day, project and task on their ERP timesheet, opening
// the week's timesheet when there is none.
func (s *Service) SetEntry(ctx context.Context, tenantID, userID uuid.UUID, in EntryInput) (*erp.Logged, error) {
	date, err := ParseDate(in.Date)
	if err != nil {
		return nil, err
	}
	if math.IsNaN(in.Hours) || in.Hours < 0 || in.Hours > MaxEntryHours {
		return nil, ValidationError("hours must be between 0 and 24")
	}
	note := strings.TrimSpace(in.Note)
	if len(note) > maxNoteLen {
		return nil, ValidationError("the note is longer than 500 characters")
	}
	tg, err := s.resolveTarget(ctx, tenantID, in.ProjectID, in.TaskID)
	if err != nil {
		return nil, err
	}
	billable, err := ResolveBillable(tg.billsHrs, in.Billable)
	if err != nil {
		return nil, err
	}
	start, end := WeekOf(date)
	out, err := s.erp.LogUserEntry(ctx, tenantID, erp.LogEntry{
		AuthUserID: userID, PeriodStart: start, PeriodEnd: end, Date: date,
		ProjectID: tg.projectID, TaskID: tg.taskID, Billable: billable, Note: note, Hours: round2(in.Hours),
	})
	return out, erpErr(err)
}

// SubmitWeek sends ERP's submit transition for the user's timesheets over the week holding weekStart.
func (s *Service) SubmitWeek(ctx context.Context, tenantID, userID uuid.UUID, weekStart time.Time) ([]erp.Timesheet, error) {
	start, end := WeekOf(weekStart)
	out, err := s.erp.SubmitUserTimesheets(ctx, tenantID, userID, start, end)
	return out, erpErr(err)
}

// ---- Timer ----

// Segment is the part of a timer run that fell on one local date.
type Segment struct {
	Date  string  `json:"date"`
	Hours float64 `json:"hours"`
}

// SplitByDay splits [start, stop) at local midnights in loc and returns the hours per local date,
// rounded to two places. A run inside one day gives one segment; one across midnight gives two.
func SplitByDay(start, stop time.Time, loc *time.Location) []Segment {
	var out []Segment
	cur := start.In(loc)
	end := stop.In(loc)
	for cur.Before(end) {
		next := time.Date(cur.Year(), cur.Month(), cur.Day()+1, 0, 0, 0, 0, loc)
		if next.After(end) {
			next = end
		}
		out = append(out, Segment{Date: cur.Format(dateLayout), Hours: round2(next.Sub(cur).Hours())})
		cur = next
	}
	return out
}

// StopInput is a finished timer run. The browser sends its own start and stop, so a stop queued
// offline logs the time it really ran.
type StopInput struct {
	ProjectID uuid.UUID  `json:"project_id"`
	TaskID    *uuid.UUID `json:"task_id"`
	StartedAt time.Time  `json:"started_at"`
	StoppedAt time.Time  `json:"stopped_at"`
	Note      string     `json:"note"`
	Billable  *bool      `json:"billable"`
}

// PartialStop is a timer stop whose run crossed midnight and failed after logging its first
// date(s). Logged lists them, so the browser retries only the rest instead of adding them twice.
type PartialStop struct {
	Logged []Segment
	Err    error
}

func (e *PartialStop) Error() string { return "timer stop logged part of the run: " + e.Err.Error() }
func (e *PartialStop) Unwrap() error { return e.Err }

// Stopped is what a timer stop logged: one ERP entry per local date the run covered.
type Stopped struct {
	Segments []Segment    `json:"segments"`
	Entries  []erp.Logged `json:"entries"`
}

// StopTimer adds a timer run to the user's ERP entries, split at local midnight in loc, and
// clears the user's running timer on the task.
func (s *Service) StopTimer(ctx context.Context, tenantID, userID uuid.UUID, in StopInput, loc *time.Location) (*Stopped, error) {
	if in.StartedAt.IsZero() || in.StoppedAt.IsZero() || !in.StoppedAt.After(in.StartedAt) {
		return nil, ValidationError("stopped_at must be after started_at")
	}
	if in.StoppedAt.Sub(in.StartedAt) > MaxTimerSpan {
		return nil, ValidationError("a timer that ran over 24 hours must be entered by hand on the week")
	}
	if in.StoppedAt.After(s.now().Add(5 * time.Minute)) {
		return nil, ValidationError("stopped_at is in the future")
	}
	note := strings.TrimSpace(in.Note)
	if len(note) > maxNoteLen {
		return nil, ValidationError("the note is longer than 500 characters")
	}
	tg, err := s.resolveTarget(ctx, tenantID, in.ProjectID, in.TaskID)
	if err != nil {
		return nil, err
	}
	billable, err := ResolveBillable(tg.billsHrs, in.Billable)
	if err != nil {
		return nil, err
	}
	out, err := s.logRun(ctx, tenantID, userID, tg, in, billable, note, loc)
	if err != nil {
		return nil, err
	}
	if tg.taskID != nil {
		if err := s.clearTimer(ctx, tenantID, userID, *tg.taskID); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// logRun adds a validated timer run to ERP, one add per local date it covered.
func (s *Service) logRun(ctx context.Context, tenantID, userID uuid.UUID, tg target, in StopInput, billable bool, note string, loc *time.Location) (*Stopped, error) {
	out := &Stopped{Segments: []Segment{}, Entries: []erp.Logged{}}
	for _, seg := range SplitByDay(in.StartedAt, in.StoppedAt, loc) {
		if seg.Hours <= 0 {
			continue
		}
		date, _ := time.Parse(dateLayout, seg.Date)
		start, end := WeekOf(date)
		logged, err := s.erp.LogUserEntry(ctx, tenantID, erp.LogEntry{
			AuthUserID: userID, PeriodStart: start, PeriodEnd: end, Date: date,
			ProjectID: tg.projectID, TaskID: tg.taskID, Billable: billable, Note: note, Hours: seg.Hours, Add: true,
		})
		if err != nil {
			if len(out.Segments) > 0 {
				s.log.Warn("timer stop logged part of the run", zap.Int("logged_dates", len(out.Segments)), zap.Error(err))
				return nil, &PartialStop{Logged: out.Segments, Err: erpErr(err)}
			}
			return nil, erpErr(err)
		}
		out.Segments = append(out.Segments, seg)
		out.Entries = append(out.Entries, *logged)
	}
	return out, nil
}
