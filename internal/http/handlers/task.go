package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata" // My Work buckets honour the caller's ?tz= on images without zoneinfo

	"github.com/Bengo-Hub/pagination"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/projects-service/internal/http/authz"
	"github.com/bengobox/projects-service/internal/modules/rbac"
	"github.com/bengobox/projects-service/internal/services/tasks"
)

// TaskHandler handles task HTTP endpoints.
type TaskHandler struct {
	log *zap.Logger
	svc *tasks.Service
}

// NewTaskHandler creates a new task handler.
func NewTaskHandler(log *zap.Logger, svc *tasks.Service) *TaskHandler {
	return &TaskHandler{log: log.Named("task.handler"), svc: svc}
}

// RegisterRoutes registers the task routes, every one behind tracking (the task_tracking plan
// feature) and the Gantt chart also behind gantt (gantt_chart). Flat patterns under
// /projects/{projectID} (see CommentHandler.RegisterRoutes for why).
func (h *TaskHandler) RegisterRoutes(r chi.Router, tracking, gantt func(http.Handler) http.Handler) {
	view := authz.Require(rbac.PermTasksView, rbac.PermTasksViewOwn)
	change := authz.Require(rbac.PermTasksChange, rbac.PermTasksChangeOwn)
	r.Group(func(g chi.Router) {
		g.Use(tracking)
		g.Route("/projects/{projectID}/tasks", func(tr chi.Router) {
			tr.With(view).Get("/", h.List)
			tr.With(authz.Require(rbac.PermTasksAdd)).Post("/", h.Create)
			tr.Route("/{taskID}", func(t chi.Router) {
				t.With(view).Get("/", h.Get)
				t.With(change).Put("/", h.Update)
				t.With(change).Patch("/status", h.ChangeStatus)
				t.With(change).Patch("/dates", h.Reschedule)
				t.With(authz.Require(rbac.PermTasksDelete)).Delete("/", h.Delete)
				t.With(authz.Require(rbac.PermTasksChange)).Post("/dependencies", h.AddDependency)
				t.With(authz.Require(rbac.PermTasksChange)).Delete("/dependencies/{depID}", h.RemoveDependency)
			})
		})
		g.With(view).Get("/projects/{projectID}/statuses", h.GetStatuses)
		g.With(authz.Require(rbac.PermProjectsChange)).Put("/projects/{projectID}/statuses", h.SetStatuses)
		g.With(view).Get("/projects/{projectID}/board-counts", h.BoardCounts)
		g.With(gantt, authz.Require(rbac.PermTasksView)).Get("/projects/{projectID}/gantt", h.Gantt)
		g.With(authz.Require(rbac.PermTasksView)).Get("/tasks/trend", h.Trend)
		g.With(view).Get("/my-work", h.MyWork)
		g.With(view).Get("/my-work/counts", h.MyWorkCounts)
	})
}

// ownOnly returns the caller when they may only see or change tasks assigned to them (they hold
// the _own code but not the full one), nil otherwise.
func ownOnly(r *http.Request, full string) *uuid.UUID {
	a := accessOf(r)
	if a.Has(full) {
		return nil
	}
	id := a.AuthUserID
	return &id
}

// Trend returns monthly task flow (created, completed, overdue at month end) for one project
// (?project_id=) or, for callers who see every project, the whole tenant; over the last ?months=
// (1..24, default 6).
func (h *TaskHandler) Trend(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := parseTenant(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	var projectID *uuid.UUID
	if v := q.Get("project_id"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			respondError(w, http.StatusBadRequest, "invalid project id")
			return
		}
		visible, err := accessOf(r).CanSeeProject(r.Context(), id)
		if err != nil {
			serverError(w, h.log, "membership check failed", err)
			return
		}
		if !visible {
			respondError(w, http.StatusNotFound, "project not found")
			return
		}
		projectID = &id
	} else if accessOf(r).VisibleTo() != nil {
		authz.Forbidden(w, rbac.PermProjectsManage)
		return
	}
	months, _ := strconv.Atoi(q.Get("months"))
	rows, err := h.svc.Trend(r.Context(), tenantID, projectID, months, time.Now())
	if err != nil {
		serverError(w, h.log, "task trend failed", err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": rows})
}

// parseDay reads a date query parameter (YYYY-MM-DD or RFC 3339); "" is no bound.
func parseDay(w http.ResponseWriter, v, name string) (*time.Time, bool) {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil, true
	}
	for _, layout := range []string{time.DateOnly, time.RFC3339Nano} {
		if t, err := time.Parse(layout, v); err == nil {
			return &t, true
		}
	}
	respondError(w, http.StatusBadRequest, "invalid "+name+": use YYYY-MM-DD")
	return nil, false
}

// dueBounds reads ?due_from= and ?due_to= (both inclusive).
func dueBounds(w http.ResponseWriter, r *http.Request) (*time.Time, *time.Time, bool) {
	q := r.URL.Query()
	from, ok := parseDay(w, q.Get("due_from"), "due_from")
	if !ok {
		return nil, nil, false
	}
	to, ok := parseDay(w, q.Get("due_to"), "due_to")
	if !ok {
		return nil, nil, false
	}
	if from != nil && to != nil && to.Before(*from) {
		respondError(w, http.StatusBadRequest, "due_to is before due_from")
		return nil, nil, false
	}
	return from, to, true
}

// List returns one page of a project's tasks (?limit, ?page or ?offset; ?status for one board
// column, ?priority, ?assignee=me|<user id> over every assignee, ?due_from and ?due_to for the
// calendar). A caller limited to their own tasks sees only those assigned to them.
func (h *TaskHandler) List(w http.ResponseWriter, r *http.Request) {
	tenantID, projectID, ok := projectScope(w, r)
	if !ok {
		return
	}
	p := pagination.Parse(r)
	q := r.URL.Query()
	from, to, ok := dueBounds(w, r)
	if !ok {
		return
	}
	filter := tasks.ListTasksFilter{
		Status:   q.Get("status"),
		Priority: q.Get("priority"),
		DueFrom:  from,
		DueTo:    to,
		Limit:    p.Limit,
		Offset:   p.Offset,
	}
	switch a := strings.TrimSpace(q.Get("assignee")); a {
	case "":
	case "me":
		me := actorID(r)
		filter.Assignee = &me
	default:
		id, err := uuid.Parse(a)
		if err != nil {
			respondError(w, http.StatusBadRequest, "assignee must be me or a user id")
			return
		}
		filter.Assignee = &id
	}
	if own := ownOnly(r, rbac.PermTasksView); own != nil {
		filter.Assignee = own
	}
	items, total, err := h.svc.ListTasks(r.Context(), tenantID, projectID, filter)
	if err != nil {
		serverError(w, h.log, "list tasks failed", err)
		return
	}
	respondJSON(w, http.StatusOK, pagination.NewResponse(items, total, p))
}

func taskParam(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "taskID"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid task id")
		return uuid.Nil, false
	}
	return id, true
}

// ensureOwn answers 404 when the caller is limited to their own tasks and this one is not theirs.
func (h *TaskHandler) ensureOwn(w http.ResponseWriter, r *http.Request, full string, tenantID, projectID, taskID uuid.UUID) bool {
	own := ownOnly(r, full)
	if own == nil {
		return true
	}
	mine, err := h.svc.IsAssignee(r.Context(), tenantID, projectID, taskID, *own)
	if err != nil {
		serverError(w, h.log, "task assignee check failed", err)
		return false
	}
	if !mine {
		respondError(w, http.StatusNotFound, "task not found")
		return false
	}
	return true
}

// Get returns a task with its dependencies, assignees, its latest 50 comments and its comment
// count.
func (h *TaskHandler) Get(w http.ResponseWriter, r *http.Request) {
	tenantID, projectID, ok := projectScope(w, r)
	if !ok {
		return
	}
	taskID, ok := taskParam(w, r)
	if !ok || !h.ensureOwn(w, r, rbac.PermTasksView, tenantID, projectID, taskID) {
		return
	}
	t, err := h.svc.GetTask(r.Context(), tenantID, projectID, taskID)
	if errors.Is(err, tasks.ErrNotFound) {
		respondError(w, http.StatusNotFound, "task not found")
		return
	}
	if err != nil {
		serverError(w, h.log, "get task failed", err)
		return
	}
	respondJSON(w, http.StatusOK, t)
}

func (h *TaskHandler) writeErr(w http.ResponseWriter, err error, notFound, msg string) {
	var verr tasks.ValidationError
	var conflict *tasks.ScheduleConflict
	switch {
	case errors.Is(err, tasks.ErrNotFound):
		respondError(w, http.StatusNotFound, notFound)
	case errors.Is(err, tasks.ErrCircularDependency):
		respondError(w, http.StatusConflict, err.Error())
	case errors.As(err, &conflict):
		ids := make([]string, len(conflict.BlockingTaskIDs))
		for i, id := range conflict.BlockingTaskIDs {
			ids[i] = id.String()
		}
		respondJSON(w, http.StatusConflict, map[string]any{
			"error": conflict.Error(), "code": "schedule_conflict", "blocking_task_ids": ids,
		})
	case errors.As(err, &verr):
		respondError(w, http.StatusBadRequest, verr.Error())
	default:
		serverError(w, h.log, msg, err)
	}
}

// Create creates a new task.
func (h *TaskHandler) Create(w http.ResponseWriter, r *http.Request) {
	tenantID, projectID, ok := projectScope(w, r)
	if !ok {
		return
	}
	var input tasks.CreateTaskInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	t, err := h.svc.CreateTask(noticeCtx(r), tenantID, projectID, input)
	if err != nil {
		h.writeErr(w, err, "project not found", "create task failed")
		return
	}
	respondJSON(w, http.StatusCreated, t)
}

// Update updates a task. A caller holding only change_own may update tasks assigned to them.
func (h *TaskHandler) Update(w http.ResponseWriter, r *http.Request) {
	tenantID, projectID, ok := projectScope(w, r)
	if !ok {
		return
	}
	taskID, ok := taskParam(w, r)
	if !ok || !h.ensureOwn(w, r, rbac.PermTasksChange, tenantID, projectID, taskID) {
		return
	}
	var input tasks.UpdateTaskInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	t, err := h.svc.UpdateTask(noticeCtx(r), tenantID, projectID, taskID, input)
	if err != nil {
		h.writeErr(w, err, "task not found", "update task failed")
		return
	}
	respondJSON(w, http.StatusOK, t)
}

// ChangeStatus moves a task to another column: {"status": key}. An Idempotency-Key header makes
// an offline replay safe: a key already applied to this task in the last 24 hours changes nothing
// and answers the task with Idempotent-Replayed: true.
func (h *TaskHandler) ChangeStatus(w http.ResponseWriter, r *http.Request) {
	tenantID, projectID, ok := projectScope(w, r)
	if !ok {
		return
	}
	taskID, ok := taskParam(w, r)
	if !ok || !h.ensureOwn(w, r, rbac.PermTasksChange, tenantID, projectID, taskID) {
		return
	}
	var body struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	t, replayed, err := h.svc.ChangeStatus(noticeCtx(r), tenantID, projectID, taskID, body.Status, r.Header.Get("Idempotency-Key"))
	if err != nil {
		h.writeErr(w, err, "task not found", "change task status failed")
		return
	}
	if replayed {
		w.Header().Set("Idempotent-Replayed", "true")
	}
	respondJSON(w, http.StatusOK, t)
}

// Reschedule moves a task's dates (a Gantt or calendar drag): {"start_date", "due_date"}, either
// optional, "" clears. Breaking a finish-to-start dependency answers 409 schedule_conflict with
// blocking_task_ids.
func (h *TaskHandler) Reschedule(w http.ResponseWriter, r *http.Request) {
	tenantID, projectID, ok := projectScope(w, r)
	if !ok {
		return
	}
	taskID, ok := taskParam(w, r)
	if !ok || !h.ensureOwn(w, r, rbac.PermTasksChange, tenantID, projectID, taskID) {
		return
	}
	var input tasks.RescheduleInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	t, err := h.svc.Reschedule(r.Context(), tenantID, projectID, taskID, input)
	if err != nil {
		h.writeErr(w, err, "task not found", "reschedule task failed")
		return
	}
	respondJSON(w, http.StatusOK, t)
}

// GetStatuses returns the project's board columns: {"data": [{key, label, category, order}]}.
func (h *TaskHandler) GetStatuses(w http.ResponseWriter, r *http.Request) {
	tenantID, projectID, ok := projectScope(w, r)
	if !ok {
		return
	}
	list, err := h.svc.GetStatuses(r.Context(), tenantID, projectID)
	if err != nil {
		h.writeErr(w, err, "project not found", "get statuses failed")
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": list})
}

// SetStatuses replaces the project's board columns: {"statuses": [...]}.
func (h *TaskHandler) SetStatuses(w http.ResponseWriter, r *http.Request) {
	tenantID, projectID, ok := projectScope(w, r)
	if !ok {
		return
	}
	var body struct {
		Statuses []tasks.Status `json:"statuses"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	list, err := h.svc.SetStatuses(r.Context(), tenantID, projectID, body.Statuses)
	if err != nil {
		h.writeErr(w, err, "project not found", "set statuses failed")
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": list})
}

// BoardCounts returns the task count of every column in one query: {"data": [{status, count}]}.
func (h *TaskHandler) BoardCounts(w http.ResponseWriter, r *http.Request) {
	tenantID, projectID, ok := projectScope(w, r)
	if !ok {
		return
	}
	counts, err := h.svc.BoardCounts(r.Context(), tenantID, projectID, ownOnly(r, rbac.PermTasksView))
	if err != nil {
		h.writeErr(w, err, "project not found", "board counts failed")
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": counts})
}

// location reads ?tz= (an IANA zone such as Africa/Nairobi); UTC when absent.
func location(w http.ResponseWriter, r *http.Request) (*time.Location, bool) {
	tz := strings.TrimSpace(r.URL.Query().Get("tz"))
	if tz == "" {
		return time.UTC, true
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid tz")
		return nil, false
	}
	return loc, true
}

// MyWork returns one page of the caller's open tasks across every project they can see
// (?bucket=overdue|today|week|later, ?due_from, ?due_to, ?tz, ?limit, ?page), soonest due first,
// each with edges.assignees and edges.project {id, name}.
func (h *TaskHandler) MyWork(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := parseTenant(w, r)
	if !ok {
		return
	}
	me := actorID(r)
	if me == uuid.Nil {
		respondError(w, http.StatusBadRequest, "my work needs a signed-in user")
		return
	}
	loc, ok := location(w, r)
	if !ok {
		return
	}
	from, to, ok := dueBounds(w, r)
	if !ok {
		return
	}
	p := pagination.Parse(r)
	items, total, err := h.svc.MyWork(r.Context(), tenantID, me, tasks.MyWorkFilter{
		Bucket: r.URL.Query().Get("bucket"), DueFrom: from, DueTo: to, VisibleTo: accessOf(r).VisibleTo(),
		Now: time.Now(), Location: loc, Limit: p.Limit, Offset: p.Offset,
	})
	if err != nil {
		h.writeErr(w, err, "not found", "my work failed")
		return
	}
	respondJSON(w, http.StatusOK, pagination.NewResponse(items, total, p))
}

// MyWorkCounts returns the caller's open task count per bucket in one query:
// {"data": {"overdue", "today", "week", "later"}} (?tz).
func (h *TaskHandler) MyWorkCounts(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := parseTenant(w, r)
	if !ok {
		return
	}
	me := actorID(r)
	if me == uuid.Nil {
		respondError(w, http.StatusBadRequest, "my work needs a signed-in user")
		return
	}
	loc, ok := location(w, r)
	if !ok {
		return
	}
	counts, err := h.svc.MyWorkCounts(r.Context(), tenantID, me, accessOf(r).VisibleTo(), time.Now(), loc)
	if err != nil {
		serverError(w, h.log, "my work counts failed", err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": counts})
}

// Delete deletes a task.
func (h *TaskHandler) Delete(w http.ResponseWriter, r *http.Request) {
	tenantID, projectID, ok := projectScope(w, r)
	if !ok {
		return
	}
	taskID, ok := taskParam(w, r)
	if !ok {
		return
	}
	if err := h.svc.DeleteTask(r.Context(), tenantID, projectID, taskID); err != nil {
		h.writeErr(w, err, "task not found", "delete task failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// AddDependency adds a dependency between tasks.
func (h *TaskHandler) AddDependency(w http.ResponseWriter, r *http.Request) {
	tenantID, projectID, ok := projectScope(w, r)
	if !ok {
		return
	}
	taskID, ok := taskParam(w, r)
	if !ok {
		return
	}
	var input tasks.AddDependencyInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := h.svc.AddDependency(r.Context(), tenantID, projectID, taskID, input.DependsOnTaskID, input.DependencyType); err != nil {
		h.writeErr(w, err, "task not found", "add dependency failed")
		return
	}
	w.WriteHeader(http.StatusCreated)
}

// RemoveDependency removes a dependency between tasks.
func (h *TaskHandler) RemoveDependency(w http.ResponseWriter, r *http.Request) {
	tenantID, projectID, ok := projectScope(w, r)
	if !ok {
		return
	}
	taskID, ok := taskParam(w, r)
	if !ok {
		return
	}
	depID, err := uuid.Parse(chi.URLParam(r, "depID"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid dependency id")
		return
	}
	if err := h.svc.RemoveDependency(r.Context(), tenantID, projectID, taskID, depID); err != nil {
		h.writeErr(w, err, "dependency not found", "remove dependency failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Gantt returns up to 2000 tasks with dependencies for the Gantt chart, each marked critical when
// it lies on the critical path: {"data": [...], "truncated", "limit": 2000, "critical_days"}.
func (h *TaskHandler) Gantt(w http.ResponseWriter, r *http.Request) {
	tenantID, projectID, ok := projectScope(w, r)
	if !ok {
		return
	}
	data, err := h.svc.GetGanttData(r.Context(), tenantID, projectID)
	if err != nil {
		serverError(w, h.log, "gantt failed", err)
		return
	}
	respondJSON(w, http.StatusOK, data)
}
