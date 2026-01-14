package tender

import (
	"context"
	"database/sql"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// MockEventPublisher records published events for testing.
type MockEventPublisher struct {
	mu     sync.Mutex
	events []*DomainEvent
}

func NewMockEventPublisher() *MockEventPublisher {
	return &MockEventPublisher{
		events: make([]*DomainEvent, 0),
	}
}

func (m *MockEventPublisher) PublishInTx(_ context.Context, _ *sql.Tx, event *DomainEvent) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, event)
	return nil
}

func (m *MockEventPublisher) Events() []*DomainEvent {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.events
}

func (m *MockEventPublisher) Clear() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = make([]*DomainEvent, 0)
}

func (m *MockEventPublisher) LastEvent() *DomainEvent {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.events) == 0 {
		return nil
	}
	return m.events[len(m.events)-1]
}

func (m *MockEventPublisher) EventCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.events)
}

func TestNewDomainEvent(t *testing.T) {
	tenantID := uuid.New()
	aggregateID := uuid.New()
	eventType := EventTenderCreated
	data := map[string]any{"key": "value"}

	event := NewDomainEvent(tenantID, aggregateID, eventType, data)

	assert.NotEqual(t, uuid.Nil, event.ID)
	assert.Equal(t, tenantID, event.TenantID)
	assert.Equal(t, aggregateID, event.AggregateID)
	assert.Equal(t, AggregateTypeTender, event.AggregateType)
	assert.Equal(t, eventType, event.EventType)
	assert.Equal(t, data, event.Data)
	assert.False(t, event.Timestamp.IsZero())
}

func TestEventConstants(t *testing.T) {
	// Verify event type naming convention
	eventTypes := []string{
		EventTenderCreated,
		EventTenderUpdated,
		EventTenderStatusChanged,
		EventTenderDecisionMade,
		EventTenderSubmitted,
		EventTenderAwarded,
		EventTenderDeleted,
		EventDocumentUploaded,
		EventDocumentVersioned,
		EventDocumentDeleted,
		EventCommitteeCreated,
		EventCommitteeDissolved,
		EventMemberAdded,
		EventMemberRemoved,
		EventEvaluationCreated,
		EventEvaluationSubmitted,
		EventEvaluationDeleted,
		EventMeetingScheduled,
		EventMeetingStarted,
		EventMeetingCompleted,
		EventMeetingCancelled,
		EventSectionCreated,
		EventSectionAssigned,
		EventSectionSubmittedReview,
		EventSectionApproved,
		EventSectionRejected,
		EventSubmissionCreated,
		EventSubmissionSubmitted,
		EventSubmissionConfirmed,
	}

	for _, eventType := range eventTypes {
		// All should follow projects.entity.action pattern
		assert.Contains(t, eventType, "projects.")
		assert.NotEmpty(t, eventType)
	}
}

func TestTenderCreatedData(t *testing.T) {
	tenderID := uuid.New()
	tenderNumber := "TND-2024-001"
	title := "Test Tender"
	clientName := "Test Client"
	deadline := time.Now().Add(30 * 24 * time.Hour)
	createdBy := uuid.New()

	data := TenderCreatedData(tenderID, tenderNumber, title, clientName, deadline, createdBy)

	assert.Equal(t, tenderID.String(), data["tender_id"])
	assert.Equal(t, tenderNumber, data["tender_number"])
	assert.Equal(t, title, data["title"])
	assert.Equal(t, clientName, data["client_name"])
	assert.Equal(t, createdBy.String(), data["created_by"])
	assert.NotEmpty(t, data["deadline"])
}

func TestTenderStatusChangedData(t *testing.T) {
	tenderID := uuid.New()
	oldStatus := "new"
	newStatus := "evaluating"

	data := TenderStatusChangedData(tenderID, oldStatus, newStatus)

	assert.Equal(t, tenderID.String(), data["tender_id"])
	assert.Equal(t, oldStatus, data["old_status"])
	assert.Equal(t, newStatus, data["new_status"])
}

func TestTenderDecisionData(t *testing.T) {
	tenderID := uuid.New()
	decision := "go"
	rationale := "Strong opportunity"
	decidedBy := uuid.New()

	data := TenderDecisionData(tenderID, decision, rationale, decidedBy)

	assert.Equal(t, tenderID.String(), data["tender_id"])
	assert.Equal(t, decision, data["decision"])
	assert.Equal(t, rationale, data["rationale"])
	assert.Equal(t, decidedBy.String(), data["decided_by"])
}

func TestTenderSubmittedData(t *testing.T) {
	tenderID := uuid.New()
	submissionID := uuid.New()
	submissionType := "email"
	submittedBy := uuid.New()

	data := TenderSubmittedData(tenderID, submissionID, submissionType, submittedBy)

	assert.Equal(t, tenderID.String(), data["tender_id"])
	assert.Equal(t, submissionID.String(), data["submission_id"])
	assert.Equal(t, submissionType, data["submission_type"])
	assert.Equal(t, submittedBy.String(), data["submitted_by"])
}

func TestTenderAwardedData(t *testing.T) {
	tenderID := uuid.New()
	awardValue := 500000.0
	currency := "KES"

	data := TenderAwardedData(tenderID, awardValue, currency)

	assert.Equal(t, tenderID.String(), data["tender_id"])
	assert.Equal(t, awardValue, data["award_value"])
	assert.Equal(t, currency, data["currency"])
}

func TestDocumentUploadedData(t *testing.T) {
	tenderID := uuid.New()
	documentID := uuid.New()
	name := "Technical Proposal"
	documentType := "technical"
	uploadedBy := uuid.New()

	data := DocumentUploadedData(tenderID, documentID, name, documentType, uploadedBy)

	assert.Equal(t, tenderID.String(), data["tender_id"])
	assert.Equal(t, documentID.String(), data["document_id"])
	assert.Equal(t, name, data["name"])
	assert.Equal(t, documentType, data["document_type"])
	assert.Equal(t, uploadedBy.String(), data["uploaded_by"])
}

func TestCommitteeCreatedData(t *testing.T) {
	tenderID := uuid.New()
	committeeID := uuid.New()
	name := "Evaluation Committee"
	committeeType := "evaluation"

	data := CommitteeCreatedData(tenderID, committeeID, name, committeeType)

	assert.Equal(t, tenderID.String(), data["tender_id"])
	assert.Equal(t, committeeID.String(), data["committee_id"])
	assert.Equal(t, name, data["name"])
	assert.Equal(t, committeeType, data["committee_type"])
}

func TestMemberAddedData(t *testing.T) {
	committeeID := uuid.New()
	memberID := uuid.New()
	userID := uuid.New()
	role := "chair"

	data := MemberAddedData(committeeID, memberID, userID, role)

	assert.Equal(t, committeeID.String(), data["committee_id"])
	assert.Equal(t, memberID.String(), data["member_id"])
	assert.Equal(t, userID.String(), data["user_id"])
	assert.Equal(t, role, data["role"])
}

func TestEvaluationCreatedData(t *testing.T) {
	tenderID := uuid.New()
	evaluationID := uuid.New()
	evaluatorID := uuid.New()

	data := EvaluationCreatedData(tenderID, evaluationID, evaluatorID)

	assert.Equal(t, tenderID.String(), data["tender_id"])
	assert.Equal(t, evaluationID.String(), data["evaluation_id"])
	assert.Equal(t, evaluatorID.String(), data["evaluator_id"])
}

func TestEvaluationSubmittedData(t *testing.T) {
	tenderID := uuid.New()
	evaluationID := uuid.New()
	vote := "go"
	overallScore := 85.5

	data := EvaluationSubmittedData(tenderID, evaluationID, vote, overallScore)

	assert.Equal(t, tenderID.String(), data["tender_id"])
	assert.Equal(t, evaluationID.String(), data["evaluation_id"])
	assert.Equal(t, vote, data["vote"])
	assert.Equal(t, overallScore, data["overall_score"])
}

func TestMeetingScheduledData(t *testing.T) {
	tenderID := uuid.New()
	meetingID := uuid.New()
	title := "Kickoff Meeting"
	scheduledAt := time.Now().Add(7 * 24 * time.Hour)
	attendees := []uuid.UUID{uuid.New(), uuid.New(), uuid.New()}

	data := MeetingScheduledData(tenderID, meetingID, title, scheduledAt, attendees)

	assert.Equal(t, tenderID.String(), data["tender_id"])
	assert.Equal(t, meetingID.String(), data["meeting_id"])
	assert.Equal(t, title, data["title"])
	assert.NotEmpty(t, data["scheduled_at"])
	attendeeList := data["attendees"].([]string)
	assert.Len(t, attendeeList, 3)
}

func TestMeetingCompletedData(t *testing.T) {
	tenderID := uuid.New()
	meetingID := uuid.New()
	decisionsCount := 3
	actionItemsCount := 5

	data := MeetingCompletedData(tenderID, meetingID, decisionsCount, actionItemsCount)

	assert.Equal(t, tenderID.String(), data["tender_id"])
	assert.Equal(t, meetingID.String(), data["meeting_id"])
	assert.Equal(t, decisionsCount, data["decisions_count"])
	assert.Equal(t, actionItemsCount, data["action_items_count"])
}

func TestSectionAssignedData(t *testing.T) {
	tenderID := uuid.New()
	sectionID := uuid.New()
	assigneeID := uuid.New()
	title := "Executive Summary"

	data := SectionAssignedData(tenderID, sectionID, assigneeID, title)

	assert.Equal(t, tenderID.String(), data["tender_id"])
	assert.Equal(t, sectionID.String(), data["section_id"])
	assert.Equal(t, assigneeID.String(), data["assignee_id"])
	assert.Equal(t, title, data["title"])
}

func TestSectionReviewData(t *testing.T) {
	tenderID := uuid.New()
	sectionID := uuid.New()
	status := "approved"
	reviewerID := uuid.New()

	data := SectionReviewData(tenderID, sectionID, status, reviewerID)

	assert.Equal(t, tenderID.String(), data["tender_id"])
	assert.Equal(t, sectionID.String(), data["section_id"])
	assert.Equal(t, status, data["status"])
	assert.Equal(t, reviewerID.String(), data["reviewer_id"])
}

func TestSubmissionConfirmedData(t *testing.T) {
	tenderID := uuid.New()
	submissionID := uuid.New()
	deliveredAt := time.Now()

	data := SubmissionConfirmedData(tenderID, submissionID, deliveredAt)

	assert.Equal(t, tenderID.String(), data["tender_id"])
	assert.Equal(t, submissionID.String(), data["submission_id"])
	assert.NotEmpty(t, data["delivered_at"])
}

func TestNoOpEventPublisher(t *testing.T) {
	publisher := NewNoOpEventPublisher()

	event := NewDomainEvent(uuid.New(), uuid.New(), EventTenderCreated, map[string]any{"test": true})

	// Should not error
	err := publisher.PublishInTx(context.Background(), nil, event)
	require.NoError(t, err)
}

func TestMockEventPublisher(t *testing.T) {
	publisher := NewMockEventPublisher()
	ctx := context.Background()

	// Initially empty
	assert.Equal(t, 0, publisher.EventCount())
	assert.Nil(t, publisher.LastEvent())

	// Publish first event
	event1 := NewDomainEvent(uuid.New(), uuid.New(), EventTenderCreated, map[string]any{"test": 1})
	err := publisher.PublishInTx(ctx, nil, event1)
	require.NoError(t, err)
	assert.Equal(t, 1, publisher.EventCount())
	assert.Equal(t, event1, publisher.LastEvent())

	// Publish second event
	event2 := NewDomainEvent(uuid.New(), uuid.New(), EventTenderUpdated, map[string]any{"test": 2})
	err = publisher.PublishInTx(ctx, nil, event2)
	require.NoError(t, err)
	assert.Equal(t, 2, publisher.EventCount())
	assert.Equal(t, event2, publisher.LastEvent())

	// Get all events
	events := publisher.Events()
	assert.Len(t, events, 2)
	assert.Equal(t, event1, events[0])
	assert.Equal(t, event2, events[1])

	// Clear events
	publisher.Clear()
	assert.Equal(t, 0, publisher.EventCount())
	assert.Nil(t, publisher.LastEvent())
}

func TestDomainEventJSONStructure(t *testing.T) {
	tenantID := uuid.New()
	aggregateID := uuid.New()
	data := map[string]any{
		"tender_id": aggregateID.String(),
		"title":     "Test Tender",
	}

	event := NewDomainEvent(tenantID, aggregateID, EventTenderCreated, data)

	// Verify the structure matches the expected event format from docs
	assert.NotEmpty(t, event.ID)            // event_id
	assert.Equal(t, tenantID, event.TenantID) // tenant_id
	assert.NotEmpty(t, event.EventType)      // event_type
	assert.False(t, event.Timestamp.IsZero()) // timestamp
	assert.NotNil(t, event.Data)             // data
}

func TestEventTypeNamingConvention(t *testing.T) {
	// Verify all events follow the pattern: projects.{entity}.{action}
	tests := []struct {
		eventType string
		entity    string
		action    string
	}{
		{EventTenderCreated, "tender", "created"},
		{EventTenderUpdated, "tender", "updated"},
		{EventTenderStatusChanged, "tender", "status_changed"},
		{EventTenderDecisionMade, "tender", "decision_made"},
		{EventTenderSubmitted, "tender", "submitted"},
		{EventTenderAwarded, "tender", "awarded"},
		{EventTenderDeleted, "tender", "deleted"},
		{EventDocumentUploaded, "tender_document", "uploaded"},
		{EventDocumentVersioned, "tender_document", "versioned"},
		{EventCommitteeCreated, "tender_committee", "created"},
		{EventCommitteeDissolved, "tender_committee", "dissolved"},
		{EventMemberAdded, "tender_committee", "member_added"},
		{EventMeetingScheduled, "tender_meeting", "scheduled"},
		{EventMeetingStarted, "tender_meeting", "started"},
		{EventMeetingCompleted, "tender_meeting", "completed"},
		{EventMeetingCancelled, "tender_meeting", "cancelled"},
		{EventSectionApproved, "tender_section", "approved"},
		{EventSectionRejected, "tender_section", "rejected"},
		{EventSubmissionSubmitted, "tender_submission", "submitted"},
		{EventSubmissionConfirmed, "tender_submission", "delivery_confirmed"},
	}

	for _, tt := range tests {
		t.Run(tt.eventType, func(t *testing.T) {
			expected := "projects." + tt.entity + "." + tt.action
			assert.Equal(t, expected, tt.eventType)
		})
	}
}

func TestMeetingScheduledData_EmptyAttendees(t *testing.T) {
	tenderID := uuid.New()
	meetingID := uuid.New()
	title := "Solo Meeting"
	scheduledAt := time.Now()
	attendees := []uuid.UUID{}

	data := MeetingScheduledData(tenderID, meetingID, title, scheduledAt, attendees)

	attendeeList := data["attendees"].([]string)
	assert.Len(t, attendeeList, 0)
}
