package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/Bengo-Hub/pagination"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/projects-service/internal/ent"
	"github.com/bengobox/projects-service/internal/http/authz"
	"github.com/bengobox/projects-service/internal/modules/rbac"
	"github.com/bengobox/projects-service/internal/services/tenders"
)

// TenderHandler handles tender management HTTP endpoints.
type TenderHandler struct {
	log *zap.Logger
	svc *tenders.Service
}

// NewTenderHandler creates a new tender handler.
func NewTenderHandler(log *zap.Logger, svc *tenders.Service) *TenderHandler {
	return &TenderHandler{log: log.Named("tender.handler"), svc: svc}
}

// RegisterRoutes registers tender routes. metrics must be before /{id} to avoid chi conflict.
// Committees, evaluations, meetings, documents, sections, the go/no-go decision, submissions, the
// outcome and the conversion are tender management (tenders.manage). A section's assignee and
// reviewer act on it with tenders.view (the service checks who they are). Tenders sit under the
// project_management plan feature (mutations, router layer 2); the conversion's budget retry also
// needs budget (the budget_tracking feature) and projects.financials.manage.
func (h *TenderHandler) RegisterRoutes(r chi.Router, budget func(http.Handler) http.Handler) {
	view := authz.Require(rbac.PermTendersView)
	manage := authz.Require(rbac.PermTendersManage)
	change := authz.Require(rbac.PermTendersChange)
	r.Route("/tenders", func(tr chi.Router) {
		tr.With(view).Get("/", h.List)
		tr.With(authz.Require(rbac.PermTendersAdd)).Post("/", h.Create)
		tr.With(view).Get("/metrics", h.Metrics) // must be before /{id}
		tr.Route("/{id}", func(tid chi.Router) {
			tid.With(view).Get("/", h.Get)
			tid.With(change).Put("/", h.Update)
			tid.With(authz.Require(rbac.PermTendersDelete)).Delete("/", h.Delete)

			tid.With(change).Post("/status", h.ChangeStatus)
			tid.With(view).Get("/evaluation-summary", h.EvaluationSummary)
			tid.With(manage).Post("/decision", h.Decide)
			tid.With(manage).Post("/outcome", h.RecordOutcome)
			tid.With(manage, authz.Require(rbac.PermProjectsAdd)).Post("/convert", h.Convert)
			tid.With(manage, authz.Require(rbac.PermFinancialsManage), budget).Post("/conversion/push-budget", h.PushConversionBudget)

			tid.With(view).Get("/sections", h.ListSections)
			tid.With(manage).Post("/sections", h.CreateSection)
			tid.With(view).Put("/sections/{sectionID}", h.UpdateSection)
			tid.With(manage).Delete("/sections/{sectionID}", h.DeleteSection)
			tid.With(view).Post("/sections/{sectionID}/submit", h.SubmitSection)
			tid.With(view).Post("/sections/{sectionID}/approve", h.ApproveSection)
			tid.With(view).Post("/sections/{sectionID}/request-changes", h.RequestSectionChanges)

			tid.With(view).Get("/submissions", h.ListSubmissions)
			tid.With(manage).Post("/submissions", h.Submit)
			tid.With(manage).Post("/documents/uploads", h.RequestUpload)

			tid.With(manage).Post("/committees", h.CreateCommittee)
			tid.With(view).Get("/committees", h.ListCommittees)
			tid.With(manage).Post("/committees/{committeeID}/members", h.AddCommitteeMember)
			tid.With(manage).Delete("/committees/{committeeID}/members/{userID}", h.RemoveCommitteeMember)

			tid.With(manage).Post("/evaluations", h.SubmitEvaluation)
			tid.With(view).Get("/evaluations", h.ListEvaluations)

			tid.With(manage).Post("/meetings", h.ScheduleMeeting)
			tid.With(view).Get("/meetings", h.ListMeetings)

			tid.With(manage).Post("/documents", h.AddDocument)
			tid.With(view).Get("/documents", h.ListDocuments)
			tid.With(manage).Delete("/documents/{docID}", h.DeleteDocument)
		})
	})
}

func (h *TenderHandler) writeErr(w http.ResponseWriter, err error, notFound, msg string) {
	var verr tenders.ValidationError
	var serr *tenders.StateError
	switch {
	case errors.Is(err, tenders.ErrNotFound):
		respondError(w, http.StatusNotFound, notFound)
	case errors.As(err, &verr):
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": verr.Error(), "code": "validation_error"})
	case errors.As(err, &serr):
		respondJSON(w, http.StatusConflict, map[string]string{"error": serr.Msg, "code": serr.Code})
	case errors.Is(err, tenders.ErrNotYours):
		respondJSON(w, http.StatusForbidden, map[string]string{"error": err.Error(), "code": "not_assignee"})
	case errors.Is(err, tenders.ErrOwnSection):
		respondJSON(w, http.StatusForbidden, map[string]string{"error": err.Error(), "code": "own_section"})
	case errors.Is(err, tenders.ErrNotReviewer):
		respondJSON(w, http.StatusForbidden, map[string]string{"error": err.Error(), "code": "not_reviewer"})
	case errors.Is(err, tenders.ErrUnavailable):
		respondJSON(w, http.StatusServiceUnavailable, map[string]string{"error": err.Error(), "code": "uploads_unavailable"})
	default:
		serverError(w, h.log, msg, err)
	}
}

// List returns one page of the tenant's tenders (?limit, ?page or ?offset; ?status, ?priority,
// ?deadline_before and ?deadline_after as RFC 3339).
func (h *TenderHandler) List(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := parseTenant(w, r)
	if !ok {
		return
	}
	p := pagination.Parse(r)
	q := r.URL.Query()
	filter := tenders.ListTendersFilter{
		Status:   q.Get("status"),
		Priority: q.Get("priority"),
		Q:        q.Get("q"),
		Sort:     q.Get("sort"),
		Limit:    p.Limit,
		Offset:   p.Offset,
	}
	if filter.Sort != "" && !slices.Contains(tenders.SortOrders, filter.Sort) {
		respondError(w, http.StatusBadRequest, "sort must be one of "+strings.Join(tenders.SortOrders, ", "))
		return
	}
	if len(filter.Q) > 200 {
		respondError(w, http.StatusBadRequest, "q is at most 200 characters")
		return
	}
	for key, dst := range map[string]**time.Time{"deadline_before": &filter.DeadlineBefore, "deadline_after": &filter.DeadlineAfter} {
		if v := q.Get(key); v != "" {
			t, err := time.Parse(time.RFC3339, v)
			if err != nil {
				respondError(w, http.StatusBadRequest, "invalid "+key)
				return
			}
			*dst = &t
		}
	}
	items, total, err := h.svc.ListTenders(r.Context(), tenantID, filter)
	if err != nil {
		serverError(w, h.log, "list tenders failed", err)
		return
	}
	respondJSON(w, http.StatusOK, pagination.NewResponse(items, total, p))
}

func (h *TenderHandler) Create(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := parseTenant(w, r)
	if !ok {
		return
	}
	var input tenders.CreateTenderInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	input.CreatedBy = actorID(r)
	t, err := h.svc.CreateTender(r.Context(), tenantID, input)
	if err != nil {
		h.writeErr(w, err, "tender not found", "create tender failed")
		return
	}
	respondJSON(w, http.StatusCreated, t)
}

func (h *TenderHandler) Metrics(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := parseTenant(w, r)
	if !ok {
		return
	}
	metrics, err := h.svc.GetTenderMetrics(r.Context(), tenantID)
	if err != nil {
		serverError(w, h.log, "tender metrics failed", err)
		return
	}
	respondJSON(w, http.StatusOK, metrics)
}

func (h *TenderHandler) Get(w http.ResponseWriter, r *http.Request) {
	tenantID, tenderID, ok := tenderParams(w, r)
	if !ok {
		return
	}
	t, err := h.svc.GetTender(r.Context(), tenantID, tenderID)
	if err != nil {
		h.writeErr(w, err, "tender not found", "get tender failed")
		return
	}
	view := tenderView{Tender: t, AllowedMoves: tenders.ManualMoves(t.Status)}
	if pid := tenders.ConvertedProjectID(t.Metadata); pid != uuid.Nil {
		// Membership scoping: the tender is tenant-wide, its project is not.
		if view.CanOpenProject, err = accessOf(r).CanSeeProject(r.Context(), pid); err != nil {
			serverError(w, h.log, "check converted project", err)
			return
		}
	}
	respondJSON(w, http.StatusOK, view)
}

// tenderView is a tender with what the caller can do next: the manual status moves, and whether
// they may open the project a converted tender became (membership scoping).
type tenderView struct {
	*ent.Tender
	AllowedMoves   []string `json:"allowed_moves"`
	CanOpenProject bool     `json:"can_open_project"`
}

func (h *TenderHandler) Update(w http.ResponseWriter, r *http.Request) {
	tenantID, tenderID, ok := tenderParams(w, r)
	if !ok {
		return
	}
	var input tenders.UpdateTenderInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	t, err := h.svc.UpdateTender(r.Context(), tenantID, tenderID, input)
	if err != nil {
		h.writeErr(w, err, "tender not found", "update tender failed")
		return
	}
	respondJSON(w, http.StatusOK, t)
}

func (h *TenderHandler) Delete(w http.ResponseWriter, r *http.Request) {
	tenantID, tenderID, ok := tenderParams(w, r)
	if !ok {
		return
	}
	if err := h.svc.DeleteTender(r.Context(), tenantID, tenderID); err != nil {
		h.writeErr(w, err, "tender not found", "delete tender failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *TenderHandler) CreateCommittee(w http.ResponseWriter, r *http.Request) {
	tenantID, tenderID, ok := tenderParams(w, r)
	if !ok {
		return
	}
	var input tenders.CreateCommitteeInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	c, err := h.svc.CreateCommittee(r.Context(), tenantID, tenderID, input)
	if err != nil {
		h.writeErr(w, err, "tender not found", "create committee failed")
		return
	}
	respondJSON(w, http.StatusCreated, c)
}

func (h *TenderHandler) ListCommittees(w http.ResponseWriter, r *http.Request) {
	tenantID, tenderID, ok := tenderParams(w, r)
	if !ok {
		return
	}
	items, err := h.svc.ListCommittees(r.Context(), tenantID, tenderID)
	if err != nil {
		serverError(w, h.log, "list committees failed", err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": items})
}

func uuidPath(w http.ResponseWriter, r *http.Request, name, label string) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, name))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid "+label)
		return uuid.Nil, false
	}
	return id, true
}

func (h *TenderHandler) AddCommitteeMember(w http.ResponseWriter, r *http.Request) {
	tenantID, tenderID, ok := tenderParams(w, r)
	if !ok {
		return
	}
	committeeID, ok := uuidPath(w, r, "committeeID", "committee id")
	if !ok {
		return
	}
	var input tenders.AddCommitteeMemberInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	m, err := h.svc.AddCommitteeMember(r.Context(), tenantID, tenderID, committeeID, input)
	if err != nil {
		h.writeErr(w, err, "committee not found", "add committee member failed")
		return
	}
	respondJSON(w, http.StatusCreated, m)
}

func (h *TenderHandler) RemoveCommitteeMember(w http.ResponseWriter, r *http.Request) {
	tenantID, tenderID, ok := tenderParams(w, r)
	if !ok {
		return
	}
	committeeID, ok := uuidPath(w, r, "committeeID", "committee id")
	if !ok {
		return
	}
	userID, ok := uuidPath(w, r, "userID", "user id")
	if !ok {
		return
	}
	if err := h.svc.RemoveCommitteeMember(r.Context(), tenantID, tenderID, committeeID, userID); err != nil {
		h.writeErr(w, err, "member not found", "remove committee member failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// SubmitEvaluation records the caller's evaluation of the tender.
func (h *TenderHandler) SubmitEvaluation(w http.ResponseWriter, r *http.Request) {
	tenantID, tenderID, ok := tenderParams(w, r)
	if !ok {
		return
	}
	var input tenders.SubmitEvaluationInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if actor := actorID(r); actor != uuid.Nil {
		input.EvaluatorID = actor
	}
	e, err := h.svc.SubmitEvaluation(r.Context(), tenantID, tenderID, input)
	if err != nil {
		h.writeErr(w, err, "tender not found", "submit evaluation failed")
		return
	}
	respondJSON(w, http.StatusCreated, e)
}

func (h *TenderHandler) ListEvaluations(w http.ResponseWriter, r *http.Request) {
	tenantID, tenderID, ok := tenderParams(w, r)
	if !ok {
		return
	}
	items, err := h.svc.ListEvaluations(r.Context(), tenantID, tenderID)
	if err != nil {
		serverError(w, h.log, "list evaluations failed", err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": items})
}

func (h *TenderHandler) ScheduleMeeting(w http.ResponseWriter, r *http.Request) {
	tenantID, tenderID, ok := tenderParams(w, r)
	if !ok {
		return
	}
	var input tenders.ScheduleMeetingInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	input.CreatedBy = actorID(r)
	m, err := h.svc.ScheduleMeeting(r.Context(), tenantID, tenderID, input)
	if err != nil {
		h.writeErr(w, err, "tender not found", "schedule meeting failed")
		return
	}
	respondJSON(w, http.StatusCreated, m)
}

func (h *TenderHandler) ListMeetings(w http.ResponseWriter, r *http.Request) {
	tenantID, tenderID, ok := tenderParams(w, r)
	if !ok {
		return
	}
	items, err := h.svc.ListMeetings(r.Context(), tenantID, tenderID)
	if err != nil {
		serverError(w, h.log, "list meetings failed", err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": items})
}

func (h *TenderHandler) AddDocument(w http.ResponseWriter, r *http.Request) {
	tenantID, tenderID, ok := tenderParams(w, r)
	if !ok {
		return
	}
	var input tenders.AddDocumentInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	input.UploadedBy = actorID(r)
	doc, err := h.svc.AddDocument(r.Context(), tenantID, tenderID, input)
	if err != nil {
		h.writeErr(w, err, "tender not found", "add document failed")
		return
	}
	respondJSON(w, http.StatusCreated, doc)
}

func (h *TenderHandler) ListDocuments(w http.ResponseWriter, r *http.Request) {
	tenantID, tenderID, ok := tenderParams(w, r)
	if !ok {
		return
	}
	items, err := h.svc.ListDocuments(r.Context(), tenantID, tenderID)
	if err != nil {
		serverError(w, h.log, "list documents failed", err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": items})
}

func (h *TenderHandler) DeleteDocument(w http.ResponseWriter, r *http.Request) {
	tenantID, tenderID, ok := tenderParams(w, r)
	if !ok {
		return
	}
	docID, ok := uuidPath(w, r, "docID", "document id")
	if !ok {
		return
	}
	if err := h.svc.DeleteDocument(r.Context(), tenantID, tenderID, docID); err != nil {
		h.writeErr(w, err, "document not found", "delete document failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func tenderParams(w http.ResponseWriter, r *http.Request) (uuid.UUID, uuid.UUID, bool) {
	tenantID, ok := parseTenant(w, r)
	if !ok {
		return uuid.Nil, uuid.Nil, false
	}
	tenderID, ok := uuidPath(w, r, "id", "tender id")
	if !ok {
		return uuid.Nil, uuid.Nil, false
	}
	return tenantID, tenderID, true
}
