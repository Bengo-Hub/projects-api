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
	manage := authz.Require(rbac.PermMembersManage)
	r.Route("/projects/{projectID}/members", func(mr chi.Router) {
		mr.With(authz.Require(rbac.PermMembersView)).Get("/", h.List)
		mr.With(manage).Post("/", h.Add)
		mr.With(manage).Put("/{userID}", h.UpdateRole)
		mr.With(manage).Delete("/{userID}", h.Remove)
	})
}

func (h *MemberHandler) writeErr(w http.ResponseWriter, err error, msg string) {
	var verr members.ValidationError
	switch {
	case errors.Is(err, members.ErrNotFound):
		respondError(w, http.StatusNotFound, "member not found")
	case errors.Is(err, members.ErrAlreadyMember):
		respondError(w, http.StatusConflict, err.Error())
	case errors.As(err, &verr):
		respondError(w, http.StatusBadRequest, verr.Error())
	default:
		serverError(w, h.log, msg, err)
	}
}

func userParam(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "userID"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid user id")
		return uuid.Nil, false
	}
	return id, true
}

func (h *MemberHandler) List(w http.ResponseWriter, r *http.Request) {
	tenantID, projectID, ok := projectScope(w, r)
	if !ok {
		return
	}
	items, err := h.svc.ListMembers(r.Context(), tenantID, projectID)
	if err != nil {
		serverError(w, h.log, "list members failed", err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": items})
}

func (h *MemberHandler) Add(w http.ResponseWriter, r *http.Request) {
	tenantID, projectID, ok := projectScope(w, r)
	if !ok {
		return
	}
	var input members.AddMemberInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	m, err := h.svc.AddMember(r.Context(), tenantID, projectID, input)
	if err != nil {
		h.writeErr(w, err, "add member failed")
		return
	}
	respondJSON(w, http.StatusCreated, m)
}

func (h *MemberHandler) UpdateRole(w http.ResponseWriter, r *http.Request) {
	tenantID, projectID, ok := projectScope(w, r)
	if !ok {
		return
	}
	userID, ok := userParam(w, r)
	if !ok {
		return
	}
	var input members.UpdateMemberInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	m, err := h.svc.UpdateMemberRole(r.Context(), tenantID, projectID, userID, input)
	if err != nil {
		h.writeErr(w, err, "update member failed")
		return
	}
	respondJSON(w, http.StatusOK, m)
}

func (h *MemberHandler) Remove(w http.ResponseWriter, r *http.Request) {
	tenantID, projectID, ok := projectScope(w, r)
	if !ok {
		return
	}
	userID, ok := userParam(w, r)
	if !ok {
		return
	}
	if err := h.svc.RemoveMember(r.Context(), tenantID, projectID, userID); err != nil {
		h.writeErr(w, err, "remove member failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
