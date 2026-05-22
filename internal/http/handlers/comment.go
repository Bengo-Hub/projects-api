package handlers

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"go.uber.org/zap"

	authclient "github.com/Bengo-Hub/shared-auth-client"
	httpware "github.com/Bengo-Hub/httpware"
	"github.com/bengobox/projects-service/internal/services/comments"
)

// CommentHandler handles comment HTTP endpoints.
type CommentHandler struct {
	log *zap.Logger
	svc *comments.Service
}

// NewCommentHandler creates a new comment handler.
func NewCommentHandler(log *zap.Logger, svc *comments.Service) *CommentHandler {
	return &CommentHandler{log: log.Named("comment.handler"), svc: svc}
}

// RegisterRoutes registers comment routes.
func (h *CommentHandler) RegisterRoutes(r chi.Router) {
	r.Route("/projects/{projectID}", func(pr chi.Router) {
		pr.Get("/comments", h.ListByProject)
		pr.Post("/comments", h.CreateProjectComment)
		pr.Route("/tasks/{taskID}/comments", func(tr chi.Router) {
			tr.Get("/", h.ListByTask)
			tr.Post("/", h.CreateTaskComment)
		})
		pr.Put("/comments/{commentID}", h.Update)
		pr.Delete("/comments/{commentID}", h.Delete)
	})
}

func (h *CommentHandler) ListByProject(w http.ResponseWriter, r *http.Request) {
	tenantID, projectID, ok := commentProjectParams(w, r)
	if !ok {
		return
	}
	items, err := h.svc.ListCommentsByProject(r.Context(), tenantID, projectID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": items})
}

func (h *CommentHandler) CreateProjectComment(w http.ResponseWriter, r *http.Request) {
	tenantID, projectID, ok := commentProjectParams(w, r)
	if !ok {
		return
	}
	input := extractCommentInput(r)
	c, err := h.svc.CreateProjectComment(r.Context(), tenantID, projectID, input)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusCreated, c)
}

func (h *CommentHandler) ListByTask(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := parseTenant(w, r)
	if !ok {
		return
	}
	taskID, err := uuid.Parse(chi.URLParam(r, "taskID"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid task id")
		return
	}
	items, err := h.svc.ListCommentsByTask(r.Context(), tenantID, taskID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": items})
}

func (h *CommentHandler) CreateTaskComment(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := parseTenant(w, r)
	if !ok {
		return
	}
	taskID, err := uuid.Parse(chi.URLParam(r, "taskID"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid task id")
		return
	}
	input := extractCommentInput(r)
	c, err := h.svc.CreateTaskComment(r.Context(), tenantID, taskID, input)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusCreated, c)
}

func (h *CommentHandler) Update(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := parseTenant(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "commentID"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid comment id")
		return
	}
	var input comments.UpdateCommentInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	c, err := h.svc.UpdateComment(r.Context(), tenantID, id, input)
	if errors.Is(err, comments.ErrNotFound) {
		respondError(w, http.StatusNotFound, "comment not found")
		return
	}
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, c)
}

func (h *CommentHandler) Delete(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := parseTenant(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "commentID"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid comment id")
		return
	}
	if err := h.svc.DeleteComment(r.Context(), tenantID, id); errors.Is(err, comments.ErrNotFound) {
		respondError(w, http.StatusNotFound, "comment not found")
		return
	} else if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// extractCommentInput decodes the request body and fills user_id from claims.
func extractCommentInput(r *http.Request) comments.CreateCommentInput {
	var input comments.CreateCommentInput
	_ = json.NewDecoder(r.Body).Decode(&input)
	if claims, ok := authclient.ClaimsFromContext(r.Context()); ok {
		if uid, err := uuid.Parse(claims.Subject); err == nil {
			input.UserID = uid
		}
	}
	return input
}

func commentProjectParams(w http.ResponseWriter, r *http.Request) (uuid.UUID, uuid.UUID, bool) {
	tenantID, ok := parseTenant(w, r)
	if !ok {
		return uuid.Nil, uuid.Nil, false
	}
	projectID, err := uuid.Parse(chi.URLParam(r, "projectID"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid project id")
		return uuid.Nil, uuid.Nil, false
	}
	return tenantID, projectID, true
}

func parseTenant(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	tenantIDStr := httpware.GetTenantID(r.Context())
	tenantID, err := uuid.Parse(tenantIDStr)
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid tenant id")
		return uuid.Nil, false
	}
	return tenantID, true
}
