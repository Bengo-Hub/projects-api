package handlers

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/Bengo-Hub/pagination"
	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"

	"github.com/bengobox/projects-service/internal/http/authz"
	"github.com/bengobox/projects-service/internal/modules/rbac"
	"github.com/bengobox/projects-service/internal/services/projects"
)

// ProjectHandler handles project HTTP endpoints.
type ProjectHandler struct {
	log         *zap.Logger
	svc         *projects.Service
	contacts    contactSearcher
	costCenters costCenterLister
}

// NewProjectHandler creates a new project handler.
func NewProjectHandler(log *zap.Logger, svc *projects.Service) *ProjectHandler {
	return &ProjectHandler{log: log.Named("project.handler"), svc: svc}
}

// RegisterRoutes registers project routes on the given router.
func (h *ProjectHandler) RegisterRoutes(r chi.Router) {
	view := authz.Require(rbac.PermProjectsView)
	r.Route("/projects", func(pr chi.Router) {
		pr.With(view).Get("/", h.List)
		pr.With(authz.Require(rbac.PermProjectsAdd)).Post("/", h.Create)
		pr.With(view).Get("/metrics", h.Metrics)
		pr.Route("/{projectID}", func(p chi.Router) {
			p.With(view).Get("/", h.Get)
			p.With(authz.Require(rbac.PermProjectsChange)).Put("/", h.Update)
			p.With(authz.Require(rbac.PermProjectsDelete)).Delete("/", h.Delete)
			p.With(view).Get("/summary", h.Summary)
		})
	})
	h.registerLookups(r)
}

// List returns a paginated list of the projects the caller may see.
func (h *ProjectHandler) List(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := parseTenant(w, r)
	if !ok {
		return
	}
	p := pagination.Parse(r)
	filter := projects.ListProjectsFilter{
		Status:    r.URL.Query().Get("status"),
		Limit:     p.Limit,
		Offset:    p.Offset,
		VisibleTo: accessOf(r).VisibleTo(),
	}
	items, total, err := h.svc.ListProjects(r.Context(), tenantID, filter)
	if err != nil {
		serverError(w, h.log, "list projects failed", err)
		return
	}
	respondJSON(w, http.StatusOK, pagination.NewResponse(items, total, p))
}

// Metrics returns project counts by status, grouped in SQL, over the projects the caller may see.
func (h *ProjectHandler) Metrics(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := parseTenant(w, r)
	if !ok {
		return
	}
	m, err := h.svc.GetProjectMetrics(r.Context(), tenantID, accessOf(r).VisibleTo())
	if err != nil {
		serverError(w, h.log, "project metrics failed", err)
		return
	}
	respondJSON(w, http.StatusOK, m)
}

// Get returns a single project.
func (h *ProjectHandler) Get(w http.ResponseWriter, r *http.Request) {
	tenantID, id, ok := projectScope(w, r)
	if !ok {
		return
	}
	p, err := h.svc.GetProject(r.Context(), tenantID, id)
	if errors.Is(err, projects.ErrNotFound) {
		respondError(w, http.StatusNotFound, "project not found")
		return
	}
	if err != nil {
		serverError(w, h.log, "get project failed", err)
		return
	}
	respondJSON(w, http.StatusOK, p)
}

// Create creates a new project owned by the caller.
func (h *ProjectHandler) Create(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := parseTenant(w, r)
	if !ok {
		return
	}
	var input projects.CreateProjectInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	input.OwnerID = actorID(r)
	p, err := h.svc.CreateProject(r.Context(), tenantID, input)
	var verr projects.ValidationError
	if errors.As(err, &verr) {
		respondError(w, http.StatusBadRequest, verr.Error())
		return
	}
	if err != nil {
		serverError(w, h.log, "create project failed", err)
		return
	}
	respondJSON(w, http.StatusCreated, p)
}

// Update updates a project.
func (h *ProjectHandler) Update(w http.ResponseWriter, r *http.Request) {
	tenantID, id, ok := projectScope(w, r)
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
	var verr projects.ValidationError
	if errors.As(err, &verr) {
		respondError(w, http.StatusBadRequest, verr.Error())
		return
	}
	if err != nil {
		serverError(w, h.log, "update project failed", err)
		return
	}
	respondJSON(w, http.StatusOK, p)
}

// Delete deletes a project.
func (h *ProjectHandler) Delete(w http.ResponseWriter, r *http.Request) {
	tenantID, id, ok := projectScope(w, r)
	if !ok {
		return
	}
	if err := h.svc.DeleteProject(r.Context(), tenantID, id); errors.Is(err, projects.ErrNotFound) {
		respondError(w, http.StatusNotFound, "project not found")
		return
	} else if err != nil {
		serverError(w, h.log, "delete project failed", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Summary returns aggregated stats for a project.
func (h *ProjectHandler) Summary(w http.ResponseWriter, r *http.Request) {
	tenantID, id, ok := projectScope(w, r)
	if !ok {
		return
	}
	summary, err := h.svc.GetProjectSummary(r.Context(), tenantID, id)
	if errors.Is(err, projects.ErrNotFound) {
		respondError(w, http.StatusNotFound, "project not found")
		return
	}
	if err != nil {
		serverError(w, h.log, "project summary failed", err)
		return
	}
	respondJSON(w, http.StatusOK, summary)
}
