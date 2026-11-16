package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

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

// RegisterRoutes registers task routes nested under /projects/{projectID}.
func (h *TaskHandler) RegisterRoutes(r chi.Router) {
	view := authz.Require(rbac.PermTasksView, rbac.PermTasksViewOwn)
	change := authz.Require(rbac.PermTasksChange, rbac.PermTasksChangeOwn)
	r.Route("/projects/{projectID}/tasks", func(tr chi.Router) {
		tr.With(view).Get("/", h.List)
		tr.With(authz.Require(rbac.PermTasksAdd)).Post("/", h.Create)
		tr.Route("/{taskID}", func(t chi.Router) {
			t.With(view).Get("/", h.Get)
			t.With(change).Put("/", h.Update)
			t.With(authz.Require(rbac.PermTasksDelete)).Delete("/", h.Delete)
			t.With(authz.Require(rbac.PermTasksChange)).Post("/dependencies", h.AddDependency)
			t.With(authz.Require(rbac.PermTasksChange)).Delete("/dependencies/{depID}", h.RemoveDependency)
		})
	})
	r.With(authz.Require(rbac.PermTasksView)).Get("/projects/{projectID}/gantt", h.Gantt)
	r.With(authz.Require(rbac.PermTasksView)).Get("/tasks/trend", h.Trend)
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

// List returns one page of a project's tasks (?limit, ?page or ?offset; ?status, ?priority,
// ?assignee_id). A caller limited to their own tasks sees only those assigned to them.
func (h *TaskHandler) List(w http.ResponseWriter, r *http.Request) {
	tenantID, projectID, ok := projectScope(w, r)
	if !ok {
		return
	}
	p := pagination.Parse(r)
	q := r.URL.Query()
	filter := tasks.ListTasksFilter{
		Status:   q.Get("status"),
		Priority: q.Get("priority"),
		Limit:    p.Limit,
		Offset:   p.Offset,
	}
	if a := q.Get("assignee_id"); a != "" {
		id, err := uuid.Parse(a)
		if err != nil {
			respondError(w, http.StatusBadRequest, "invalid assignee id")
			return
		}
		filter.AssigneeID = &id
	}
	if own := ownOnly(r, rbac.PermTasksView); own != nil {
		filter.AssigneeID = own
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

// Get returns a task with its dependencies, its latest 50 comments and its comment count.
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
	switch {
	case errors.Is(err, tasks.ErrNotFound):
		respondError(w, http.StatusNotFound, notFound)
	case errors.Is(err, tasks.ErrCircularDependency):
		respondError(w, http.StatusConflict, err.Error())
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
	t, err := h.svc.CreateTask(r.Context(), tenantID, projectID, input)
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
	t, err := h.svc.UpdateTask(r.Context(), tenantID, projectID, taskID, input)
	if err != nil {
		h.writeErr(w, err, "task not found", "update task failed")
		return
	}
	respondJSON(w, http.StatusOK, t)
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

// Gantt returns up to 2000 tasks with dependencies for the Gantt chart:
// {"data": [...], "truncated": bool, "limit": 2000}.
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
