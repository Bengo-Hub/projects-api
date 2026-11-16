package erp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
)

// The fake mirrors erp-api's for-user handlers (attendance_timesheet_self.go): the same paths,
// query and body fields, and ent's JSON for timesheets and entries (decimal hours as strings,
// RFC 3339 dates, omitted empty fields).
func TestUserTimesheetContract(t *testing.T) {
	tenant, user, project, task, sheet := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	sheetJSON := `{"id":"` + sheet.String() + `","tenant_id":"` + tenant.String() + `","employee_id":"` + uuid.NewString() +
		`","period_start":"2026-11-16T00:00:00Z","period_end":"2026-11-22T00:00:00Z","status":"draft","total_hours":"3.25","total_overtime_hours":"0","is_active":true}`
	entryJSON := `{"id":"` + uuid.NewString() + `","tenant_id":"` + tenant.String() + `","timesheet_id":"` + sheet.String() +
		`","project_id":"` + project.String() + `","task_id":"` + task.String() + `","billable":true,"date":"2026-11-17T00:00:00Z",` +
		`"task_description":"review","regular_hours":"3.25","overtime_hours":"0","break_hours":"0","created_at":"2026-11-17T08:00:00Z"}`
	var posted map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-Key") != "k" || r.Header.Get("X-Tenant-ID") != tenant.String() {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.Method + " " + r.URL.Path {
		case "GET /api/v1/hrm/attendance/timesheets/for-user":
			q := r.URL.Query()
			if q.Get("auth_user_id") == uuid.Nil.String() {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"error":"This user has no employee record in ERP.","code":"no_employee_record"}`))
				return
			}
			if q.Get("auth_user_id") != user.String() || q.Get("from") != "2026-11-16" || q.Get("to") != "2026-11-22" {
				t.Errorf("week query %s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`{"employee_id":"` + uuid.NewString() + `","timesheets":[` + sheetJSON + `],"entries":[` + entryJSON + `]}`))
		case "POST /api/v1/hrm/attendance/timesheets/for-user/entries":
			raw, _ := io.ReadAll(r.Body)
			posted = map[string]any{}
			_ = json.Unmarshal(raw, &posted)
			if posted["date"] == "2026-11-23" {
				w.WriteHeader(http.StatusConflict)
				_, _ = w.Write([]byte(`{"error":"attendance: the timesheet is submitted or approved","code":"timesheet_locked"}`))
				return
			}
			_, _ = w.Write([]byte(`{"employee_id":"` + uuid.NewString() + `","timesheet":` + sheetJSON + `,"entry":` + entryJSON + `}`))
		case "POST /api/v1/hrm/attendance/timesheets/for-user/submit":
			_, _ = w.Write([]byte(`{"data":[` + sheetJSON + `]}`))
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	c := NewClient(srv.URL, "k")
	ctx := context.Background()
	day := func(s string) time.Time { d, _ := time.Parse("2006-01-02", s); return d }

	week, err := c.UserTimesheets(ctx, tenant, user, day("2026-11-16"), day("2026-11-22"))
	if err != nil {
		t.Fatal(err)
	}
	e := week.Entries[0]
	if len(week.Timesheets) != 1 || week.Timesheets[0].Status != "draft" || week.Timesheets[0].TotalHours != 3.25 {
		t.Fatalf("timesheets %+v", week.Timesheets)
	}
	if *e.ProjectID != project || *e.TaskID != task || !e.Billable || e.RegularHours != 3.25 || e.Date.Format("2006-01-02") != "2026-11-17" {
		t.Fatalf("entry %+v", e)
	}

	if _, err := c.UserTimesheets(ctx, tenant, uuid.Nil, day("2026-11-16"), day("2026-11-22")); !errors.Is(err, ErrNoEmployee) {
		t.Fatalf("no employee: %v", err)
	}

	logged, err := c.LogUserEntry(ctx, tenant, LogEntry{
		AuthUserID: user, PeriodStart: day("2026-11-16"), PeriodEnd: day("2026-11-22"), Date: day("2026-11-17"),
		ProjectID: project, TaskID: &task, Billable: true, Note: "review", Hours: 1.25, Add: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if logged.Entry.RegularHours != 3.25 || logged.Timesheet.ID != sheet {
		t.Fatalf("logged %+v", logged)
	}
	want := map[string]any{
		"auth_user_id": user.String(), "period_start": "2026-11-16", "period_end": "2026-11-22", "date": "2026-11-17",
		"project_id": project.String(), "task_id": task.String(), "billable": true, "regular_hours": "1.25",
		"task_description": "review", "mode": "add",
	}
	for k, v := range want {
		if posted[k] != v {
			t.Errorf("posted %s = %v, want %v", k, posted[k], v)
		}
	}

	_, err = c.LogUserEntry(ctx, tenant, LogEntry{AuthUserID: user, PeriodStart: day("2026-11-23"), PeriodEnd: day("2026-11-29"), Date: day("2026-11-23"), ProjectID: project, Hours: 1})
	var erpErr *Error
	if !errors.As(err, &erpErr) || erpErr.Status != http.StatusConflict || erpErr.Code != "timesheet_locked" {
		t.Fatalf("locked: %v", err)
	}
	if _, ok := posted["task_id"]; ok || posted["mode"] != "set" {
		t.Fatalf("a project-level set must omit task_id: %v", posted)
	}

	sheets, err := c.SubmitUserTimesheets(ctx, tenant, user, day("2026-11-16"), day("2026-11-22"))
	if err != nil || len(sheets) != 1 {
		t.Fatalf("submit: %v %v", sheets, err)
	}
	if _, err := NewClient("", "").UserTimesheets(ctx, tenant, user, day("2026-11-16"), day("2026-11-22")); err != ErrNotConfigured {
		t.Fatalf("unconfigured: %v", err)
	}
}
