package handlers

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"go.uber.org/zap"

	httpware "github.com/Bengo-Hub/httpware"
	"github.com/bengobox/projects-service/internal/services/members"
)

// MemberHandler handles project member HTTP endpoints.
type MemberHandler struct {
	log *zap.Logger
	svc *members.Service
}

// NewMemberHandler creates a new member handler.
func NewMemberHandler(log *zap.Logger, svc *members.Service) *MemberHandler {
	return &MemberHandler{log: log.Named("member.handler"), svc: svc}
}

// RegisterRoutes registers member routes.
func (h *MemberHandler) RegisterRoutes(r chi.Router) {
	r.Route("/projects/{projectID}/members", func(mr chi.Router) {
		mr.Get("/", h.List)
		mr.Post("/", h.Add)
		mr.Put("/{userID}", h.UpdateRole)
		mr.Delete("/{userID}", h.Remove)
	})
}

func (h *MemberHandler) List(w http.ResponseWriter, r *http.Request) {
	tenantID, projectID, ok := memberProjectParams(w, r)
	if !ok {
		return
	}
	items, err := h.svc.ListMembers(r.Context(), tenantID, projectID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": items})
}

func (h *MemberHandler) Add(w http.ResponseWriter, r *http.Request) {
	tenantID, projectID, ok := memberProjectParams(w, r)
	if !ok {
		return
	}
	var input members.AddMemberInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	m, err := h.svc.AddMember(r.Context(), tenantID, projectID, input)
	if errors.Is(err, members.ErrAlreadyMember) {
		respondError(w, http.StatusConflict, err.Error())
		return
	}
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusCreated, m)
}

func (h *MemberHandler) UpdateRole(w http.ResponseWriter, r *http.Request) {
	tenantID, projectID, ok := memberProjectParams(w, r)
	if !ok {
		return
	}
	userID, err := uuid.Parse(chi.URLParam(r, "userID"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid user id")
		return
	}
	var input members.UpdateMemberInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	m, err := h.svc.UpdateMemberRole(r.Context(), tenantID, projectID, userID, input)
	if errors.Is(err, members.ErrNotFound) {
		respondError(w, http.StatusNotFound, "member not found")
		return
	}
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, m)
}

func (h *MemberHandler) Remove(w http.ResponseWriter, r *http.Request) {
	tenantID, projectID, ok := memberProjectParams(w, r)
	if !ok {
		return
	}
	userID, err := uuid.Parse(chi.URLParam(r, "userID"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid user id")
		return
	}
	if err := h.svc.RemoveMember(r.Context(), tenantID, projectID, userID); errors.Is(err, members.ErrNotFound) {
		respondError(w, http.StatusNotFound, "member not found")
		return
	} else if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func memberProjectParams(w http.ResponseWriter, r *http.Request) (uuid.UUID, uuid.UUID, bool) {
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
