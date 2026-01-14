package tender

import (
	"context"
	"fmt"
	"time"

	"github.com/bengobox/projects-service/internal/ent"
	"github.com/bengobox/projects-service/internal/ent/tender"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// Service handles tender operations with database persistence.
type Service struct {
	logger *zap.Logger
	db     *ent.Client
}

// NewService creates a new tender service.
func NewService(logger *zap.Logger, db *ent.Client) *Service {
	return &Service{
		logger: logger,
		db:     db,
	}
}

// CreateParams holds parameters for creating a tender.
type CreateParams struct {
	TenantID              uuid.UUID
	Title                 string
	Description           string
	ClientName            string
	ClientContact         string
	ClientEmail           string
	Source                string
	SourceURL             string
	Priority              string
	EstimatedValue        *float64
	Currency              string
	PublicationDate       *time.Time
	Deadline              time.Time
	ClarificationDeadline *time.Time
	SubmissionMethod      string
	SubmissionAddress     string
	Categories            []string
	RequirementsSummary   map[string]any
	CreatedBy             uuid.UUID
	Metadata              map[string]any
}

// UpdateParams holds parameters for updating a tender.
type UpdateParams struct {
	Title                 *string
	Description           *string
	ClientName            *string
	ClientContact         *string
	ClientEmail           *string
	Source                *string
	SourceURL             *string
	Status                *string
	Priority              *string
	EstimatedValue        *float64
	Currency              *string
	PublicationDate       *time.Time
	Deadline              *time.Time
	ClarificationDeadline *time.Time
	SubmissionMethod      *string
	SubmissionAddress     *string
	Categories            []string
	RequirementsSummary   map[string]any
	Metadata              map[string]any
}

// ListParams holds parameters for listing tenders.
type ListParams struct {
	TenantID     uuid.UUID
	Status       string
	Priority     string
	Source       string
	CreatedBy    *uuid.UUID
	DeadlineFrom *time.Time
	DeadlineTo   *time.Time
	Limit        int
	Offset       int
}

// DecisionParams holds parameters for recording a tender decision.
type DecisionParams struct {
	Decision  string
	Rationale string
	DecidedBy uuid.UUID
}

// Create creates a new tender with auto-generated tender number.
func (s *Service) Create(ctx context.Context, params CreateParams) (*ent.Tender, error) {
	tenderNumber := s.generateTenderNumber(params.TenantID)

	builder := s.db.Tender.Create().
		SetTenantID(params.TenantID).
		SetTenderNumber(tenderNumber).
		SetTitle(params.Title).
		SetClientName(params.ClientName).
		SetDeadline(params.Deadline).
		SetCreatedBy(params.CreatedBy)

	if params.Description != "" {
		builder.SetDescription(params.Description)
	}
	if params.ClientContact != "" {
		builder.SetClientContact(params.ClientContact)
	}
	if params.ClientEmail != "" {
		builder.SetClientEmail(params.ClientEmail)
	}
	if params.Source != "" {
		builder.SetSource(params.Source)
	}
	if params.SourceURL != "" {
		builder.SetSourceURL(params.SourceURL)
	}
	if params.Priority != "" {
		builder.SetPriority(params.Priority)
	}
	if params.EstimatedValue != nil {
		builder.SetEstimatedValue(*params.EstimatedValue)
	}
	if params.Currency != "" {
		builder.SetCurrency(params.Currency)
	}
	if params.PublicationDate != nil {
		builder.SetPublicationDate(*params.PublicationDate)
	}
	if params.ClarificationDeadline != nil {
		builder.SetClarificationDeadline(*params.ClarificationDeadline)
	}
	if params.SubmissionMethod != "" {
		builder.SetSubmissionMethod(params.SubmissionMethod)
	}
	if params.SubmissionAddress != "" {
		builder.SetSubmissionAddress(params.SubmissionAddress)
	}
	if len(params.Categories) > 0 {
		builder.SetCategories(params.Categories)
	}
	if params.RequirementsSummary != nil {
		builder.SetRequirementsSummary(params.RequirementsSummary)
	}
	if params.Metadata != nil {
		builder.SetMetadata(params.Metadata)
	}

	t, err := builder.Save(ctx)
	if err != nil {
		s.logger.Error("failed to create tender",
			zap.String("tenant_id", params.TenantID.String()),
			zap.String("title", params.Title),
			zap.Error(err),
		)
		return nil, fmt.Errorf("failed to create tender: %w", err)
	}

	s.logger.Info("tender created",
		zap.String("tender_id", t.ID.String()),
		zap.String("tender_number", tenderNumber),
		zap.String("tenant_id", params.TenantID.String()),
	)

	return t, nil
}

// Get retrieves a tender by ID within a tenant.
func (s *Service) Get(ctx context.Context, tenantID, tenderID uuid.UUID) (*ent.Tender, error) {
	t, err := s.db.Tender.Query().
		Where(
			tender.ID(tenderID),
			tender.TenantID(tenantID),
		).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to get tender: %w", err)
	}
	return t, nil
}

// GetByNumber retrieves a tender by tender number within a tenant.
func (s *Service) GetByNumber(ctx context.Context, tenantID uuid.UUID, tenderNumber string) (*ent.Tender, error) {
	t, err := s.db.Tender.Query().
		Where(
			tender.TenderNumber(tenderNumber),
			tender.TenantID(tenantID),
		).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to get tender by number: %w", err)
	}
	return t, nil
}

// List retrieves tenders with optional filtering.
func (s *Service) List(ctx context.Context, params ListParams) ([]*ent.Tender, int, error) {
	query := s.db.Tender.Query().
		Where(tender.TenantID(params.TenantID))

	if params.Status != "" {
		query = query.Where(tender.Status(params.Status))
	}
	if params.Priority != "" {
		query = query.Where(tender.Priority(params.Priority))
	}
	if params.Source != "" {
		query = query.Where(tender.Source(params.Source))
	}
	if params.CreatedBy != nil {
		query = query.Where(tender.CreatedBy(*params.CreatedBy))
	}
	if params.DeadlineFrom != nil {
		query = query.Where(tender.DeadlineGTE(*params.DeadlineFrom))
	}
	if params.DeadlineTo != nil {
		query = query.Where(tender.DeadlineLTE(*params.DeadlineTo))
	}

	total, err := query.Clone().Count(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to count tenders: %w", err)
	}

	if params.Limit > 0 {
		query = query.Limit(params.Limit)
	}
	if params.Offset > 0 {
		query = query.Offset(params.Offset)
	}

	query = query.Order(ent.Desc(tender.FieldCreatedAt))

	tenders, err := query.All(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to list tenders: %w", err)
	}

	return tenders, total, nil
}

// Update updates an existing tender.
func (s *Service) Update(ctx context.Context, tenantID, tenderID uuid.UUID, params UpdateParams) (*ent.Tender, error) {
	_, err := s.Get(ctx, tenantID, tenderID)
	if err != nil {
		return nil, err
	}

	builder := s.db.Tender.UpdateOneID(tenderID)

	if params.Title != nil {
		builder.SetTitle(*params.Title)
	}
	if params.Description != nil {
		builder.SetDescription(*params.Description)
	}
	if params.ClientName != nil {
		builder.SetClientName(*params.ClientName)
	}
	if params.ClientContact != nil {
		builder.SetClientContact(*params.ClientContact)
	}
	if params.ClientEmail != nil {
		builder.SetClientEmail(*params.ClientEmail)
	}
	if params.Source != nil {
		builder.SetSource(*params.Source)
	}
	if params.SourceURL != nil {
		builder.SetSourceURL(*params.SourceURL)
	}
	if params.Status != nil {
		builder.SetStatus(*params.Status)
	}
	if params.Priority != nil {
		builder.SetPriority(*params.Priority)
	}
	if params.EstimatedValue != nil {
		builder.SetEstimatedValue(*params.EstimatedValue)
	}
	if params.Currency != nil {
		builder.SetCurrency(*params.Currency)
	}
	if params.PublicationDate != nil {
		builder.SetPublicationDate(*params.PublicationDate)
	}
	if params.Deadline != nil {
		builder.SetDeadline(*params.Deadline)
	}
	if params.ClarificationDeadline != nil {
		builder.SetClarificationDeadline(*params.ClarificationDeadline)
	}
	if params.SubmissionMethod != nil {
		builder.SetSubmissionMethod(*params.SubmissionMethod)
	}
	if params.SubmissionAddress != nil {
		builder.SetSubmissionAddress(*params.SubmissionAddress)
	}
	if params.Categories != nil {
		builder.SetCategories(params.Categories)
	}
	if params.RequirementsSummary != nil {
		builder.SetRequirementsSummary(params.RequirementsSummary)
	}
	if params.Metadata != nil {
		builder.SetMetadata(params.Metadata)
	}

	t, err := builder.Save(ctx)
	if err != nil {
		s.logger.Error("failed to update tender",
			zap.String("tender_id", tenderID.String()),
			zap.Error(err),
		)
		return nil, fmt.Errorf("failed to update tender: %w", err)
	}

	s.logger.Info("tender updated",
		zap.String("tender_id", t.ID.String()),
	)

	return t, nil
}

// UpdateStatus updates the status of a tender.
func (s *Service) UpdateStatus(ctx context.Context, tenantID, tenderID uuid.UUID, status string) (*ent.Tender, error) {
	_, err := s.Get(ctx, tenantID, tenderID)
	if err != nil {
		return nil, err
	}

	t, err := s.db.Tender.UpdateOneID(tenderID).
		SetStatus(status).
		Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to update tender status: %w", err)
	}

	s.logger.Info("tender status updated",
		zap.String("tender_id", t.ID.String()),
		zap.String("status", status),
	)

	return t, nil
}

// RecordDecision records a go/no-go decision for a tender.
func (s *Service) RecordDecision(ctx context.Context, tenantID, tenderID uuid.UUID, params DecisionParams) (*ent.Tender, error) {
	_, err := s.Get(ctx, tenantID, tenderID)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	newStatus := "evaluating"
	if params.Decision == "go" {
		newStatus = "preparing"
	} else if params.Decision == "no_go" {
		newStatus = "withdrawn"
	}

	t, err := s.db.Tender.UpdateOneID(tenderID).
		SetDecision(params.Decision).
		SetDecisionRationale(params.Rationale).
		SetDecisionDate(now).
		SetDecidedBy(params.DecidedBy).
		SetStatus(newStatus).
		Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to record tender decision: %w", err)
	}

	s.logger.Info("tender decision recorded",
		zap.String("tender_id", t.ID.String()),
		zap.String("decision", params.Decision),
	)

	return t, nil
}

// Delete removes a tender by ID.
func (s *Service) Delete(ctx context.Context, tenantID, tenderID uuid.UUID) error {
	_, err := s.Get(ctx, tenantID, tenderID)
	if err != nil {
		return err
	}

	err = s.db.Tender.DeleteOneID(tenderID).Exec(ctx)
	if err != nil {
		s.logger.Error("failed to delete tender",
			zap.String("tender_id", tenderID.String()),
			zap.Error(err),
		)
		return fmt.Errorf("failed to delete tender: %w", err)
	}

	s.logger.Info("tender deleted",
		zap.String("tender_id", tenderID.String()),
		zap.String("tenant_id", tenantID.String()),
	)

	return nil
}

// generateTenderNumber generates a unique tender reference number.
func (s *Service) generateTenderNumber(tenantID uuid.UUID) string {
	now := time.Now()
	shortID := uuid.New().String()[:8]
	return fmt.Sprintf("TND-%d%02d-%s", now.Year(), now.Month(), shortID)
}
