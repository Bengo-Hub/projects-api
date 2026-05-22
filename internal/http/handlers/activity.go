package handlers

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"go.uber.org/zap"

	httpware "github.com/Bengo-Hub/httpware"
	"github.com/bengobox/projects-service/internal/ent"
	entactivity "github.com/bengobox/projects-service/internal/ent/activity"
)

// ActivityHandler handles activity read-only endpoints.
type ActivityHandler struct {
	log    *zap.Logger
	client *ent.Client
}

// NewActivityHandler creates a new activity handler.
func NewActivityHandler(log *zap.Logger, client *ent.Client) *ActivityHandler {
	return &ActivityHandler{log: log.Named("activity.handler"), client: client}
}

// RegisterRoutes registers activity routes.
func (h *ActivityHandler) RegisterRoutes(r chi.Router) {
	r.Get("/projects/{projectID}/activities", h.ListByProject)
	r.Get("/projects/{projectID}/tasks/{taskID}/activities", h.ListByTask)
}

func (h *ActivityHandler) ListByProject(w http.ResponseWriter, r *http.Request) {
	tenantIDStr := httpware.GetTenantID(r.Context())
	tenantID, err := uuid.Parse(tenantIDStr)
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid tenant id")
		return
	}
	projectID, err := uuid.Parse(chi.URLParam(r, "projectID"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid project id")
		return
	}
	items, err := h.client.Activity.Query().
		Where(entactivity.TenantID(tenantID), entactivity.ProjectID(projectID)).
		Order(ent.Desc(entactivity.FieldOccurredAt)).
		Limit(100).
		All(r.Context())
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": items})
}

func (h *ActivityHandler) ListByTask(w http.ResponseWriter, r *http.Request) {
	tenantIDStr := httpware.GetTenantID(r.Context())
	tenantID, err := uuid.Parse(tenantIDStr)
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid tenant id")
		return
	}
	taskID, err := uuid.Parse(chi.URLParam(r, "taskID"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid task id")
		return
	}
	items, err := h.client.Activity.Query().
		Where(entactivity.TenantID(tenantID), entactivity.TaskID(taskID)).
		Order(ent.Desc(entactivity.FieldOccurredAt)).
		Limit(100).
		All(r.Context())
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": items})
}
