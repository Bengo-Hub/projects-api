package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"go.uber.org/zap"

	authclient "github.com/Bengo-Hub/shared-auth-client"
	httpware "github.com/Bengo-Hub/httpware"
	"github.com/bengobox/projects-service/internal/services/projects"
)

// ProjectHandler handles project HTTP endpoints.
type ProjectHandler struct {
	log *zap.Logger
	svc *projects.Service
}

// NewProjectHandler creates a new project handler.
func NewProjectHandler(log *zap.Logger, svc *projects.Service) *ProjectHandler {
	return &ProjectHandler{log: log.Named("project.handler"), svc: svc}
}

// RegisterRoutes registers project routes on the given router.
func (h *ProjectHandler) RegisterRoutes(r chi.Router) {
	r.Route("/projects", func(pr chi.Router) {
		pr.Get("/", h.List)
		pr.Post("/", h.Create)
		pr.Route("/{projectID}", func(p chi.Router) {
			p.Get("/", h.Get)
			p.Put("/", h.Update)
			p.Delete("/", h.Delete)
			p.Get("/summary", h.Summary)
		})
	})
}

// List returns a paginated list of projects.
func (h *ProjectHandler) List(w http.ResponseWriter, r *http.Request) {
	tenantIDStr := httpware.GetTenantID(r.Context())
	tenantID, err := uuid.Parse(tenantIDStr)
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid tenant id")
		return
	}
	filter := projects.ListProjectsFilter{
		Status: r.URL.Query().Get("status"),
	}
	if p := r.URL.Query().Get("page"); p != "" {
		filter.Page, _ = strconv.Atoi(p)
	}
	if ps := r.URL.Query().Get("page_size"); ps != "" {
		filter.PageSize, _ = strconv.Atoi(ps)
	}
	items, total, err := h.svc.ListProjects(r.Context(), tenantID, filter)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": items, "total": total})
}

// Get returns a single project.
func (h *ProjectHandler) Get(w http.ResponseWriter, r *http.Request) {
	tenantID, id, ok := projectParams(w, r)
	if !ok {
		return
	}
	p, err := h.svc.GetProject(r.Context(), tenantID, id)
	if errors.Is(err, projects.ErrNotFound) {
		respondError(w, http.StatusNotFound, "project not found")
		return
	}
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, p)
}

// Create creates a new project.
func (h *ProjectHandler) Create(w http.ResponseWriter, r *http.Request) {
	tenantIDStr := httpware.GetTenantID(r.Context())
	tenantID, err := uuid.Parse(tenantIDStr)
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid tenant id")
		return
	}
	claims, _ := authclient.ClaimsFromContext(r.Context())

	var input projects.CreateProjectInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if claims != nil {
		if ownerID, err := uuid.Parse(claims.Subject); err == nil {
			input.OwnerID = ownerID
		}
	}
	p, err := h.svc.CreateProject(r.Context(), tenantID, input)
	if err != nil {
		if _, ok := err.(projects.ValidationError); ok {
			respondError(w, http.StatusBadRequest, err.Error())
			return
		}
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusCreated, p)
}

// Update updates a project.
func (h *ProjectHandler) Update(w http.ResponseWriter, r *http.Request) {
	tenantID, id, ok := projectParams(w, r)
	if !ok {
		return
	}
	var input projects.UpdateProjectInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	p, err := h.svc.UpdateProject(r.Context(), tenantID, id, input)
	if errors.Is(err, projects.ErrNotFound) {
		respondError(w, http.StatusNotFound, "project not found")
		return
	}
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, p)
}

// Delete deletes a project.
func (h *ProjectHandler) Delete(w http.ResponseWriter, r *http.Request) {
	tenantID, id, ok := projectParams(w, r)
	if !ok {
		return
	}
	if err := h.svc.DeleteProject(r.Context(), tenantID, id); errors.Is(err, projects.ErrNotFound) {
		respondError(w, http.StatusNotFound, "project not found")
		return
	} else if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Summary returns aggregated stats for a project.
func (h *ProjectHandler) Summary(w http.ResponseWriter, r *http.Request) {
	tenantID, id, ok := projectParams(w, r)
	if !ok {
		return
	}
	summary, err := h.svc.GetProjectSummary(r.Context(), tenantID, id)
	if errors.Is(err, projects.ErrNotFound) {
		respondError(w, http.StatusNotFound, "project not found")
		return
	}
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, summary)
}

// projectParams extracts tenantID and projectID from the request.
func projectParams(w http.ResponseWriter, r *http.Request) (uuid.UUID, uuid.UUID, bool) {
	tenantIDStr := httpware.GetTenantID(r.Context())
	tenantID, err := uuid.Parse(tenantIDStr)
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid tenant id")
		return uuid.Nil, uuid.Nil, false
	}
	id, err := uuid.Parse(chi.URLParam(r, "projectID"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid project id")
		return uuid.Nil, uuid.Nil, false
	}
	return tenantID, id, true
}

// respondError writes a JSON error response. Defined here to avoid duplicating the one in health.go.
func respondError(w http.ResponseWriter, status int, msg string) {
	respondJSON(w, status, map[string]string{"error": msg})
}
