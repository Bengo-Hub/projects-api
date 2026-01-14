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

// TenderSectionHandler handles tender section HTTP requests.
type TenderSectionHandler struct {
	logger  *zap.Logger
	service tender.TenderServiceInterface
}

// NewTenderSectionHandler creates a new tender section handler.
func NewTenderSectionHandler(logger *zap.Logger, service tender.TenderServiceInterface) *TenderSectionHandler {
	return &TenderSectionHandler{
		logger:  logger,
		service: service,
	}
}

// Request/Response types

type CreateSectionRequest struct {
	ParentID            *uuid.UUID       `json:"parent_id,omitempty"`
	Title               string           `json:"title" validate:"required,min=1,max=255"`
	Description         string           `json:"description,omitempty" validate:"max=2000"`
	SectionNumber       string           `json:"section_number,omitempty" validate:"max=50"`
	SortOrder           int              `json:"sort_order,omitempty" validate:"min=0"`
	SectionType         string           `json:"section_type,omitempty" validate:"omitempty,oneof=executive_summary technical approach methodology pricing appendix cover_letter references qualifications other"`
	AssigneeID          *uuid.UUID       `json:"assignee_id,omitempty"`
	DueDate             *time.Time       `json:"due_date,omitempty"`
	PageLimit           *int             `json:"page_limit,omitempty" validate:"omitempty,min=1"`
	ComplianceChecklist []map[string]any `json:"compliance_checklist,omitempty"`
	Metadata            map[string]any   `json:"metadata,omitempty"`
}

type UpdateSectionRequest struct {
	Title               *string          `json:"title,omitempty" validate:"omitempty,min=1,max=255"`
	Description         *string          `json:"description,omitempty" validate:"omitempty,max=2000"`
	SectionNumber       *string          `json:"section_number,omitempty" validate:"omitempty,max=50"`
	SortOrder           *int             `json:"sort_order,omitempty" validate:"omitempty,min=0"`
	SectionType         *string          `json:"section_type,omitempty" validate:"omitempty,oneof=executive_summary technical approach methodology pricing appendix cover_letter references qualifications other"`
	AssigneeID          *uuid.UUID       `json:"assignee_id,omitempty"`
	Status              *string          `json:"status,omitempty" validate:"omitempty,oneof=not_started in_progress review approved rejected"`
	DueDate             *time.Time       `json:"due_date,omitempty"`
	Content             *string          `json:"content,omitempty"`
	WordCount           *int             `json:"word_count,omitempty" validate:"omitempty,min=0"`
	PageLimit           *int             `json:"page_limit,omitempty" validate:"omitempty,min=1"`
	ReviewStatus        *string          `json:"review_status,omitempty" validate:"omitempty,oneof=pending_technical pending_legal pending_management approved rejected"`
	ReviewerComments    *string          `json:"reviewer_comments,omitempty" validate:"omitempty,max=2000"`
	ComplianceChecklist []map[string]any `json:"compliance_checklist,omitempty"`
	IsCompliant         *bool            `json:"is_compliant,omitempty"`
	Metadata            map[string]any   `json:"metadata,omitempty"`
}

type AssignSectionRequest struct {
	AssigneeID uuid.UUID `json:"assignee_id" validate:"required"`
}

type SubmitForReviewRequest struct {
	ReviewerID uuid.UUID `json:"reviewer_id" validate:"required"`
}

type ReviewSectionRequest struct {
	Comments string `json:"comments,omitempty" validate:"max=2000"`
}

type SectionResponse struct {
	ID                  uuid.UUID          `json:"id"`
	TenantID            uuid.UUID          `json:"tenant_id"`
	TenderID            uuid.UUID          `json:"tender_id"`
	ParentID            *uuid.UUID         `json:"parent_id,omitempty"`
	Title               string             `json:"title"`
	Description         string             `json:"description,omitempty"`
	SectionNumber       string             `json:"section_number,omitempty"`
	SortOrder           int                `json:"sort_order"`
	SectionType         string             `json:"section_type"`
	AssigneeID          *uuid.UUID         `json:"assignee_id,omitempty"`
	Status              string             `json:"status"`
	DueDate             *time.Time         `json:"due_date,omitempty"`
	Content             string             `json:"content,omitempty"`
	WordCount           int                `json:"word_count,omitempty"`
	PageLimit           int                `json:"page_limit,omitempty"`
	ReviewStatus        string             `json:"review_status,omitempty"`
	ReviewerID          *uuid.UUID         `json:"reviewer_id,omitempty"`
	ReviewerComments    string             `json:"reviewer_comments,omitempty"`
	ReviewedAt          *time.Time         `json:"reviewed_at,omitempty"`
	ComplianceChecklist []map[string]any   `json:"compliance_checklist,omitempty"`
	IsCompliant         bool               `json:"is_compliant"`
	StartedAt           *time.Time         `json:"started_at,omitempty"`
	CompletedAt         *time.Time         `json:"completed_at,omitempty"`
	CreatedAt           time.Time          `json:"created_at"`
	UpdatedAt           time.Time          `json:"updated_at"`
	Metadata            map[string]any     `json:"metadata,omitempty"`
	Children            []SectionResponse  `json:"children,omitempty"`
}

type ListSectionsResponse struct {
	Sections []SectionResponse `json:"sections"`
	Total    int               `json:"total"`
	Limit    int               `json:"limit"`
	Offset   int               `json:"offset"`
}

// Handlers

func (h *TenderSectionHandler) Create(w http.ResponseWriter, r *http.Request) {
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

	var req CreateSectionRequest
	if err := validation.BindAndValidate(r, &req); err != nil {
		response.BindError(w, err)
		return
	}

	section, err := h.service.CreateSection(r.Context(), tender.SectionCreateParams{
		TenantID:            tenantID,
		TenderID:            tenderID,
		ParentID:            req.ParentID,
		Title:               req.Title,
		Description:         req.Description,
		SectionNumber:       req.SectionNumber,
		SortOrder:           req.SortOrder,
		SectionType:         req.SectionType,
		AssigneeID:          req.AssigneeID,
		DueDate:             req.DueDate,
		PageLimit:           req.PageLimit,
		ComplianceChecklist: req.ComplianceChecklist,
		Metadata:            req.Metadata,
	})
	if err != nil {
		if errors.Is(err, tender.ErrNotFound) {
			response.NotFound(w, "tender")
			return
		}
		h.logger.Error("failed to create section", zap.Error(err))
		response.InternalError(w)
		return
	}

	response.Created(w, h.toSectionResponse(section))
}

func (h *TenderSectionHandler) Get(w http.ResponseWriter, r *http.Request) {
	tenantID, err := h.getTenantID(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid tenant ID")
		return
	}

	sectionID, err := h.getSectionID(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid section ID")
		return
	}

	section, err := h.service.GetSection(r.Context(), tenantID, sectionID)
	if err != nil {
		if errors.Is(err, tender.ErrSectionNotFound) {
			response.NotFound(w, "section")
			return
		}
		h.logger.Error("failed to get section", zap.Error(err))
		response.InternalError(w)
		return
	}

	response.OK(w, h.toSectionResponse(section))
}

func (h *TenderSectionHandler) List(w http.ResponseWriter, r *http.Request) {
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

	params := tender.SectionListParams{
		TenantID:   tenantID,
		TenderID:   tenderID,
		Status:     r.URL.Query().Get("status"),
		ParentOnly: r.URL.Query().Get("parent_only") == "true",
		Limit:      h.parseIntQuery(r, "limit", 50),
		Offset:     h.parseIntQuery(r, "offset", 0),
	}

	// Parse assignee_id if provided
	if aid := r.URL.Query().Get("assignee_id"); aid != "" {
		if assigneeID, err := uuid.Parse(aid); err == nil {
			params.AssigneeID = &assigneeID
		}
	}

	sections, total, err := h.service.ListSections(r.Context(), params)
	if err != nil {
		h.logger.Error("failed to list sections", zap.Error(err))
		response.InternalError(w)
		return
	}

	resp := ListSectionsResponse{
		Sections: make([]SectionResponse, len(sections)),
		Total:    total,
		Limit:    params.Limit,
		Offset:   params.Offset,
	}

	for i, s := range sections {
		resp.Sections[i] = h.toSectionResponse(s)
	}

	response.OK(w, resp)
}

func (h *TenderSectionHandler) Update(w http.ResponseWriter, r *http.Request) {
	tenantID, err := h.getTenantID(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid tenant ID")
		return
	}

	sectionID, err := h.getSectionID(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid section ID")
		return
	}

	var req UpdateSectionRequest
	if err := validation.BindAndValidate(r, &req); err != nil {
		response.BindError(w, err)
		return
	}

	section, err := h.service.UpdateSection(r.Context(), tenantID, sectionID, tender.SectionUpdateParams{
		Title:               req.Title,
		Description:         req.Description,
		SectionNumber:       req.SectionNumber,
		SortOrder:           req.SortOrder,
		SectionType:         req.SectionType,
		AssigneeID:          req.AssigneeID,
		Status:              req.Status,
		DueDate:             req.DueDate,
		Content:             req.Content,
		WordCount:           req.WordCount,
		PageLimit:           req.PageLimit,
		ReviewStatus:        req.ReviewStatus,
		ReviewerComments:    req.ReviewerComments,
		ComplianceChecklist: req.ComplianceChecklist,
		IsCompliant:         req.IsCompliant,
		Metadata:            req.Metadata,
	})
	if err != nil {
		if errors.Is(err, tender.ErrSectionNotFound) {
			response.NotFound(w, "section")
			return
		}
		h.logger.Error("failed to update section", zap.Error(err))
		response.InternalError(w)
		return
	}

	response.OK(w, h.toSectionResponse(section))
}

func (h *TenderSectionHandler) Assign(w http.ResponseWriter, r *http.Request) {
	tenantID, err := h.getTenantID(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid tenant ID")
		return
	}

	sectionID, err := h.getSectionID(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid section ID")
		return
	}

	var req AssignSectionRequest
	if err := validation.BindAndValidate(r, &req); err != nil {
		response.BindError(w, err)
		return
	}

	section, err := h.service.AssignSection(r.Context(), tenantID, sectionID, req.AssigneeID)
	if err != nil {
		if errors.Is(err, tender.ErrSectionNotFound) {
			response.NotFound(w, "section")
			return
		}
		h.logger.Error("failed to assign section", zap.Error(err))
		response.InternalError(w)
		return
	}

	response.OK(w, h.toSectionResponse(section))
}

func (h *TenderSectionHandler) SubmitForReview(w http.ResponseWriter, r *http.Request) {
	tenantID, err := h.getTenantID(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid tenant ID")
		return
	}

	sectionID, err := h.getSectionID(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid section ID")
		return
	}

	var req SubmitForReviewRequest
	if err := validation.BindAndValidate(r, &req); err != nil {
		response.BindError(w, err)
		return
	}

	section, err := h.service.SubmitSectionForReview(r.Context(), tenantID, sectionID, req.ReviewerID)
	if err != nil {
		if errors.Is(err, tender.ErrSectionNotFound) {
			response.NotFound(w, "section")
			return
		}
		h.logger.Error("failed to submit section for review", zap.Error(err))
		response.InternalError(w)
		return
	}

	response.OK(w, h.toSectionResponse(section))
}

func (h *TenderSectionHandler) Approve(w http.ResponseWriter, r *http.Request) {
	tenantID, err := h.getTenantID(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid tenant ID")
		return
	}

	sectionID, err := h.getSectionID(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid section ID")
		return
	}

	userID, err := h.getUserID(r)
	if err != nil {
		response.Unauthorized(w)
		return
	}

	var req ReviewSectionRequest
	if err := validation.BindAndValidate(r, &req); err != nil {
		response.BindError(w, err)
		return
	}

	section, err := h.service.ApproveSection(r.Context(), tenantID, sectionID, userID, req.Comments)
	if err != nil {
		if errors.Is(err, tender.ErrSectionNotFound) {
			response.NotFound(w, "section")
			return
		}
		h.logger.Error("failed to approve section", zap.Error(err))
		response.InternalError(w)
		return
	}

	response.OK(w, h.toSectionResponse(section))
}

func (h *TenderSectionHandler) Reject(w http.ResponseWriter, r *http.Request) {
	tenantID, err := h.getTenantID(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid tenant ID")
		return
	}

	sectionID, err := h.getSectionID(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid section ID")
		return
	}

	userID, err := h.getUserID(r)
	if err != nil {
		response.Unauthorized(w)
		return
	}

	var req ReviewSectionRequest
	if err := validation.BindAndValidate(r, &req); err != nil {
		response.BindError(w, err)
		return
	}

	section, err := h.service.RejectSection(r.Context(), tenantID, sectionID, userID, req.Comments)
	if err != nil {
		if errors.Is(err, tender.ErrSectionNotFound) {
			response.NotFound(w, "section")
			return
		}
		h.logger.Error("failed to reject section", zap.Error(err))
		response.InternalError(w)
		return
	}

	response.OK(w, h.toSectionResponse(section))
}

func (h *TenderSectionHandler) Delete(w http.ResponseWriter, r *http.Request) {
	tenantID, err := h.getTenantID(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid tenant ID")
		return
	}

	sectionID, err := h.getSectionID(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid section ID")
		return
	}

	err = h.service.DeleteSection(r.Context(), tenantID, sectionID)
	if err != nil {
		if errors.Is(err, tender.ErrSectionNotFound) {
			response.NotFound(w, "section")
			return
		}
		h.logger.Error("failed to delete section", zap.Error(err))
		response.InternalError(w)
		return
	}

	response.NoContent(w)
}

func (h *TenderSectionHandler) Progress(w http.ResponseWriter, r *http.Request) {
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

	progress, err := h.service.GetSectionProgress(r.Context(), tenantID, tenderID)
	if err != nil {
		h.logger.Error("failed to get section progress", zap.Error(err))
		response.InternalError(w)
		return
	}

	response.OK(w, progress)
}

// RegisterRoutes registers section routes nested under tenders.
func (h *TenderSectionHandler) RegisterRoutes(r chi.Router) {
	r.Post("/", h.Create)
	r.Get("/", h.List)
	r.Get("/progress", h.Progress)
	r.Get("/{sectionID}", h.Get)
	r.Patch("/{sectionID}", h.Update)
	r.Post("/{sectionID}/assign", h.Assign)
	r.Post("/{sectionID}/submit-review", h.SubmitForReview)
	r.Post("/{sectionID}/approve", h.Approve)
	r.Post("/{sectionID}/reject", h.Reject)
	r.Delete("/{sectionID}", h.Delete)
}

// Helper methods

func (h *TenderSectionHandler) getTenantID(r *http.Request) (uuid.UUID, error) {
	return uuid.Parse(chi.URLParam(r, "tenantID"))
}

func (h *TenderSectionHandler) getTenderID(r *http.Request) (uuid.UUID, error) {
	return uuid.Parse(chi.URLParam(r, "tenderID"))
}

func (h *TenderSectionHandler) getSectionID(r *http.Request) (uuid.UUID, error) {
	return uuid.Parse(chi.URLParam(r, "sectionID"))
}

func (h *TenderSectionHandler) getUserID(r *http.Request) (uuid.UUID, error) {
	userIDStr := r.Header.Get("X-User-ID")
	if userIDStr == "" {
		if userID, ok := r.Context().Value("user_id").(uuid.UUID); ok {
			return userID, nil
		}
		return uuid.Nil, errors.New("user ID not found")
	}
	return uuid.Parse(userIDStr)
}

func (h *TenderSectionHandler) parseIntQuery(r *http.Request, key string, defaultVal int) int {
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

func (h *TenderSectionHandler) toSectionResponse(s *ent.TenderSection) SectionResponse {
	resp := SectionResponse{
		ID:          s.ID,
		TenantID:    s.TenantID,
		TenderID:    s.TenderID,
		Title:       s.Title,
		SortOrder:   s.SortOrder,
		SectionType: s.SectionType,
		Status:      s.Status,
		IsCompliant: s.IsCompliant,
		CreatedAt:   s.CreatedAt,
		UpdatedAt:   s.UpdatedAt,
	}

	if s.ParentID != uuid.Nil {
		resp.ParentID = &s.ParentID
	}
	if s.Description != "" {
		resp.Description = s.Description
	}
	if s.SectionNumber != "" {
		resp.SectionNumber = s.SectionNumber
	}
	if s.AssigneeID != uuid.Nil {
		resp.AssigneeID = &s.AssigneeID
	}
	if !s.DueDate.IsZero() {
		resp.DueDate = &s.DueDate
	}
	if s.Content != "" {
		resp.Content = s.Content
	}
	if s.WordCount > 0 {
		resp.WordCount = s.WordCount
	}
	if s.PageLimit > 0 {
		resp.PageLimit = s.PageLimit
	}
	if s.ReviewStatus != "" {
		resp.ReviewStatus = s.ReviewStatus
	}
	if s.ReviewerID != uuid.Nil {
		resp.ReviewerID = &s.ReviewerID
	}
	if s.ReviewerComments != "" {
		resp.ReviewerComments = s.ReviewerComments
	}
	if !s.ReviewedAt.IsZero() {
		resp.ReviewedAt = &s.ReviewedAt
	}
	if len(s.ComplianceChecklist) > 0 {
		resp.ComplianceChecklist = s.ComplianceChecklist
	}
	if !s.StartedAt.IsZero() {
		resp.StartedAt = &s.StartedAt
	}
	if !s.CompletedAt.IsZero() {
		resp.CompletedAt = &s.CompletedAt
	}
	if s.Metadata != nil {
		resp.Metadata = s.Metadata
	}

	// Include children if loaded
	if s.Edges.Children != nil {
		resp.Children = make([]SectionResponse, len(s.Edges.Children))
		for i, child := range s.Edges.Children {
			resp.Children[i] = h.toSectionResponse(child)
		}
	}

	return resp
}
