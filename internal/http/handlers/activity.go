package handlers

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"

	"github.com/bengobox/projects-service/internal/http/authz"
	"github.com/bengobox/projects-service/internal/modules/rbac"
	"github.com/bengobox/projects-service/internal/services/activity"
)

// ActivityHandler serves the project and task activity feeds.
type ActivityHandler struct {
	log *zap.Logger
	svc *activity.Service
}

// NewActivityHandler creates a new activity handler.
func NewActivityHandler(log *zap.Logger, svc *activity.Service) *ActivityHandler {
	return &ActivityHandler{log: log.Named("activity.handler"), svc: svc}
}

// RegisterRoutes registers activity routes. The feed is part of seeing the project.
func (h *ActivityHandler) RegisterRoutes(r chi.Router) {
	view := authz.Require(rbac.PermProjectsView)
	r.With(view).Get("/projects/{projectID}/activities", h.ListByProject)
	r.With(view).Get("/projects/{projectID}/tasks/{taskID}/activities", h.ListByTask)
}

// ListByProject returns the project's latest 100 entries, newest first.
func (h *ActivityHandler) ListByProject(w http.ResponseWriter, r *http.Request) {
	tenantID, projectID, ok := projectScope(w, r)
	if !ok {
		return
	}
	items, err := h.svc.ListByProject(r.Context(), tenantID, projectID)
	if err != nil {
		serverError(w, h.log, "project activity failed", err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": items})
}

// ListByTask returns the task's latest 100 entries, newest first.
func (h *ActivityHandler) ListByTask(w http.ResponseWriter, r *http.Request) {
	tenantID, projectID, ok := projectScope(w, r)
	if !ok {
		return
	}
	taskID, ok := taskParam(w, r)
	if !ok {
		return
	}
	items, err := h.svc.ListByTask(r.Context(), tenantID, projectID, taskID)
	if err != nil {
		serverError(w, h.log, "task activity failed", err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": items})
}
