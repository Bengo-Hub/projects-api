package tender

import (
	"context"

	"github.com/bengobox/projects-service/internal/ent"
	"github.com/google/uuid"
)

// TenderServiceInterface defines the contract for tender service operations.
// Both Service and EventAwareService implement this interface.
type TenderServiceInterface interface {
	// Tender CRUD
	Create(ctx context.Context, params CreateParams) (*ent.Tender, error)
	Get(ctx context.Context, tenantID, tenderID uuid.UUID) (*ent.Tender, error)
	GetByNumber(ctx context.Context, tenantID uuid.UUID, tenderNumber string) (*ent.Tender, error)
	List(ctx context.Context, params ListParams) ([]*ent.Tender, int, error)
	Update(ctx context.Context, tenantID, tenderID uuid.UUID, params UpdateParams) (*ent.Tender, error)
	UpdateStatus(ctx context.Context, tenantID, tenderID uuid.UUID, status string) (*ent.Tender, error)
	RecordDecision(ctx context.Context, tenantID, tenderID uuid.UUID, params DecisionParams) (*ent.Tender, error)
	Delete(ctx context.Context, tenantID, tenderID uuid.UUID) error

	// Document operations
	CreateDocument(ctx context.Context, params DocumentCreateParams) (*ent.TenderDocument, error)
	GetDocument(ctx context.Context, tenantID, documentID uuid.UUID) (*ent.TenderDocument, error)
	ListDocuments(ctx context.Context, params DocumentListParams) ([]*ent.TenderDocument, int, error)
	UpdateDocument(ctx context.Context, tenantID, documentID uuid.UUID, params DocumentUpdateParams) (*ent.TenderDocument, error)
	DeleteDocument(ctx context.Context, tenantID, documentID uuid.UUID) error
	CreateDocumentVersion(ctx context.Context, tenantID, documentID uuid.UUID, params DocumentCreateParams) (*ent.TenderDocument, error)

	// Committee operations
	CreateCommittee(ctx context.Context, params CommitteeCreateParams) (*ent.TenderCommittee, error)
	GetCommittee(ctx context.Context, tenantID, committeeID uuid.UUID) (*ent.TenderCommittee, error)
	ListCommittees(ctx context.Context, tenantID, tenderID uuid.UUID) ([]*ent.TenderCommittee, error)
	UpdateCommittee(ctx context.Context, tenantID, committeeID uuid.UUID, params CommitteeUpdateParams) (*ent.TenderCommittee, error)
	DissolveCommittee(ctx context.Context, tenantID, committeeID uuid.UUID) (*ent.TenderCommittee, error)
	DeleteCommittee(ctx context.Context, tenantID, committeeID uuid.UUID) error

	// Member operations
	AddMember(ctx context.Context, params MemberCreateParams) (*ent.TenderCommitteeMember, error)
	GetMember(ctx context.Context, tenantID, memberID uuid.UUID) (*ent.TenderCommitteeMember, error)
	UpdateMember(ctx context.Context, tenantID, memberID uuid.UUID, params MemberUpdateParams) (*ent.TenderCommitteeMember, error)
	RemoveMember(ctx context.Context, tenantID, memberID uuid.UUID) error

	// Evaluation operations
	CreateEvaluation(ctx context.Context, params EvaluationCreateParams) (*ent.TenderEvaluation, error)
	GetEvaluation(ctx context.Context, tenantID, evaluationID uuid.UUID) (*ent.TenderEvaluation, error)
	ListEvaluations(ctx context.Context, tenantID, tenderID uuid.UUID) ([]*ent.TenderEvaluation, error)
	UpdateEvaluation(ctx context.Context, tenantID, evaluationID uuid.UUID, params EvaluationUpdateParams) (*ent.TenderEvaluation, error)
	SubmitEvaluation(ctx context.Context, tenantID, evaluationID uuid.UUID) (*ent.TenderEvaluation, error)
	DeleteEvaluation(ctx context.Context, tenantID, evaluationID uuid.UUID) error
	GetEvaluationSummary(ctx context.Context, tenantID, tenderID uuid.UUID) (*EvaluationSummary, error)

	// Meeting operations
	CreateMeeting(ctx context.Context, params MeetingCreateParams) (*ent.TenderMeeting, error)
	GetMeeting(ctx context.Context, tenantID, meetingID uuid.UUID) (*ent.TenderMeeting, error)
	ListMeetings(ctx context.Context, params MeetingListParams) ([]*ent.TenderMeeting, int, error)
	UpdateMeeting(ctx context.Context, tenantID, meetingID uuid.UUID, params MeetingUpdateParams) (*ent.TenderMeeting, error)
	StartMeeting(ctx context.Context, tenantID, meetingID uuid.UUID) (*ent.TenderMeeting, error)
	EndMeeting(ctx context.Context, tenantID, meetingID uuid.UUID, minutes string, decisions []map[string]any, actionItems []map[string]any) (*ent.TenderMeeting, error)
	CancelMeeting(ctx context.Context, tenantID, meetingID uuid.UUID) (*ent.TenderMeeting, error)
	DeleteMeeting(ctx context.Context, tenantID, meetingID uuid.UUID) error

	// Section operations
	CreateSection(ctx context.Context, params SectionCreateParams) (*ent.TenderSection, error)
	GetSection(ctx context.Context, tenantID, sectionID uuid.UUID) (*ent.TenderSection, error)
	ListSections(ctx context.Context, params SectionListParams) ([]*ent.TenderSection, int, error)
	UpdateSection(ctx context.Context, tenantID, sectionID uuid.UUID, params SectionUpdateParams) (*ent.TenderSection, error)
	AssignSection(ctx context.Context, tenantID, sectionID, assigneeID uuid.UUID) (*ent.TenderSection, error)
	SubmitSectionForReview(ctx context.Context, tenantID, sectionID uuid.UUID, reviewerID uuid.UUID) (*ent.TenderSection, error)
	ApproveSection(ctx context.Context, tenantID, sectionID uuid.UUID, reviewerID uuid.UUID, comments string) (*ent.TenderSection, error)
	RejectSection(ctx context.Context, tenantID, sectionID uuid.UUID, reviewerID uuid.UUID, comments string) (*ent.TenderSection, error)
	DeleteSection(ctx context.Context, tenantID, sectionID uuid.UUID) error
	GetSectionProgress(ctx context.Context, tenantID, tenderID uuid.UUID) (*SectionProgress, error)

	// Submission operations
	CreateSubmission(ctx context.Context, params SubmissionCreateParams) (*ent.TenderSubmission, error)
	GetSubmission(ctx context.Context, tenantID, submissionID uuid.UUID) (*ent.TenderSubmission, error)
	ListSubmissions(ctx context.Context, tenantID, tenderID uuid.UUID) ([]*ent.TenderSubmission, error)
	UpdateSubmission(ctx context.Context, tenantID, submissionID uuid.UUID, params SubmissionUpdateParams) (*ent.TenderSubmission, error)
	SubmitTender(ctx context.Context, tenantID, submissionID uuid.UUID, submittedBy uuid.UUID) (*ent.TenderSubmission, error)
	ConfirmDelivery(ctx context.Context, tenantID, submissionID uuid.UUID, deliveryProofURL string) (*ent.TenderSubmission, error)
	RecordEmailTracking(ctx context.Context, tenantID, submissionID uuid.UUID, messageID string, opened bool) (*ent.TenderSubmission, error)
	DeleteSubmission(ctx context.Context, tenantID, submissionID uuid.UUID) error
}

// Compile-time interface compliance checks
var (
	_ TenderServiceInterface = (*Service)(nil)
	_ TenderServiceInterface = (*EventAwareService)(nil)
)
