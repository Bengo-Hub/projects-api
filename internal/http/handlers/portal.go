package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"

	"github.com/Bengo-Hub/pagination"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/projects-service/internal/http/authz"
	"github.com/bengobox/projects-service/internal/modules/rbac"
	"github.com/bengobox/projects-service/internal/services/attachments"
	"github.com/bengobox/projects-service/internal/services/comments"
	"github.com/bengobox/projects-service/internal/services/milestones"
	"github.com/bengobox/projects-service/internal/services/portal"
	"github.com/bengobox/projects-service/internal/services/tasks"
)

// PortalHandler serves the client portal (sprint 14): the team's client and sharing routes under
// /projects/{projectID}, and the client's own routes under /portal.
type PortalHandler struct {
	log         *zap.Logger
	svc         *portal.Service
	milestones  *milestones.Service
	tasks       *tasks.Service
	comments    *comments.Service
	attachments *attachments.Service
}

// NewPortalHandler creates the portal handler. attachments may be nil (no object storage): file
// routes then answer 503.
func NewPortalHandler(log *zap.Logger, svc *portal.Service, ms *milestones.Service, ts *tasks.Service,
	cs *comments.Service, as *attachments.Service) *PortalHandler {
	return &PortalHandler{log: log.Named("portal.handler"), svc: svc, milestones: ms, tasks: ts, comments: cs, attachments: as}
}

// RegisterRoutes registers every portal route behind the client_portal plan feature. Flat patterns
// on purpose (see CommentHandler.RegisterRoutes).
//
// Team routes go through membership scoping (projectScope): invites, revocation and sharing need
// projects.portal.manage; the client thread needs comments.view or comments.add.
//
// Client routes need projects.portal.view, and each one with a project in its path answers 404
// unless the caller is an active client of that project (clientScope), whatever else they hold.
func (h *PortalHandler) RegisterRoutes(r chi.Router, feature func(http.Handler) http.Handler) {
	manage := authz.Require(rbac.PermPortalManage)
	view := authz.Require(rbac.PermPortalView)
	r.Group(func(g chi.Router) {
		g.Use(feature)
		g.With(manage).Get("/projects/{projectID}/clients", h.ListClients)
		g.With(manage).Post("/projects/{projectID}/clients", h.Invite)
		g.With(manage).Delete("/projects/{projectID}/clients/{memberID}", h.Revoke)
		g.With(manage).Patch("/projects/{projectID}/tasks/{taskID}/sharing", h.ShareTask)
		g.With(manage).Patch("/projects/{projectID}/attachments/{attachmentID}/sharing", h.ShareFile)
		g.With(authz.Require(rbac.PermCommentsView)).Get("/projects/{projectID}/client-thread", h.TeamThread)
		g.With(authz.Require(rbac.PermCommentsAdd)).Post("/projects/{projectID}/client-thread", h.TeamPost)

		g.With(view).Get("/portal/projects", h.MyProjects)
		g.With(view).Get("/portal/projects/{projectID}", h.Summary)
		g.With(view).Get("/portal/projects/{projectID}/milestones", h.Milestones)
		g.With(view).Post("/portal/projects/{projectID}/milestones/{milestoneID}/decision", h.Decide)
		g.With(view).Get("/portal/projects/{projectID}/tasks", h.SharedTasks)
		g.With(view).Get("/portal/projects/{projectID}/files", h.SharedFiles)
		g.With(view).Get("/portal/projects/{projectID}/invoices", h.Invoices)
		g.With(view).Get("/portal/projects/{projectID}/thread", h.ClientThread)
		g.With(view).Post("/portal/projects/{projectID}/thread", h.ClientPost)
	})
}

func (h *PortalHandler) writeErr(w http.ResponseWriter, err error, msg string) {
	var pv portal.ValidationError
	var mv milestones.ValidationError
	var cv comments.ValidationError
	var tv tasks.ValidationError
	var av attachments.ValidationError
	switch {
	case errors.Is(err, portal.ErrNotFound), errors.Is(err, milestones.ErrNotFound), errors.Is(err, comments.ErrNotFound),
		errors.Is(err, tasks.ErrNotFound), errors.Is(err, attachments.ErrNotFound):
		respondError(w, http.StatusNotFound, "not found")
	case errors.Is(err, portal.ErrContactNotFound):
		respondJSON(w, http.StatusNotFound, map[string]string{"error": err.Error(), "code": "contact_not_found"})
	case errors.Is(err, portal.ErrCRMUnavailable):
		respondJSON(w, http.StatusServiceUnavailable, map[string]string{"error": err.Error(), "code": "crm_unavailable"})
	case errors.Is(err, portal.ErrAlreadyClient):
		respondJSON(w, http.StatusConflict, map[string]string{"error": err.Error(), "code": "already_client"})
	case errors.Is(err, milestones.ErrNotReady):
		respondJSON(w, http.StatusConflict, map[string]string{"error": err.Error(), "code": "milestone_not_ready"})
	case errors.Is(err, comments.ErrKeyReused):
		respondJSON(w, http.StatusConflict, map[string]any{"error": err.Error(), "code": "idempotency_key_reused"})
	case errors.As(err, &pv):
		respondJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": pv.Error(), "code": "validation_error"})
	case errors.As(err, &mv), errors.As(err, &cv), errors.As(err, &tv), errors.As(err, &av):
		respondError(w, http.StatusBadRequest, err.Error())
	default:
		serverError(w, h.log, msg, err)
	}
}

// clientScope parses the tenant and the project and checks that the caller is an active client
// of it. Anything else (another project, another tenant, a staff member, a service key) answers
// 404, the same as a project that does not exist.
func (h *PortalHandler) clientScope(w http.ResponseWriter, r *http.Request) (uuid.UUID, uuid.UUID, bool) {
	tenantID, ok := parseTenant(w, r)
	if !ok {
		return uuid.Nil, uuid.Nil, false
	}
	projectID, ok := uuidParam(w, r, "projectID")
	if !ok {
		return uuid.Nil, uuid.Nil, false
	}
	is, err := h.svc.IsClient(r.Context(), tenantID, projectID, actorID(r))
	if err != nil {
		serverError(w, h.log, "portal client check failed", err)
		return uuid.Nil, uuid.Nil, false
	}
	if !is {
		respondError(w, http.StatusNotFound, "project not found")
		return uuid.Nil, uuid.Nil, false
	}
	return tenantID, projectID, true
}

// ---- team routes ----

func (h *PortalHandler) ListClients(w http.ResponseWriter, r *http.Request) {
	tenantID, projectID, ok := projectScope(w, r)
	if !ok {
		return
	}
	list, err := h.svc.ListClients(r.Context(), tenantID, projectID)
	if err != nil {
		serverError(w, h.log, "list clients failed", err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": list})
}

// Invite answers 201 with the new client, or 200 with resent true when the contact was already
// invited and the invite went out again.
func (h *PortalHandler) Invite(w http.ResponseWriter, r *http.Request) {
	tenantID, projectID, ok := projectScope(w, r)
	if !ok {
		return
	}
	var body struct {
		CRMContactID uuid.UUID `json:"crm_contact_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	c, resent, err := h.svc.Invite(noticeCtx(r), tenantID, projectID, body.CRMContactID, actorID(r))
	if err != nil {
		h.writeErr(w, err, "invite client failed")
		return
	}
	status := http.StatusCreated
	if resent {
		status = http.StatusOK
	}
	respondJSON(w, status, map[string]any{"client": c, "resent": resent})
}

func (h *PortalHandler) Revoke(w http.ResponseWriter, r *http.Request) {
	tenantID, projectID, ok := projectScope(w, r)
	if !ok {
		return
	}
	memberID, ok := uuidParam(w, r, "memberID")
	if !ok {
		return
	}
	if err := h.svc.Revoke(r.Context(), tenantID, projectID, memberID); err != nil {
		h.writeErr(w, err, "revoke client failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type sharingBody struct {
	ClientVisible *bool `json:"client_visible"`
}

func decodeSharing(w http.ResponseWriter, r *http.Request) (bool, bool) {
	var body sharingBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ClientVisible == nil {
		respondError(w, http.StatusBadRequest, "client_visible (true or false) is required")
		return false, false
	}
	return *body.ClientVisible, true
}

func (h *PortalHandler) ShareTask(w http.ResponseWriter, r *http.Request) {
	tenantID, projectID, ok := projectScope(w, r)
	if !ok {
		return
	}
	taskID, ok := taskParam(w, r)
	if !ok {
		return
	}
	visible, ok := decodeSharing(w, r)
	if !ok {
		return
	}
	t, err := h.tasks.SetClientVisible(r.Context(), tenantID, projectID, taskID, visible)
	if err != nil {
		h.writeErr(w, err, "share task failed")
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"id": t.ID, "client_visible": tasks.IsClientVisible(t.Metadata)})
}

func (h *PortalHandler) ShareFile(w http.ResponseWriter, r *http.Request) {
	tenantID, projectID, ok := projectScope(w, r)
	if !ok {
		return
	}
	id, ok := uuidParam(w, r, "attachmentID")
	if !ok {
		return
	}
	visible, ok := decodeSharing(w, r)
	if !ok {
		return
	}
	if h.attachments == nil {
		respondJSON(w, http.StatusServiceUnavailable, map[string]string{"error": attachments.ErrUnavailable.Error(), "code": "uploads_unavailable"})
		return
	}
	it, err := h.attachments.SetClientVisible(r.Context(), tenantID, projectID, id, visible)
	if err != nil {
		h.writeErr(w, err, "share file failed")
		return
	}
	respondJSON(w, http.StatusOK, it)
}

func (h *PortalHandler) thread(w http.ResponseWriter, r *http.Request, tenantID, projectID uuid.UUID) {
	p := pagination.Parse(r)
	items, total, err := h.comments.ListClientThread(r.Context(), tenantID, projectID, p.Limit, p.Offset)
	if err != nil {
		serverError(w, h.log, "list client thread failed", err)
		return
	}
	respondJSON(w, http.StatusOK, pagination.NewResponse(items, total, p))
}

func (h *PortalHandler) post(w http.ResponseWriter, r *http.Request, tenantID, projectID uuid.UUID) {
	input, ok := decodeComment(w, r)
	if !ok {
		return
	}
	c, replayed, err := h.comments.CreateClientThreadComment(r.Context(), tenantID, projectID, input)
	if err != nil {
		h.writeErr(w, err, "post to client thread failed")
		return
	}
	if replayed {
		w.Header().Set("Idempotent-Replayed", "true")
		respondJSON(w, http.StatusOK, c)
		return
	}
	respondJSON(w, http.StatusCreated, c)
}

// TeamThread is the client thread for the team (comments.view).
func (h *PortalHandler) TeamThread(w http.ResponseWriter, r *http.Request) {
	if tenantID, projectID, ok := projectScope(w, r); ok {
		h.thread(w, r, tenantID, projectID)
	}
}

// TeamPost posts to the client thread as a teammate (comments.add).
func (h *PortalHandler) TeamPost(w http.ResponseWriter, r *http.Request) {
	if tenantID, projectID, ok := projectScope(w, r); ok {
		h.post(w, r, tenantID, projectID)
	}
}

// ---- client routes ----

// MyProjects lists the projects the caller is a client of. It is the portal's landing call, so it
// first attaches the caller's pending invites: only for a caller holding the client role, signed
// in to this very tenant (the token's tenant is the request's), matched by their SSO email.
func (h *PortalHandler) MyProjects(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := parseTenant(w, r)
	if !ok {
		return
	}
	a := accessOf(r)
	if a.AuthUserID != uuid.Nil && slices.Contains(a.Roles, rbac.RoleClient) && a.Claims != nil &&
		strings.EqualFold(a.Claims.TenantID, tenantID.String()) {
		if _, err := h.svc.Activate(r.Context(), tenantID, a.AuthUserID, a.Email); err != nil {
			if !errors.Is(err, portal.ErrEmailNotVerified) {
				serverError(w, h.log, "activate client invites failed", err)
				return
			}
			// Invites wait for a proven address: still list what the client already has, and tell
			// the portal to ask them to verify their email.
			w.Header().Set("X-Client-Invites", "email_not_verified")
		}
	}
	p := pagination.Parse(r)
	items, total, err := h.svc.ListProjects(r.Context(), tenantID, a.AuthUserID, p.Limit, p.Offset)
	if err != nil {
		serverError(w, h.log, "list client projects failed", err)
		return
	}
	respondJSON(w, http.StatusOK, pagination.NewResponse(items, total, p))
}

func (h *PortalHandler) Summary(w http.ResponseWriter, r *http.Request) {
	tenantID, projectID, ok := h.clientScope(w, r)
	if !ok {
		return
	}
	s, err := h.svc.ProjectSummary(r.Context(), tenantID, projectID)
	if err != nil {
		h.writeErr(w, err, "portal summary failed")
		return
	}
	respondJSON(w, http.StatusOK, s)
}

func (h *PortalHandler) Milestones(w http.ResponseWriter, r *http.Request) {
	tenantID, projectID, ok := h.clientScope(w, r)
	if !ok {
		return
	}
	list, err := h.svc.Milestones(r.Context(), tenantID, projectID)
	if err != nil {
		h.writeErr(w, err, "portal milestones failed")
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": list})
}

// Decide records the client's approve or request_changes on a milestone ready for review.
func (h *PortalHandler) Decide(w http.ResponseWriter, r *http.Request) {
	tenantID, projectID, ok := h.clientScope(w, r)
	if !ok {
		return
	}
	milestoneID, ok := uuidParam(w, r, "milestoneID")
	if !ok {
		return
	}
	var in milestones.DecisionInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	m, err := h.milestones.Decide(noticeCtx(r), tenantID, projectID, milestoneID, actorID(r), in)
	if err != nil {
		h.writeErr(w, err, "milestone decision failed")
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"id": m.ID, "status": m.Status, "decision": in.Decision})
}

func (h *PortalHandler) SharedTasks(w http.ResponseWriter, r *http.Request) {
	tenantID, projectID, ok := h.clientScope(w, r)
	if !ok {
		return
	}
	p := pagination.Parse(r)
	items, total, err := h.svc.SharedTasks(r.Context(), tenantID, projectID, p.Limit, p.Offset)
	if err != nil {
		h.writeErr(w, err, "portal tasks failed")
		return
	}
	respondJSON(w, http.StatusOK, pagination.NewResponse(items, total, p))
}

func (h *PortalHandler) SharedFiles(w http.ResponseWriter, r *http.Request) {
	tenantID, projectID, ok := h.clientScope(w, r)
	if !ok {
		return
	}
	p := pagination.Parse(r)
	if h.attachments == nil {
		respondJSON(w, http.StatusOK, pagination.NewResponse([]attachments.SharedFile{}, 0, p))
		return
	}
	items, total, err := h.attachments.ListShared(r.Context(), tenantID, projectID, p.Limit, p.Offset)
	if err != nil {
		serverError(w, h.log, "portal files failed", err)
		return
	}
	respondJSON(w, http.StatusOK, pagination.NewResponse(items, total, p))
}

// Invoices answers an empty list for now: treasury-api has no service route that lists a project's
// invoices with client-safe fields (its invoices carry project_id in metadata, but the S2S routes
// read one invoice by id or reference only). The portal screen shows the note.
func (h *PortalHandler) Invoices(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := h.clientScope(w, r); !ok {
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{
		"data": []any{}, "available": false,
		"note": "Invoices will appear here once billing is connected to the portal.",
	})
}

func (h *PortalHandler) ClientThread(w http.ResponseWriter, r *http.Request) {
	if tenantID, projectID, ok := h.clientScope(w, r); ok {
		h.thread(w, r, tenantID, projectID)
	}
}

func (h *PortalHandler) ClientPost(w http.ResponseWriter, r *http.Request) {
	if tenantID, projectID, ok := h.clientScope(w, r); ok {
		h.post(w, r, tenantID, projectID)
	}
}
