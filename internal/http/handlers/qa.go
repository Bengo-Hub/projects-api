package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/Bengo-Hub/pagination"
	authclient "github.com/Bengo-Hub/shared-auth-client"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/projects-service/internal/platform/catalog"
	"github.com/bengobox/projects-service/internal/services/qa"
)

// QAHandler serves the QA and acceptance routes for a tenant's own team, under /{tenantID}/qa.
type QAHandler struct {
	log *zap.Logger
	svc *qa.Service
}

func NewQAHandler(log *zap.Logger, svc *qa.Service) *QAHandler {
	return &QAHandler{log: log.Named("qa.handler"), svc: svc}
}

// RegisterRoutes mounts the routes with their permission gates.
func (h *QAHandler) RegisterRoutes(r chi.Router) {
	view, run, manage := requireQA(PermQAView), requireQA(PermQARun), requireQA(PermQAManage)
	r.Route("/qa", func(q chi.Router) {
		q.With(view).Get("/catalog/features", h.CatalogFeatures)

		q.With(view).Get("/templates", h.ListTemplates)
		q.With(manage).Post("/templates", h.CreateTemplate)
		q.With(view).Get("/templates/{templateID}", h.GetTemplate)
		q.With(manage).Patch("/templates/{templateID}", h.UpdateTemplate)
		q.With(manage).Post("/templates/{templateID}/criteria", h.AddCriteria)
		q.With(manage).Put("/templates/{templateID}/criteria/{criterionID}", h.UpdateCriterion)
		q.With(manage).Delete("/templates/{templateID}/criteria/{criterionID}", h.DeleteCriterion)

		q.With(view).Get("/sessions", h.ListSessions)
		q.With(manage).Post("/sessions", h.CreateSession)
		q.With(view).Get("/sessions/{sessionID}", h.GetSession)
		q.With(view).Get("/sessions/{sessionID}/outcome", h.Outcome)
		q.With(manage).Post("/sessions/{sessionID}/open", h.OpenSession)
		q.With(manage).Post("/sessions/{sessionID}/close", h.CloseSession)
		q.With(manage).Post("/sessions/{sessionID}/decision", h.Decide)
		q.With(view).Get("/sessions/{sessionID}/invites", h.ListInvites)
		q.With(manage).Post("/sessions/{sessionID}/invites", h.Invite)
		q.With(manage).Delete("/sessions/{sessionID}/invites/{inviteID}", h.RevokeInvite)
		q.With(view).Get("/sessions/{sessionID}/results", h.ListResults)
		q.With(run).Post("/sessions/{sessionID}/results", h.RecordResult)
		q.With(view).Get("/sessions/{sessionID}/defects", h.ListDefects)
		q.With(manage).Post("/sessions/{sessionID}/defects", h.RaiseDefect)
		q.With(view).Get("/sessions/{sessionID}/report.pdf", h.Report)
	})
}

const qaBodyLimit = 64 << 10

func decodeBody(w http.ResponseWriter, r *http.Request, limit int64, v any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, limit)).Decode(v); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return false
	}
	return true
}

func uuidParam(w http.ResponseWriter, r *http.Request, name string) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, name))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid "+name)
		return uuid.Nil, false
	}
	return id, true
}

func (h *QAHandler) CatalogFeatures(w http.ResponseWriter, r *http.Request) {
	a, ok := qaActor(w, r)
	if !ok {
		return
	}
	feats, err := h.svc.Catalog().Features(r.Context(), a.TenantID, r.URL.Query().Get("service"))
	if errors.Is(err, catalog.ErrUnavailable) {
		respondJSON(w, http.StatusOK, map[string]any{"data": []catalog.Feature{}, "available": false})
		return
	}
	if err != nil {
		h.log.Warn("catalogue read failed", zap.Error(err))
		respondError(w, http.StatusBadGateway, "feature catalogue unavailable")
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": feats, "available": true})
}

func (h *QAHandler) ListTemplates(w http.ResponseWriter, r *http.Request) {
	a, ok := qaActor(w, r)
	if !ok {
		return
	}
	p := pagination.Parse(r)
	items, total, err := h.svc.ListTemplates(r.Context(), a, qa.TemplateFilter{
		ServiceTag: r.URL.Query().Get("service_tag"), Status: r.URL.Query().Get("status"), Limit: p.Limit, Offset: p.Offset,
	})
	if err != nil {
		qaError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, pagination.NewResponse(items, total, p))
}

func (h *QAHandler) CreateTemplate(w http.ResponseWriter, r *http.Request) {
	a, ok := qaActor(w, r)
	if !ok {
		return
	}
	var in qa.TemplateInput
	if !decodeBody(w, r, qaBodyLimit, &in) {
		return
	}
	t, err := h.svc.CreateTemplate(r.Context(), a, in)
	if err != nil {
		qaError(w, err)
		return
	}
	respondJSON(w, http.StatusCreated, t)
}

func (h *QAHandler) GetTemplate(w http.ResponseWriter, r *http.Request) {
	a, ok := qaActor(w, r)
	if !ok {
		return
	}
	id, ok := uuidParam(w, r, "templateID")
	if !ok {
		return
	}
	t, err := h.svc.GetTemplate(r.Context(), a, id)
	if err != nil {
		qaError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, t)
}

func (h *QAHandler) UpdateTemplate(w http.ResponseWriter, r *http.Request) {
	a, ok := qaActor(w, r)
	if !ok {
		return
	}
	id, ok := uuidParam(w, r, "templateID")
	if !ok {
		return
	}
	var in qa.TemplatePatch
	if !decodeBody(w, r, qaBodyLimit, &in) {
		return
	}
	t, err := h.svc.UpdateTemplate(r.Context(), a, id, in)
	if err != nil {
		qaError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, t)
}

// AddCriteria takes {"criteria": [...]}; it is also the import path for a whole test plan.
func (h *QAHandler) AddCriteria(w http.ResponseWriter, r *http.Request) {
	a, ok := qaActor(w, r)
	if !ok {
		return
	}
	id, ok := uuidParam(w, r, "templateID")
	if !ok {
		return
	}
	var in struct {
		Criteria []qa.CriterionInput `json:"criteria"`
	}
	if !decodeBody(w, r, 4<<20, &in) {
		return
	}
	out, err := h.svc.AddCriteria(r.Context(), a, id, in.Criteria)
	if err != nil {
		qaError(w, err)
		return
	}
	respondJSON(w, http.StatusCreated, map[string]any{"data": out})
}

func (h *QAHandler) UpdateCriterion(w http.ResponseWriter, r *http.Request) {
	a, ok := qaActor(w, r)
	if !ok {
		return
	}
	tid, ok := uuidParam(w, r, "templateID")
	if !ok {
		return
	}
	cid, ok := uuidParam(w, r, "criterionID")
	if !ok {
		return
	}
	var in qa.CriterionInput
	if !decodeBody(w, r, qaBodyLimit, &in) {
		return
	}
	c, err := h.svc.UpdateCriterion(r.Context(), a, tid, cid, in)
	if err != nil {
		qaError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, c)
}

func (h *QAHandler) DeleteCriterion(w http.ResponseWriter, r *http.Request) {
	a, ok := qaActor(w, r)
	if !ok {
		return
	}
	tid, ok := uuidParam(w, r, "templateID")
	if !ok {
		return
	}
	cid, ok := uuidParam(w, r, "criterionID")
	if !ok {
		return
	}
	if err := h.svc.DeleteCriterion(r.Context(), a, tid, cid); err != nil {
		qaError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *QAHandler) ListSessions(w http.ResponseWriter, r *http.Request) {
	a, ok := qaActor(w, r)
	if !ok {
		return
	}
	p := pagination.Parse(r)
	items, total, err := h.svc.ListSessions(r.Context(), a.TenantID, qa.SessionFilter{Status: r.URL.Query().Get("status"), Limit: p.Limit, Offset: p.Offset})
	if err != nil {
		qaError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, pagination.NewResponse(items, total, p))
}

func (h *QAHandler) CreateSession(w http.ResponseWriter, r *http.Request) {
	a, ok := qaActor(w, r)
	if !ok {
		return
	}
	var in qa.SessionInput
	if !decodeBody(w, r, qaBodyLimit, &in) {
		return
	}
	if claims, ok := authclient.ClaimsFromContext(r.Context()); ok && claims != nil {
		in.TenantSlug = claims.GetTenantSlug()
	}
	s, err := h.svc.CreateSession(r.Context(), a, in)
	if err != nil {
		qaError(w, err)
		return
	}
	respondJSON(w, http.StatusCreated, s)
}

// sessionAction runs a tenant-scoped action on {sessionID}.
func (h *QAHandler) sessionAction(w http.ResponseWriter, r *http.Request, fn func(a qa.Actor, id uuid.UUID) (any, error)) {
	a, ok := qaActor(w, r)
	if !ok {
		return
	}
	id, ok := uuidParam(w, r, "sessionID")
	if !ok {
		return
	}
	out, err := fn(a, id)
	if err != nil {
		qaError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, out)
}

func (h *QAHandler) GetSession(w http.ResponseWriter, r *http.Request) {
	h.sessionAction(w, r, func(a qa.Actor, id uuid.UUID) (any, error) { return h.svc.GetSession(r.Context(), a.TenantID, id) })
}

func (h *QAHandler) Outcome(w http.ResponseWriter, r *http.Request) {
	h.sessionAction(w, r, func(a qa.Actor, id uuid.UUID) (any, error) { return h.svc.Outcome(r.Context(), a.TenantID, id) })
}

func (h *QAHandler) OpenSession(w http.ResponseWriter, r *http.Request) {
	h.sessionAction(w, r, func(a qa.Actor, id uuid.UUID) (any, error) { return h.svc.OpenSession(r.Context(), a.TenantID, id) })
}

func (h *QAHandler) CloseSession(w http.ResponseWriter, r *http.Request) {
	h.sessionAction(w, r, func(a qa.Actor, id uuid.UUID) (any, error) { return h.svc.CloseSession(r.Context(), a.TenantID, id) })
}

// Decide takes {"decision": "accepted" | "rejected", "reason": "..."}.
func (h *QAHandler) Decide(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Decision string `json:"decision"`
		Reason   string `json:"reason"`
	}
	if !decodeBody(w, r, qaBodyLimit, &in) {
		return
	}
	if in.Decision != "accepted" && in.Decision != "rejected" {
		respondError(w, http.StatusBadRequest, "decision must be accepted or rejected")
		return
	}
	h.sessionAction(w, r, func(a qa.Actor, id uuid.UUID) (any, error) {
		return h.svc.DecideSession(r.Context(), a.TenantID, id, in.Decision == "accepted", in.Reason)
	})
}

func (h *QAHandler) ListInvites(w http.ResponseWriter, r *http.Request) {
	h.sessionAction(w, r, func(a qa.Actor, id uuid.UUID) (any, error) {
		items, err := h.svc.ListInvites(r.Context(), a.TenantID, id)
		return map[string]any{"data": items}, err
	})
}

// Invite takes {"invites": [{"email", "name", "role"}]}. The inviter's name on the notice comes
// from auth-api, never from the request.
func (h *QAHandler) Invite(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Invites []qa.InviteInput `json:"invites"`
	}
	if !decodeBody(w, r, qaBodyLimit, &in) {
		return
	}
	h.sessionAction(w, r, func(a qa.Actor, id uuid.UUID) (any, error) {
		items, err := h.svc.Invite(r.Context(), a, id, in.Invites)
		return map[string]any{"data": items}, err
	})
}

func (h *QAHandler) RevokeInvite(w http.ResponseWriter, r *http.Request) {
	a, ok := qaActor(w, r)
	if !ok {
		return
	}
	sid, ok := uuidParam(w, r, "sessionID")
	if !ok {
		return
	}
	iid, ok := uuidParam(w, r, "inviteID")
	if !ok {
		return
	}
	if err := h.svc.RevokeInvite(r.Context(), a.TenantID, sid, iid); err != nil {
		qaError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *QAHandler) ListResults(w http.ResponseWriter, r *http.Request) {
	history, _ := strconv.ParseBool(r.URL.Query().Get("history"))
	h.sessionAction(w, r, func(a qa.Actor, id uuid.UUID) (any, error) {
		items, err := h.svc.ListResults(r.Context(), a.TenantID, id, history)
		return map[string]any{"data": items}, err
	})
}

// RecordResult records a verdict by a signed-in team member, as themselves.
func (h *QAHandler) RecordResult(w http.ResponseWriter, r *http.Request) {
	var in qa.ResultInput
	if !decodeBody(w, r, qaBodyLimit, &in) {
		return
	}
	h.sessionAction(w, r, func(a qa.Actor, id uuid.UUID) (any, error) {
		if a.Email == "" {
			return nil, qa.ValidationError{Msg: "the token carries no email to record the result under"}
		}
		sess, err := h.svc.GetSession(r.Context(), a.TenantID, id)
		if err != nil {
			return nil, err
		}
		var uid *uuid.UUID
		if a.UserID != uuid.Nil {
			uid = &a.UserID
		}
		tester, err := h.svc.TesterFromEmail(a.Email, uid, nil)
		if err != nil {
			return nil, err
		}
		return h.svc.RecordResult(r.Context(), sess, tester, in)
	})
}

func (h *QAHandler) ListDefects(w http.ResponseWriter, r *http.Request) {
	h.sessionAction(w, r, func(a qa.Actor, id uuid.UUID) (any, error) {
		items, err := h.svc.ListDefects(r.Context(), a.TenantID, id)
		return map[string]any{"data": items}, err
	})
}

// RaiseDefect takes {"criterion_id"}: raises (or retries) the criterion's ticket by hand.
func (h *QAHandler) RaiseDefect(w http.ResponseWriter, r *http.Request) {
	var in struct {
		CriterionID uuid.UUID `json:"criterion_id"`
	}
	if !decodeBody(w, r, qaBodyLimit, &in) {
		return
	}
	h.sessionAction(w, r, func(a qa.Actor, id uuid.UUID) (any, error) {
		return h.svc.RaiseDefect(r.Context(), a.TenantID, id, in.CriterionID)
	})
}

func (h *QAHandler) Report(w http.ResponseWriter, r *http.Request) {
	a, ok := qaActor(w, r)
	if !ok {
		return
	}
	id, ok := uuidParam(w, r, "sessionID")
	if !ok {
		return
	}
	pdf, hash, _, err := h.svc.ReportFor(r.Context(), a.TenantID, id)
	if err != nil {
		qaError(w, err)
		return
	}
	writePDF(w, pdf, hash, "acceptance-"+id.String()+".pdf")
}

// writePDF sends a report inline (the shared PdfPreview shows it) with its SHA-256.
func writePDF(w http.ResponseWriter, pdf []byte, hash, filename string) {
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", `inline; filename="`+filename+`"`)
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Document-SHA256", hash)
	w.Header().Set("Content-Length", strconv.Itoa(len(pdf)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(pdf)
}
