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

// TenderMeetingHandler handles tender meeting HTTP requests.
type TenderMeetingHandler struct {
	logger  *zap.Logger
	service tender.TenderServiceInterface
}

// NewTenderMeetingHandler creates a new tender meeting handler.
func NewTenderMeetingHandler(logger *zap.Logger, service tender.TenderServiceInterface) *TenderMeetingHandler {
	return &TenderMeetingHandler{
		logger:  logger,
		service: service,
	}
}

// Request/Response types

type CreateMeetingRequest struct {
	CommitteeID     *uuid.UUID     `json:"committee_id,omitempty"`
	Title           string         `json:"title" validate:"required,min=1,max=255"`
	Description     string         `json:"description,omitempty" validate:"max=2000"`
	MeetingType     string         `json:"meeting_type,omitempty" validate:"omitempty,oneof=evaluation kickoff review decision clarification"`
	ScheduledAt     time.Time      `json:"scheduled_at" validate:"required"`
	DurationMinutes int            `json:"duration_minutes,omitempty" validate:"omitempty,min=15,max=480"`
	Location        string         `json:"location,omitempty" validate:"max=500"`
	Platform        string         `json:"platform,omitempty" validate:"omitempty,oneof=google_meet teams zoom zoho_meet"`
	MeetingURL      string         `json:"meeting_url,omitempty" validate:"omitempty,url"`
	MeetingID       string         `json:"meeting_id,omitempty" validate:"max=255"`
	Attendees       []uuid.UUID    `json:"attendees,omitempty"`
	Agenda          string         `json:"agenda,omitempty" validate:"max=5000"`
	Metadata        map[string]any `json:"metadata,omitempty"`
}

type UpdateMeetingRequest struct {
	Title           *string          `json:"title,omitempty" validate:"omitempty,min=1,max=255"`
	Description     *string          `json:"description,omitempty" validate:"omitempty,max=2000"`
	MeetingType     *string          `json:"meeting_type,omitempty" validate:"omitempty,oneof=evaluation kickoff review decision clarification"`
	ScheduledAt     *time.Time       `json:"scheduled_at,omitempty"`
	DurationMinutes *int             `json:"duration_minutes,omitempty" validate:"omitempty,min=15,max=480"`
	Location        *string          `json:"location,omitempty" validate:"omitempty,max=500"`
	Platform        *string          `json:"platform,omitempty" validate:"omitempty,oneof=google_meet teams zoom zoho_meet"`
	MeetingURL      *string          `json:"meeting_url,omitempty" validate:"omitempty,url"`
	MeetingID       *string          `json:"meeting_id,omitempty" validate:"omitempty,max=255"`
	Status          *string          `json:"status,omitempty" validate:"omitempty,oneof=scheduled in_progress completed cancelled"`
	Attendees       []uuid.UUID      `json:"attendees,omitempty"`
	Agenda          *string          `json:"agenda,omitempty" validate:"omitempty,max=5000"`
	Minutes         *string          `json:"minutes,omitempty" validate:"omitempty,max=10000"`
	Decisions       []map[string]any `json:"decisions,omitempty"`
	ActionItems     []map[string]any `json:"action_items,omitempty"`
	RecordingURL    *string          `json:"recording_url,omitempty" validate:"omitempty,url"`
	Metadata        map[string]any   `json:"metadata,omitempty"`
}

type EndMeetingRequest struct {
	Minutes     string           `json:"minutes,omitempty" validate:"max=10000"`
	Decisions   []map[string]any `json:"decisions,omitempty"`
	ActionItems []map[string]any `json:"action_items,omitempty"`
}

type MeetingResponse struct {
	ID              uuid.UUID          `json:"id"`
	TenantID        uuid.UUID          `json:"tenant_id"`
	TenderID        uuid.UUID          `json:"tender_id"`
	CommitteeID     *uuid.UUID         `json:"committee_id,omitempty"`
	Title           string             `json:"title"`
	Description     string             `json:"description,omitempty"`
	MeetingType     string             `json:"meeting_type"`
	ScheduledAt     time.Time          `json:"scheduled_at"`
	DurationMinutes int                `json:"duration_minutes"`
	Location        string             `json:"location,omitempty"`
	Platform        string             `json:"platform,omitempty"`
	MeetingURL      string             `json:"meeting_url,omitempty"`
	MeetingID       string             `json:"meeting_id,omitempty"`
	Status          string             `json:"status"`
	Attendees       []uuid.UUID        `json:"attendees,omitempty"`
	Agenda          string             `json:"agenda,omitempty"`
	Minutes         string             `json:"minutes,omitempty"`
	Decisions       []map[string]any   `json:"decisions,omitempty"`
	ActionItems     []map[string]any   `json:"action_items,omitempty"`
	RecordingURL    string             `json:"recording_url,omitempty"`
	OrganizedBy     uuid.UUID          `json:"organized_by"`
	StartedAt       *time.Time         `json:"started_at,omitempty"`
	EndedAt         *time.Time         `json:"ended_at,omitempty"`
	CreatedAt       time.Time          `json:"created_at"`
	UpdatedAt       time.Time          `json:"updated_at"`
	Metadata        map[string]any     `json:"metadata,omitempty"`
}

type ListMeetingsResponse struct {
	Meetings []MeetingResponse `json:"meetings"`
	Total    int               `json:"total"`
	Limit    int               `json:"limit"`
	Offset   int               `json:"offset"`
}

// Handlers

func (h *TenderMeetingHandler) Create(w http.ResponseWriter, r *http.Request) {
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

	var req CreateMeetingRequest
	if err := validation.BindAndValidate(r, &req); err != nil {
		response.BindError(w, err)
		return
	}

	meeting, err := h.service.CreateMeeting(r.Context(), tender.MeetingCreateParams{
		TenantID:        tenantID,
		TenderID:        tenderID,
		CommitteeID:     req.CommitteeID,
		Title:           req.Title,
		Description:     req.Description,
		MeetingType:     req.MeetingType,
		ScheduledAt:     req.ScheduledAt,
		DurationMinutes: req.DurationMinutes,
		Location:        req.Location,
		Platform:        req.Platform,
		MeetingURL:      req.MeetingURL,
		MeetingID:       req.MeetingID,
		Attendees:       req.Attendees,
		Agenda:          req.Agenda,
		OrganizedBy:     userID,
		Metadata:        req.Metadata,
	})
	if err != nil {
		if errors.Is(err, tender.ErrNotFound) {
			response.NotFound(w, "tender")
			return
		}
		h.logger.Error("failed to create meeting", zap.Error(err))
		response.InternalError(w)
		return
	}

	response.Created(w, h.toMeetingResponse(meeting))
}

func (h *TenderMeetingHandler) Get(w http.ResponseWriter, r *http.Request) {
	tenantID, err := h.getTenantID(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid tenant ID")
		return
	}

	meetingID, err := h.getMeetingID(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid meeting ID")
		return
	}

	meeting, err := h.service.GetMeeting(r.Context(), tenantID, meetingID)
	if err != nil {
		if errors.Is(err, tender.ErrMeetingNotFound) {
			response.NotFound(w, "meeting")
			return
		}
		h.logger.Error("failed to get meeting", zap.Error(err))
		response.InternalError(w)
		return
	}

	response.OK(w, h.toMeetingResponse(meeting))
}

func (h *TenderMeetingHandler) List(w http.ResponseWriter, r *http.Request) {
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

	params := tender.MeetingListParams{
		TenantID:    tenantID,
		TenderID:    tenderID,
		Status:      r.URL.Query().Get("status"),
		MeetingType: r.URL.Query().Get("meeting_type"),
		Limit:       h.parseIntQuery(r, "limit", 20),
		Offset:      h.parseIntQuery(r, "offset", 0),
	}

	// Parse committee_id if provided
	if cid := r.URL.Query().Get("committee_id"); cid != "" {
		if committeeID, err := uuid.Parse(cid); err == nil {
			params.CommitteeID = &committeeID
		}
	}

	// Parse date filters
	if from := r.URL.Query().Get("from_date"); from != "" {
		if t, err := time.Parse(time.RFC3339, from); err == nil {
			params.FromDate = &t
		}
	}
	if to := r.URL.Query().Get("to_date"); to != "" {
		if t, err := time.Parse(time.RFC3339, to); err == nil {
			params.ToDate = &t
		}
	}

	meetings, total, err := h.service.ListMeetings(r.Context(), params)
	if err != nil {
		h.logger.Error("failed to list meetings", zap.Error(err))
		response.InternalError(w)
		return
	}

	resp := ListMeetingsResponse{
		Meetings: make([]MeetingResponse, len(meetings)),
		Total:    total,
		Limit:    params.Limit,
		Offset:   params.Offset,
	}

	for i, m := range meetings {
		resp.Meetings[i] = h.toMeetingResponse(m)
	}

	response.OK(w, resp)
}

func (h *TenderMeetingHandler) Update(w http.ResponseWriter, r *http.Request) {
	tenantID, err := h.getTenantID(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid tenant ID")
		return
	}

	meetingID, err := h.getMeetingID(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid meeting ID")
		return
	}

	var req UpdateMeetingRequest
	if err := validation.BindAndValidate(r, &req); err != nil {
		response.BindError(w, err)
		return
	}

	meeting, err := h.service.UpdateMeeting(r.Context(), tenantID, meetingID, tender.MeetingUpdateParams{
		Title:           req.Title,
		Description:     req.Description,
		MeetingType:     req.MeetingType,
		ScheduledAt:     req.ScheduledAt,
		DurationMinutes: req.DurationMinutes,
		Location:        req.Location,
		Platform:        req.Platform,
		MeetingURL:      req.MeetingURL,
		MeetingID:       req.MeetingID,
		Status:          req.Status,
		Attendees:       req.Attendees,
		Agenda:          req.Agenda,
		Minutes:         req.Minutes,
		Decisions:       req.Decisions,
		ActionItems:     req.ActionItems,
		RecordingURL:    req.RecordingURL,
		Metadata:        req.Metadata,
	})
	if err != nil {
		if errors.Is(err, tender.ErrMeetingNotFound) {
			response.NotFound(w, "meeting")
			return
		}
		h.logger.Error("failed to update meeting", zap.Error(err))
		response.InternalError(w)
		return
	}

	response.OK(w, h.toMeetingResponse(meeting))
}

func (h *TenderMeetingHandler) Start(w http.ResponseWriter, r *http.Request) {
	tenantID, err := h.getTenantID(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid tenant ID")
		return
	}

	meetingID, err := h.getMeetingID(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid meeting ID")
		return
	}

	meeting, err := h.service.StartMeeting(r.Context(), tenantID, meetingID)
	if err != nil {
		if errors.Is(err, tender.ErrMeetingNotFound) {
			response.NotFound(w, "meeting")
			return
		}
		h.logger.Error("failed to start meeting", zap.Error(err))
		response.InternalError(w)
		return
	}

	response.OK(w, h.toMeetingResponse(meeting))
}

func (h *TenderMeetingHandler) End(w http.ResponseWriter, r *http.Request) {
	tenantID, err := h.getTenantID(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid tenant ID")
		return
	}

	meetingID, err := h.getMeetingID(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid meeting ID")
		return
	}

	var req EndMeetingRequest
	if err := validation.BindAndValidate(r, &req); err != nil {
		response.BindError(w, err)
		return
	}

	meeting, err := h.service.EndMeeting(r.Context(), tenantID, meetingID, req.Minutes, req.Decisions, req.ActionItems)
	if err != nil {
		if errors.Is(err, tender.ErrMeetingNotFound) {
			response.NotFound(w, "meeting")
			return
		}
		h.logger.Error("failed to end meeting", zap.Error(err))
		response.InternalError(w)
		return
	}

	response.OK(w, h.toMeetingResponse(meeting))
}

func (h *TenderMeetingHandler) Cancel(w http.ResponseWriter, r *http.Request) {
	tenantID, err := h.getTenantID(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid tenant ID")
		return
	}

	meetingID, err := h.getMeetingID(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid meeting ID")
		return
	}

	meeting, err := h.service.CancelMeeting(r.Context(), tenantID, meetingID)
	if err != nil {
		if errors.Is(err, tender.ErrMeetingNotFound) {
			response.NotFound(w, "meeting")
			return
		}
		h.logger.Error("failed to cancel meeting", zap.Error(err))
		response.InternalError(w)
		return
	}

	response.OK(w, h.toMeetingResponse(meeting))
}

func (h *TenderMeetingHandler) Delete(w http.ResponseWriter, r *http.Request) {
	tenantID, err := h.getTenantID(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid tenant ID")
		return
	}

	meetingID, err := h.getMeetingID(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid meeting ID")
		return
	}

	err = h.service.DeleteMeeting(r.Context(), tenantID, meetingID)
	if err != nil {
		if errors.Is(err, tender.ErrMeetingNotFound) {
			response.NotFound(w, "meeting")
			return
		}
		h.logger.Error("failed to delete meeting", zap.Error(err))
		response.InternalError(w)
		return
	}

	response.NoContent(w)
}

// RegisterRoutes registers meeting routes nested under tenders.
func (h *TenderMeetingHandler) RegisterRoutes(r chi.Router) {
	r.Post("/", h.Create)
	r.Get("/", h.List)
	r.Get("/{meetingID}", h.Get)
	r.Patch("/{meetingID}", h.Update)
	r.Post("/{meetingID}/start", h.Start)
	r.Post("/{meetingID}/end", h.End)
	r.Post("/{meetingID}/cancel", h.Cancel)
	r.Delete("/{meetingID}", h.Delete)
}

// Helper methods

func (h *TenderMeetingHandler) getTenantID(r *http.Request) (uuid.UUID, error) {
	return uuid.Parse(chi.URLParam(r, "tenantID"))
}

func (h *TenderMeetingHandler) getTenderID(r *http.Request) (uuid.UUID, error) {
	return uuid.Parse(chi.URLParam(r, "tenderID"))
}

func (h *TenderMeetingHandler) getMeetingID(r *http.Request) (uuid.UUID, error) {
	return uuid.Parse(chi.URLParam(r, "meetingID"))
}

func (h *TenderMeetingHandler) getUserID(r *http.Request) (uuid.UUID, error) {
	userIDStr := r.Header.Get("X-User-ID")
	if userIDStr == "" {
		if userID, ok := r.Context().Value("user_id").(uuid.UUID); ok {
			return userID, nil
		}
		return uuid.Nil, errors.New("user ID not found")
	}
	return uuid.Parse(userIDStr)
}

func (h *TenderMeetingHandler) parseIntQuery(r *http.Request, key string, defaultVal int) int {
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

func (h *TenderMeetingHandler) toMeetingResponse(m *ent.TenderMeeting) MeetingResponse {
	resp := MeetingResponse{
		ID:              m.ID,
		TenantID:        m.TenantID,
		TenderID:        m.TenderID,
		Title:           m.Title,
		MeetingType:     m.MeetingType,
		ScheduledAt:     m.ScheduledAt,
		DurationMinutes: m.DurationMinutes,
		Status:          m.Status,
		OrganizedBy:     m.OrganizedBy,
		CreatedAt:       m.CreatedAt,
		UpdatedAt:       m.UpdatedAt,
	}

	if m.CommitteeID != uuid.Nil {
		resp.CommitteeID = &m.CommitteeID
	}
	if m.Description != "" {
		resp.Description = m.Description
	}
	if m.Location != "" {
		resp.Location = m.Location
	}
	if m.Platform != "" {
		resp.Platform = m.Platform
	}
	if m.MeetingURL != "" {
		resp.MeetingURL = m.MeetingURL
	}
	if m.MeetingID != "" {
		resp.MeetingID = m.MeetingID
	}
	if len(m.Attendees) > 0 {
		resp.Attendees = m.Attendees
	}
	if m.Agenda != "" {
		resp.Agenda = m.Agenda
	}
	if m.Minutes != "" {
		resp.Minutes = m.Minutes
	}
	if len(m.Decisions) > 0 {
		resp.Decisions = m.Decisions
	}
	if len(m.ActionItems) > 0 {
		resp.ActionItems = m.ActionItems
	}
	if m.RecordingURL != "" {
		resp.RecordingURL = m.RecordingURL
	}
	if !m.StartedAt.IsZero() {
		resp.StartedAt = &m.StartedAt
	}
	if !m.EndedAt.IsZero() {
		resp.EndedAt = &m.EndedAt
	}
	if m.Metadata != nil {
		resp.Metadata = m.Metadata
	}

	return resp
}
