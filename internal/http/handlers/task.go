package handlers

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/projects-service/internal/ent"
	"github.com/bengobox/projects-service/internal/http/response"
	"github.com/bengobox/projects-service/internal/services/task"
	"github.com/bengobox/projects-service/internal/shared/validation"
)

// TaskHandler handles task HTTP requests.
type TaskHandler struct {
	logger  *zap.Logger
	service *task.Service
}

// NewTaskHandler creates a new task handler.
func NewTaskHandler(logger *zap.Logger, service *task.Service) *TaskHandler {
	return &TaskHandler{
		logger:  logger,
		service: service,
	}
}

// CreateTaskRequest represents a request to create a task.
type CreateTaskRequest struct {
	Title       string         `json:"title" validate:"required,min=1,max=255"`
	Description string         `json:"description,omitempty" validate:"max=2000"`
	Status      string         `json:"status,omitempty" validate:"omitempty,task_status"`
	Priority    string         `json:"priority,omitempty" validate:"omitempty,priority"`
	AssigneeID  *uuid.UUID     `json:"assignee_id,omitempty"`
	DueDate     *time.Time     `json:"due_date,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

// UpdateTaskRequest represents a request to update a task.
type UpdateTaskRequest struct {
	Title       *string        `json:"title,omitempty" validate:"omitempty,min=1,max=255"`
	Description *string        `json:"description,omitempty" validate:"omitempty,max=2000"`
	Status      *string        `json:"status,omitempty" validate:"omitempty,task_status"`
	Priority    *string        `json:"priority,omitempty" validate:"omitempty,priority"`
	AssigneeID  *uuid.UUID     `json:"assignee_id,omitempty"`
	DueDate     *time.Time     `json:"due_date,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

// TaskResponse represents a task in API responses.
type TaskResponse struct {
	ID          uuid.UUID      `json:"id"`
	TenantID    uuid.UUID      `json:"tenant_id"`
	ProjectID   uuid.UUID      `json:"project_id"`
	Title       string         `json:"title"`
	Description string         `json:"description,omitempty"`
	Status      string         `json:"status"`
	Priority    string         `json:"priority"`
	AssigneeID  *uuid.UUID     `json:"assignee_id,omitempty"`
	DueDate     *time.Time     `json:"due_date,omitempty"`
	CompletedAt *time.Time     `json:"completed_at,omitempty"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

// ListTasksResponse represents a paginated list of tasks.
type ListTasksResponse struct {
	Tasks  []TaskResponse `json:"tasks"`
	Total  int            `json:"total"`
	Limit  int            `json:"limit"`
	Offset int            `json:"offset"`
}

// Create handles POST /projects/{projectID}/tasks
func (h *TaskHandler) Create(w http.ResponseWriter, r *http.Request) {
	tenantID, err := h.getTenantID(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid tenant ID")
		return
	}

	projectID, err := h.getProjectID(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid project ID")
		return
	}

	var req CreateTaskRequest
	if err := validation.BindAndValidate(r, &req); err != nil {
		response.BindError(w, err)
		return
	}

	params := task.CreateParams{
		TenantID:    tenantID,
		ProjectID:   projectID,
		Title:       req.Title,
		Description: req.Description,
		Status:      req.Status,
		Priority:    req.Priority,
		AssigneeID:  req.AssigneeID,
		DueDate:     req.DueDate,
		Metadata:    req.Metadata,
	}

	t, err := h.service.Create(r.Context(), params)
	if err != nil {
		if errors.Is(err, task.ErrProjectNotFound) {
			response.NotFound(w, "project")
			return
		}
		h.logger.Error("failed to create task", zap.Error(err))
		response.InternalError(w)
		return
	}

	response.Created(w, h.toTaskResponse(t))
}

// Get handles GET /projects/{projectID}/tasks/{taskID}
func (h *TaskHandler) Get(w http.ResponseWriter, r *http.Request) {
	tenantID, err := h.getTenantID(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid tenant ID")
		return
	}

	taskID, err := h.getTaskID(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid task ID")
		return
	}

	t, err := h.service.Get(r.Context(), tenantID, taskID)
	if err != nil {
		if errors.Is(err, task.ErrNotFound) {
			response.NotFound(w, "task")
			return
		}
		h.logger.Error("failed to get task", zap.Error(err))
		response.InternalError(w)
		return
	}

	response.OK(w, h.toTaskResponse(t))
}

// List handles GET /projects/{projectID}/tasks
func (h *TaskHandler) List(w http.ResponseWriter, r *http.Request) {
	tenantID, err := h.getTenantID(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid tenant ID")
		return
	}

	projectID, err := h.getProjectID(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid project ID")
		return
	}

	limit := h.parseIntQuery(r, "limit", 20)
	offset := h.parseIntQuery(r, "offset", 0)

	params := task.ListParams{
		TenantID:  tenantID,
		ProjectID: &projectID,
		Status:    r.URL.Query().Get("status"),
		Priority:  r.URL.Query().Get("priority"),
		Limit:     limit,
		Offset:    offset,
	}

	if assigneeIDStr := r.URL.Query().Get("assignee_id"); assigneeIDStr != "" {
		assigneeID, err := uuid.Parse(assigneeIDStr)
		if err == nil {
			params.AssigneeID = &assigneeID
		}
	}

	tasks, total, err := h.service.List(r.Context(), params)
	if err != nil {
		h.logger.Error("failed to list tasks", zap.Error(err))
		response.InternalError(w)
		return
	}

	resp := ListTasksResponse{
		Tasks:  make([]TaskResponse, len(tasks)),
		Total:  total,
		Limit:  limit,
		Offset: offset,
	}

	for i, t := range tasks {
		resp.Tasks[i] = h.toTaskResponse(t)
	}

	response.OK(w, resp)
}

// Update handles PATCH /projects/{projectID}/tasks/{taskID}
func (h *TaskHandler) Update(w http.ResponseWriter, r *http.Request) {
	tenantID, err := h.getTenantID(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid tenant ID")
		return
	}

	taskID, err := h.getTaskID(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid task ID")
		return
	}

	var req UpdateTaskRequest
	if err := validation.BindAndValidate(r, &req); err != nil {
		response.BindError(w, err)
		return
	}

	params := task.UpdateParams{
		Title:       req.Title,
		Description: req.Description,
		Status:      req.Status,
		Priority:    req.Priority,
		AssigneeID:  req.AssigneeID,
		DueDate:     req.DueDate,
		Metadata:    req.Metadata,
	}

	t, err := h.service.Update(r.Context(), tenantID, taskID, params)
	if err != nil {
		if errors.Is(err, task.ErrNotFound) {
			response.NotFound(w, "task")
			return
		}
		h.logger.Error("failed to update task", zap.Error(err))
		response.InternalError(w)
		return
	}

	response.OK(w, h.toTaskResponse(t))
}

// Delete handles DELETE /projects/{projectID}/tasks/{taskID}
func (h *TaskHandler) Delete(w http.ResponseWriter, r *http.Request) {
	tenantID, err := h.getTenantID(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid tenant ID")
		return
	}

	taskID, err := h.getTaskID(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid task ID")
		return
	}

	err = h.service.Delete(r.Context(), tenantID, taskID)
	if err != nil {
		if errors.Is(err, task.ErrNotFound) {
			response.NotFound(w, "task")
			return
		}
		h.logger.Error("failed to delete task", zap.Error(err))
		response.InternalError(w)
		return
	}

	response.NoContent(w)
}

// RegisterRoutes registers task routes on the given router.
// Tasks are nested under projects: /projects/{projectID}/tasks
func (h *TaskHandler) RegisterRoutes(r chi.Router) {
	r.Post("/", h.Create)
	r.Get("/", h.List)
	r.Get("/{taskID}", h.Get)
	r.Patch("/{taskID}", h.Update)
	r.Delete("/{taskID}", h.Delete)
}

// Helper methods

func (h *TaskHandler) getTenantID(r *http.Request) (uuid.UUID, error) {
	tenantIDStr := chi.URLParam(r, "tenantID")
	return uuid.Parse(tenantIDStr)
}

func (h *TaskHandler) getProjectID(r *http.Request) (uuid.UUID, error) {
	projectIDStr := chi.URLParam(r, "projectID")
	return uuid.Parse(projectIDStr)
}

func (h *TaskHandler) getTaskID(r *http.Request) (uuid.UUID, error) {
	taskIDStr := chi.URLParam(r, "taskID")
	return uuid.Parse(taskIDStr)
}

func (h *TaskHandler) parseIntQuery(r *http.Request, key string, defaultVal int) int {
	val := r.URL.Query().Get(key)
	if val == "" {
		return defaultVal
	}
	parsed, err := strconv.Atoi(val)
	if err != nil {
		return defaultVal
	}
	return parsed
}

func (h *TaskHandler) toTaskResponse(t *ent.Task) TaskResponse {
	resp := TaskResponse{
		ID:        t.ID,
		TenantID:  t.TenantID,
		ProjectID: t.ProjectID,
		Title:     t.Title,
		Status:    t.Status,
		Priority:  t.Priority,
		CreatedAt: t.CreatedAt,
		UpdatedAt: t.UpdatedAt,
	}

	if t.Description != "" {
		resp.Description = t.Description
	}
	if t.AssigneeID != uuid.Nil {
		resp.AssigneeID = &t.AssigneeID
	}
	if !t.DueDate.IsZero() {
		resp.DueDate = &t.DueDate
	}
	if !t.CompletedAt.IsZero() {
		resp.CompletedAt = &t.CompletedAt
	}
	if t.Metadata != nil {
		resp.Metadata = t.Metadata
	}

	return resp
}
