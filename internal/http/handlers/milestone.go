package handlers

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"go.uber.org/zap"

	httpware "github.com/Bengo-Hub/httpware"
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
	r.Route("/projects/{projectID}/milestones", func(mr chi.Router) {
		mr.Get("/", h.List)
		mr.Post("/", h.Create)
		mr.Route("/{milestoneID}", func(m chi.Router) {
			m.Get("/", h.Get)
			m.Put("/", h.Update)
			m.Delete("/", h.Delete)
		})
	})
}

func (h *MilestoneHandler) List(w http.ResponseWriter, r *http.Request) {
	tenantID, projectID, ok := milestoneProjectParams(w, r)
	if !ok {
		return
	}
	items, err := h.svc.ListMilestones(r.Context(), tenantID, projectID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": items})
}

func (h *MilestoneHandler) Get(w http.ResponseWriter, r *http.Request) {
	tenantID, projectID, ok := milestoneProjectParams(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "milestoneID"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid milestone id")
		return
	}
	m, err := h.svc.GetMilestone(r.Context(), tenantID, projectID, id)
	if errors.Is(err, milestones.ErrNotFound) {
		respondError(w, http.StatusNotFound, "milestone not found")
		return
	}
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, m)
}

func (h *MilestoneHandler) Create(w http.ResponseWriter, r *http.Request) {
	tenantID, projectID, ok := milestoneProjectParams(w, r)
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
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusCreated, m)
}

func (h *MilestoneHandler) Update(w http.ResponseWriter, r *http.Request) {
	tenantID, projectID, ok := milestoneProjectParams(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "milestoneID"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid milestone id")
		return
	}
	var input milestones.UpdateMilestoneInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	m, err := h.svc.UpdateMilestone(r.Context(), tenantID, projectID, id, input)
	if errors.Is(err, milestones.ErrNotFound) {
		respondError(w, http.StatusNotFound, "milestone not found")
		return
	}
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, m)
}

func (h *MilestoneHandler) Delete(w http.ResponseWriter, r *http.Request) {
	tenantID, projectID, ok := milestoneProjectParams(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "milestoneID"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid milestone id")
		return
	}
	if err := h.svc.DeleteMilestone(r.Context(), tenantID, projectID, id); errors.Is(err, milestones.ErrNotFound) {
		respondError(w, http.StatusNotFound, "milestone not found")
		return
	} else if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func milestoneProjectParams(w http.ResponseWriter, r *http.Request) (uuid.UUID, uuid.UUID, bool) {
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
