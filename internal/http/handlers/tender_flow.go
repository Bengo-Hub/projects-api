package handlers

import (
	"net/http"

	"github.com/google/uuid"

	"github.com/bengobox/projects-service/internal/modules/rbac"
	"github.com/bengobox/projects-service/internal/services/tenders"
)

// The tender lifecycle beyond CRUD (sprints 1 and 2): manual status moves, the evaluation summary
// and go/no-go decision, sections and their review, submissions, the outcome, document uploads and
// the conversion of a won tender into a project.

// tenderBodyLimit bounds a tender lifecycle body (a decision carries at most 20 criteria).
const tenderBodyLimit = 256 << 10

// ChangeStatus makes a manual status move (draft and evaluating, under review, shortlisted,
// cancelled, reopening) with an optional note.
func (h *TenderHandler) ChangeStatus(w http.ResponseWriter, r *http.Request) {
	tenantID, tenderID, ok := tenderParams(w, r)
	if !ok {
		return
	}
	var in tenders.StatusInput
	if !decodeBody(w, r, tenderBodyLimit, &in) {
		return
	}
	t, err := h.svc.ChangeStatus(r.Context(), tenantID, tenderID, in)
	if err != nil {
		h.writeErr(w, err, "tender not found", "change tender status failed")
		return
	}
	respondJSON(w, http.StatusOK, t)
}

// EvaluationSummary returns the committee's scores in one aggregate, with the default go/no-go
// criteria for the decision form.
func (h *TenderHandler) EvaluationSummary(w http.ResponseWriter, r *http.Request) {
	tenantID, tenderID, ok := tenderParams(w, r)
	if !ok {
		return
	}
	out, err := h.svc.EvaluationSummary(r.Context(), tenantID, tenderID)
	if err != nil {
		h.writeErr(w, err, "tender not found", "evaluation summary failed")
		return
	}
	respondJSON(w, http.StatusOK, out)
}

// Decide records the scored go/no-go decision.
func (h *TenderHandler) Decide(w http.ResponseWriter, r *http.Request) {
	tenantID, tenderID, ok := tenderParams(w, r)
	if !ok {
		return
	}
	var in tenders.DecisionInput
	if !decodeBody(w, r, tenderBodyLimit, &in) {
		return
	}
	t, err := h.svc.Decide(r.Context(), tenantID, tenderID, in)
	if err != nil {
		h.writeErr(w, err, "tender not found", "tender decision failed")
		return
	}
	respondJSON(w, http.StatusOK, t)
}

// RecordOutcome records the award or the loss.
func (h *TenderHandler) RecordOutcome(w http.ResponseWriter, r *http.Request) {
	tenantID, tenderID, ok := tenderParams(w, r)
	if !ok {
		return
	}
	var in tenders.OutcomeInput
	if !decodeBody(w, r, tenderBodyLimit, &in) {
		return
	}
	t, err := h.svc.RecordOutcome(r.Context(), tenantID, tenderID, in)
	if err != nil {
		h.writeErr(w, err, "tender not found", "tender outcome failed")
		return
	}
	respondJSON(w, http.StatusOK, t)
}

// Convert turns a won tender into a project (201), or returns the project it already became (200,
// already_converted). A template needs projects.templates.view; the draft treasury budget needs
// projects.financials.manage on a plan with budget_tracking (otherwise the conversion still runs
// and says why the budget was not created).
func (h *TenderHandler) Convert(w http.ResponseWriter, r *http.Request) {
	tenantID, tenderID, ok := tenderParams(w, r)
	if !ok {
		return
	}
	var in tenders.ConvertInput
	if !decodeBody(w, r, tenderBodyLimit, &in) {
		return
	}
	a := accessOf(r)
	if in.TemplateID != nil && *in.TemplateID != uuid.Nil && !a.Has(rbac.PermTemplatesView) {
		respondJSON(w, http.StatusForbidden, map[string]any{"error": "permission_denied", "code": "permission_denied",
			"required_permissions": []string{rbac.PermTemplatesView}})
		return
	}
	res, err := h.svc.Convert(r.Context(), tenantID, tenderID, in, tenders.ConvertAccess{
		BudgetAllowed: a.Has(rbac.PermFinancialsManage) && FeatureOn(r, "budget_tracking"),
		CanSeeProject: a.CanSeeProject,
	})
	if err != nil {
		h.writeErr(w, err, "tender not found", "tender conversion failed")
		return
	}
	status := http.StatusCreated
	if res.AlreadyConverted {
		status = http.StatusOK
	}
	respondJSON(w, status, res)
}

// PushConversionBudget retries the converted project's draft budget in treasury.
func (h *TenderHandler) PushConversionBudget(w http.ResponseWriter, r *http.Request) {
	tenantID, tenderID, ok := tenderParams(w, r)
	if !ok {
		return
	}
	pid, err := h.svc.ConvertedProject(r.Context(), tenantID, tenderID)
	if err != nil {
		h.writeErr(w, err, "tender not found", "budget push failed")
		return
	}
	if pid != uuid.Nil {
		visible, err := accessOf(r).CanSeeProject(r.Context(), pid)
		if err != nil {
			serverError(w, h.log, "check converted project", err)
			return
		}
		if !visible {
			respondError(w, http.StatusNotFound, "project not found")
			return
		}
	}
	state, err := h.svc.PushConversionBudget(r.Context(), tenantID, tenderID)
	if err != nil {
		h.writeErr(w, err, "tender not found", "budget push failed")
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"budget": state})
}

// ListSections lists the tender's sections in order with the summary ({data, summary}).
func (h *TenderHandler) ListSections(w http.ResponseWriter, r *http.Request) {
	tenantID, tenderID, ok := tenderParams(w, r)
	if !ok {
		return
	}
	items, summary, err := h.svc.ListSections(r.Context(), tenantID, tenderID)
	if err != nil {
		h.writeErr(w, err, "tender not found", "list sections failed")
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": items, "summary": summary})
}

// CreateSection adds a section.
func (h *TenderHandler) CreateSection(w http.ResponseWriter, r *http.Request) {
	tenantID, tenderID, ok := tenderParams(w, r)
	if !ok {
		return
	}
	var in tenders.SectionInput
	if !decodeBody(w, r, tenderBodyLimit, &in) {
		return
	}
	sec, err := h.svc.CreateSection(r.Context(), tenantID, tenderID, in)
	if err != nil {
		h.writeErr(w, err, "tender not found", "create section failed")
		return
	}
	respondJSON(w, http.StatusCreated, sec)
}

func sectionParams(w http.ResponseWriter, r *http.Request) (uuid.UUID, uuid.UUID, uuid.UUID, bool) {
	tenantID, tenderID, ok := tenderParams(w, r)
	if !ok {
		return uuid.Nil, uuid.Nil, uuid.Nil, false
	}
	id, ok := uuidPath(w, r, "sectionID", "section id")
	return tenantID, tenderID, id, ok
}

func canManageTenders(r *http.Request) bool { return accessOf(r).Has(rbac.PermTendersManage) }

// UpdateSection edits a section (a tender manager, or its assignee for its status and draft).
func (h *TenderHandler) UpdateSection(w http.ResponseWriter, r *http.Request) {
	tenantID, tenderID, id, ok := sectionParams(w, r)
	if !ok {
		return
	}
	var in tenders.SectionInput
	if !decodeBody(w, r, tenderBodyLimit, &in) {
		return
	}
	sec, err := h.svc.UpdateSection(r.Context(), tenantID, tenderID, id, in, canManageTenders(r))
	if err != nil {
		h.writeErr(w, err, "section not found", "update section failed")
		return
	}
	respondJSON(w, http.StatusOK, sec)
}

// DeleteSection removes a section.
func (h *TenderHandler) DeleteSection(w http.ResponseWriter, r *http.Request) {
	tenantID, tenderID, id, ok := sectionParams(w, r)
	if !ok {
		return
	}
	if err := h.svc.DeleteSection(r.Context(), tenantID, tenderID, id); err != nil {
		h.writeErr(w, err, "section not found", "delete section failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// SubmitSection sends a section for review.
func (h *TenderHandler) SubmitSection(w http.ResponseWriter, r *http.Request) {
	tenantID, tenderID, id, ok := sectionParams(w, r)
	if !ok {
		return
	}
	sec, err := h.svc.SubmitSection(r.Context(), tenantID, tenderID, id, canManageTenders(r))
	if err != nil {
		h.writeErr(w, err, "section not found", "submit section failed")
		return
	}
	respondJSON(w, http.StatusOK, sec)
}

type reviewBody struct {
	Comment string `json:"comment"`
}

func (h *TenderHandler) review(w http.ResponseWriter, r *http.Request, approve bool) {
	tenantID, tenderID, id, ok := sectionParams(w, r)
	if !ok {
		return
	}
	var in reviewBody
	if r.ContentLength != 0 && !decodeBody(w, r, tenderBodyLimit, &in) {
		return
	}
	sec, err := h.svc.ReviewSection(r.Context(), tenantID, tenderID, id, approve, in.Comment, canManageTenders(r))
	if err != nil {
		h.writeErr(w, err, "section not found", "review section failed")
		return
	}
	respondJSON(w, http.StatusOK, sec)
}

// ApproveSection approves a section in review (its reviewer or a tender manager).
func (h *TenderHandler) ApproveSection(w http.ResponseWriter, r *http.Request) { h.review(w, r, true) }

// RequestSectionChanges sends a section back with a comment.
func (h *TenderHandler) RequestSectionChanges(w http.ResponseWriter, r *http.Request) {
	h.review(w, r, false)
}

// ListSubmissions lists the tender's submissions, newest first.
func (h *TenderHandler) ListSubmissions(w http.ResponseWriter, r *http.Request) {
	tenantID, tenderID, ok := tenderParams(w, r)
	if !ok {
		return
	}
	items, err := h.svc.ListSubmissions(r.Context(), tenantID, tenderID)
	if err != nil {
		h.writeErr(w, err, "tender not found", "list submissions failed")
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": items})
}

// Submit records a submission (online, email or physical).
func (h *TenderHandler) Submit(w http.ResponseWriter, r *http.Request) {
	tenantID, tenderID, ok := tenderParams(w, r)
	if !ok {
		return
	}
	var in tenders.SubmissionInput
	if !decodeBody(w, r, tenderBodyLimit, &in) {
		return
	}
	sub, err := h.svc.Submit(r.Context(), tenantID, tenderID, in)
	if err != nil {
		h.writeErr(w, err, "tender not found", "record submission failed")
		return
	}
	respondJSON(w, http.StatusCreated, sub)
}

// RequestUpload hands out a presigned PUT for a tender document.
func (h *TenderHandler) RequestUpload(w http.ResponseWriter, r *http.Request) {
	tenantID, tenderID, ok := tenderParams(w, r)
	if !ok {
		return
	}
	var in tenders.UploadRequest
	if !decodeBody(w, r, tenderBodyLimit, &in) {
		return
	}
	ticket, err := h.svc.RequestUpload(r.Context(), tenantID, tenderID, in)
	if err != nil {
		h.writeErr(w, err, "tender not found", "tender upload request failed")
		return
	}
	respondJSON(w, http.StatusOK, ticket)
}
