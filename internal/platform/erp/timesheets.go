package erp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/google/uuid"
)

// Time logging on the signed-in user's ERP timesheet (erp-api
// /hrm/attendance/timesheets/for-user). ERP owns the hours: projects-api reads and writes them
// here and stores none.

// ErrNoEmployee is ERP's 404 no_employee_record: the auth user has no employee in the tenant.
var ErrNoEmployee = errors.New("erp: the user has no employee record")

// Error is an ERP 4xx other than no_employee_record, with ERP's code and message.
type Error struct {
	Status  int
	Code    string
	Message string
}

func (e *Error) Error() string {
	return fmt.Sprintf("erp: status %d %s: %s", e.Status, e.Code, e.Message)
}

const dateLayout = "2006-01-02"

// Timesheet is an ERP timesheet period (status draft, submitted, approved or rejected).
type Timesheet struct {
	ID              uuid.UUID `json:"id"`
	PeriodStart     time.Time `json:"period_start"`
	PeriodEnd       time.Time `json:"period_end"`
	Status          string    `json:"status"`
	RejectionReason *string   `json:"rejection_reason,omitempty"`
	TotalHours      float64   `json:"total_hours,string"`
}

// Entry is one ERP timesheet entry: a date, project and task. Hours are decimal strings on the wire.
type Entry struct {
	ID              uuid.UUID  `json:"id"`
	TimesheetID     uuid.UUID  `json:"timesheet_id"`
	ProjectID       *uuid.UUID `json:"project_id,omitempty"`
	TaskID          *uuid.UUID `json:"task_id,omitempty"`
	Billable        bool       `json:"billable,omitempty"`
	Date            time.Time  `json:"date"`
	TaskDescription string     `json:"task_description,omitempty"`
	RegularHours    float64    `json:"regular_hours,string"`
	OvertimeHours   float64    `json:"overtime_hours,string"`
}

// UserTimesheets is the user's timesheets overlapping a range and their entries in it.
type UserTimesheets struct {
	EmployeeID uuid.UUID   `json:"employee_id"`
	Timesheets []Timesheet `json:"timesheets"`
	Entries    []Entry     `json:"entries"`
}

// LogEntry sets (Add false) or adds hours on the user's entry for one date, project and task.
// PeriodStart and PeriodEnd are the period ERP opens as a draft when no timesheet covers Date.
type LogEntry struct {
	AuthUserID  uuid.UUID
	PeriodStart time.Time
	PeriodEnd   time.Time
	Date        time.Time
	ProjectID   uuid.UUID
	TaskID      *uuid.UUID
	Billable    bool
	Note        string
	Hours       float64
	Add         bool
}

// Logged is ERP's answer to a log: the entry and its timesheet with rolled up totals.
type Logged struct {
	EmployeeID uuid.UUID `json:"employee_id"`
	Timesheet  Timesheet `json:"timesheet"`
	Entry      Entry     `json:"entry"`
}

func (c *Client) headers(tenantID uuid.UUID) map[string]string {
	return map[string]string{"X-API-Key": c.apiKey, "X-Tenant-ID": tenantID.String()}
}

// failure turns a non-2xx ERP answer into ErrNoEmployee, an *Error (4xx) or a plain error (5xx).
func failure(op string, status int, body []byte) error {
	var e struct {
		Code  string `json:"code"`
		Error string `json:"error"`
	}
	_ = json.Unmarshal(body, &e)
	if status == http.StatusNotFound && e.Code == "no_employee_record" {
		return ErrNoEmployee
	}
	if status >= 400 && status < 500 {
		return &Error{Status: status, Code: e.Code, Message: e.Error}
	}
	return fmt.Errorf("erp: %s: status %d", op, status)
}

// UserTimesheets reads the user's timesheets and entries for [from, to] (dates, inclusive).
func (c *Client) UserTimesheets(ctx context.Context, tenantID, authUserID uuid.UUID, from, to time.Time) (*UserTimesheets, error) {
	if !c.Enabled() {
		return nil, ErrNotConfigured
	}
	q := url.Values{"auth_user_id": {authUserID.String()}, "from": {from.Format(dateLayout)}, "to": {to.Format(dateLayout)}}
	resp, err := c.sc.Get(ctx, "/api/v1/hrm/attendance/timesheets/for-user?"+q.Encode(), c.headers(tenantID))
	if err != nil {
		return nil, fmt.Errorf("erp: user timesheets: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, failure("user timesheets", resp.StatusCode, resp.Body)
	}
	var out UserTimesheets
	if err := resp.DecodeJSON(&out); err != nil {
		return nil, fmt.Errorf("erp: user timesheets: %w", err)
	}
	return &out, nil
}

// LogUserEntry sets or adds the user's hours on one entry. ERP takes it as a POST, which the
// shared client never retries, so an add is never applied twice by a retry.
func (c *Client) LogUserEntry(ctx context.Context, tenantID uuid.UUID, in LogEntry) (*Logged, error) {
	if !c.Enabled() {
		return nil, ErrNotConfigured
	}
	mode := "set"
	if in.Add {
		mode = "add"
	}
	body := map[string]any{
		"auth_user_id": in.AuthUserID.String(),
		"period_start": in.PeriodStart.Format(dateLayout),
		"period_end":   in.PeriodEnd.Format(dateLayout),
		"date":         in.Date.Format(dateLayout),
		"project_id":   in.ProjectID.String(),
		"billable":     in.Billable,
		// Two decimals, the precision of ERP's hours columns.
		"regular_hours":    fmt.Sprintf("%.2f", in.Hours),
		"task_description": in.Note,
		"mode":             mode,
	}
	if in.TaskID != nil {
		body["task_id"] = in.TaskID.String()
	}
	resp, err := c.sc.Post(ctx, "/api/v1/hrm/attendance/timesheets/for-user/entries", body, c.headers(tenantID))
	if err != nil {
		return nil, fmt.Errorf("erp: log entry: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, failure("log entry", resp.StatusCode, resp.Body)
	}
	var out Logged
	if err := resp.DecodeJSON(&out); err != nil {
		return nil, fmt.Errorf("erp: log entry: %w", err)
	}
	return &out, nil
}

// SubmitUserTimesheets sends ERP's submit transition for every draft or rejected timesheet of
// the user overlapping [from, to].
func (c *Client) SubmitUserTimesheets(ctx context.Context, tenantID, authUserID uuid.UUID, from, to time.Time) ([]Timesheet, error) {
	if !c.Enabled() {
		return nil, ErrNotConfigured
	}
	body := map[string]string{"auth_user_id": authUserID.String(), "from": from.Format(dateLayout), "to": to.Format(dateLayout)}
	resp, err := c.sc.Post(ctx, "/api/v1/hrm/attendance/timesheets/for-user/submit", body, c.headers(tenantID))
	if err != nil {
		return nil, fmt.Errorf("erp: submit timesheets: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, failure("submit timesheets", resp.StatusCode, resp.Body)
	}
	var out struct {
		Data []Timesheet `json:"data"`
	}
	if err := resp.DecodeJSON(&out); err != nil {
		return nil, fmt.Errorf("erp: submit timesheets: %w", err)
	}
	return out.Data, nil
}
