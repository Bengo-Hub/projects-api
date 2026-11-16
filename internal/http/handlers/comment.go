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

// RegisterRoutes registers comment routes, all behind collaboration (the team_collaboration plan
// feature). Flat patterns on purpose: a Route("/projects/{projectID}") subrouter also claims the
// bare /projects/{projectID} path, and chi prefers it over the /projects mount, so getting a
// project, its summary and /projects/metrics all answered 404.
func (h *CommentHandler) RegisterRoutes(r chi.Router, collaboration func(http.Handler) http.Handler) {
	view := authz.Require(rbac.PermCommentsView)
	add := authz.Require(rbac.PermCommentsAdd)
	r.Group(func(g chi.Router) {
		g.Use(collaboration)
		g.With(view).Get("/projects/{projectID}/comments", h.ListByProject)
		g.With(add).Post("/projects/{projectID}/comments", h.CreateProjectComment)
		g.With(view).Get("/projects/{projectID}/tasks/{taskID}/comments", h.ListByTask)
		g.With(add).Post("/projects/{projectID}/tasks/{taskID}/comments", h.CreateTaskComment)
		g.With(authz.Require(rbac.PermCommentsManage, rbac.PermCommentsChangeOwn)).Put("/projects/{projectID}/comments/{commentID}", h.Update)
		g.With(authz.Require(rbac.PermCommentsManage, rbac.PermCommentsDeleteOwn)).Delete("/projects/{projectID}/comments/{commentID}", h.Delete)
	})
}

func (h *CommentHandler) writeErr(w http.ResponseWriter, err error, msg string) {
	var verr comments.ValidationError
	switch {
	case errors.Is(err, comments.ErrNotFound):
		respondError(w, http.StatusNotFound, "not found")
	case errors.Is(err, comments.ErrNotOwner):
		authz.Forbidden(w, rbac.PermCommentsManage)
	case errors.Is(err, comments.ErrKeyReused):
		respondJSON(w, http.StatusConflict, map[string]any{"error": err.Error(), "code": "idempotency_key_reused"})
	case errors.As(err, &verr):
		respondError(w, http.StatusBadRequest, verr.Error())
	default:
		serverError(w, h.log, msg, err)
	}
}

// ListByProject returns one page of the project's comments (?limit, ?page or ?offset).
func (h *CommentHandler) ListByProject(w http.ResponseWriter, r *http.Request) {
	tenantID, projectID, ok := projectScope(w, r)
	if !ok {
		return
	}
	p := pagination.Parse(r)
	items, total, err := h.svc.ListCommentsByProject(r.Context(), tenantID, projectID, p.Limit, p.Offset)
	if err != nil {
		serverError(w, h.log, "list project comments failed", err)
		return
	}
	respondJSON(w, http.StatusOK, pagination.NewResponse(items, total, p))
}

func (h *CommentHandler) CreateProjectComment(w http.ResponseWriter, r *http.Request) {
	tenantID, projectID, ok := projectScope(w, r)
	if !ok {
		return
	}
	input, ok := decodeComment(w, r)
	if !ok {
		return
	}
	c, replayed, err := h.svc.CreateProjectComment(noticeCtx(r), tenantID, projectID, input)
	if err != nil {
		h.writeErr(w, err, "create project comment failed")
		return
	}
	respondCreated(w, c, replayed)
}

// respondCreated answers a new comment 201, and an Idempotency-Key repeat 200 with the comment
// the first request made and Idempotent-Replayed: true.
func respondCreated(w http.ResponseWriter, c *comments.Authored, replayed bool) {
	if replayed {
		w.Header().Set("Idempotent-Replayed", "true")
		respondJSON(w, http.StatusOK, c)
		return
	}
	respondJSON(w, http.StatusCreated, c)
}

// ListByTask returns one page of the task's comments, oldest first (?limit, ?page or ?offset).
func (h *CommentHandler) ListByTask(w http.ResponseWriter, r *http.Request) {
	tenantID, projectID, ok := projectScope(w, r)
	if !ok {
		return
	}
	taskID, ok := taskParam(w, r)
	if !ok {
		return
	}
	p := pagination.Parse(r)
	items, total, err := h.svc.ListCommentsByTask(r.Context(), tenantID, projectID, taskID, p.Limit, p.Offset)
	if err != nil {
		serverError(w, h.log, "list task comments failed", err)
		return
	}
	respondJSON(w, http.StatusOK, pagination.NewResponse(items, total, p))
}

func (h *CommentHandler) CreateTaskComment(w http.ResponseWriter, r *http.Request) {
	tenantID, projectID, ok := projectScope(w, r)
	if !ok {
		return
	}
	taskID, ok := taskParam(w, r)
	if !ok {
		return
	}
	input, ok := decodeComment(w, r)
	if !ok {
		return
	}
	c, replayed, err := h.svc.CreateTaskComment(noticeCtx(r), tenantID, projectID, taskID, input)
	if err != nil {
		h.writeErr(w, err, "create task comment failed")
		return
	}
	respondCreated(w, c, replayed)
}

// ownerOnly limits a comment change to the caller's own comment unless they moderate comments.
func ownerOnly(r *http.Request) *uuid.UUID {
	if accessOf(r).Has(rbac.PermCommentsManage) {
		return nil
	}
	id := actorID(r)
	return &id
}

func commentParam(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "commentID"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid comment id")
		return uuid.Nil, false
	}
	return id, true
}

func (h *CommentHandler) Update(w http.ResponseWriter, r *http.Request) {
	tenantID, projectID, ok := projectScope(w, r)
	if !ok {
		return
	}
	id, ok := commentParam(w, r)
	if !ok {
		return
	}
	var input comments.UpdateCommentInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	c, err := h.svc.UpdateComment(noticeCtx(r), tenantID, projectID, id, input, ownerOnly(r))
	if err != nil {
		h.writeErr(w, err, "update comment failed")
		return
	}
	respondJSON(w, http.StatusOK, c)
}

func (h *CommentHandler) Delete(w http.ResponseWriter, r *http.Request) {
	tenantID, projectID, ok := projectScope(w, r)
	if !ok {
		return
	}
	id, ok := commentParam(w, r)
	if !ok {
		return
	}
	if err := h.svc.DeleteComment(r.Context(), tenantID, projectID, id, ownerOnly(r)); err != nil {
		h.writeErr(w, err, "delete comment failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// decodeComment reads the body and the optional Idempotency-Key header; the author is always the
// signed-in caller.
func decodeComment(w http.ResponseWriter, r *http.Request) (comments.CreateCommentInput, bool) {
	var input comments.CreateCommentInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return input, false
	}
	input.UserID = actorID(r)
	input.IdempotencyKey = r.Header.Get("Idempotency-Key")
	return input, true
}
