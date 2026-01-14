package handlers

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/projects-service/internal/ent"
	"github.com/bengobox/projects-service/internal/http/response"
	"github.com/bengobox/projects-service/internal/services/tender"
	"github.com/bengobox/projects-service/internal/shared/validation"
)

// TenderHandler handles tender HTTP requests.
type TenderHandler struct {
	logger  *zap.Logger
	service tender.TenderServiceInterface
}

// NewTenderHandler creates a new tender handler.
func NewTenderHandler(logger *zap.Logger, service tender.TenderServiceInterface) *TenderHandler {
	return &TenderHandler{
		logger:  logger,
		service: service,
	}
}

// Request/Response types

type CreateTenderRequest struct {
	Title                 string         `json:"title" validate:"required,min=1,max=255"`
	Description           string         `json:"description,omitempty" validate:"max=2000"`
	ClientName            string         `json:"client_name" validate:"required,min=1,max=255"`
	ClientContact         string         `json:"client_contact,omitempty" validate:"max=255"`
	ClientEmail           string         `json:"client_email,omitempty" validate:"omitempty,email"`
	Source                string         `json:"source,omitempty" validate:"omitempty,oneof=manual government_portal referral website"`
	SourceURL             string         `json:"source_url,omitempty" validate:"omitempty,url"`
	Priority              string         `json:"priority,omitempty" validate:"omitempty,priority"`
	EstimatedValue        *float64       `json:"estimated_value,omitempty" validate:"omitempty,gte=0"`
	Currency              string         `json:"currency,omitempty" validate:"omitempty,len=3"`
	PublicationDate       *time.Time     `json:"publication_date,omitempty"`
	Deadline              time.Time      `json:"deadline" validate:"required"`
	ClarificationDeadline *time.Time     `json:"clarification_deadline,omitempty"`
	SubmissionMethod      string         `json:"submission_method,omitempty" validate:"omitempty,oneof=email physical portal mixed"`
	SubmissionAddress     string         `json:"submission_address,omitempty"`
	Categories            []string       `json:"categories,omitempty"`
	RequirementsSummary   map[string]any `json:"requirements_summary,omitempty"`
	Metadata              map[string]any `json:"metadata,omitempty"`
}

type UpdateTenderRequest struct {
	Title                 *string        `json:"title,omitempty" validate:"omitempty,min=1,max=255"`
	Description           *string        `json:"description,omitempty" validate:"omitempty,max=2000"`
	ClientName            *string        `json:"client_name,omitempty" validate:"omitempty,min=1,max=255"`
	ClientContact         *string        `json:"client_contact,omitempty"`
	ClientEmail           *string        `json:"client_email,omitempty" validate:"omitempty,email"`
	Source                *string        `json:"source,omitempty" validate:"omitempty,oneof=manual government_portal referral website"`
	SourceURL             *string        `json:"source_url,omitempty" validate:"omitempty,url"`
	Status                *string        `json:"status,omitempty" validate:"omitempty,oneof=identified evaluating preparing submitted under_review shortlisted awarded lost withdrawn"`
	Priority              *string        `json:"priority,omitempty" validate:"omitempty,priority"`
	EstimatedValue        *float64       `json:"estimated_value,omitempty" validate:"omitempty,gte=0"`
	Currency              *string        `json:"currency,omitempty" validate:"omitempty,len=3"`
	PublicationDate       *time.Time     `json:"publication_date,omitempty"`
	Deadline              *time.Time     `json:"deadline,omitempty"`
	ClarificationDeadline *time.Time     `json:"clarification_deadline,omitempty"`
	SubmissionMethod      *string        `json:"submission_method,omitempty" validate:"omitempty,oneof=email physical portal mixed"`
	SubmissionAddress     *string        `json:"submission_address,omitempty"`
	Categories            []string       `json:"categories,omitempty"`
	RequirementsSummary   map[string]any `json:"requirements_summary,omitempty"`
	Metadata              map[string]any `json:"metadata,omitempty"`
}

type TenderDecisionRequest struct {
	Decision  string `json:"decision" validate:"required,oneof=go no_go"`
	Rationale string `json:"rationale,omitempty" validate:"max=2000"`
}

type TenderResponse struct {
	ID                    uuid.UUID      `json:"id"`
	TenantID              uuid.UUID      `json:"tenant_id"`
	TenderNumber          string         `json:"tender_number"`
	Title                 string         `json:"title"`
	Description           string         `json:"description,omitempty"`
	ClientName            string         `json:"client_name"`
	ClientContact         string         `json:"client_contact,omitempty"`
	ClientEmail           string         `json:"client_email,omitempty"`
	Source                string         `json:"source"`
	SourceURL             string         `json:"source_url,omitempty"`
	Status                string         `json:"status"`
	Priority              string         `json:"priority"`
	EstimatedValue        *float64       `json:"estimated_value,omitempty"`
	Currency              string         `json:"currency"`
	PublicationDate       *time.Time     `json:"publication_date,omitempty"`
	Deadline              time.Time      `json:"deadline"`
	ClarificationDeadline *time.Time     `json:"clarification_deadline,omitempty"`
	SubmissionMethod      string         `json:"submission_method,omitempty"`
	SubmissionAddress     string         `json:"submission_address,omitempty"`
	Categories            []string       `json:"categories,omitempty"`
	RequirementsSummary   map[string]any `json:"requirements_summary,omitempty"`
	Decision              string         `json:"decision,omitempty"`
	DecisionRationale     string         `json:"decision_rationale,omitempty"`
	DecisionDate          *time.Time     `json:"decision_date,omitempty"`
	DecidedBy             *uuid.UUID     `json:"decided_by,omitempty"`
	ProjectID             *uuid.UUID     `json:"project_id,omitempty"`
	CreatedBy             uuid.UUID      `json:"created_by"`
	CreatedAt             time.Time      `json:"created_at"`
	UpdatedAt             time.Time      `json:"updated_at"`
	Metadata              map[string]any `json:"metadata,omitempty"`
}

type ListTendersResponse struct {
	Tenders []TenderResponse `json:"tenders"`
	Total   int              `json:"total"`
	Limit   int              `json:"limit"`
	Offset  int              `json:"offset"`
}

// CRUD Handlers

func (h *TenderHandler) Create(w http.ResponseWriter, r *http.Request) {
	tenantID, err := h.getTenantID(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid tenant ID")
		return
	}

	userID, err := h.getUserID(r)
	if err != nil {
		response.Unauthorized(w)
		return
	}

	var req CreateTenderRequest
	if err := validation.BindAndValidate(r, &req); err != nil {
		response.BindError(w, err)
		return
	}

	params := tender.CreateParams{
		TenantID:              tenantID,
		Title:                 req.Title,
		Description:           req.Description,
		ClientName:            req.ClientName,
		ClientContact:         req.ClientContact,
		ClientEmail:           req.ClientEmail,
		Source:                req.Source,
		SourceURL:             req.SourceURL,
		Priority:              req.Priority,
		EstimatedValue:        req.EstimatedValue,
		Currency:              req.Currency,
		PublicationDate:       req.PublicationDate,
		Deadline:              req.Deadline,
		ClarificationDeadline: req.ClarificationDeadline,
		SubmissionMethod:      req.SubmissionMethod,
		SubmissionAddress:     req.SubmissionAddress,
		Categories:            req.Categories,
		RequirementsSummary:   req.RequirementsSummary,
		CreatedBy:             userID,
		Metadata:              req.Metadata,
	}

	t, err := h.service.Create(r.Context(), params)
	if err != nil {
		h.logger.Error("failed to create tender", zap.Error(err))
		response.InternalError(w)
		return
	}

	response.Created(w, h.toTenderResponse(t))
}

func (h *TenderHandler) Get(w http.ResponseWriter, r *http.Request) {
	tenantID, err := h.getTenantID(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid tenant ID")
		return
	}

	tenderID, err := h.getTenderID(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid tender ID")
		return
	}

	t, err := h.service.Get(r.Context(), tenantID, tenderID)
	if err != nil {
		if errors.Is(err, tender.ErrNotFound) {
			response.NotFound(w, "tender")
			return
		}
		h.logger.Error("failed to get tender", zap.Error(err))
		response.InternalError(w)
		return
	}

	response.OK(w, h.toTenderResponse(t))
}

func (h *TenderHandler) List(w http.ResponseWriter, r *http.Request) {
	tenantID, err := h.getTenantID(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid tenant ID")
		return
	}

	params := tender.ListParams{
		TenantID: tenantID,
		Status:   r.URL.Query().Get("status"),
		Priority: r.URL.Query().Get("priority"),
		Source:   r.URL.Query().Get("source"),
		Limit:    h.parseIntQuery(r, "limit", 20),
		Offset:   h.parseIntQuery(r, "offset", 0),
	}

	if createdByStr := r.URL.Query().Get("created_by"); createdByStr != "" {
		if createdBy, err := uuid.Parse(createdByStr); err == nil {
			params.CreatedBy = &createdBy
		}
	}

	tenders, total, err := h.service.List(r.Context(), params)
	if err != nil {
		h.logger.Error("failed to list tenders", zap.Error(err))
		response.InternalError(w)
		return
	}

	resp := ListTendersResponse{
		Tenders: make([]TenderResponse, len(tenders)),
		Total:   total,
		Limit:   params.Limit,
		Offset:  params.Offset,
	}

	for i, t := range tenders {
		resp.Tenders[i] = h.toTenderResponse(t)
	}

	response.OK(w, resp)
}

func (h *TenderHandler) Update(w http.ResponseWriter, r *http.Request) {
	tenantID, err := h.getTenantID(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid tenant ID")
		return
	}

	tenderID, err := h.getTenderID(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid tender ID")
		return
	}

	var req UpdateTenderRequest
	if err := validation.BindAndValidate(r, &req); err != nil {
		response.BindError(w, err)
		return
	}

	params := tender.UpdateParams{
		Title:                 req.Title,
		Description:           req.Description,
		ClientName:            req.ClientName,
		ClientContact:         req.ClientContact,
		ClientEmail:           req.ClientEmail,
		Source:                req.Source,
		SourceURL:             req.SourceURL,
		Status:                req.Status,
		Priority:              req.Priority,
		EstimatedValue:        req.EstimatedValue,
		Currency:              req.Currency,
		PublicationDate:       req.PublicationDate,
		Deadline:              req.Deadline,
		ClarificationDeadline: req.ClarificationDeadline,
		SubmissionMethod:      req.SubmissionMethod,
		SubmissionAddress:     req.SubmissionAddress,
		Categories:            req.Categories,
		RequirementsSummary:   req.RequirementsSummary,
		Metadata:              req.Metadata,
	}

	t, err := h.service.Update(r.Context(), tenantID, tenderID, params)
	if err != nil {
		if errors.Is(err, tender.ErrNotFound) {
			response.NotFound(w, "tender")
			return
		}
		h.logger.Error("failed to update tender", zap.Error(err))
		response.InternalError(w)
		return
	}

	response.OK(w, h.toTenderResponse(t))
}

func (h *TenderHandler) Delete(w http.ResponseWriter, r *http.Request) {
	tenantID, err := h.getTenantID(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid tenant ID")
		return
	}

	tenderID, err := h.getTenderID(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid tender ID")
		return
	}

	err = h.service.Delete(r.Context(), tenantID, tenderID)
	if err != nil {
		if errors.Is(err, tender.ErrNotFound) {
			response.NotFound(w, "tender")
			return
		}
		h.logger.Error("failed to delete tender", zap.Error(err))
		response.InternalError(w)
		return
	}

	response.NoContent(w)
}

// Decision endpoint

func (h *TenderHandler) RecordDecision(w http.ResponseWriter, r *http.Request) {
	tenantID, err := h.getTenantID(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid tenant ID")
		return
	}

	tenderID, err := h.getTenderID(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid tender ID")
		return
	}

	userID, err := h.getUserID(r)
	if err != nil {
		response.Unauthorized(w)
		return
	}

	var req TenderDecisionRequest
	if err := validation.BindAndValidate(r, &req); err != nil {
		response.BindError(w, err)
		return
	}

	t, err := h.service.RecordDecision(r.Context(), tenantID, tenderID, tender.DecisionParams{
		Decision:  req.Decision,
		Rationale: req.Rationale,
		DecidedBy: userID,
	})
	if err != nil {
		if errors.Is(err, tender.ErrNotFound) {
			response.NotFound(w, "tender")
			return
		}
		h.logger.Error("failed to record tender decision", zap.Error(err))
		response.InternalError(w)
		return
	}

	response.OK(w, h.toTenderResponse(t))
}

// RegisterRoutes registers tender routes on the given router.
func (h *TenderHandler) RegisterRoutes(r chi.Router) {
	r.Post("/", h.Create)
	r.Get("/", h.List)
	r.Get("/{tenderID}", h.Get)
	r.Patch("/{tenderID}", h.Update)
	r.Delete("/{tenderID}", h.Delete)
	r.Post("/{tenderID}/decision", h.RecordDecision)
}

// Helper methods

func (h *TenderHandler) getTenantID(r *http.Request) (uuid.UUID, error) {
	tenantIDStr := chi.URLParam(r, "tenantID")
	return uuid.Parse(tenantIDStr)
}

func (h *TenderHandler) getTenderID(r *http.Request) (uuid.UUID, error) {
	tenderIDStr := chi.URLParam(r, "tenderID")
	return uuid.Parse(tenderIDStr)
}

func (h *TenderHandler) getUserID(r *http.Request) (uuid.UUID, error) {
	userIDStr := r.Header.Get("X-User-ID")
	if userIDStr == "" {
		if userID, ok := r.Context().Value("user_id").(uuid.UUID); ok {
			return userID, nil
		}
		return uuid.Nil, errors.New("user ID not found")
	}
	return uuid.Parse(userIDStr)
}

func (h *TenderHandler) parseIntQuery(r *http.Request, key string, defaultVal int) int {
	val := r.URL.Query().Get(key)
	if val == "" {
		return defaultVal
	}
	parsed, err := strconv.Atoi(val)
	if err != nil {
		return defaultVal
	}
	return parsed
}

func (h *TenderHandler) toTenderResponse(t *ent.Tender) TenderResponse {
	resp := TenderResponse{
		ID:           t.ID,
		TenantID:     t.TenantID,
		TenderNumber: t.TenderNumber,
		Title:        t.Title,
		ClientName:   t.ClientName,
		Source:       t.Source,
		Status:       t.Status,
		Priority:     t.Priority,
		Currency:     t.Currency,
		Deadline:     t.Deadline,
		CreatedBy:    t.CreatedBy,
		CreatedAt:    t.CreatedAt,
		UpdatedAt:    t.UpdatedAt,
	}

	if t.Description != "" {
		resp.Description = t.Description
	}
	if t.ClientContact != "" {
		resp.ClientContact = t.ClientContact
	}
	if t.ClientEmail != "" {
		resp.ClientEmail = t.ClientEmail
	}
	if t.SourceURL != "" {
		resp.SourceURL = t.SourceURL
	}
	if t.EstimatedValue != 0 {
		resp.EstimatedValue = &t.EstimatedValue
	}
	if !t.PublicationDate.IsZero() {
		resp.PublicationDate = &t.PublicationDate
	}
	if !t.ClarificationDeadline.IsZero() {
		resp.ClarificationDeadline = &t.ClarificationDeadline
	}
	if t.SubmissionMethod != "" {
		resp.SubmissionMethod = t.SubmissionMethod
	}
	if t.SubmissionAddress != "" {
		resp.SubmissionAddress = t.SubmissionAddress
	}
	if len(t.Categories) > 0 {
		resp.Categories = t.Categories
	}
	if t.RequirementsSummary != nil {
		resp.RequirementsSummary = t.RequirementsSummary
	}
	if t.Decision != "" {
		resp.Decision = t.Decision
	}
	if t.DecisionRationale != "" {
		resp.DecisionRationale = t.DecisionRationale
	}
	if !t.DecisionDate.IsZero() {
		resp.DecisionDate = &t.DecisionDate
	}
	if t.DecidedBy != uuid.Nil {
		resp.DecidedBy = &t.DecidedBy
	}
	if t.ProjectID != uuid.Nil {
		resp.ProjectID = &t.ProjectID
	}
	if t.Metadata != nil {
		resp.Metadata = t.Metadata
	}

	return resp
}
