package handlers

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/Bengo-Hub/pagination"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/projects-service/internal/http/authz"
	"github.com/bengobox/projects-service/internal/modules/rbac"
	"github.com/bengobox/projects-service/internal/services/attachments"
)

// AttachmentHandler handles task and project attachments (direct uploads to object storage).
type AttachmentHandler struct {
	log *zap.Logger
	svc *attachments.Service
}

// NewAttachmentHandler creates the attachment handler.
func NewAttachmentHandler(log *zap.Logger, svc *attachments.Service) *AttachmentHandler {
	return &AttachmentHandler{log: log.Named("attachment.handler"), svc: svc}
}

// RegisterRoutes registers the attachment routes, all behind collaboration (the
// team_collaboration plan feature) and gated on the comments codes: view to list, add to upload,
// manage or delete_own to delete.
func (h *AttachmentHandler) RegisterRoutes(r chi.Router, collaboration func(http.Handler) http.Handler) {
	view := authz.Require(rbac.PermCommentsView)
	add := authz.Require(rbac.PermCommentsAdd)
	r.Group(func(g chi.Router) {
		g.Use(collaboration)
		g.With(view).Get("/projects/{projectID}/attachments", h.ListByProject)
		g.With(view).Get("/projects/{projectID}/tasks/{taskID}/attachments", h.ListByTask)
		g.With(add).Post("/projects/{projectID}/attachments/uploads", h.RequestUpload)
		g.With(add).Post("/projects/{projectID}/attachments", h.Confirm)
		g.With(authz.Require(rbac.PermCommentsManage, rbac.PermCommentsDeleteOwn)).Delete("/projects/{projectID}/attachments/{attachmentID}", h.Delete)
	})
}

func (h *AttachmentHandler) writeErr(w http.ResponseWriter, err error, msg string) {
	var verr attachments.ValidationError
	switch {
	case errors.Is(err, attachments.ErrNotFound):
		respondError(w, http.StatusNotFound, "not found")
	case errors.Is(err, attachments.ErrNotOwner):
		authz.Forbidden(w, rbac.PermCommentsManage)
	case errors.Is(err, attachments.ErrUnavailable):
		respondJSON(w, http.StatusServiceUnavailable, map[string]string{"error": err.Error(), "code": "uploads_unavailable"})
	case errors.As(err, &verr):
		respondError(w, http.StatusBadRequest, verr.Error())
	default:
		serverError(w, h.log, msg, err)
	}
}

// ListByProject returns one page of the project's attachments, newest first, each with a signed
// download url (?limit, ?page; ?task_id for one task).
func (h *AttachmentHandler) ListByProject(w http.ResponseWriter, r *http.Request) {
	tenantID, projectID, ok := projectScope(w, r)
	if !ok {
		return
	}
	var taskID *uuid.UUID
	if v := r.URL.Query().Get("task_id"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			respondError(w, http.StatusBadRequest, "invalid task id")
			return
		}
		taskID = &id
	}
	h.list(w, r, tenantID, projectID, taskID)
}

// ListByTask returns one page of a task's attachments.
func (h *AttachmentHandler) ListByTask(w http.ResponseWriter, r *http.Request) {
	tenantID, projectID, ok := projectScope(w, r)
	if !ok {
		return
	}
	taskID, ok := taskParam(w, r)
	if !ok {
		return
	}
	h.list(w, r, tenantID, projectID, &taskID)
}

func (h *AttachmentHandler) list(w http.ResponseWriter, r *http.Request, tenantID, projectID uuid.UUID, taskID *uuid.UUID) {
	p := pagination.Parse(r)
	items, total, err := h.svc.List(r.Context(), tenantID, projectID, taskID, p.Limit, p.Offset)
	if err != nil {
		serverError(w, h.log, "list attachments failed", err)
		return
	}
	respondJSON(w, http.StatusOK, pagination.NewResponse(items, total, p))
}

// RequestUpload answers a presigned PUT for a new file: {file_name, content_type, size, task_id?}
// gives {key, upload: {url, method, headers, expires_at}, max_bytes, allowed_types}. The client
// PUTs the file to upload.url with exactly upload.headers, then confirms the key.
func (h *AttachmentHandler) RequestUpload(w http.ResponseWriter, r *http.Request) {
	tenantID, projectID, ok := projectScope(w, r)
	if !ok {
		return
	}
	var req attachments.UploadRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	ticket, err := h.svc.RequestUpload(r.Context(), tenantID, projectID, req)
	if err != nil {
		h.writeErr(w, err, "request upload failed")
		return
	}
	respondJSON(w, http.StatusOK, ticket)
}

// Confirm records an uploaded file after checking it in storage: {key, file_name, task_id?}.
func (h *AttachmentHandler) Confirm(w http.ResponseWriter, r *http.Request) {
	tenantID, projectID, ok := projectScope(w, r)
	if !ok {
		return
	}
	var in attachments.ConfirmInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	item, err := h.svc.Confirm(r.Context(), tenantID, projectID, actorID(r), in)
	if err != nil {
		h.writeErr(w, err, "confirm upload failed")
		return
	}
	respondJSON(w, http.StatusCreated, item)
}

// Delete removes an attachment: the uploader's own, or any with comments.manage.
func (h *AttachmentHandler) Delete(w http.ResponseWriter, r *http.Request) {
	tenantID, projectID, ok := projectScope(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "attachmentID"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid attachment id")
		return
	}
	if err := h.svc.Delete(r.Context(), tenantID, projectID, id, ownerOnly(r)); err != nil {
		h.writeErr(w, err, "delete attachment failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
