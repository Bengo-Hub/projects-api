package handlers

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/projects-service/internal/services/activity"
	"github.com/bengobox/projects-service/internal/services/attachments"
)

// AttachmentHandler serves file links on projects and tasks.
type AttachmentHandler struct {
	log      *zap.Logger
	svc      *attachments.Service
	activity *activity.Recorder
}

// NewAttachmentHandler builds the handler. rec may be nil.
func NewAttachmentHandler(log *zap.Logger, svc *attachments.Service, rec *activity.Recorder) *AttachmentHandler {
	return &AttachmentHandler{log: log.Named("attachment.handler"), svc: svc, activity: rec}
}

// RegisterRoutes registers the attachment routes.
func (h *AttachmentHandler) RegisterRoutes(r chi.Router) {
	r.Get("/projects/{projectID}/attachments", h.ListByProject)
	r.Post("/projects/{projectID}/attachments", h.CreateOnProject)
	r.Delete("/projects/{projectID}/attachments/{attachmentID}", h.Delete)
	r.Get("/projects/{projectID}/tasks/{taskID}/attachments", h.ListByTask)
	r.Post("/projects/{projectID}/tasks/{taskID}/attachments", h.CreateOnTask)
}

func (h *AttachmentHandler) ListByProject(w http.ResponseWriter, r *http.Request) {
	tenantID, projectID, ok := commentProjectParams(w, r)
	if !ok {
		return
	}
	items, err := h.svc.ListByProject(r.Context(), tenantID, projectID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": items, "total": len(items)})
}

func (h *AttachmentHandler) ListByTask(w http.ResponseWriter, r *http.Request) {
	tenantID, projectID, ok := commentProjectParams(w, r)
	if !ok {
		return
	}
	taskID, err := uuid.Parse(chi.URLParam(r, "taskID"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid task id")
		return
	}
	items, err := h.svc.ListByTask(r.Context(), tenantID, projectID, taskID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": items, "total": len(items)})
}

func (h *AttachmentHandler) CreateOnProject(w http.ResponseWriter, r *http.Request) {
	h.create(w, r, false)
}

func (h *AttachmentHandler) CreateOnTask(w http.ResponseWriter, r *http.Request) {
	h.create(w, r, true)
}

func (h *AttachmentHandler) create(w http.ResponseWriter, r *http.Request, onTask bool) {
	tenantID, projectID, ok := commentProjectParams(w, r)
	if !ok {
		return
	}
	var taskID *uuid.UUID
	if onTask {
		id, err := uuid.Parse(chi.URLParam(r, "taskID"))
		if err != nil {
			respondError(w, http.StatusBadRequest, "invalid task id")
			return
		}
		taskID = &id
	}
	var input attachments.CreateInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	input.UploadedBy = actorID(r)
	a, err := h.svc.Create(r.Context(), tenantID, projectID, taskID, input)
	if !h.writeErr(w, err) {
		return
	}
	payload := map[string]any{"file_name": a.FileName, "attachment_id": a.ID.String()}
	if taskID != nil {
		payload["title"] = h.activity.TaskTitle(r.Context(), tenantID, *taskID)
	}
	h.activity.Record(r.Context(), activity.Entry{
		TenantID: tenantID, ProjectID: projectID, TaskID: taskID, UserID: actorID(r),
		Type: "attachment.added", Payload: payload,
	})
	respondJSON(w, http.StatusCreated, a)
}

func (h *AttachmentHandler) Delete(w http.ResponseWriter, r *http.Request) {
	tenantID, projectID, ok := commentProjectParams(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "attachmentID"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid attachment id")
		return
	}
	a, err := h.svc.Delete(r.Context(), tenantID, projectID, id)
	if !h.writeErr(w, err) {
		return
	}
	var taskID *uuid.UUID
	if a.TaskID != uuid.Nil {
		taskID = uuidPtr(a.TaskID)
	}
	h.activity.Record(r.Context(), activity.Entry{
		TenantID: tenantID, ProjectID: projectID, TaskID: taskID, UserID: actorID(r),
		Type: "attachment.removed", Payload: map[string]any{"file_name": a.FileName, "attachment_id": a.ID.String()},
	})
	w.WriteHeader(http.StatusNoContent)
}

// writeErr answers an error and reports whether the caller should continue.
func (h *AttachmentHandler) writeErr(w http.ResponseWriter, err error) bool {
	var ve attachments.ValidationError
	switch {
	case err == nil:
		return true
	case errors.As(err, &ve):
		respondError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, attachments.ErrNotFound):
		respondError(w, http.StatusNotFound, "not found")
	default:
		respondError(w, http.StatusInternalServerError, err.Error())
	}
	return false
}
