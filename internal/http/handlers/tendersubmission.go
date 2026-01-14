package handlers

import (
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/projects-service/internal/ent"
	"github.com/bengobox/projects-service/internal/http/response"
	"github.com/bengobox/projects-service/internal/services/tender"
	"github.com/bengobox/projects-service/internal/shared/validation"
)

// TenderSubmissionHandler handles tender submission HTTP requests.
type TenderSubmissionHandler struct {
	logger  *zap.Logger
	service tender.TenderServiceInterface
}

// NewTenderSubmissionHandler creates a new tender submission handler.
func NewTenderSubmissionHandler(logger *zap.Logger, service tender.TenderServiceInterface) *TenderSubmissionHandler {
	return &TenderSubmissionHandler{
		logger:  logger,
		service: service,
	}
}

// Request/Response types

type CreateSubmissionRequest struct {
	SubmissionType   string           `json:"submission_type,omitempty" validate:"omitempty,oneof=email physical portal"`
	RecipientEmail   string           `json:"recipient_email,omitempty" validate:"omitempty,email"`
	RecipientAddress string           `json:"recipient_address,omitempty" validate:"max=500"`
	PortalURL        string           `json:"portal_url,omitempty" validate:"omitempty,url"`
	CourierService   string           `json:"courier_service,omitempty" validate:"max=100"`
	Documents        []map[string]any `json:"documents,omitempty"`
	TotalPages       *int             `json:"total_pages,omitempty" validate:"omitempty,min=1"`
	CopyCount        *int             `json:"copy_count,omitempty" validate:"omitempty,min=1"`
	Notes            string           `json:"notes,omitempty" validate:"max=2000"`
	Metadata         map[string]any   `json:"metadata,omitempty"`
}

type UpdateSubmissionRequest struct {
	Status                   *string          `json:"status,omitempty" validate:"omitempty,oneof=draft preparing submitted confirmed rejected"`
	RecipientEmail           *string          `json:"recipient_email,omitempty" validate:"omitempty,email"`
	RecipientAddress         *string          `json:"recipient_address,omitempty" validate:"omitempty,max=500"`
	PortalURL                *string          `json:"portal_url,omitempty" validate:"omitempty,url"`
	PortalConfirmationNumber *string          `json:"portal_confirmation_number,omitempty" validate:"max=100"`
	CourierService           *string          `json:"courier_service,omitempty" validate:"omitempty,max=100"`
	TrackingNumber           *string          `json:"tracking_number,omitempty" validate:"max=100"`
	EstimatedDelivery        *time.Time       `json:"estimated_delivery,omitempty"`
	DeliveryProofURL         *string          `json:"delivery_proof_url,omitempty" validate:"omitempty,url"`
	Documents                []map[string]any `json:"documents,omitempty"`
	TotalPages               *int             `json:"total_pages,omitempty" validate:"omitempty,min=1"`
	CopyCount                *int             `json:"copy_count,omitempty" validate:"omitempty,min=1"`
	Notes                    *string          `json:"notes,omitempty" validate:"omitempty,max=2000"`
	RejectionReason          *string          `json:"rejection_reason,omitempty" validate:"max=1000"`
	Metadata                 map[string]any   `json:"metadata,omitempty"`
}

type ConfirmDeliveryRequest struct {
	DeliveryProofURL string `json:"delivery_proof_url,omitempty" validate:"omitempty,url"`
}

type RecordEmailTrackingRequest struct {
	MessageID string `json:"message_id" validate:"required,max=255"`
	Opened    bool   `json:"opened"`
}

type SubmissionResponse struct {
	ID                       uuid.UUID        `json:"id"`
	TenantID                 uuid.UUID        `json:"tenant_id"`
	TenderID                 uuid.UUID        `json:"tender_id"`
	SubmissionType           string           `json:"submission_type"`
	Status                   string           `json:"status"`
	RecipientEmail           string           `json:"recipient_email,omitempty"`
	RecipientAddress         string           `json:"recipient_address,omitempty"`
	PortalURL                string           `json:"portal_url,omitempty"`
	PortalConfirmationNumber string           `json:"portal_confirmation_number,omitempty"`
	CourierService           string           `json:"courier_service,omitempty"`
	TrackingNumber           string           `json:"tracking_number,omitempty"`
	EstimatedDelivery        *time.Time       `json:"estimated_delivery,omitempty"`
	DeliveredAt              *time.Time       `json:"delivered_at,omitempty"`
	DeliveryProofURL         string           `json:"delivery_proof_url,omitempty"`
	EmailMessageID           string           `json:"email_message_id,omitempty"`
	EmailOpened              bool             `json:"email_opened,omitempty"`
	EmailOpenedAt            *time.Time       `json:"email_opened_at,omitempty"`
	Documents                []map[string]any `json:"documents,omitempty"`
	TotalPages               int              `json:"total_pages,omitempty"`
	CopyCount                int              `json:"copy_count,omitempty"`
	Notes                    string           `json:"notes,omitempty"`
	RejectionReason          string           `json:"rejection_reason,omitempty"`
	SubmittedBy              *uuid.UUID       `json:"submitted_by,omitempty"`
	SubmittedAt              *time.Time       `json:"submitted_at,omitempty"`
	CreatedAt                time.Time        `json:"created_at"`
	UpdatedAt                time.Time        `json:"updated_at"`
	Metadata                 map[string]any   `json:"metadata,omitempty"`
}

type ListSubmissionsResponse struct {
	Submissions []SubmissionResponse `json:"submissions"`
	Total       int                  `json:"total"`
}

// Handlers

func (h *TenderSubmissionHandler) Create(w http.ResponseWriter, r *http.Request) {
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

	var req CreateSubmissionRequest
	if err := validation.BindAndValidate(r, &req); err != nil {
		response.BindError(w, err)
		return
	}

	submission, err := h.service.CreateSubmission(r.Context(), tender.SubmissionCreateParams{
		TenantID:         tenantID,
		TenderID:         tenderID,
		SubmissionType:   req.SubmissionType,
		RecipientEmail:   req.RecipientEmail,
		RecipientAddress: req.RecipientAddress,
		PortalURL:        req.PortalURL,
		CourierService:   req.CourierService,
		Documents:        req.Documents,
		TotalPages:       req.TotalPages,
		CopyCount:        req.CopyCount,
		Notes:            req.Notes,
		Metadata:         req.Metadata,
	})
	if err != nil {
		if errors.Is(err, tender.ErrNotFound) {
			response.NotFound(w, "tender")
			return
		}
		h.logger.Error("failed to create submission", zap.Error(err))
		response.InternalError(w)
		return
	}

	response.Created(w, h.toSubmissionResponse(submission))
}

func (h *TenderSubmissionHandler) Get(w http.ResponseWriter, r *http.Request) {
	tenantID, err := h.getTenantID(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid tenant ID")
		return
	}

	submissionID, err := h.getSubmissionID(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid submission ID")
		return
	}

	submission, err := h.service.GetSubmission(r.Context(), tenantID, submissionID)
	if err != nil {
		if errors.Is(err, tender.ErrSubmissionNotFound) {
			response.NotFound(w, "submission")
			return
		}
		h.logger.Error("failed to get submission", zap.Error(err))
		response.InternalError(w)
		return
	}

	response.OK(w, h.toSubmissionResponse(submission))
}

func (h *TenderSubmissionHandler) List(w http.ResponseWriter, r *http.Request) {
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

	submissions, err := h.service.ListSubmissions(r.Context(), tenantID, tenderID)
	if err != nil {
		h.logger.Error("failed to list submissions", zap.Error(err))
		response.InternalError(w)
		return
	}

	resp := ListSubmissionsResponse{
		Submissions: make([]SubmissionResponse, len(submissions)),
		Total:       len(submissions),
	}

	for i, s := range submissions {
		resp.Submissions[i] = h.toSubmissionResponse(s)
	}

	response.OK(w, resp)
}

func (h *TenderSubmissionHandler) Update(w http.ResponseWriter, r *http.Request) {
	tenantID, err := h.getTenantID(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid tenant ID")
		return
	}

	submissionID, err := h.getSubmissionID(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid submission ID")
		return
	}

	var req UpdateSubmissionRequest
	if err := validation.BindAndValidate(r, &req); err != nil {
		response.BindError(w, err)
		return
	}

	submission, err := h.service.UpdateSubmission(r.Context(), tenantID, submissionID, tender.SubmissionUpdateParams{
		Status:                   req.Status,
		RecipientEmail:           req.RecipientEmail,
		RecipientAddress:         req.RecipientAddress,
		PortalURL:                req.PortalURL,
		PortalConfirmationNumber: req.PortalConfirmationNumber,
		CourierService:           req.CourierService,
		TrackingNumber:           req.TrackingNumber,
		EstimatedDelivery:        req.EstimatedDelivery,
		DeliveryProofURL:         req.DeliveryProofURL,
		Documents:                req.Documents,
		TotalPages:               req.TotalPages,
		CopyCount:                req.CopyCount,
		Notes:                    req.Notes,
		RejectionReason:          req.RejectionReason,
		Metadata:                 req.Metadata,
	})
	if err != nil {
		if errors.Is(err, tender.ErrSubmissionNotFound) {
			response.NotFound(w, "submission")
			return
		}
		h.logger.Error("failed to update submission", zap.Error(err))
		response.InternalError(w)
		return
	}

	response.OK(w, h.toSubmissionResponse(submission))
}

func (h *TenderSubmissionHandler) Submit(w http.ResponseWriter, r *http.Request) {
	tenantID, err := h.getTenantID(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid tenant ID")
		return
	}

	submissionID, err := h.getSubmissionID(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid submission ID")
		return
	}

	userID, err := h.getUserID(r)
	if err != nil {
		response.Unauthorized(w)
		return
	}

	submission, err := h.service.SubmitTender(r.Context(), tenantID, submissionID, userID)
	if err != nil {
		if errors.Is(err, tender.ErrSubmissionNotFound) {
			response.NotFound(w, "submission")
			return
		}
		h.logger.Error("failed to submit tender", zap.Error(err))
		response.InternalError(w)
		return
	}

	response.OK(w, h.toSubmissionResponse(submission))
}

func (h *TenderSubmissionHandler) ConfirmDelivery(w http.ResponseWriter, r *http.Request) {
	tenantID, err := h.getTenantID(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid tenant ID")
		return
	}

	submissionID, err := h.getSubmissionID(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid submission ID")
		return
	}

	var req ConfirmDeliveryRequest
	if err := validation.BindAndValidate(r, &req); err != nil {
		response.BindError(w, err)
		return
	}

	submission, err := h.service.ConfirmDelivery(r.Context(), tenantID, submissionID, req.DeliveryProofURL)
	if err != nil {
		if errors.Is(err, tender.ErrSubmissionNotFound) {
			response.NotFound(w, "submission")
			return
		}
		h.logger.Error("failed to confirm delivery", zap.Error(err))
		response.InternalError(w)
		return
	}

	response.OK(w, h.toSubmissionResponse(submission))
}

func (h *TenderSubmissionHandler) RecordEmailTracking(w http.ResponseWriter, r *http.Request) {
	tenantID, err := h.getTenantID(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid tenant ID")
		return
	}

	submissionID, err := h.getSubmissionID(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid submission ID")
		return
	}

	var req RecordEmailTrackingRequest
	if err := validation.BindAndValidate(r, &req); err != nil {
		response.BindError(w, err)
		return
	}

	submission, err := h.service.RecordEmailTracking(r.Context(), tenantID, submissionID, req.MessageID, req.Opened)
	if err != nil {
		if errors.Is(err, tender.ErrSubmissionNotFound) {
			response.NotFound(w, "submission")
			return
		}
		h.logger.Error("failed to record email tracking", zap.Error(err))
		response.InternalError(w)
		return
	}

	response.OK(w, h.toSubmissionResponse(submission))
}

func (h *TenderSubmissionHandler) Delete(w http.ResponseWriter, r *http.Request) {
	tenantID, err := h.getTenantID(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid tenant ID")
		return
	}

	submissionID, err := h.getSubmissionID(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid submission ID")
		return
	}

	err = h.service.DeleteSubmission(r.Context(), tenantID, submissionID)
	if err != nil {
		if errors.Is(err, tender.ErrSubmissionNotFound) {
			response.NotFound(w, "submission")
			return
		}
		h.logger.Error("failed to delete submission", zap.Error(err))
		response.InternalError(w)
		return
	}

	response.NoContent(w)
}

// RegisterRoutes registers submission routes nested under tenders.
func (h *TenderSubmissionHandler) RegisterRoutes(r chi.Router) {
	r.Post("/", h.Create)
	r.Get("/", h.List)
	r.Get("/{submissionID}", h.Get)
	r.Patch("/{submissionID}", h.Update)
	r.Post("/{submissionID}/submit", h.Submit)
	r.Post("/{submissionID}/confirm-delivery", h.ConfirmDelivery)
	r.Post("/{submissionID}/email-tracking", h.RecordEmailTracking)
	r.Delete("/{submissionID}", h.Delete)
}

// Helper methods

func (h *TenderSubmissionHandler) getTenantID(r *http.Request) (uuid.UUID, error) {
	return uuid.Parse(chi.URLParam(r, "tenantID"))
}

func (h *TenderSubmissionHandler) getTenderID(r *http.Request) (uuid.UUID, error) {
	return uuid.Parse(chi.URLParam(r, "tenderID"))
}

func (h *TenderSubmissionHandler) getSubmissionID(r *http.Request) (uuid.UUID, error) {
	return uuid.Parse(chi.URLParam(r, "submissionID"))
}

func (h *TenderSubmissionHandler) getUserID(r *http.Request) (uuid.UUID, error) {
	userIDStr := r.Header.Get("X-User-ID")
	if userIDStr == "" {
		if userID, ok := r.Context().Value("user_id").(uuid.UUID); ok {
			return userID, nil
		}
		return uuid.Nil, errors.New("user ID not found")
	}
	return uuid.Parse(userIDStr)
}

func (h *TenderSubmissionHandler) toSubmissionResponse(s *ent.TenderSubmission) SubmissionResponse {
	resp := SubmissionResponse{
		ID:             s.ID,
		TenantID:       s.TenantID,
		TenderID:       s.TenderID,
		SubmissionType: s.SubmissionType,
		Status:         s.Status,
		EmailOpened:    s.EmailOpened,
		CreatedAt:      s.CreatedAt,
		UpdatedAt:      s.UpdatedAt,
	}

	if s.RecipientEmail != "" {
		resp.RecipientEmail = s.RecipientEmail
	}
	if s.RecipientAddress != "" {
		resp.RecipientAddress = s.RecipientAddress
	}
	if s.PortalURL != "" {
		resp.PortalURL = s.PortalURL
	}
	if s.PortalConfirmationNumber != "" {
		resp.PortalConfirmationNumber = s.PortalConfirmationNumber
	}
	if s.CourierService != "" {
		resp.CourierService = s.CourierService
	}
	if s.TrackingNumber != "" {
		resp.TrackingNumber = s.TrackingNumber
	}
	if !s.EstimatedDelivery.IsZero() {
		resp.EstimatedDelivery = &s.EstimatedDelivery
	}
	if !s.DeliveredAt.IsZero() {
		resp.DeliveredAt = &s.DeliveredAt
	}
	if s.DeliveryProofURL != "" {
		resp.DeliveryProofURL = s.DeliveryProofURL
	}
	if s.EmailMessageID != "" {
		resp.EmailMessageID = s.EmailMessageID
	}
	if !s.EmailOpenedAt.IsZero() {
		resp.EmailOpenedAt = &s.EmailOpenedAt
	}
	if len(s.Documents) > 0 {
		resp.Documents = s.Documents
	}
	if s.TotalPages > 0 {
		resp.TotalPages = s.TotalPages
	}
	if s.CopyCount > 0 {
		resp.CopyCount = s.CopyCount
	}
	if s.Notes != "" {
		resp.Notes = s.Notes
	}
	if s.RejectionReason != "" {
		resp.RejectionReason = s.RejectionReason
	}
	if s.SubmittedBy != uuid.Nil {
		resp.SubmittedBy = &s.SubmittedBy
	}
	if !s.SubmittedAt.IsZero() {
		resp.SubmittedAt = &s.SubmittedAt
	}
	if s.Metadata != nil {
		resp.Metadata = s.Metadata
	}

	return resp
}
