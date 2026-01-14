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
	"github.com/bengobox/projects-service/internal/services/project"
	"github.com/bengobox/projects-service/internal/shared/validation"
)

// ProjectHandler handles project HTTP requests.
type ProjectHandler struct {
	logger  *zap.Logger
	service *project.Service
}

// NewProjectHandler creates a new project handler.
func NewProjectHandler(logger *zap.Logger, service *project.Service) *ProjectHandler {
	return &ProjectHandler{
		logger:  logger,
		service: service,
	}
}

// CreateProjectRequest represents a request to create a project.
type CreateProjectRequest struct {
	Name        string         `json:"name" validate:"required,min=1,max=255"`
	Description string         `json:"description,omitempty" validate:"max=2000"`
	Status      string         `json:"status,omitempty" validate:"omitempty,project_status"`
	StartDate   *time.Time     `json:"start_date,omitempty"`
	EndDate     *time.Time     `json:"end_date,omitempty"`
	Budget      *float64       `json:"budget,omitempty" validate:"omitempty,gte=0"`
	Currency    string         `json:"currency,omitempty" validate:"omitempty,len=3"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

// UpdateProjectRequest represents a request to update a project.
type UpdateProjectRequest struct {
	Name        *string        `json:"name,omitempty" validate:"omitempty,min=1,max=255"`
	Description *string        `json:"description,omitempty" validate:"omitempty,max=2000"`
	Status      *string        `json:"status,omitempty" validate:"omitempty,project_status"`
	StartDate   *time.Time     `json:"start_date,omitempty"`
	EndDate     *time.Time     `json:"end_date,omitempty"`
	Budget      *float64       `json:"budget,omitempty" validate:"omitempty,gte=0"`
	Currency    *string        `json:"currency,omitempty" validate:"omitempty,len=3"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

// ProjectResponse represents a project in API responses.
type ProjectResponse struct {
	ID          uuid.UUID      `json:"id"`
	TenantID    uuid.UUID      `json:"tenant_id"`
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Status      string         `json:"status"`
	StartDate   *time.Time     `json:"start_date,omitempty"`
	EndDate     *time.Time     `json:"end_date,omitempty"`
	Budget      *float64       `json:"budget,omitempty"`
	Currency    string         `json:"currency"`
	OwnerID     uuid.UUID      `json:"owner_id"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

// ListProjectsResponse represents a paginated list of projects.
type ListProjectsResponse struct {
	Projects []ProjectResponse `json:"projects"`
	Total    int               `json:"total"`
	Limit    int               `json:"limit"`
	Offset   int               `json:"offset"`
}

// Create handles POST /projects
func (h *ProjectHandler) Create(w http.ResponseWriter, r *http.Request) {
	tenantID, err := h.getTenantID(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid tenant ID")
		return
	}

	userID, err := h.getUserID(r)
	if err != nil {
		response.Unauthorized(w)
		return
	}

	var req CreateProjectRequest
	if err := validation.BindAndValidate(r, &req); err != nil {
		response.BindError(w, err)
		return
	}

	params := project.CreateParams{
		TenantID:    tenantID,
		Name:        req.Name,
		Description: req.Description,
		Status:      req.Status,
		StartDate:   req.StartDate,
		EndDate:     req.EndDate,
		Budget:      req.Budget,
		Currency:    req.Currency,
		OwnerID:     userID,
		Metadata:    req.Metadata,
	}

	p, err := h.service.Create(r.Context(), params)
	if err != nil {
		h.logger.Error("failed to create project", zap.Error(err))
		response.InternalError(w)
		return
	}

	response.Created(w, h.toProjectResponse(p))
}

// Get handles GET /projects/{projectID}
func (h *ProjectHandler) Get(w http.ResponseWriter, r *http.Request) {
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

	p, err := h.service.Get(r.Context(), tenantID, projectID)
	if err != nil {
		if errors.Is(err, project.ErrNotFound) {
			response.NotFound(w, "project")
			return
		}
		h.logger.Error("failed to get project", zap.Error(err))
		response.InternalError(w)
		return
	}

	response.OK(w, h.toProjectResponse(p))
}

// List handles GET /projects
func (h *ProjectHandler) List(w http.ResponseWriter, r *http.Request) {
	tenantID, err := h.getTenantID(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid tenant ID")
		return
	}

	params := project.ListParams{
		TenantID: tenantID,
		Status:   r.URL.Query().Get("status"),
		Limit:    h.parseIntQuery(r, "limit", 20),
		Offset:   h.parseIntQuery(r, "offset", 0),
	}

	if ownerIDStr := r.URL.Query().Get("owner_id"); ownerIDStr != "" {
		ownerID, err := uuid.Parse(ownerIDStr)
		if err == nil {
			params.OwnerID = &ownerID
		}
	}

	projects, total, err := h.service.List(r.Context(), params)
	if err != nil {
		h.logger.Error("failed to list projects", zap.Error(err))
		response.InternalError(w)
		return
	}

	resp := ListProjectsResponse{
		Projects: make([]ProjectResponse, len(projects)),
		Total:    total,
		Limit:    params.Limit,
		Offset:   params.Offset,
	}

	for i, p := range projects {
		resp.Projects[i] = h.toProjectResponse(p)
	}

	response.OK(w, resp)
}

// Update handles PATCH /projects/{projectID}
func (h *ProjectHandler) Update(w http.ResponseWriter, r *http.Request) {
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

	var req UpdateProjectRequest
	if err := validation.BindAndValidate(r, &req); err != nil {
		response.BindError(w, err)
		return
	}

	params := project.UpdateParams{
		Name:        req.Name,
		Description: req.Description,
		Status:      req.Status,
		StartDate:   req.StartDate,
		EndDate:     req.EndDate,
		Budget:      req.Budget,
		Currency:    req.Currency,
		Metadata:    req.Metadata,
	}

	p, err := h.service.Update(r.Context(), tenantID, projectID, params)
	if err != nil {
		if errors.Is(err, project.ErrNotFound) {
			response.NotFound(w, "project")
			return
		}
		h.logger.Error("failed to update project", zap.Error(err))
		response.InternalError(w)
		return
	}

	response.OK(w, h.toProjectResponse(p))
}

// Delete handles DELETE /projects/{projectID}
func (h *ProjectHandler) Delete(w http.ResponseWriter, r *http.Request) {
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

	err = h.service.Delete(r.Context(), tenantID, projectID)
	if err != nil {
		if errors.Is(err, project.ErrNotFound) {
			response.NotFound(w, "project")
			return
		}
		h.logger.Error("failed to delete project", zap.Error(err))
		response.InternalError(w)
		return
	}

	response.NoContent(w)
}

// RegisterRoutes registers project routes on the given router.
func (h *ProjectHandler) RegisterRoutes(r chi.Router) {
	r.Post("/", h.Create)
	r.Get("/", h.List)
	r.Get("/{projectID}", h.Get)
	r.Patch("/{projectID}", h.Update)
	r.Delete("/{projectID}", h.Delete)
}

// Helper methods

func (h *ProjectHandler) getTenantID(r *http.Request) (uuid.UUID, error) {
	tenantIDStr := chi.URLParam(r, "tenantID")
	return uuid.Parse(tenantIDStr)
}

func (h *ProjectHandler) getProjectID(r *http.Request) (uuid.UUID, error) {
	projectIDStr := chi.URLParam(r, "projectID")
	return uuid.Parse(projectIDStr)
}

func (h *ProjectHandler) getUserID(r *http.Request) (uuid.UUID, error) {
	// Extract user ID from auth context
	// This assumes the auth middleware sets the user ID in context
	userIDStr := r.Header.Get("X-User-ID")
	if userIDStr == "" {
		// Fallback: try to get from context (set by auth middleware)
		if userID, ok := r.Context().Value("user_id").(uuid.UUID); ok {
			return userID, nil
		}
		return uuid.Nil, errors.New("user ID not found")
	}
	return uuid.Parse(userIDStr)
}

func (h *ProjectHandler) parseIntQuery(r *http.Request, key string, defaultVal int) int {
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

func (h *ProjectHandler) toProjectResponse(p *ent.Project) ProjectResponse {
	resp := ProjectResponse{
		ID:        p.ID,
		TenantID:  p.TenantID,
		Name:      p.Name,
		Status:    p.Status,
		Currency:  p.Currency,
		OwnerID:   p.OwnerID,
		CreatedAt: p.CreatedAt,
		UpdatedAt: p.UpdatedAt,
	}

	if p.Description != "" {
		resp.Description = p.Description
	}
	if !p.StartDate.IsZero() {
		resp.StartDate = &p.StartDate
	}
	if !p.EndDate.IsZero() {
		resp.EndDate = &p.EndDate
	}
	if p.Budget != 0 {
		resp.Budget = &p.Budget
	}
	if p.Metadata != nil {
		resp.Metadata = p.Metadata
	}

	return resp
}
