package handlers

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/bengobox/projects-service/internal/services/tenders"
)

// registerWorkflowRoutes adds the decision, status, outcome, section and submission routes
// (sprint 1, US-1.6 to US-1.13) under /tenders/{id}.
func (h *TenderHandler) registerWorkflowRoutes(tid chi.Router) {
	tid.Get("/evaluation-summary", h.EvaluationSummary)
	tid.Post("/decision", h.Decision)
	tid.Post("/status", h.UpdateStatus)
	tid.Post("/outcome", h.Outcome)

	tid.Get("/sections", h.ListSections)
	tid.Post("/sections", h.CreateSection)
	tid.Put("/sections/{sectionID}", h.UpdateSection)
	tid.Delete("/sections/{sectionID}", h.DeleteSection)
	tid.Post("/sections/{sectionID}/submit", h.SubmitSection)
	tid.Post("/sections/{sectionID}/approve", h.ApproveSection)
	tid.Post("/sections/{sectionID}/request-changes", h.RequestSectionChanges)

	tid.Post("/final-document", h.AddFinalDocument)
	tid.Post("/ready", h.SetReady)

	tid.Post("/submit", h.Submit)
	tid.Get("/submissions", h.ListSubmissions)
}

// tenderError answers a tender service error: 404 not found, 400 invalid input, 409 a status
// change the tender's current status does not allow.
func tenderError(w http.ResponseWriter, err error) {
	var ve tenders.ValidationError
	var te tenders.ErrTransition
	switch {
	case errors.Is(err, tenders.ErrNotFound):
		respondError(w, http.StatusNotFound, "not found")
	case errors.As(err, &ve):
		respondError(w, http.StatusBadRequest, err.Error())
	case errors.As(err, &te):
		respondError(w, http.StatusConflict, err.Error())
	default:
		respondError(w, http.StatusInternalServerError, err.Error())
	}
}

// decode reads a JSON body; an empty body leaves v at its zero value.
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	if r.Body == nil || r.ContentLength == 0 {
		return true
	}
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return false
	}
	return true
}

func sectionParams(w http.ResponseWriter, r *http.Request) (uuid.UUID, uuid.UUID, uuid.UUID, bool) {
	tenantID, tenderID, ok := tenderParams(w, r)
	if !ok {
		return uuid.Nil, uuid.Nil, uuid.Nil, false
	}
	id, err := uuid.Parse(chi.URLParam(r, "sectionID"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid section id")
		return uuid.Nil, uuid.Nil, uuid.Nil, false
	}
	return tenantID, tenderID, id, true
}

func (h *TenderHandler) EvaluationSummary(w http.ResponseWriter, r *http.Request) {
	tenantID, tenderID, ok := tenderParams(w, r)
	if !ok {
		return
	}
	sum, err := h.svc.GetEvaluationSummary(r.Context(), tenantID, tenderID)
	if err != nil {
		tenderError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, sum)
}

func (h *TenderHandler) Decision(w http.ResponseWriter, r *http.Request) {
	tenantID, tenderID, ok := tenderParams(w, r)
	if !ok {
		return
	}
	var input tenders.DecisionInput
	if !decode(w, r, &input) {
		return
	}
	input.DecidedBy = actorID(r)
	t, err := h.svc.MakeDecision(r.Context(), tenantID, tenderID, input)
	if err != nil {
		tenderError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, t)
}

func (h *TenderHandler) UpdateStatus(w http.ResponseWriter, r *http.Request) {
	tenantID, tenderID, ok := tenderParams(w, r)
	if !ok {
		return
	}
	var input tenders.StatusInput
	if !decode(w, r, &input) {
		return
	}
	input.ChangedBy = actorID(r)
	t, err := h.svc.UpdateStatus(r.Context(), tenantID, tenderID, input)
	if err != nil {
		tenderError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, t)
}

func (h *TenderHandler) Outcome(w http.ResponseWriter, r *http.Request) {
	tenantID, tenderID, ok := tenderParams(w, r)
	if !ok {
		return
	}
	var input tenders.OutcomeInput
	if !decode(w, r, &input) {
		return
	}
	input.RecordedBy = actorID(r)
	t, err := h.svc.RecordOutcome(r.Context(), tenantID, tenderID, input)
	if err != nil {
		tenderError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, t)
}

func (h *TenderHandler) ListSections(w http.ResponseWriter, r *http.Request) {
	tenantID, tenderID, ok := tenderParams(w, r)
	if !ok {
		return
	}
	items, sum, err := h.svc.ListSections(r.Context(), tenantID, tenderID)
	if err != nil {
		tenderError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": items, "total": len(items), "summary": sum})
}

func (h *TenderHandler) CreateSection(w http.ResponseWriter, r *http.Request) {
	tenantID, tenderID, ok := tenderParams(w, r)
	if !ok {
		return
	}
	var input tenders.CreateSectionInput
	if !decode(w, r, &input) {
		return
	}
	sec, err := h.svc.CreateSection(r.Context(), tenantID, tenderID, input)
	if err != nil {
		tenderError(w, err)
		return
	}
	respondJSON(w, http.StatusCreated, sec)
}

func (h *TenderHandler) UpdateSection(w http.ResponseWriter, r *http.Request) {
	tenantID, tenderID, id, ok := sectionParams(w, r)
	if !ok {
		return
	}
	var input tenders.UpdateSectionInput
	if !decode(w, r, &input) {
		return
	}
	sec, err := h.svc.UpdateSection(r.Context(), tenantID, tenderID, id, input)
	if err != nil {
		tenderError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, sec)
}

func (h *TenderHandler) DeleteSection(w http.ResponseWriter, r *http.Request) {
	tenantID, tenderID, id, ok := sectionParams(w, r)
	if !ok {
		return
	}
	if err := h.svc.DeleteSection(r.Context(), tenantID, tenderID, id); err != nil {
		tenderError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *TenderHandler) SubmitSection(w http.ResponseWriter, r *http.Request) {
	tenantID, tenderID, id, ok := sectionParams(w, r)
	if !ok {
		return
	}
	var input tenders.SubmitSectionInput
	if !decode(w, r, &input) {
		return
	}
	sec, err := h.svc.SubmitSection(r.Context(), tenantID, tenderID, id, input)
	if err != nil {
		tenderError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, sec)
}

func (h *TenderHandler) ApproveSection(w http.ResponseWriter, r *http.Request) {
	tenantID, tenderID, id, ok := sectionParams(w, r)
	if !ok {
		return
	}
	var input tenders.ReviewInput
	if !decode(w, r, &input) {
		return
	}
	input.ReviewedBy = actorID(r)
	sec, err := h.svc.ApproveSection(r.Context(), tenantID, tenderID, id, input)
	if err != nil {
		tenderError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, sec)
}

func (h *TenderHandler) RequestSectionChanges(w http.ResponseWriter, r *http.Request) {
	tenantID, tenderID, id, ok := sectionParams(w, r)
	if !ok {
		return
	}
	var input tenders.ReviewInput
	if !decode(w, r, &input) {
		return
	}
	input.ReviewedBy = actorID(r)
	sec, err := h.svc.RequestSectionChanges(r.Context(), tenantID, tenderID, id, input)
	if err != nil {
		tenderError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, sec)
}

func (h *TenderHandler) AddFinalDocument(w http.ResponseWriter, r *http.Request) {
	tenantID, tenderID, ok := tenderParams(w, r)
	if !ok {
		return
	}
	var input tenders.FinalDocumentInput
	if !decode(w, r, &input) {
		return
	}
	input.UploadedBy = actorID(r)
	doc, err := h.svc.AddFinalDocument(r.Context(), tenantID, tenderID, input)
	if err != nil {
		tenderError(w, err)
		return
	}
	respondJSON(w, http.StatusCreated, doc)
}

func (h *TenderHandler) SetReady(w http.ResponseWriter, r *http.Request) {
	tenantID, tenderID, ok := tenderParams(w, r)
	if !ok {
		return
	}
	input := struct {
		Ready *bool `json:"ready"`
	}{}
	if !decode(w, r, &input) {
		return
	}
	ready := input.Ready == nil || *input.Ready // an empty body means "mark ready"
	t, err := h.svc.SetReady(r.Context(), tenantID, tenderID, ready)
	if err != nil {
		tenderError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, t)
}

func (h *TenderHandler) Submit(w http.ResponseWriter, r *http.Request) {
	tenantID, tenderID, ok := tenderParams(w, r)
	if !ok {
		return
	}
	var input tenders.SubmitTenderInput
	if !decode(w, r, &input) {
		return
	}
	input.SubmittedBy = actorID(r)
	sub, err := h.svc.SubmitTender(r.Context(), tenantID, tenderID, input)
	if err != nil {
		tenderError(w, err)
		return
	}
	respondJSON(w, http.StatusCreated, sub)
}

func (h *TenderHandler) ListSubmissions(w http.ResponseWriter, r *http.Request) {
	tenantID, tenderID, ok := tenderParams(w, r)
	if !ok {
		return
	}
	items, err := h.svc.ListSubmissions(r.Context(), tenantID, tenderID)
	if err != nil {
		tenderError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": items, "total": len(items)})
}
