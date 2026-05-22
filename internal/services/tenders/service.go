package tenders

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	sharedcache "github.com/Bengo-Hub/cache"
	"github.com/bengobox/projects-service/internal/ent"
	enttender "github.com/bengobox/projects-service/internal/ent/tender"
	enttendercommittee "github.com/bengobox/projects-service/internal/ent/tendercommittee"
	enttendercommitteemember "github.com/bengobox/projects-service/internal/ent/tendercommitteemember"
	enttenderdoc "github.com/bengobox/projects-service/internal/ent/tenderdocument"
	enttendereval "github.com/bengobox/projects-service/internal/ent/tenderevaluation"
	enttendermeeting "github.com/bengobox/projects-service/internal/ent/tendermeeting"
)

// Service handles tender management operations.
type Service struct {
	client *ent.Client
	cache  *sharedcache.Aside
	log    *zap.Logger
}

// NewService creates a new tenders service.
func NewService(client *ent.Client, cache *sharedcache.Aside, log *zap.Logger) *Service {
	return &Service{client: client, cache: cache, log: log.Named("tenders.svc")}
}

// CreateTenderInput holds data for creating a tender.
type CreateTenderInput struct {
	Title          string         `json:"title"`
	ClientName     string         `json:"client_name"`
	Source         string         `json:"source"`
	Status         string         `json:"status"`
	Priority       string         `json:"priority"`
	EstimatedValue *float64       `json:"estimated_value"`
	Currency       string         `json:"currency"`
	Deadline       *time.Time     `json:"deadline"`
	Description    string         `json:"description"`
	SubmissionType string         `json:"submission_type"`
	Metadata       map[string]any `json:"metadata"`
	CreatedBy      uuid.UUID      `json:"created_by"`
}

// UpdateTenderInput holds data for updating a tender.
type UpdateTenderInput struct {
	Title          *string        `json:"title"`
	ClientName     *string        `json:"client_name"`
	Source         *string        `json:"source"`
	Status         *string        `json:"status"`
	Priority       *string        `json:"priority"`
	EstimatedValue *float64       `json:"estimated_value"`
	Currency       *string        `json:"currency"`
	Deadline       *time.Time     `json:"deadline"`
	Description    *string        `json:"description"`
	SubmissionType *string        `json:"submission_type"`
	Metadata       map[string]any `json:"metadata"`
}

// ListTendersFilter holds filter options for listing tenders.
type ListTendersFilter struct {
	Status          string     `json:"status"`
	Priority        string     `json:"priority"`
	DeadlineBefore  *time.Time `json:"deadline_before"`
	DeadlineAfter   *time.Time `json:"deadline_after"`
	Page            int        `json:"page"`
	PageSize        int        `json:"page_size"`
}

// CreateCommitteeInput holds data for creating a committee.
type CreateCommitteeInput struct {
	Name string `json:"name"`
}

// AddCommitteeMemberInput holds data for adding a committee member.
type AddCommitteeMemberInput struct {
	UserID uuid.UUID `json:"user_id"`
	Role   string    `json:"role"`
}

// SubmitEvaluationInput holds data for submitting an evaluation.
type SubmitEvaluationInput struct {
	EvaluatorID uuid.UUID `json:"evaluator_id"`
	Score       float64   `json:"score"`
	Notes       string    `json:"notes"`
	Criteria    string    `json:"criteria"`
}

// ScheduleMeetingInput holds data for scheduling a meeting.
type ScheduleMeetingInput struct {
	Title       string     `json:"title"`
	ScheduledAt time.Time  `json:"scheduled_at"`
	Platform    string     `json:"platform"`
	MeetingURL  string     `json:"meeting_url"`
	Notes       string     `json:"notes"`
	CreatedBy   uuid.UUID  `json:"created_by"`
}

// AddDocumentInput holds data for adding a document.
type AddDocumentInput struct {
	FileURL    string    `json:"file_url"`
	FileName   string    `json:"file_name"`
	FileSize   *int64    `json:"file_size"`
	MimeType   string    `json:"mime_type"`
	UploadedBy uuid.UUID `json:"uploaded_by"`
}

// generateTenderNumber generates the next tender number for the tenant.
func (s *Service) generateTenderNumber(ctx context.Context, tenantID uuid.UUID) (string, error) {
	year := time.Now().Year()
	count, err := s.client.Tender.Query().
		Where(enttender.TenantID(tenantID)).Count(ctx)
	if err != nil {
		return "", fmt.Errorf("count tenders: %w", err)
	}
	return fmt.Sprintf("TND-%d-%04d", year, count+1), nil
}

// CreateTender creates a new tender with an auto-generated number.
func (s *Service) CreateTender(ctx context.Context, tenantID uuid.UUID, input CreateTenderInput) (*ent.Tender, error) {
	if input.Title == "" {
		return nil, ErrValidation("title is required")
	}
	if input.ClientName == "" {
		return nil, ErrValidation("client_name is required")
	}
	number, err := s.generateTenderNumber(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	status := input.Status
	if status == "" {
		status = "draft"
	}
	priority := input.Priority
	if priority == "" {
		priority = "medium"
	}
	currency := input.Currency
	if currency == "" {
		currency = "KES"
	}
	submType := input.SubmissionType
	if submType == "" {
		submType = "physical"
	}
	c := s.client.Tender.Create().
		SetTenantID(tenantID).
		SetNumber(number).
		SetTitle(input.Title).
		SetClientName(input.ClientName).
		SetStatus(status).
		SetPriority(priority).
		SetCurrency(currency).
		SetSubmissionType(submType).
		SetCreatedBy(input.CreatedBy)
	if input.Source != "" {
		c = c.SetSource(input.Source)
	}
	if input.EstimatedValue != nil {
		c = c.SetEstimatedValue(*input.EstimatedValue)
	}
	if input.Deadline != nil {
		c = c.SetDeadline(*input.Deadline)
	}
	if input.Description != "" {
		c = c.SetDescription(input.Description)
	}
	if input.Metadata != nil {
		c = c.SetMetadata(input.Metadata)
	}
	t, err := c.Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("create tender: %w", err)
	}
	s.log.Info("tender created", zap.String("id", t.ID.String()), zap.String("number", t.Number))
	return t, nil
}

// ListTenders returns a paginated list of tenders for the tenant.
func (s *Service) ListTenders(ctx context.Context, tenantID uuid.UUID, filter ListTendersFilter) ([]*ent.Tender, int, error) {
	if filter.PageSize <= 0 {
		filter.PageSize = 20
	}
	if filter.Page <= 0 {
		filter.Page = 1
	}
	offset := (filter.Page - 1) * filter.PageSize

	q := s.client.Tender.Query().Where(enttender.TenantID(tenantID))
	if filter.Status != "" {
		q = q.Where(enttender.Status(filter.Status))
	}
	if filter.Priority != "" {
		q = q.Where(enttender.Priority(filter.Priority))
	}
	if filter.DeadlineBefore != nil {
		q = q.Where(enttender.DeadlineLTE(*filter.DeadlineBefore))
	}
	if filter.DeadlineAfter != nil {
		q = q.Where(enttender.DeadlineGTE(*filter.DeadlineAfter))
	}
	total, err := q.Count(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("count tenders: %w", err)
	}
	items, err := q.Order(ent.Desc(enttender.FieldCreatedAt)).
		Offset(offset).Limit(filter.PageSize).All(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("list tenders: %w", err)
	}
	return items, total, nil
}

// GetTender fetches a tender with all edges.
func (s *Service) GetTender(ctx context.Context, tenantID, id uuid.UUID) (*ent.Tender, error) {
	t, err := s.client.Tender.Query().
		Where(enttender.ID(id), enttender.TenantID(tenantID)).
		WithCommittees(func(q *ent.TenderCommitteeQuery) { q.WithMembers() }).
		WithEvaluations().
		WithMeetings().
		WithDocuments().
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get tender: %w", err)
	}
	return t, nil
}

// UpdateTender updates an existing tender.
func (s *Service) UpdateTender(ctx context.Context, tenantID, id uuid.UUID, input UpdateTenderInput) (*ent.Tender, error) {
	t, err := s.client.Tender.Query().
		Where(enttender.ID(id), enttender.TenantID(tenantID)).Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("fetch tender: %w", err)
	}
	u := t.Update()
	if input.Title != nil {
		u = u.SetTitle(*input.Title)
	}
	if input.ClientName != nil {
		u = u.SetClientName(*input.ClientName)
	}
	if input.Source != nil {
		u = u.SetSource(*input.Source)
	}
	if input.Status != nil {
		u = u.SetStatus(*input.Status)
		if *input.Status == "submitted" {
			now := time.Now()
			u = u.SetSubmittedAt(now)
		}
	}
	if input.Priority != nil {
		u = u.SetPriority(*input.Priority)
	}
	if input.EstimatedValue != nil {
		u = u.SetEstimatedValue(*input.EstimatedValue)
	}
	if input.Currency != nil {
		u = u.SetCurrency(*input.Currency)
	}
	if input.Deadline != nil {
		u = u.SetDeadline(*input.Deadline)
	}
	if input.Description != nil {
		u = u.SetDescription(*input.Description)
	}
	if input.SubmissionType != nil {
		u = u.SetSubmissionType(*input.SubmissionType)
	}
	if input.Metadata != nil {
		u = u.SetMetadata(input.Metadata)
	}
	updated, err := u.Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("update tender: %w", err)
	}
	return updated, nil
}

// DeleteTender deletes a tender.
func (s *Service) DeleteTender(ctx context.Context, tenantID, id uuid.UUID) error {
	n, err := s.client.Tender.Delete().
		Where(enttender.ID(id), enttender.TenantID(tenantID)).Exec(ctx)
	if err != nil {
		return fmt.Errorf("delete tender: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// GetTenderMetrics returns aggregate counts and values by status.
func (s *Service) GetTenderMetrics(ctx context.Context, tenantID uuid.UUID) (map[string]any, error) {
	statuses := []string{"draft", "evaluating", "submitted", "awarded", "lost", "cancelled"}
	counts := map[string]int{}
	for _, st := range statuses {
		n, err := s.client.Tender.Query().
			Where(enttender.TenantID(tenantID), enttender.Status(st)).Count(ctx)
		if err != nil {
			return nil, fmt.Errorf("count by status %s: %w", st, err)
		}
		counts[st] = n
	}
	total, err := s.client.Tender.Query().Where(enttender.TenantID(tenantID)).Count(ctx)
	if err != nil {
		return nil, fmt.Errorf("count total: %w", err)
	}
	return map[string]any{
		"total":  total,
		"counts": counts,
	}, nil
}

// --- Committee operations ---

// CreateCommittee creates a new committee for a tender.
func (s *Service) CreateCommittee(ctx context.Context, tenantID, tenderID uuid.UUID, input CreateCommitteeInput) (*ent.TenderCommittee, error) {
	c, err := s.client.TenderCommittee.Create().
		SetTenderID(tenderID).
		SetTenantID(tenantID).
		SetName(input.Name).
		Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("create committee: %w", err)
	}
	return c, nil
}

// ListCommittees lists committees for a tender.
func (s *Service) ListCommittees(ctx context.Context, tenantID, tenderID uuid.UUID) ([]*ent.TenderCommittee, error) {
	items, err := s.client.TenderCommittee.Query().
		Where(enttendercommittee.TenderID(tenderID), enttendercommittee.TenantID(tenantID)).
		WithMembers().
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list committees: %w", err)
	}
	return items, nil
}

// AddCommitteeMember adds a user to a committee.
func (s *Service) AddCommitteeMember(ctx context.Context, tenantID, committeeID uuid.UUID, input AddCommitteeMemberInput) (*ent.TenderCommitteeMember, error) {
	role := input.Role
	if role == "" {
		role = "member"
	}
	m, err := s.client.TenderCommitteeMember.Create().
		SetCommitteeID(committeeID).
		SetTenantID(tenantID).
		SetUserID(input.UserID).
		SetRole(role).
		Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("add committee member: %w", err)
	}
	return m, nil
}

// RemoveCommitteeMember removes a user from a committee.
func (s *Service) RemoveCommitteeMember(ctx context.Context, tenantID, committeeID, userID uuid.UUID) error {
	n, err := s.client.TenderCommitteeMember.Delete().
		Where(enttendercommitteemember.CommitteeID(committeeID), enttendercommitteemember.UserID(userID)).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("remove committee member: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// --- Evaluation operations ---

// SubmitEvaluation submits an evaluation for a tender.
func (s *Service) SubmitEvaluation(ctx context.Context, tenantID, tenderID uuid.UUID, input SubmitEvaluationInput) (*ent.TenderEvaluation, error) {
	c := s.client.TenderEvaluation.Create().
		SetTenderID(tenderID).
		SetTenantID(tenantID).
		SetEvaluatorID(input.EvaluatorID).
		SetScore(input.Score)
	if input.Notes != "" {
		c = c.SetNotes(input.Notes)
	}
	if input.Criteria != "" {
		c = c.SetCriteria(input.Criteria)
	}
	e, err := c.Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("submit evaluation: %w", err)
	}
	return e, nil
}

// ListEvaluations lists evaluations for a tender.
func (s *Service) ListEvaluations(ctx context.Context, tenantID, tenderID uuid.UUID) ([]*ent.TenderEvaluation, error) {
	items, err := s.client.TenderEvaluation.Query().
		Where(enttendereval.TenderID(tenderID), enttendereval.TenantID(tenantID)).
		Order(ent.Desc(enttendereval.FieldEvaluatedAt)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list evaluations: %w", err)
	}
	return items, nil
}

// --- Meeting operations ---

// ScheduleMeeting schedules a meeting for a tender.
func (s *Service) ScheduleMeeting(ctx context.Context, tenantID, tenderID uuid.UUID, input ScheduleMeetingInput) (*ent.TenderMeeting, error) {
	platform := input.Platform
	if platform == "" {
		platform = "physical"
	}
	c := s.client.TenderMeeting.Create().
		SetTenderID(tenderID).
		SetTenantID(tenantID).
		SetTitle(input.Title).
		SetScheduledAt(input.ScheduledAt).
		SetPlatform(platform).
		SetCreatedBy(input.CreatedBy)
	if input.MeetingURL != "" {
		c = c.SetMeetingURL(input.MeetingURL)
	}
	if input.Notes != "" {
		c = c.SetNotes(input.Notes)
	}
	m, err := c.Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("schedule meeting: %w", err)
	}
	return m, nil
}

// ListMeetings lists meetings for a tender.
func (s *Service) ListMeetings(ctx context.Context, tenantID, tenderID uuid.UUID) ([]*ent.TenderMeeting, error) {
	items, err := s.client.TenderMeeting.Query().
		Where(enttendermeeting.TenderID(tenderID), enttendermeeting.TenantID(tenantID)).
		Order(ent.Asc(enttendermeeting.FieldScheduledAt)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list meetings: %w", err)
	}
	return items, nil
}

// --- Document operations ---

// AddDocument adds a document to a tender.
func (s *Service) AddDocument(ctx context.Context, tenantID, tenderID uuid.UUID, input AddDocumentInput) (*ent.TenderDocument, error) {
	c := s.client.TenderDocument.Create().
		SetTenderID(tenderID).
		SetTenantID(tenantID).
		SetFileURL(input.FileURL).
		SetFileName(input.FileName).
		SetUploadedBy(input.UploadedBy)
	if input.FileSize != nil {
		c = c.SetFileSize(*input.FileSize)
	}
	if input.MimeType != "" {
		c = c.SetMimeType(input.MimeType)
	}
	doc, err := c.Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("add document: %w", err)
	}
	return doc, nil
}

// ListDocuments lists documents for a tender.
func (s *Service) ListDocuments(ctx context.Context, tenantID, tenderID uuid.UUID) ([]*ent.TenderDocument, error) {
	items, err := s.client.TenderDocument.Query().
		Where(enttenderdoc.TenderID(tenderID), enttenderdoc.TenantID(tenantID)).
		Order(ent.Desc(enttenderdoc.FieldUploadedAt)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list documents: %w", err)
	}
	return items, nil
}

// DeleteDocument removes a document from a tender.
func (s *Service) DeleteDocument(ctx context.Context, tenantID, id uuid.UUID) error {
	n, err := s.client.TenderDocument.Delete().
		Where(enttenderdoc.ID(id), enttenderdoc.TenantID(tenantID)).Exec(ctx)
	if err != nil {
		return fmt.Errorf("delete document: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
