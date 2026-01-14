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

// TenderCommitteeHandler handles tender committee HTTP requests.
type TenderCommitteeHandler struct {
	logger  *zap.Logger
	service tender.TenderServiceInterface
}

// NewTenderCommitteeHandler creates a new tender committee handler.
func NewTenderCommitteeHandler(logger *zap.Logger, service tender.TenderServiceInterface) *TenderCommitteeHandler {
	return &TenderCommitteeHandler{
		logger:  logger,
		service: service,
	}
}

// Request/Response types

type CreateCommitteeRequest struct {
	Name          string         `json:"name" validate:"required,min=1,max=255"`
	CommitteeType string         `json:"committee_type,omitempty" validate:"omitempty,oneof=evaluation technical financial legal compliance"`
	ChairID       *uuid.UUID     `json:"chair_id,omitempty"`
	Mandate       string         `json:"mandate,omitempty" validate:"max=2000"`
	Metadata      map[string]any `json:"metadata,omitempty"`
}

type UpdateCommitteeRequest struct {
	Name          *string        `json:"name,omitempty" validate:"omitempty,min=1,max=255"`
	CommitteeType *string        `json:"committee_type,omitempty" validate:"omitempty,oneof=evaluation technical financial legal compliance"`
	ChairID       *uuid.UUID     `json:"chair_id,omitempty"`
	Mandate       *string        `json:"mandate,omitempty" validate:"omitempty,max=2000"`
	Status        *string        `json:"status,omitempty" validate:"omitempty,oneof=active dissolved"`
	Metadata      map[string]any `json:"metadata,omitempty"`
}

type AddMemberRequest struct {
	UserID    uuid.UUID      `json:"user_id" validate:"required"`
	Role      string         `json:"role,omitempty" validate:"omitempty,oneof=chair secretary member observer"`
	Expertise string         `json:"expertise,omitempty" validate:"max=500"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

type UpdateMemberRequest struct {
	Role      *string        `json:"role,omitempty" validate:"omitempty,oneof=chair secretary member observer"`
	Expertise *string        `json:"expertise,omitempty" validate:"omitempty,max=500"`
	IsActive  *bool          `json:"is_active,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

type CommitteeResponse struct {
	ID            uuid.UUID          `json:"id"`
	TenantID      uuid.UUID          `json:"tenant_id"`
	TenderID      uuid.UUID          `json:"tender_id"`
	Name          string             `json:"name"`
	CommitteeType string             `json:"committee_type"`
	ChairID       *uuid.UUID         `json:"chair_id,omitempty"`
	Mandate       string             `json:"mandate,omitempty"`
	Status        string             `json:"status"`
	DissolvedAt   *time.Time         `json:"dissolved_at,omitempty"`
	CreatedAt     time.Time          `json:"created_at"`
	UpdatedAt     time.Time          `json:"updated_at"`
	Metadata      map[string]any     `json:"metadata,omitempty"`
	Members       []MemberResponse   `json:"members,omitempty"`
}

type MemberResponse struct {
	ID          uuid.UUID      `json:"id"`
	TenantID    uuid.UUID      `json:"tenant_id"`
	CommitteeID uuid.UUID      `json:"committee_id"`
	UserID      uuid.UUID      `json:"user_id"`
	Role        string         `json:"role"`
	Expertise   string         `json:"expertise,omitempty"`
	IsActive    bool           `json:"is_active"`
	AddedBy     uuid.UUID      `json:"added_by"`
	AddedAt     time.Time      `json:"added_at"`
	LeftAt      *time.Time     `json:"left_at,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

type ListCommitteesResponse struct {
	Committees []CommitteeResponse `json:"committees"`
	Total      int                 `json:"total"`
}

// Committee Handlers

func (h *TenderCommitteeHandler) Create(w http.ResponseWriter, r *http.Request) {
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

	var req CreateCommitteeRequest
	if err := validation.BindAndValidate(r, &req); err != nil {
		response.BindError(w, err)
		return
	}

	committee, err := h.service.CreateCommittee(r.Context(), tender.CommitteeCreateParams{
		TenantID:      tenantID,
		TenderID:      tenderID,
		Name:          req.Name,
		CommitteeType: req.CommitteeType,
		ChairID:       req.ChairID,
		Mandate:       req.Mandate,
		Metadata:      req.Metadata,
	})
	if err != nil {
		if errors.Is(err, tender.ErrNotFound) {
			response.NotFound(w, "tender")
			return
		}
		h.logger.Error("failed to create committee", zap.Error(err))
		response.InternalError(w)
		return
	}

	response.Created(w, h.toCommitteeResponse(committee))
}

func (h *TenderCommitteeHandler) Get(w http.ResponseWriter, r *http.Request) {
	tenantID, err := h.getTenantID(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid tenant ID")
		return
	}

	committeeID, err := h.getCommitteeID(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid committee ID")
		return
	}

	committee, err := h.service.GetCommittee(r.Context(), tenantID, committeeID)
	if err != nil {
		if errors.Is(err, tender.ErrCommitteeNotFound) {
			response.NotFound(w, "committee")
			return
		}
		h.logger.Error("failed to get committee", zap.Error(err))
		response.InternalError(w)
		return
	}

	response.OK(w, h.toCommitteeResponse(committee))
}

func (h *TenderCommitteeHandler) List(w http.ResponseWriter, r *http.Request) {
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

	committees, err := h.service.ListCommittees(r.Context(), tenantID, tenderID)
	if err != nil {
		h.logger.Error("failed to list committees", zap.Error(err))
		response.InternalError(w)
		return
	}

	resp := ListCommitteesResponse{
		Committees: make([]CommitteeResponse, len(committees)),
		Total:      len(committees),
	}

	for i, c := range committees {
		resp.Committees[i] = h.toCommitteeResponse(c)
	}

	response.OK(w, resp)
}

func (h *TenderCommitteeHandler) Update(w http.ResponseWriter, r *http.Request) {
	tenantID, err := h.getTenantID(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid tenant ID")
		return
	}

	committeeID, err := h.getCommitteeID(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid committee ID")
		return
	}

	var req UpdateCommitteeRequest
	if err := validation.BindAndValidate(r, &req); err != nil {
		response.BindError(w, err)
		return
	}

	committee, err := h.service.UpdateCommittee(r.Context(), tenantID, committeeID, tender.CommitteeUpdateParams{
		Name:          req.Name,
		CommitteeType: req.CommitteeType,
		ChairID:       req.ChairID,
		Mandate:       req.Mandate,
		Status:        req.Status,
		Metadata:      req.Metadata,
	})
	if err != nil {
		if errors.Is(err, tender.ErrCommitteeNotFound) {
			response.NotFound(w, "committee")
			return
		}
		h.logger.Error("failed to update committee", zap.Error(err))
		response.InternalError(w)
		return
	}

	response.OK(w, h.toCommitteeResponse(committee))
}

func (h *TenderCommitteeHandler) Dissolve(w http.ResponseWriter, r *http.Request) {
	tenantID, err := h.getTenantID(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid tenant ID")
		return
	}

	committeeID, err := h.getCommitteeID(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid committee ID")
		return
	}

	committee, err := h.service.DissolveCommittee(r.Context(), tenantID, committeeID)
	if err != nil {
		if errors.Is(err, tender.ErrCommitteeNotFound) {
			response.NotFound(w, "committee")
			return
		}
		h.logger.Error("failed to dissolve committee", zap.Error(err))
		response.InternalError(w)
		return
	}

	response.OK(w, h.toCommitteeResponse(committee))
}

func (h *TenderCommitteeHandler) Delete(w http.ResponseWriter, r *http.Request) {
	tenantID, err := h.getTenantID(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid tenant ID")
		return
	}

	committeeID, err := h.getCommitteeID(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid committee ID")
		return
	}

	err = h.service.DeleteCommittee(r.Context(), tenantID, committeeID)
	if err != nil {
		if errors.Is(err, tender.ErrCommitteeNotFound) {
			response.NotFound(w, "committee")
			return
		}
		h.logger.Error("failed to delete committee", zap.Error(err))
		response.InternalError(w)
		return
	}

	response.NoContent(w)
}

// Member Handlers

func (h *TenderCommitteeHandler) AddMember(w http.ResponseWriter, r *http.Request) {
	tenantID, err := h.getTenantID(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid tenant ID")
		return
	}

	committeeID, err := h.getCommitteeID(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid committee ID")
		return
	}

	userID, err := h.getUserID(r)
	if err != nil {
		response.Unauthorized(w)
		return
	}

	var req AddMemberRequest
	if err := validation.BindAndValidate(r, &req); err != nil {
		response.BindError(w, err)
		return
	}

	member, err := h.service.AddMember(r.Context(), tender.MemberCreateParams{
		TenantID:    tenantID,
		CommitteeID: committeeID,
		UserID:      req.UserID,
		Role:        req.Role,
		Expertise:   req.Expertise,
		AddedBy:     userID,
		Metadata:    req.Metadata,
	})
	if err != nil {
		if errors.Is(err, tender.ErrCommitteeNotFound) {
			response.NotFound(w, "committee")
			return
		}
		if errors.Is(err, tender.ErrDuplicateMember) {
			response.Error(w, http.StatusConflict, "user is already a member of this committee")
			return
		}
		h.logger.Error("failed to add member", zap.Error(err))
		response.InternalError(w)
		return
	}

	response.Created(w, h.toMemberResponse(member))
}

func (h *TenderCommitteeHandler) GetMember(w http.ResponseWriter, r *http.Request) {
	tenantID, err := h.getTenantID(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid tenant ID")
		return
	}

	memberID, err := h.getMemberID(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid member ID")
		return
	}

	member, err := h.service.GetMember(r.Context(), tenantID, memberID)
	if err != nil {
		if errors.Is(err, tender.ErrMemberNotFound) {
			response.NotFound(w, "member")
			return
		}
		h.logger.Error("failed to get member", zap.Error(err))
		response.InternalError(w)
		return
	}

	response.OK(w, h.toMemberResponse(member))
}

func (h *TenderCommitteeHandler) UpdateMember(w http.ResponseWriter, r *http.Request) {
	tenantID, err := h.getTenantID(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid tenant ID")
		return
	}

	memberID, err := h.getMemberID(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid member ID")
		return
	}

	var req UpdateMemberRequest
	if err := validation.BindAndValidate(r, &req); err != nil {
		response.BindError(w, err)
		return
	}

	member, err := h.service.UpdateMember(r.Context(), tenantID, memberID, tender.MemberUpdateParams{
		Role:      req.Role,
		Expertise: req.Expertise,
		IsActive:  req.IsActive,
		Metadata:  req.Metadata,
	})
	if err != nil {
		if errors.Is(err, tender.ErrMemberNotFound) {
			response.NotFound(w, "member")
			return
		}
		h.logger.Error("failed to update member", zap.Error(err))
		response.InternalError(w)
		return
	}

	response.OK(w, h.toMemberResponse(member))
}

func (h *TenderCommitteeHandler) RemoveMember(w http.ResponseWriter, r *http.Request) {
	tenantID, err := h.getTenantID(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid tenant ID")
		return
	}

	memberID, err := h.getMemberID(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid member ID")
		return
	}

	err = h.service.RemoveMember(r.Context(), tenantID, memberID)
	if err != nil {
		if errors.Is(err, tender.ErrMemberNotFound) {
			response.NotFound(w, "member")
			return
		}
		h.logger.Error("failed to remove member", zap.Error(err))
		response.InternalError(w)
		return
	}

	response.NoContent(w)
}

// RegisterRoutes registers committee routes nested under tenders.
func (h *TenderCommitteeHandler) RegisterRoutes(r chi.Router) {
	r.Post("/", h.Create)
	r.Get("/", h.List)
	r.Get("/{committeeID}", h.Get)
	r.Patch("/{committeeID}", h.Update)
	r.Post("/{committeeID}/dissolve", h.Dissolve)
	r.Delete("/{committeeID}", h.Delete)

	// Member routes
	r.Post("/{committeeID}/members", h.AddMember)
	r.Get("/{committeeID}/members/{memberID}", h.GetMember)
	r.Patch("/{committeeID}/members/{memberID}", h.UpdateMember)
	r.Delete("/{committeeID}/members/{memberID}", h.RemoveMember)
}

// Helper methods

func (h *TenderCommitteeHandler) getTenantID(r *http.Request) (uuid.UUID, error) {
	return uuid.Parse(chi.URLParam(r, "tenantID"))
}

func (h *TenderCommitteeHandler) getTenderID(r *http.Request) (uuid.UUID, error) {
	return uuid.Parse(chi.URLParam(r, "tenderID"))
}

func (h *TenderCommitteeHandler) getCommitteeID(r *http.Request) (uuid.UUID, error) {
	return uuid.Parse(chi.URLParam(r, "committeeID"))
}

func (h *TenderCommitteeHandler) getMemberID(r *http.Request) (uuid.UUID, error) {
	return uuid.Parse(chi.URLParam(r, "memberID"))
}

func (h *TenderCommitteeHandler) getUserID(r *http.Request) (uuid.UUID, error) {
	userIDStr := r.Header.Get("X-User-ID")
	if userIDStr == "" {
		if userID, ok := r.Context().Value("user_id").(uuid.UUID); ok {
			return userID, nil
		}
		return uuid.Nil, errors.New("user ID not found")
	}
	return uuid.Parse(userIDStr)
}

func (h *TenderCommitteeHandler) toCommitteeResponse(c *ent.TenderCommittee) CommitteeResponse {
	resp := CommitteeResponse{
		ID:            c.ID,
		TenantID:      c.TenantID,
		TenderID:      c.TenderID,
		Name:          c.Name,
		CommitteeType: c.CommitteeType,
		Status:        c.Status,
		CreatedAt:     c.CreatedAt,
		UpdatedAt:     c.UpdatedAt,
	}

	if c.ChairID != uuid.Nil {
		resp.ChairID = &c.ChairID
	}
	if c.Mandate != "" {
		resp.Mandate = c.Mandate
	}
	if !c.DissolvedAt.IsZero() {
		resp.DissolvedAt = &c.DissolvedAt
	}
	if c.Metadata != nil {
		resp.Metadata = c.Metadata
	}

	// Include members if loaded
	if c.Edges.Members != nil {
		resp.Members = make([]MemberResponse, len(c.Edges.Members))
		for i, m := range c.Edges.Members {
			resp.Members[i] = h.toMemberResponse(m)
		}
	}

	return resp
}

func (h *TenderCommitteeHandler) toMemberResponse(m *ent.TenderCommitteeMember) MemberResponse {
	resp := MemberResponse{
		ID:          m.ID,
		TenantID:    m.TenantID,
		CommitteeID: m.CommitteeID,
		UserID:      m.UserID,
		Role:        m.Role,
		IsActive:    m.IsActive,
		AddedBy:     m.AddedBy,
		AddedAt:     m.AddedAt,
	}

	if m.Expertise != "" {
		resp.Expertise = m.Expertise
	}
	if !m.LeftAt.IsZero() {
		resp.LeftAt = &m.LeftAt
	}
	if m.Metadata != nil {
		resp.Metadata = m.Metadata
	}

	return resp
}
