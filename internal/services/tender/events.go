package tender

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	events "github.com/Bengo-Hub/shared-events"
	"github.com/google/uuid"
)

// Event type constants following the pattern: service.entity.action
const (
	// Tender lifecycle events
	EventTenderCreated       = "projects.tender.created"
	EventTenderUpdated       = "projects.tender.updated"
	EventTenderStatusChanged = "projects.tender.status_changed"
	EventTenderDecisionMade  = "projects.tender.decision_made"
	EventTenderSubmitted     = "projects.tender.submitted"
	EventTenderAwarded       = "projects.tender.awarded"
	EventTenderDeleted       = "projects.tender.deleted"

	// Document events
	EventDocumentUploaded   = "projects.tender_document.uploaded"
	EventDocumentVersioned  = "projects.tender_document.versioned"
	EventDocumentDeleted    = "projects.tender_document.deleted"

	// Committee events
	EventCommitteeCreated   = "projects.tender_committee.created"
	EventCommitteeDissolved = "projects.tender_committee.dissolved"
	EventMemberAdded        = "projects.tender_committee.member_added"
	EventMemberRemoved      = "projects.tender_committee.member_removed"

	// Evaluation events
	EventEvaluationCreated   = "projects.tender_evaluation.created"
	EventEvaluationSubmitted = "projects.tender_evaluation.submitted"
	EventEvaluationDeleted   = "projects.tender_evaluation.deleted"

	// Meeting events
	EventMeetingScheduled = "projects.tender_meeting.scheduled"
	EventMeetingStarted   = "projects.tender_meeting.started"
	EventMeetingCompleted = "projects.tender_meeting.completed"
	EventMeetingCancelled = "projects.tender_meeting.cancelled"

	// Section events
	EventSectionCreated         = "projects.tender_section.created"
	EventSectionAssigned        = "projects.tender_section.assigned"
	EventSectionSubmittedReview = "projects.tender_section.submitted_for_review"
	EventSectionApproved        = "projects.tender_section.approved"
	EventSectionRejected        = "projects.tender_section.rejected"

	// Submission events
	EventSubmissionCreated   = "projects.tender_submission.created"
	EventSubmissionSubmitted = "projects.tender_submission.submitted"
	EventSubmissionConfirmed = "projects.tender_submission.delivery_confirmed"
)

// Aggregate type constant
const AggregateTypeTender = "Tender"

// EventPublisher defines the interface for publishing domain events.
type EventPublisher interface {
	// PublishInTx publishes an event within an existing transaction.
	PublishInTx(ctx context.Context, tx *sql.Tx, event *DomainEvent) error
}

// DomainEvent represents a domain event to be published.
type DomainEvent struct {
	ID            uuid.UUID      `json:"event_id"`
	TenantID      uuid.UUID      `json:"tenant_id"`
	AggregateType string         `json:"aggregate_type"`
	AggregateID   uuid.UUID      `json:"aggregate_id"`
	EventType     string         `json:"event_type"`
	Timestamp     time.Time      `json:"timestamp"`
	Data          map[string]any `json:"data"`
}

// NewDomainEvent creates a new domain event with auto-generated ID and timestamp.
func NewDomainEvent(tenantID, aggregateID uuid.UUID, eventType string, data map[string]any) *DomainEvent {
	return &DomainEvent{
		ID:            uuid.New(),
		TenantID:      tenantID,
		AggregateType: AggregateTypeTender,
		AggregateID:   aggregateID,
		EventType:     eventType,
		Timestamp:     time.Now().UTC(),
		Data:          data,
	}
}

// OutboxEventPublisher implements EventPublisher using the outbox pattern.
type OutboxEventPublisher struct {
	repo OutboxRepository
}

// OutboxRepository defines the interface for outbox operations.
type OutboxRepository interface {
	CreateOutboxRecord(ctx context.Context, tx *sql.Tx, record *events.OutboxRecord) error
}

// NewOutboxEventPublisher creates a new outbox-based event publisher.
func NewOutboxEventPublisher(repo OutboxRepository) *OutboxEventPublisher {
	return &OutboxEventPublisher{repo: repo}
}

// PublishInTx publishes an event by inserting it into the outbox table within the transaction.
func (p *OutboxEventPublisher) PublishInTx(ctx context.Context, tx *sql.Tx, event *DomainEvent) error {
	payload, err := json.Marshal(event.Data)
	if err != nil {
		return err
	}

	record := &events.OutboxRecord{
		ID:            event.ID,
		TenantID:      event.TenantID,
		AggregateType: event.AggregateType,
		AggregateID:   event.AggregateID,
		EventType:     event.EventType,
		Payload:       payload,
		Status:        events.StatusPending,
		Attempts:      0,
		CreatedAt:     event.Timestamp,
	}

	return p.repo.CreateOutboxRecord(ctx, tx, record)
}

// NoOpEventPublisher is a no-op implementation for when events are disabled.
type NoOpEventPublisher struct{}

// NewNoOpEventPublisher creates a no-op event publisher.
func NewNoOpEventPublisher() *NoOpEventPublisher {
	return &NoOpEventPublisher{}
}

// PublishInTx does nothing for no-op publisher.
func (p *NoOpEventPublisher) PublishInTx(_ context.Context, _ *sql.Tx, _ *DomainEvent) error {
	return nil
}

// Event data builders for type safety and consistency

// TenderCreatedData builds event data for tender creation.
func TenderCreatedData(tenderID uuid.UUID, tenderNumber, title, clientName string, deadline time.Time, createdBy uuid.UUID) map[string]any {
	return map[string]any{
		"tender_id":     tenderID.String(),
		"tender_number": tenderNumber,
		"title":         title,
		"client_name":   clientName,
		"deadline":      deadline.Format(time.RFC3339),
		"created_by":    createdBy.String(),
	}
}

// TenderStatusChangedData builds event data for status changes.
func TenderStatusChangedData(tenderID uuid.UUID, oldStatus, newStatus string) map[string]any {
	return map[string]any{
		"tender_id":  tenderID.String(),
		"old_status": oldStatus,
		"new_status": newStatus,
	}
}

// TenderDecisionData builds event data for tender decisions.
func TenderDecisionData(tenderID uuid.UUID, decision, rationale string, decidedBy uuid.UUID) map[string]any {
	return map[string]any{
		"tender_id":  tenderID.String(),
		"decision":   decision,
		"rationale":  rationale,
		"decided_by": decidedBy.String(),
	}
}

// TenderSubmittedData builds event data for tender submission.
func TenderSubmittedData(tenderID, submissionID uuid.UUID, submissionType string, submittedBy uuid.UUID) map[string]any {
	return map[string]any{
		"tender_id":       tenderID.String(),
		"submission_id":   submissionID.String(),
		"submission_type": submissionType,
		"submitted_by":    submittedBy.String(),
	}
}

// TenderAwardedData builds event data for tender award.
func TenderAwardedData(tenderID uuid.UUID, awardValue float64, currency string) map[string]any {
	return map[string]any{
		"tender_id":   tenderID.String(),
		"award_value": awardValue,
		"currency":    currency,
	}
}

// DocumentUploadedData builds event data for document upload.
func DocumentUploadedData(tenderID, documentID uuid.UUID, name, documentType string, uploadedBy uuid.UUID) map[string]any {
	return map[string]any{
		"tender_id":     tenderID.String(),
		"document_id":   documentID.String(),
		"name":          name,
		"document_type": documentType,
		"uploaded_by":   uploadedBy.String(),
	}
}

// CommitteeCreatedData builds event data for committee creation.
func CommitteeCreatedData(tenderID, committeeID uuid.UUID, name, committeeType string) map[string]any {
	return map[string]any{
		"tender_id":      tenderID.String(),
		"committee_id":   committeeID.String(),
		"name":           name,
		"committee_type": committeeType,
	}
}

// MemberAddedData builds event data for adding committee member.
func MemberAddedData(committeeID, memberID, userID uuid.UUID, role string) map[string]any {
	return map[string]any{
		"committee_id": committeeID.String(),
		"member_id":    memberID.String(),
		"user_id":      userID.String(),
		"role":         role,
	}
}

// EvaluationCreatedData builds event data for evaluation creation.
func EvaluationCreatedData(tenderID, evaluationID, evaluatorID uuid.UUID) map[string]any {
	return map[string]any{
		"tender_id":     tenderID.String(),
		"evaluation_id": evaluationID.String(),
		"evaluator_id":  evaluatorID.String(),
	}
}

// EvaluationSubmittedData builds event data for evaluation submission.
func EvaluationSubmittedData(tenderID, evaluationID uuid.UUID, vote string, overallScore float64) map[string]any {
	return map[string]any{
		"tender_id":     tenderID.String(),
		"evaluation_id": evaluationID.String(),
		"vote":          vote,
		"overall_score": overallScore,
	}
}

// MeetingScheduledData builds event data for meeting scheduling.
func MeetingScheduledData(tenderID, meetingID uuid.UUID, title string, scheduledAt time.Time, attendees []uuid.UUID) map[string]any {
	attendeeStrings := make([]string, len(attendees))
	for i, a := range attendees {
		attendeeStrings[i] = a.String()
	}
	return map[string]any{
		"tender_id":    tenderID.String(),
		"meeting_id":   meetingID.String(),
		"title":        title,
		"scheduled_at": scheduledAt.Format(time.RFC3339),
		"attendees":    attendeeStrings,
	}
}

// MeetingCompletedData builds event data for meeting completion.
func MeetingCompletedData(tenderID, meetingID uuid.UUID, decisionsCount, actionItemsCount int) map[string]any {
	return map[string]any{
		"tender_id":          tenderID.String(),
		"meeting_id":         meetingID.String(),
		"decisions_count":    decisionsCount,
		"action_items_count": actionItemsCount,
	}
}

// SectionAssignedData builds event data for section assignment.
func SectionAssignedData(tenderID, sectionID, assigneeID uuid.UUID, title string) map[string]any {
	return map[string]any{
		"tender_id":   tenderID.String(),
		"section_id":  sectionID.String(),
		"assignee_id": assigneeID.String(),
		"title":       title,
	}
}

// SectionReviewData builds event data for section review status changes.
func SectionReviewData(tenderID, sectionID uuid.UUID, status string, reviewerID uuid.UUID) map[string]any {
	return map[string]any{
		"tender_id":   tenderID.String(),
		"section_id":  sectionID.String(),
		"status":      status,
		"reviewer_id": reviewerID.String(),
	}
}

// SubmissionConfirmedData builds event data for delivery confirmation.
func SubmissionConfirmedData(tenderID, submissionID uuid.UUID, deliveredAt time.Time) map[string]any {
	return map[string]any{
		"tender_id":     tenderID.String(),
		"submission_id": submissionID.String(),
		"delivered_at":  deliveredAt.Format(time.RFC3339),
	}
}
