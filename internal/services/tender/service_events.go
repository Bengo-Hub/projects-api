package tender

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/bengobox/projects-service/internal/ent"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// EventAwareService wraps Service to add event publishing capabilities.
// It uses database transactions to ensure atomicity between domain operations and event publishing.
type EventAwareService struct {
	*Service
	sqlDB     *sql.DB
	publisher EventPublisher
}

// NewEventAwareService creates a new event-aware tender service.
// If publisher is nil, a no-op publisher is used (events disabled).
func NewEventAwareService(logger *zap.Logger, db *ent.Client, sqlDB *sql.DB, publisher EventPublisher) *EventAwareService {
	if publisher == nil {
		publisher = NewNoOpEventPublisher()
	}
	return &EventAwareService{
		Service:   NewService(logger, db),
		sqlDB:     sqlDB,
		publisher: publisher,
	}
}

// Create creates a new tender and publishes a TenderCreated event.
func (s *EventAwareService) Create(ctx context.Context, params CreateParams) (*ent.Tender, error) {
	// For operations that need transactional events, we use a transaction
	tx, err := s.sqlDB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() {
		if err != nil {
			tx.Rollback()
		}
	}()

	// Create tender using base service (uses Ent which participates in same DB)
	tender, err := s.Service.Create(ctx, params)
	if err != nil {
		return nil, err
	}

	// Publish event within transaction
	event := NewDomainEvent(
		params.TenantID,
		tender.ID,
		EventTenderCreated,
		TenderCreatedData(tender.ID, tender.TenderNumber, tender.Title, tender.ClientName, tender.Deadline, params.CreatedBy),
	)
	if err = s.publisher.PublishInTx(ctx, tx, event); err != nil {
		return nil, fmt.Errorf("publish event: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit transaction: %w", err)
	}

	s.logger.Info("tender created with event",
		zap.String("tender_id", tender.ID.String()),
		zap.String("event_id", event.ID.String()),
	)

	return tender, nil
}

// UpdateStatus updates a tender's status and publishes a StatusChanged event.
func (s *EventAwareService) UpdateStatus(ctx context.Context, tenantID, tenderID uuid.UUID, status string) (*ent.Tender, error) {
	// Get current tender to capture old status
	current, err := s.Service.Get(ctx, tenantID, tenderID)
	if err != nil {
		return nil, err
	}
	oldStatus := current.Status

	tx, err := s.sqlDB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() {
		if err != nil {
			tx.Rollback()
		}
	}()

	tender, err := s.Service.UpdateStatus(ctx, tenantID, tenderID, status)
	if err != nil {
		return nil, err
	}

	// Only publish if status actually changed
	if oldStatus != status {
		event := NewDomainEvent(
			tenantID,
			tenderID,
			EventTenderStatusChanged,
			TenderStatusChangedData(tenderID, oldStatus, status),
		)
		if err = s.publisher.PublishInTx(ctx, tx, event); err != nil {
			return nil, fmt.Errorf("publish event: %w", err)
		}

		// Special events for significant status transitions
		if status == "awarded" {
			awardEvent := NewDomainEvent(
				tenantID,
				tenderID,
				EventTenderAwarded,
				TenderAwardedData(tenderID, tender.EstimatedValue, tender.Currency),
			)
			if err = s.publisher.PublishInTx(ctx, tx, awardEvent); err != nil {
				return nil, fmt.Errorf("publish award event: %w", err)
			}
		}
	}

	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit transaction: %w", err)
	}

	return tender, nil
}

// RecordDecision records a decision and publishes a DecisionMade event.
func (s *EventAwareService) RecordDecision(ctx context.Context, tenantID, tenderID uuid.UUID, params DecisionParams) (*ent.Tender, error) {
	tx, err := s.sqlDB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() {
		if err != nil {
			tx.Rollback()
		}
	}()

	tender, err := s.Service.RecordDecision(ctx, tenantID, tenderID, params)
	if err != nil {
		return nil, err
	}

	event := NewDomainEvent(
		tenantID,
		tenderID,
		EventTenderDecisionMade,
		TenderDecisionData(tenderID, params.Decision, params.Rationale, params.DecidedBy),
	)
	if err = s.publisher.PublishInTx(ctx, tx, event); err != nil {
		return nil, fmt.Errorf("publish event: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit transaction: %w", err)
	}

	return tender, nil
}

// Delete deletes a tender and publishes a TenderDeleted event.
func (s *EventAwareService) Delete(ctx context.Context, tenantID, tenderID uuid.UUID) error {
	// Get tender info before deletion for event data
	tender, err := s.Service.Get(ctx, tenantID, tenderID)
	if err != nil {
		return err
	}

	tx, err := s.sqlDB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() {
		if err != nil {
			tx.Rollback()
		}
	}()

	if err = s.Service.Delete(ctx, tenantID, tenderID); err != nil {
		return err
	}

	event := NewDomainEvent(
		tenantID,
		tenderID,
		EventTenderDeleted,
		map[string]any{
			"tender_id":     tenderID.String(),
			"tender_number": tender.TenderNumber,
			"title":         tender.Title,
		},
	)
	if err = s.publisher.PublishInTx(ctx, tx, event); err != nil {
		return fmt.Errorf("publish event: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	return nil
}

// CreateDocument creates a document and publishes a DocumentUploaded event.
func (s *EventAwareService) CreateDocument(ctx context.Context, params DocumentCreateParams) (*ent.TenderDocument, error) {
	tx, err := s.sqlDB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() {
		if err != nil {
			tx.Rollback()
		}
	}()

	doc, err := s.Service.CreateDocument(ctx, params)
	if err != nil {
		return nil, err
	}

	event := NewDomainEvent(
		params.TenantID,
		params.TenderID,
		EventDocumentUploaded,
		DocumentUploadedData(params.TenderID, doc.ID, doc.Name, doc.DocumentType, params.UploadedBy),
	)
	if err = s.publisher.PublishInTx(ctx, tx, event); err != nil {
		return nil, fmt.Errorf("publish event: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit transaction: %w", err)
	}

	return doc, nil
}

// CreateDocumentVersion creates a new document version and publishes event.
func (s *EventAwareService) CreateDocumentVersion(ctx context.Context, tenantID, documentID uuid.UUID, params DocumentCreateParams) (*ent.TenderDocument, error) {
	tx, err := s.sqlDB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() {
		if err != nil {
			tx.Rollback()
		}
	}()

	doc, err := s.Service.CreateDocumentVersion(ctx, tenantID, documentID, params)
	if err != nil {
		return nil, err
	}

	event := NewDomainEvent(
		tenantID,
		doc.TenderID,
		EventDocumentVersioned,
		map[string]any{
			"tender_id":        doc.TenderID.String(),
			"document_id":      doc.ID.String(),
			"previous_version": documentID.String(),
			"version":          doc.Version,
			"name":             doc.Name,
		},
	)
	if err = s.publisher.PublishInTx(ctx, tx, event); err != nil {
		return nil, fmt.Errorf("publish event: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit transaction: %w", err)
	}

	return doc, nil
}

// CreateCommittee creates a committee and publishes event.
func (s *EventAwareService) CreateCommittee(ctx context.Context, params CommitteeCreateParams) (*ent.TenderCommittee, error) {
	tx, err := s.sqlDB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() {
		if err != nil {
			tx.Rollback()
		}
	}()

	committee, err := s.Service.CreateCommittee(ctx, params)
	if err != nil {
		return nil, err
	}

	event := NewDomainEvent(
		params.TenantID,
		params.TenderID,
		EventCommitteeCreated,
		CommitteeCreatedData(params.TenderID, committee.ID, committee.Name, committee.CommitteeType),
	)
	if err = s.publisher.PublishInTx(ctx, tx, event); err != nil {
		return nil, fmt.Errorf("publish event: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit transaction: %w", err)
	}

	return committee, nil
}

// DissolveCommittee dissolves a committee and publishes event.
func (s *EventAwareService) DissolveCommittee(ctx context.Context, tenantID, committeeID uuid.UUID) (*ent.TenderCommittee, error) {
	committee, err := s.Service.GetCommittee(ctx, tenantID, committeeID)
	if err != nil {
		return nil, err
	}

	tx, err := s.sqlDB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() {
		if err != nil {
			tx.Rollback()
		}
	}()

	dissolved, err := s.Service.DissolveCommittee(ctx, tenantID, committeeID)
	if err != nil {
		return nil, err
	}

	event := NewDomainEvent(
		tenantID,
		committee.TenderID,
		EventCommitteeDissolved,
		map[string]any{
			"tender_id":    committee.TenderID.String(),
			"committee_id": committeeID.String(),
			"name":         committee.Name,
		},
	)
	if err = s.publisher.PublishInTx(ctx, tx, event); err != nil {
		return nil, fmt.Errorf("publish event: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit transaction: %w", err)
	}

	return dissolved, nil
}

// AddMember adds a committee member and publishes event.
func (s *EventAwareService) AddMember(ctx context.Context, params MemberCreateParams) (*ent.TenderCommitteeMember, error) {
	tx, err := s.sqlDB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() {
		if err != nil {
			tx.Rollback()
		}
	}()

	member, err := s.Service.AddMember(ctx, params)
	if err != nil {
		return nil, err
	}

	// Get committee to get tender ID
	committee, err := s.Service.GetCommittee(ctx, params.TenantID, params.CommitteeID)
	if err != nil {
		return nil, err
	}

	event := NewDomainEvent(
		params.TenantID,
		committee.TenderID,
		EventMemberAdded,
		MemberAddedData(params.CommitteeID, member.ID, params.UserID, member.Role),
	)
	if err = s.publisher.PublishInTx(ctx, tx, event); err != nil {
		return nil, fmt.Errorf("publish event: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit transaction: %w", err)
	}

	return member, nil
}

// CreateEvaluation creates an evaluation and publishes event.
func (s *EventAwareService) CreateEvaluation(ctx context.Context, params EvaluationCreateParams) (*ent.TenderEvaluation, error) {
	tx, err := s.sqlDB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() {
		if err != nil {
			tx.Rollback()
		}
	}()

	eval, err := s.Service.CreateEvaluation(ctx, params)
	if err != nil {
		return nil, err
	}

	event := NewDomainEvent(
		params.TenantID,
		params.TenderID,
		EventEvaluationCreated,
		EvaluationCreatedData(params.TenderID, eval.ID, params.EvaluatorID),
	)
	if err = s.publisher.PublishInTx(ctx, tx, event); err != nil {
		return nil, fmt.Errorf("publish event: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit transaction: %w", err)
	}

	return eval, nil
}

// SubmitEvaluation submits an evaluation and publishes event.
func (s *EventAwareService) SubmitEvaluation(ctx context.Context, tenantID, evaluationID uuid.UUID) (*ent.TenderEvaluation, error) {
	tx, err := s.sqlDB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() {
		if err != nil {
			tx.Rollback()
		}
	}()

	eval, err := s.Service.SubmitEvaluation(ctx, tenantID, evaluationID)
	if err != nil {
		return nil, err
	}

	event := NewDomainEvent(
		tenantID,
		eval.TenderID,
		EventEvaluationSubmitted,
		EvaluationSubmittedData(eval.TenderID, eval.ID, eval.Vote, eval.OverallScore),
	)
	if err = s.publisher.PublishInTx(ctx, tx, event); err != nil {
		return nil, fmt.Errorf("publish event: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit transaction: %w", err)
	}

	return eval, nil
}

// CreateMeeting creates a meeting and publishes event.
func (s *EventAwareService) CreateMeeting(ctx context.Context, params MeetingCreateParams) (*ent.TenderMeeting, error) {
	tx, err := s.sqlDB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() {
		if err != nil {
			tx.Rollback()
		}
	}()

	meeting, err := s.Service.CreateMeeting(ctx, params)
	if err != nil {
		return nil, err
	}

	event := NewDomainEvent(
		params.TenantID,
		params.TenderID,
		EventMeetingScheduled,
		MeetingScheduledData(params.TenderID, meeting.ID, meeting.Title, meeting.ScheduledAt, params.Attendees),
	)
	if err = s.publisher.PublishInTx(ctx, tx, event); err != nil {
		return nil, fmt.Errorf("publish event: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit transaction: %w", err)
	}

	return meeting, nil
}

// StartMeeting starts a meeting and publishes event.
func (s *EventAwareService) StartMeeting(ctx context.Context, tenantID, meetingID uuid.UUID) (*ent.TenderMeeting, error) {
	tx, err := s.sqlDB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() {
		if err != nil {
			tx.Rollback()
		}
	}()

	meeting, err := s.Service.StartMeeting(ctx, tenantID, meetingID)
	if err != nil {
		return nil, err
	}

	event := NewDomainEvent(
		tenantID,
		meeting.TenderID,
		EventMeetingStarted,
		map[string]any{
			"tender_id":  meeting.TenderID.String(),
			"meeting_id": meetingID.String(),
			"title":      meeting.Title,
			"started_at": meeting.StartedAt.Format("2006-01-02T15:04:05Z07:00"),
		},
	)
	if err = s.publisher.PublishInTx(ctx, tx, event); err != nil {
		return nil, fmt.Errorf("publish event: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit transaction: %w", err)
	}

	return meeting, nil
}

// EndMeeting ends a meeting and publishes event.
func (s *EventAwareService) EndMeeting(ctx context.Context, tenantID, meetingID uuid.UUID, minutes string, decisions []map[string]any, actionItems []map[string]any) (*ent.TenderMeeting, error) {
	tx, err := s.sqlDB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() {
		if err != nil {
			tx.Rollback()
		}
	}()

	meeting, err := s.Service.EndMeeting(ctx, tenantID, meetingID, minutes, decisions, actionItems)
	if err != nil {
		return nil, err
	}

	event := NewDomainEvent(
		tenantID,
		meeting.TenderID,
		EventMeetingCompleted,
		MeetingCompletedData(meeting.TenderID, meetingID, len(decisions), len(actionItems)),
	)
	if err = s.publisher.PublishInTx(ctx, tx, event); err != nil {
		return nil, fmt.Errorf("publish event: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit transaction: %w", err)
	}

	return meeting, nil
}

// CancelMeeting cancels a meeting and publishes event.
func (s *EventAwareService) CancelMeeting(ctx context.Context, tenantID, meetingID uuid.UUID) (*ent.TenderMeeting, error) {
	tx, err := s.sqlDB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() {
		if err != nil {
			tx.Rollback()
		}
	}()

	meeting, err := s.Service.CancelMeeting(ctx, tenantID, meetingID)
	if err != nil {
		return nil, err
	}

	event := NewDomainEvent(
		tenantID,
		meeting.TenderID,
		EventMeetingCancelled,
		map[string]any{
			"tender_id":  meeting.TenderID.String(),
			"meeting_id": meetingID.String(),
			"title":      meeting.Title,
		},
	)
	if err = s.publisher.PublishInTx(ctx, tx, event); err != nil {
		return nil, fmt.Errorf("publish event: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit transaction: %w", err)
	}

	return meeting, nil
}

// AssignSection assigns a section and publishes event.
func (s *EventAwareService) AssignSection(ctx context.Context, tenantID, sectionID, assigneeID uuid.UUID) (*ent.TenderSection, error) {
	tx, err := s.sqlDB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() {
		if err != nil {
			tx.Rollback()
		}
	}()

	section, err := s.Service.AssignSection(ctx, tenantID, sectionID, assigneeID)
	if err != nil {
		return nil, err
	}

	event := NewDomainEvent(
		tenantID,
		section.TenderID,
		EventSectionAssigned,
		SectionAssignedData(section.TenderID, sectionID, assigneeID, section.Title),
	)
	if err = s.publisher.PublishInTx(ctx, tx, event); err != nil {
		return nil, fmt.Errorf("publish event: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit transaction: %w", err)
	}

	return section, nil
}

// ApproveSection approves a section and publishes event.
func (s *EventAwareService) ApproveSection(ctx context.Context, tenantID, sectionID uuid.UUID, reviewerID uuid.UUID, comments string) (*ent.TenderSection, error) {
	tx, err := s.sqlDB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() {
		if err != nil {
			tx.Rollback()
		}
	}()

	section, err := s.Service.ApproveSection(ctx, tenantID, sectionID, reviewerID, comments)
	if err != nil {
		return nil, err
	}

	event := NewDomainEvent(
		tenantID,
		section.TenderID,
		EventSectionApproved,
		SectionReviewData(section.TenderID, sectionID, "approved", reviewerID),
	)
	if err = s.publisher.PublishInTx(ctx, tx, event); err != nil {
		return nil, fmt.Errorf("publish event: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit transaction: %w", err)
	}

	return section, nil
}

// RejectSection rejects a section and publishes event.
func (s *EventAwareService) RejectSection(ctx context.Context, tenantID, sectionID uuid.UUID, reviewerID uuid.UUID, comments string) (*ent.TenderSection, error) {
	tx, err := s.sqlDB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() {
		if err != nil {
			tx.Rollback()
		}
	}()

	section, err := s.Service.RejectSection(ctx, tenantID, sectionID, reviewerID, comments)
	if err != nil {
		return nil, err
	}

	event := NewDomainEvent(
		tenantID,
		section.TenderID,
		EventSectionRejected,
		SectionReviewData(section.TenderID, sectionID, "rejected", reviewerID),
	)
	if err = s.publisher.PublishInTx(ctx, tx, event); err != nil {
		return nil, fmt.Errorf("publish event: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit transaction: %w", err)
	}

	return section, nil
}

// SubmitTender submits a tender and publishes event.
func (s *EventAwareService) SubmitTender(ctx context.Context, tenantID, submissionID uuid.UUID, submittedBy uuid.UUID) (*ent.TenderSubmission, error) {
	tx, err := s.sqlDB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() {
		if err != nil {
			tx.Rollback()
		}
	}()

	submission, err := s.Service.SubmitTender(ctx, tenantID, submissionID, submittedBy)
	if err != nil {
		return nil, err
	}

	event := NewDomainEvent(
		tenantID,
		submission.TenderID,
		EventSubmissionSubmitted,
		TenderSubmittedData(submission.TenderID, submissionID, submission.SubmissionType, submittedBy),
	)
	if err = s.publisher.PublishInTx(ctx, tx, event); err != nil {
		return nil, fmt.Errorf("publish event: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit transaction: %w", err)
	}

	return submission, nil
}

// ConfirmDelivery confirms delivery and publishes event.
func (s *EventAwareService) ConfirmDelivery(ctx context.Context, tenantID, submissionID uuid.UUID, deliveryProofURL string) (*ent.TenderSubmission, error) {
	tx, err := s.sqlDB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() {
		if err != nil {
			tx.Rollback()
		}
	}()

	submission, err := s.Service.ConfirmDelivery(ctx, tenantID, submissionID, deliveryProofURL)
	if err != nil {
		return nil, err
	}

	event := NewDomainEvent(
		tenantID,
		submission.TenderID,
		EventSubmissionConfirmed,
		SubmissionConfirmedData(submission.TenderID, submissionID, submission.DeliveredAt),
	)
	if err = s.publisher.PublishInTx(ctx, tx, event); err != nil {
		return nil, fmt.Errorf("publish event: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit transaction: %w", err)
	}

	return submission, nil
}
