package handlers

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/projects-service/internal/http/authz"
	"github.com/bengobox/projects-service/internal/modules/rbac"
	"github.com/bengobox/projects-service/internal/services/milestones"
)

// MilestoneHandler handles milestone HTTP endpoints.
type MilestoneHandler struct {
	log *zap.Logger
	svc *milestones.Service
}

// NewMilestoneHandler creates a new milestone handler.
func NewMilestoneHandler(log *zap.Logger, svc *milestones.Service) *MilestoneHandler {
	return &MilestoneHandler{log: log.Named("milestone.handler"), svc: svc}
}

// RegisterRoutes registers milestone routes.
func (h *MilestoneHandler) RegisterRoutes(r chi.Router) {
	view := authz.Require(rbac.PermMilestonesView)
	r.Route("/projects/{projectID}/milestones", func(mr chi.Router) {
		mr.With(view).Get("/", h.List)
		mr.With(authz.Require(rbac.PermMilestonesAdd)).Post("/", h.Create)
		mr.Route("/{milestoneID}", func(m chi.Router) {
			m.With(view).Get("/", h.Get)
			m.With(authz.Require(rbac.PermMilestonesChange)).Put("/", h.Update)
			m.With(authz.Require(rbac.PermMilestonesDelete)).Delete("/", h.Delete)
		})
	})
}

func milestoneParam(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "milestoneID"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid milestone id")
		return uuid.Nil, false
	}
	return id, true
}

func (h *MilestoneHandler) writeErr(w http.ResponseWriter, err error, notFound, msg string) {
	var verr milestones.ValidationError
	switch {
	case errors.Is(err, milestones.ErrNotFound):
		respondError(w, http.StatusNotFound, notFound)
	case errors.As(err, &verr):
		respondError(w, http.StatusBadRequest, verr.Error())
	default:
		serverError(w, h.log, msg, err)
	}
}

func (h *MilestoneHandler) List(w http.ResponseWriter, r *http.Request) {
	tenantID, projectID, ok := projectScope(w, r)
	if !ok {
		return
	}
	items, err := h.svc.ListMilestones(r.Context(), tenantID, projectID)
	if err != nil {
		serverError(w, h.log, "list milestones failed", err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": items})
}

func (h *MilestoneHandler) Get(w http.ResponseWriter, r *http.Request) {
	tenantID, projectID, ok := projectScope(w, r)
	if !ok {
		return
	}
	id, ok := milestoneParam(w, r)
	if !ok {
		return
	}
	m, err := h.svc.GetMilestone(r.Context(), tenantID, projectID, id)
	if err != nil {
		h.writeErr(w, err, "milestone not found", "get milestone failed")
		return
	}
	respondJSON(w, http.StatusOK, m)
}

func (h *MilestoneHandler) Create(w http.ResponseWriter, r *http.Request) {
	tenantID, projectID, ok := projectScope(w, r)
	if !ok {
		return
	}
	var input milestones.CreateMilestoneInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	m, err := h.svc.CreateMilestone(r.Context(), tenantID, projectID, input)
	if err != nil {
		h.writeErr(w, err, "project not found", "create milestone failed")
		return
	}
	respondJSON(w, http.StatusCreated, m)
}

func (h *MilestoneHandler) Update(w http.ResponseWriter, r *http.Request) {
	tenantID, projectID, ok := projectScope(w, r)
	if !ok {
		return
	}
	id, ok := milestoneParam(w, r)
	if !ok {
		return
	}
	var input milestones.UpdateMilestoneInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	m, err := h.svc.UpdateMilestone(r.Context(), tenantID, projectID, id, input)
	if err != nil {
		h.writeErr(w, err, "milestone not found", "update milestone failed")
		return
	}
	respondJSON(w, http.StatusOK, m)
}

func (h *MilestoneHandler) Delete(w http.ResponseWriter, r *http.Request) {
	tenantID, projectID, ok := projectScope(w, r)
	if !ok {
		return
	}
	id, ok := milestoneParam(w, r)
	if !ok {
		return
	}
	if err := h.svc.DeleteMilestone(r.Context(), tenantID, projectID, id); err != nil {
		h.writeErr(w, err, "milestone not found", "delete milestone failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
