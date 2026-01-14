package tender

import (
	"context"
	"fmt"
	"time"

	"github.com/bengobox/projects-service/internal/ent"
	"github.com/bengobox/projects-service/internal/ent/tender"
	"github.com/bengobox/projects-service/internal/ent/tendermeeting"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// MeetingCreateParams holds parameters for creating a tender meeting.
type MeetingCreateParams struct {
	TenantID        uuid.UUID
	TenderID        uuid.UUID
	CommitteeID     *uuid.UUID
	Title           string
	Description     string
	MeetingType     string
	ScheduledAt     time.Time
	DurationMinutes int
	Location        string
	Platform        string
	MeetingURL      string
	MeetingID       string
	Attendees       []uuid.UUID
	Agenda          string
	OrganizedBy     uuid.UUID
	Metadata        map[string]any
}

// MeetingUpdateParams holds parameters for updating a meeting.
type MeetingUpdateParams struct {
	Title           *string
	Description     *string
	MeetingType     *string
	ScheduledAt     *time.Time
	DurationMinutes *int
	Location        *string
	Platform        *string
	MeetingURL      *string
	MeetingID       *string
	Status          *string
	Attendees       []uuid.UUID
	Agenda          *string
	Minutes         *string
	Decisions       []map[string]any
	ActionItems     []map[string]any
	RecordingURL    *string
	StartedAt       *time.Time
	EndedAt         *time.Time
	Metadata        map[string]any
}

// MeetingListParams holds parameters for listing meetings.
type MeetingListParams struct {
	TenantID    uuid.UUID
	TenderID    uuid.UUID
	CommitteeID *uuid.UUID
	Status      string
	MeetingType string
	FromDate    *time.Time
	ToDate      *time.Time
	Limit       int
	Offset      int
}

// CreateMeeting creates a new tender meeting.
func (s *Service) CreateMeeting(ctx context.Context, params MeetingCreateParams) (*ent.TenderMeeting, error) {
	// Verify tender exists and belongs to tenant
	_, err := s.db.Tender.Query().
		Where(
			tender.ID(params.TenderID),
			tender.TenantID(params.TenantID),
		).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to verify tender: %w", err)
	}

	builder := s.db.TenderMeeting.Create().
		SetTenantID(params.TenantID).
		SetTenderID(params.TenderID).
		SetTitle(params.Title).
		SetScheduledAt(params.ScheduledAt).
		SetOrganizedBy(params.OrganizedBy)

	if params.CommitteeID != nil {
		builder.SetCommitteeID(*params.CommitteeID)
	}
	if params.Description != "" {
		builder.SetDescription(params.Description)
	}
	if params.MeetingType != "" {
		builder.SetMeetingType(params.MeetingType)
	}
	if params.DurationMinutes > 0 {
		builder.SetDurationMinutes(params.DurationMinutes)
	}
	if params.Location != "" {
		builder.SetLocation(params.Location)
	}
	if params.Platform != "" {
		builder.SetPlatform(params.Platform)
	}
	if params.MeetingURL != "" {
		builder.SetMeetingURL(params.MeetingURL)
	}
	if params.MeetingID != "" {
		builder.SetMeetingID(params.MeetingID)
	}
	if len(params.Attendees) > 0 {
		builder.SetAttendees(params.Attendees)
	}
	if params.Agenda != "" {
		builder.SetAgenda(params.Agenda)
	}
	if params.Metadata != nil {
		builder.SetMetadata(params.Metadata)
	}

	meeting, err := builder.Save(ctx)
	if err != nil {
		s.logger.Error("failed to create tender meeting",
			zap.String("tender_id", params.TenderID.String()),
			zap.String("title", params.Title),
			zap.Error(err),
		)
		return nil, fmt.Errorf("failed to create tender meeting: %w", err)
	}

	s.logger.Info("tender meeting created",
		zap.String("meeting_id", meeting.ID.String()),
		zap.String("tender_id", params.TenderID.String()),
	)

	return meeting, nil
}

// GetMeeting retrieves a meeting by ID.
func (s *Service) GetMeeting(ctx context.Context, tenantID, meetingID uuid.UUID) (*ent.TenderMeeting, error) {
	meeting, err := s.db.TenderMeeting.Query().
		Where(
			tendermeeting.ID(meetingID),
			tendermeeting.TenantID(tenantID),
		).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrMeetingNotFound
		}
		return nil, fmt.Errorf("failed to get tender meeting: %w", err)
	}
	return meeting, nil
}

// ListMeetings retrieves meetings for a tender with optional filters.
func (s *Service) ListMeetings(ctx context.Context, params MeetingListParams) ([]*ent.TenderMeeting, int, error) {
	query := s.db.TenderMeeting.Query().
		Where(
			tendermeeting.TenantID(params.TenantID),
			tendermeeting.TenderID(params.TenderID),
		)

	if params.CommitteeID != nil {
		query = query.Where(tendermeeting.CommitteeID(*params.CommitteeID))
	}
	if params.Status != "" {
		query = query.Where(tendermeeting.Status(params.Status))
	}
	if params.MeetingType != "" {
		query = query.Where(tendermeeting.MeetingType(params.MeetingType))
	}
	if params.FromDate != nil {
		query = query.Where(tendermeeting.ScheduledAtGTE(*params.FromDate))
	}
	if params.ToDate != nil {
		query = query.Where(tendermeeting.ScheduledAtLTE(*params.ToDate))
	}

	total, err := query.Clone().Count(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to count meetings: %w", err)
	}

	if params.Limit > 0 {
		query = query.Limit(params.Limit)
	}
	if params.Offset > 0 {
		query = query.Offset(params.Offset)
	}

	query = query.Order(ent.Asc(tendermeeting.FieldScheduledAt))

	meetings, err := query.All(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to list meetings: %w", err)
	}

	return meetings, total, nil
}

// UpdateMeeting updates a tender meeting.
func (s *Service) UpdateMeeting(ctx context.Context, tenantID, meetingID uuid.UUID, params MeetingUpdateParams) (*ent.TenderMeeting, error) {
	_, err := s.GetMeeting(ctx, tenantID, meetingID)
	if err != nil {
		return nil, err
	}

	builder := s.db.TenderMeeting.UpdateOneID(meetingID)

	if params.Title != nil {
		builder.SetTitle(*params.Title)
	}
	if params.Description != nil {
		builder.SetDescription(*params.Description)
	}
	if params.MeetingType != nil {
		builder.SetMeetingType(*params.MeetingType)
	}
	if params.ScheduledAt != nil {
		builder.SetScheduledAt(*params.ScheduledAt)
	}
	if params.DurationMinutes != nil {
		builder.SetDurationMinutes(*params.DurationMinutes)
	}
	if params.Location != nil {
		builder.SetLocation(*params.Location)
	}
	if params.Platform != nil {
		builder.SetPlatform(*params.Platform)
	}
	if params.MeetingURL != nil {
		builder.SetMeetingURL(*params.MeetingURL)
	}
	if params.MeetingID != nil {
		builder.SetMeetingID(*params.MeetingID)
	}
	if params.Status != nil {
		builder.SetStatus(*params.Status)
	}
	if params.Attendees != nil {
		builder.SetAttendees(params.Attendees)
	}
	if params.Agenda != nil {
		builder.SetAgenda(*params.Agenda)
	}
	if params.Minutes != nil {
		builder.SetMinutes(*params.Minutes)
	}
	if params.Decisions != nil {
		builder.SetDecisions(params.Decisions)
	}
	if params.ActionItems != nil {
		builder.SetActionItems(params.ActionItems)
	}
	if params.RecordingURL != nil {
		builder.SetRecordingURL(*params.RecordingURL)
	}
	if params.StartedAt != nil {
		builder.SetStartedAt(*params.StartedAt)
	}
	if params.EndedAt != nil {
		builder.SetEndedAt(*params.EndedAt)
	}
	if params.Metadata != nil {
		builder.SetMetadata(params.Metadata)
	}

	meeting, err := builder.Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to update tender meeting: %w", err)
	}

	return meeting, nil
}

// StartMeeting marks a meeting as in progress.
func (s *Service) StartMeeting(ctx context.Context, tenantID, meetingID uuid.UUID) (*ent.TenderMeeting, error) {
	now := time.Now()
	status := "in_progress"
	return s.UpdateMeeting(ctx, tenantID, meetingID, MeetingUpdateParams{
		Status:    &status,
		StartedAt: &now,
	})
}

// EndMeeting marks a meeting as completed.
func (s *Service) EndMeeting(ctx context.Context, tenantID, meetingID uuid.UUID, minutes string, decisions []map[string]any, actionItems []map[string]any) (*ent.TenderMeeting, error) {
	now := time.Now()
	status := "completed"
	params := MeetingUpdateParams{
		Status:  &status,
		EndedAt: &now,
	}

	if minutes != "" {
		params.Minutes = &minutes
	}
	if decisions != nil {
		params.Decisions = decisions
	}
	if actionItems != nil {
		params.ActionItems = actionItems
	}

	return s.UpdateMeeting(ctx, tenantID, meetingID, params)
}

// CancelMeeting cancels a scheduled meeting.
func (s *Service) CancelMeeting(ctx context.Context, tenantID, meetingID uuid.UUID) (*ent.TenderMeeting, error) {
	status := "cancelled"
	return s.UpdateMeeting(ctx, tenantID, meetingID, MeetingUpdateParams{
		Status: &status,
	})
}

// DeleteMeeting removes a meeting.
func (s *Service) DeleteMeeting(ctx context.Context, tenantID, meetingID uuid.UUID) error {
	_, err := s.GetMeeting(ctx, tenantID, meetingID)
	if err != nil {
		return err
	}

	err = s.db.TenderMeeting.DeleteOneID(meetingID).Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to delete meeting: %w", err)
	}

	s.logger.Info("tender meeting deleted",
		zap.String("meeting_id", meetingID.String()),
	)

	return nil
}
