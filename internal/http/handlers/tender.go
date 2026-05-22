package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"go.uber.org/zap"

	authclient "github.com/Bengo-Hub/shared-auth-client"
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
func (h *TenderHandler) RegisterRoutes(r chi.Router) {
	r.Route("/tenders", func(tr chi.Router) {
		tr.Get("/", h.List)
		tr.Post("/", h.Create)
		tr.Get("/metrics", h.Metrics) // must be before /{id}
		tr.Route("/{id}", func(tid chi.Router) {
			tid.Get("/", h.Get)
			tid.Put("/", h.Update)
			tid.Delete("/", h.Delete)

			tid.Post("/committees", h.CreateCommittee)
			tid.Get("/committees", h.ListCommittees)
			tid.Post("/committees/{committeeID}/members", h.AddCommitteeMember)
			tid.Delete("/committees/{committeeID}/members/{userID}", h.RemoveCommitteeMember)

			tid.Post("/evaluations", h.SubmitEvaluation)
			tid.Get("/evaluations", h.ListEvaluations)

			tid.Post("/meetings", h.ScheduleMeeting)
			tid.Get("/meetings", h.ListMeetings)

			tid.Post("/documents", h.AddDocument)
			tid.Get("/documents", h.ListDocuments)
			tid.Delete("/documents/{docID}", h.DeleteDocument)
		})
	})
}

func (h *TenderHandler) List(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := parseTenant(w, r)
	if !ok {
		return
	}
	filter := tenders.ListTendersFilter{
		Status:   r.URL.Query().Get("status"),
		Priority: r.URL.Query().Get("priority"),
	}
	if p := r.URL.Query().Get("page"); p != "" {
		filter.Page, _ = strconv.Atoi(p)
	}
	if ps := r.URL.Query().Get("page_size"); ps != "" {
		filter.PageSize, _ = strconv.Atoi(ps)
	}
	if db := r.URL.Query().Get("deadline_before"); db != "" {
		if t, err := time.Parse(time.RFC3339, db); err == nil {
			filter.DeadlineBefore = &t
		}
	}
	if da := r.URL.Query().Get("deadline_after"); da != "" {
		if t, err := time.Parse(time.RFC3339, da); err == nil {
			filter.DeadlineAfter = &t
		}
	}
	items, total, err := h.svc.ListTenders(r.Context(), tenantID, filter)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": items, "total": total})
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
	if claims, ok := authclient.ClaimsFromContext(r.Context()); ok {
		if uid, err := uuid.Parse(claims.Subject); err == nil {
			input.CreatedBy = uid
		}
	}
	t, err := h.svc.CreateTender(r.Context(), tenantID, input)
	if err != nil {
		if _, ok := err.(tenders.ValidationError); ok {
			respondError(w, http.StatusBadRequest, err.Error())
			return
		}
		respondError(w, http.StatusInternalServerError, err.Error())
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
		respondError(w, http.StatusInternalServerError, err.Error())
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
	if errors.Is(err, tenders.ErrNotFound) {
		respondError(w, http.StatusNotFound, "tender not found")
		return
	}
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, t)
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
	if errors.Is(err, tenders.ErrNotFound) {
		respondError(w, http.StatusNotFound, "tender not found")
		return
	}
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, t)
}

func (h *TenderHandler) Delete(w http.ResponseWriter, r *http.Request) {
	tenantID, tenderID, ok := tenderParams(w, r)
	if !ok {
		return
	}
	if err := h.svc.DeleteTender(r.Context(), tenantID, tenderID); errors.Is(err, tenders.ErrNotFound) {
		respondError(w, http.StatusNotFound, "tender not found")
		return
	} else if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
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
		respondError(w, http.StatusInternalServerError, err.Error())
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
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": items})
}

func (h *TenderHandler) AddCommitteeMember(w http.ResponseWriter, r *http.Request) {
	tenantID, _, ok := tenderParams(w, r)
	if !ok {
		return
	}
	committeeID, err := uuid.Parse(chi.URLParam(r, "committeeID"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid committee id")
		return
	}
	var input tenders.AddCommitteeMemberInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	m, err := h.svc.AddCommitteeMember(r.Context(), tenantID, committeeID, input)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusCreated, m)
}

func (h *TenderHandler) RemoveCommitteeMember(w http.ResponseWriter, r *http.Request) {
	tenantID, _, ok := tenderParams(w, r)
	if !ok {
		return
	}
	committeeID, err := uuid.Parse(chi.URLParam(r, "committeeID"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid committee id")
		return
	}
	userID, err := uuid.Parse(chi.URLParam(r, "userID"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid user id")
		return
	}
	if err := h.svc.RemoveCommitteeMember(r.Context(), tenantID, committeeID, userID); errors.Is(err, tenders.ErrNotFound) {
		respondError(w, http.StatusNotFound, "member not found")
		return
	} else if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

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
	if claims, ok := authclient.ClaimsFromContext(r.Context()); ok {
		if uid, err := uuid.Parse(claims.Subject); err == nil {
			input.EvaluatorID = uid
		}
	}
	e, err := h.svc.SubmitEvaluation(r.Context(), tenantID, tenderID, input)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
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
		respondError(w, http.StatusInternalServerError, err.Error())
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
	if claims, ok := authclient.ClaimsFromContext(r.Context()); ok {
		if uid, err := uuid.Parse(claims.Subject); err == nil {
			input.CreatedBy = uid
		}
	}
	m, err := h.svc.ScheduleMeeting(r.Context(), tenantID, tenderID, input)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
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
		respondError(w, http.StatusInternalServerError, err.Error())
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
	if claims, ok := authclient.ClaimsFromContext(r.Context()); ok {
		if uid, err := uuid.Parse(claims.Subject); err == nil {
			input.UploadedBy = uid
		}
	}
	doc, err := h.svc.AddDocument(r.Context(), tenantID, tenderID, input)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
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
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": items})
}

func (h *TenderHandler) DeleteDocument(w http.ResponseWriter, r *http.Request) {
	tenantID, _, ok := tenderParams(w, r)
	if !ok {
		return
	}
	docID, err := uuid.Parse(chi.URLParam(r, "docID"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid document id")
		return
	}
	if err := h.svc.DeleteDocument(r.Context(), tenantID, docID); errors.Is(err, tenders.ErrNotFound) {
		respondError(w, http.StatusNotFound, "document not found")
		return
	} else if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func tenderParams(w http.ResponseWriter, r *http.Request) (uuid.UUID, uuid.UUID, bool) {
	tenantID, ok := parseTenant(w, r)
	if !ok {
		return uuid.Nil, uuid.Nil, false
	}
	tenderID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid tender id")
		return uuid.Nil, uuid.Nil, false
	}
	return tenantID, tenderID, true
}
