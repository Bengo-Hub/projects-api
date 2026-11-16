package handlers

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/projects-service/internal/http/authz"
	"github.com/bengobox/projects-service/internal/modules/rbac"
	"github.com/bengobox/projects-service/internal/platform/erp"
	"github.com/bengobox/projects-service/internal/services/timetracking"
)

// TimeHandler serves time tracking: the signed-in user's week, entries, timer and submit. The hours
// live in the user's ERP timesheet; approval stays in ERP.
type TimeHandler struct {
	log *zap.Logger
	svc *timetracking.Service
}

func NewTimeHandler(log *zap.Logger, svc *timetracking.Service) *TimeHandler {
	return &TimeHandler{log: log.Named("time.handler"), svc: svc}
}

// defaultTimeZone is where a day starts when the browser sends no ?tz=.
const defaultTimeZone = "Africa/Nairobi"

// RegisterRoutes mounts the routes behind gates (the time_tracking feature and ERP's attendance
// feature, since the time is logged on ERP timesheets). projects.time.view_own reads one's own
// time and projects.time.add logs it; view and manage (which imply them) cover other people.
func (h *TimeHandler) RegisterRoutes(r chi.Router, gates ...func(http.Handler) http.Handler) {
	r.Group(func(g chi.Router) {
		g.Use(gates...)
		viewOwn := authz.Require(rbac.PermTimeViewOwn)
		add := authz.Require(rbac.PermTimeAdd)
		g.With(viewOwn).Get("/time/week", h.Week)
		g.With(add).Put("/time/entries", h.PutEntry)
		g.With(add).Post("/time/week/submit", h.SubmitWeek)
		g.With(viewOwn).Get("/time/timer", h.RunningTimer)
		g.With(add).Post("/time/timer/stop", h.StopTimer)
		g.With(viewOwn).Get("/tasks/{taskID}/timer", h.TaskTimer)
		g.With(add).Post("/tasks/{taskID}/timer", h.StartTimer)
		g.With(add).Delete("/tasks/{taskID}/timer", h.ClearTimer)
	})
}

// timeLocation reads ?tz= (IANA); Africa/Nairobi when absent.
func timeLocation(w http.ResponseWriter, r *http.Request) (*time.Location, bool) {
	tz := strings.TrimSpace(r.URL.Query().Get("tz"))
	if tz == "" {
		tz = defaultTimeZone
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid tz")
		return nil, false
	}
	return loc, true
}

// whose returns the user whose time the request reads or writes: the caller, or ?user_id= (GET)
// or the body's user_id when the caller holds otherCode. The caller must be a signed-in user.
func whose(w http.ResponseWriter, r *http.Request, raw, otherCode string) (uuid.UUID, bool) {
	me, ok := actingUser(w, r)
	if !ok {
		return uuid.Nil, false
	}
	if raw == "" || strings.EqualFold(raw, me.String()) {
		return me, true
	}
	other, err := uuid.Parse(raw)
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid user_id")
		return uuid.Nil, false
	}
	if !accessOf(r).Has(otherCode) {
		authz.Forbidden(w, otherCode)
		return uuid.Nil, false
	}
	return other, true
}

// weekStart reads ?week_start= (any date of the week; the Monday when absent is this week's).
func weekStart(w http.ResponseWriter, raw string, loc *time.Location) (time.Time, bool) {
	if strings.TrimSpace(raw) == "" {
		return time.Now().In(loc), true
	}
	d, err := timetracking.ParseDate(raw)
	if err != nil {
		respondError(w, http.StatusBadRequest, "week_start must be YYYY-MM-DD")
		return time.Time{}, false
	}
	return d, true
}

// writeErr maps the time tracking and ERP errors. ERP's own 4xx (timesheet_locked,
// nothing_to_submit, feature_not_available) pass through with ERP's code.
func (h *TimeHandler) writeErr(w http.ResponseWriter, err error) {
	var partial *timetracking.PartialStop
	if errors.As(err, &partial) {
		status, body := h.errorBody(partial.Err)
		body["logged_segments"] = partial.Logged
		respondJSON(w, status, body)
		return
	}
	status, body := h.errorBody(err)
	respondJSON(w, status, body)
}

func (h *TimeHandler) errorBody(err error) (int, map[string]any) {
	var verr timetracking.ValidationError
	var erpErr *erp.Error
	switch {
	case errors.Is(err, timetracking.ErrNoEmployee):
		return http.StatusConflict, map[string]any{"error": err.Error(), "code": "no_employee_record"}
	case errors.Is(err, timetracking.ErrNotFound):
		return http.StatusNotFound, map[string]any{"error": "project or task not found", "code": "not_found"}
	case errors.As(err, &verr):
		return http.StatusUnprocessableEntity, map[string]any{"error": verr.Error(), "code": "validation_error"}
	case errors.As(err, &erpErr):
		msg := erpErr.Message
		if msg == "" {
			msg = "ERP refused the request"
		}
		return erpErr.Status, map[string]any{"error": msg, "code": erpErr.Code}
	case errors.Is(err, erp.ErrNotConfigured):
		return http.StatusServiceUnavailable, map[string]any{"error": "time tracking is not available: ERP is not connected", "code": "erp_unavailable"}
	default:
		h.log.Warn("time tracking call failed", zap.Error(err))
		return http.StatusBadGateway, map[string]any{"error": "ERP is not answering; try again shortly", "code": "erp_unavailable"}
	}
}

// Week godoc
// @Summary My week of time
// @Description The user's ERP timesheet time for the week (Monday to Sunday) holding ?week_start= (default this week in ?tz=), by project, task and day, with the timesheet status. ?user_id= reads someone else's week (projects.time.view). 409 no_employee_record when the user has no ERP employee record.
// @Tags time
// @Produce json
// @Param tenantID path string true "Tenant"
// @Success 200 {object} timetracking.Week
// @Router /api/v1/{tenantID}/time/week [get]
func (h *TimeHandler) Week(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := parseTenant(w, r)
	if !ok {
		return
	}
	user, ok := whose(w, r, r.URL.Query().Get("user_id"), rbac.PermTimeView)
	if !ok {
		return
	}
	loc, ok := timeLocation(w, r)
	if !ok {
		return
	}
	start, ok := weekStart(w, r.URL.Query().Get("week_start"), loc)
	if !ok {
		return
	}
	week, err := h.svc.Week(r.Context(), tenantID, user, start)
	if err != nil {
		h.writeErr(w, err)
		return
	}
	respondJSON(w, http.StatusOK, week)
}

type putEntryBody struct {
	timetracking.EntryInput
	UserID string `json:"user_id"`
}

// PutEntry godoc
// @Summary Set a day's hours on a project
// @Description Sets the hours of one day on a project (and optional task) in the user's ERP timesheet, opening the week's timesheet when there is none. billable defaults to the project's billing type (time_and_materials bills hours) and cannot be true on a project that does not bill hours. user_id logs for someone else (projects.time.manage). 409 timesheet_locked once the week is submitted.
// @Tags time
// @Accept json
// @Produce json
// @Param tenantID path string true "Tenant"
// @Success 200 {object} erp.Logged
// @Router /api/v1/{tenantID}/time/entries [put]
func (h *TimeHandler) PutEntry(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := parseTenant(w, r)
	if !ok {
		return
	}
	var b putEntryBody
	if !decodeBody(w, r, 16<<10, &b) {
		return
	}
	user, ok := whose(w, r, b.UserID, rbac.PermTimeManage)
	if !ok || !h.canSeeProject(w, r, b.ProjectID) {
		return
	}
	out, err := h.svc.SetEntry(r.Context(), tenantID, user, b.EntryInput)
	if err != nil {
		h.writeErr(w, err)
		return
	}
	respondJSON(w, http.StatusOK, out)
}

// canSeeProject applies membership scoping: a project the caller cannot see answers 404.
func (h *TimeHandler) canSeeProject(w http.ResponseWriter, r *http.Request, projectID uuid.UUID) bool {
	if projectID == uuid.Nil {
		respondError(w, http.StatusBadRequest, "project_id is required")
		return false
	}
	visible, err := accessOf(r).CanSeeProject(r.Context(), projectID)
	if err != nil {
		serverError(w, h.log, "membership check failed", err)
		return false
	}
	if !visible {
		respondError(w, http.StatusNotFound, "project not found")
		return false
	}
	return true
}

type submitWeekBody struct {
	WeekStart string `json:"week_start"`
}

// SubmitWeek godoc
// @Summary Submit my week
// @Description Sends ERP's submit transition for the user's timesheets over the week holding week_start (default this week in ?tz=). Approval happens in ERP. 409 nothing_to_submit when the week is already submitted.
// @Tags time
// @Accept json
// @Produce json
// @Param tenantID path string true "Tenant"
// @Success 200 {object} map[string]interface{}
// @Router /api/v1/{tenantID}/time/week/submit [post]
func (h *TimeHandler) SubmitWeek(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := parseTenant(w, r)
	if !ok {
		return
	}
	var b submitWeekBody
	if !decodeBody(w, r, 4<<10, &b) {
		return
	}
	user, ok := actingUser(w, r)
	if !ok {
		return
	}
	loc, ok := timeLocation(w, r)
	if !ok {
		return
	}
	start, ok := weekStart(w, b.WeekStart, loc)
	if !ok {
		return
	}
	sheets, err := h.svc.SubmitWeek(r.Context(), tenantID, user, start)
	if err != nil {
		h.writeErr(w, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": sheets})
}

// StopTimer godoc
// @Summary Log a timer run
// @Description Adds a finished timer run (started_at, stopped_at, RFC 3339 from the browser, so a stop queued offline logs the real run) to the user's ERP entries for the project and task, split at midnight in ?tz= (default Africa/Nairobi), and clears the user's running timer on the task. A partial failure across midnight answers the error with logged_segments.
// @Tags time
// @Accept json
// @Produce json
// @Param tenantID path string true "Tenant"
// @Success 200 {object} timetracking.Stopped
// @Router /api/v1/{tenantID}/time/timer/stop [post]
func (h *TimeHandler) StopTimer(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := parseTenant(w, r)
	if !ok {
		return
	}
	var in timetracking.StopInput
	if !decodeBody(w, r, 16<<10, &in) {
		return
	}
	user, ok := actingUser(w, r)
	if !ok || !h.canSeeProject(w, r, in.ProjectID) {
		return
	}
	loc, ok := timeLocation(w, r)
	if !ok {
		return
	}
	out, err := h.svc.StopTimer(r.Context(), tenantID, user, in, loc)
	if err != nil {
		h.writeErr(w, err)
		return
	}
	respondJSON(w, http.StatusOK, out)
}

// RunningTimer godoc
// @Summary My running timer
// @Description The user's running timer on any task ({data: null} when none), so a reload or another device picks it up.
// @Tags time
// @Produce json
// @Param tenantID path string true "Tenant"
// @Success 200 {object} map[string]interface{}
// @Router /api/v1/{tenantID}/time/timer [get]
func (h *TimeHandler) RunningTimer(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := parseTenant(w, r)
	if !ok {
		return
	}
	user, ok := actingUser(w, r)
	if !ok {
		return
	}
	t, err := h.svc.RunningTimer(r.Context(), tenantID, user)
	if err != nil {
		serverError(w, h.log, "running timer failed", err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": t})
}

// taskScope resolves the task of a timer route and applies membership scoping (404 otherwise).
func (h *TimeHandler) taskScope(w http.ResponseWriter, r *http.Request) (uuid.UUID, uuid.UUID, uuid.UUID, bool) {
	tenantID, ok := parseTenant(w, r)
	if !ok {
		return uuid.Nil, uuid.Nil, uuid.Nil, false
	}
	taskID, ok := uuidParam(w, r, "taskID")
	if !ok {
		return uuid.Nil, uuid.Nil, uuid.Nil, false
	}
	user, ok := actingUser(w, r)
	if !ok {
		return uuid.Nil, uuid.Nil, uuid.Nil, false
	}
	projectID, err := h.svc.TaskProject(r.Context(), tenantID, taskID)
	if errors.Is(err, timetracking.ErrNotFound) {
		respondError(w, http.StatusNotFound, "task not found")
		return uuid.Nil, uuid.Nil, uuid.Nil, false
	}
	if err != nil {
		serverError(w, h.log, "load task failed", err)
		return uuid.Nil, uuid.Nil, uuid.Nil, false
	}
	visible, err := accessOf(r).CanSeeProject(r.Context(), projectID)
	if err != nil {
		serverError(w, h.log, "membership check failed", err)
		return uuid.Nil, uuid.Nil, uuid.Nil, false
	}
	if !visible {
		respondError(w, http.StatusNotFound, "task not found")
		return uuid.Nil, uuid.Nil, uuid.Nil, false
	}
	return tenantID, taskID, user, true
}

// TaskTimer godoc
// @Summary My timer on a task
// @Description The user's running timer on this task ({data: null} when none).
// @Tags time
// @Produce json
// @Param tenantID path string true "Tenant"
// @Param taskID path string true "Task ID"
// @Success 200 {object} map[string]interface{}
// @Router /api/v1/{tenantID}/tasks/{taskID}/timer [get]
func (h *TimeHandler) TaskTimer(w http.ResponseWriter, r *http.Request) {
	tenantID, taskID, user, ok := h.taskScope(w, r)
	if !ok {
		return
	}
	t, err := h.svc.TaskTimer(r.Context(), tenantID, user, taskID)
	if err != nil {
		serverError(w, h.log, "task timer failed", err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": t})
}

// StartTimer godoc
// @Summary Start my timer on a task
// @Description Starts the user's timer on the task (kept in task metadata so it survives a reload). One timer per user: a timer running on another task is stopped first and its time logged to ERP (split at midnight in ?tz=). Starting the task's running timer again keeps its start.
// @Tags time
// @Produce json
// @Param tenantID path string true "Tenant"
// @Param taskID path string true "Task ID"
// @Success 200 {object} timetracking.Started
// @Router /api/v1/{tenantID}/tasks/{taskID}/timer [post]
func (h *TimeHandler) StartTimer(w http.ResponseWriter, r *http.Request) {
	tenantID, taskID, user, ok := h.taskScope(w, r)
	if !ok {
		return
	}
	loc, ok := timeLocation(w, r)
	if !ok {
		return
	}
	out, err := h.svc.StartTimer(r.Context(), tenantID, user, taskID, loc)
	if err != nil {
		h.writeErr(w, err)
		return
	}
	respondJSON(w, http.StatusOK, out)
}

// ClearTimer godoc
// @Summary Discard my timer on a task
// @Description Clears the user's running timer on the task without logging any time.
// @Tags time
// @Param tenantID path string true "Tenant"
// @Param taskID path string true "Task ID"
// @Success 204
// @Router /api/v1/{tenantID}/tasks/{taskID}/timer [delete]
func (h *TimeHandler) ClearTimer(w http.ResponseWriter, r *http.Request) {
	tenantID, taskID, user, ok := h.taskScope(w, r)
	if !ok {
		return
	}
	if err := h.svc.ClearTimer(r.Context(), tenantID, user, taskID); err != nil {
		serverError(w, h.log, "clear timer failed", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
