package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"go.uber.org/zap"

	httpware "github.com/Bengo-Hub/httpware"
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
	r.Route("/projects/{projectID}/tasks", func(tr chi.Router) {
		tr.Get("/", h.List)
		tr.Post("/", h.Create)
		tr.Route("/{taskID}", func(t chi.Router) {
			t.Get("/", h.Get)
			t.Put("/", h.Update)
			t.Delete("/", h.Delete)
			t.Post("/dependencies", h.AddDependency)
			t.Delete("/dependencies/{depID}", h.RemoveDependency)
		})
	})
	r.Get("/projects/{projectID}/gantt", h.Gantt)
}

// List returns paginated tasks for a project.
func (h *TaskHandler) List(w http.ResponseWriter, r *http.Request) {
	tenantID, projectID, ok := taskProjectParams(w, r)
	if !ok {
		return
	}
	filter := tasks.ListTasksFilter{
		Status:   r.URL.Query().Get("status"),
		Priority: r.URL.Query().Get("priority"),
	}
	if p := r.URL.Query().Get("page"); p != "" {
		filter.Page, _ = strconv.Atoi(p)
	}
	if ps := r.URL.Query().Get("page_size"); ps != "" {
		filter.PageSize, _ = strconv.Atoi(ps)
	}
	if a := r.URL.Query().Get("assignee_id"); a != "" {
		if id, err := uuid.Parse(a); err == nil {
			filter.AssigneeID = &id
		}
	}
	items, total, err := h.svc.ListTasks(r.Context(), tenantID, projectID, filter)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": items, "total": total})
}

// Get returns a single task.
func (h *TaskHandler) Get(w http.ResponseWriter, r *http.Request) {
	tenantID, projectID, ok := taskProjectParams(w, r)
	if !ok {
		return
	}
	taskID, err := uuid.Parse(chi.URLParam(r, "taskID"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid task id")
		return
	}
	t, err := h.svc.GetTask(r.Context(), tenantID, projectID, taskID)
	if errors.Is(err, tasks.ErrNotFound) {
		respondError(w, http.StatusNotFound, "task not found")
		return
	}
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, t)
}

// Create creates a new task.
func (h *TaskHandler) Create(w http.ResponseWriter, r *http.Request) {
	tenantID, projectID, ok := taskProjectParams(w, r)
	if !ok {
		return
	}
	var input tasks.CreateTaskInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	t, err := h.svc.CreateTask(r.Context(), tenantID, projectID, input)
	if errors.Is(err, tasks.ErrNotFound) {
		respondError(w, http.StatusNotFound, "project not found")
		return
	}
	if err != nil {
		if _, ok := err.(tasks.ValidationError); ok {
			respondError(w, http.StatusBadRequest, err.Error())
			return
		}
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusCreated, t)
}

// Update updates a task.
func (h *TaskHandler) Update(w http.ResponseWriter, r *http.Request) {
	tenantID, projectID, ok := taskProjectParams(w, r)
	if !ok {
		return
	}
	taskID, err := uuid.Parse(chi.URLParam(r, "taskID"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid task id")
		return
	}
	var input tasks.UpdateTaskInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	t, err := h.svc.UpdateTask(r.Context(), tenantID, projectID, taskID, input)
	if errors.Is(err, tasks.ErrNotFound) {
		respondError(w, http.StatusNotFound, "task not found")
		return
	}
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, t)
}

// Delete deletes a task.
func (h *TaskHandler) Delete(w http.ResponseWriter, r *http.Request) {
	tenantID, projectID, ok := taskProjectParams(w, r)
	if !ok {
		return
	}
	taskID, err := uuid.Parse(chi.URLParam(r, "taskID"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid task id")
		return
	}
	if err := h.svc.DeleteTask(r.Context(), tenantID, projectID, taskID); errors.Is(err, tasks.ErrNotFound) {
		respondError(w, http.StatusNotFound, "task not found")
		return
	} else if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// AddDependency adds a dependency between tasks.
func (h *TaskHandler) AddDependency(w http.ResponseWriter, r *http.Request) {
	tenantID, _, ok := taskProjectParams(w, r)
	if !ok {
		return
	}
	taskID, err := uuid.Parse(chi.URLParam(r, "taskID"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid task id")
		return
	}
	var input tasks.AddDependencyInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := h.svc.AddDependency(r.Context(), tenantID, taskID, input.DependsOnTaskID, input.DependencyType); err != nil {
		if errors.Is(err, tasks.ErrCircularDependency) {
			respondError(w, http.StatusConflict, err.Error())
			return
		}
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusCreated)
}

// RemoveDependency removes a dependency between tasks.
func (h *TaskHandler) RemoveDependency(w http.ResponseWriter, r *http.Request) {
	tenantID, _, ok := taskProjectParams(w, r)
	if !ok {
		return
	}
	taskID, err := uuid.Parse(chi.URLParam(r, "taskID"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid task id")
		return
	}
	depID, err := uuid.Parse(chi.URLParam(r, "depID"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid dependency id")
		return
	}
	if err := h.svc.RemoveDependency(r.Context(), tenantID, taskID, depID); errors.Is(err, tasks.ErrNotFound) {
		respondError(w, http.StatusNotFound, "dependency not found")
		return
	} else if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Gantt returns tasks with dependencies for Gantt chart rendering.
func (h *TaskHandler) Gantt(w http.ResponseWriter, r *http.Request) {
	tenantID, projectID, ok := taskProjectParams(w, r)
	if !ok {
		return
	}
	taskData, err := h.svc.GetGanttData(r.Context(), tenantID, projectID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": taskData})
}

// taskProjectParams extracts tenantID and projectID from the request context/URL.
func taskProjectParams(w http.ResponseWriter, r *http.Request) (uuid.UUID, uuid.UUID, bool) {
	tenantIDStr := httpware.GetTenantID(r.Context())
	tenantID, err := uuid.Parse(tenantIDStr)
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid tenant id")
		return uuid.Nil, uuid.Nil, false
	}
	projectID, err := uuid.Parse(chi.URLParam(r, "projectID"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid project id")
		return uuid.Nil, uuid.Nil, false
	}
	return tenantID, projectID, true
}
