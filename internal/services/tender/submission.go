package tender

import (
	"context"
	"fmt"
	"time"

	"github.com/bengobox/projects-service/internal/ent"
	"github.com/bengobox/projects-service/internal/ent/tender"
	"github.com/bengobox/projects-service/internal/ent/tendersubmission"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// SubmissionCreateParams holds parameters for creating a tender submission.
type SubmissionCreateParams struct {
	TenantID                 uuid.UUID
	TenderID                 uuid.UUID
	SubmissionType           string
	RecipientEmail           string
	RecipientAddress         string
	PortalURL                string
	CourierService           string
	Documents                []map[string]any
	TotalPages               *int
	CopyCount                *int
	Notes                    string
	Metadata                 map[string]any
}

// SubmissionUpdateParams holds parameters for updating a submission.
type SubmissionUpdateParams struct {
	Status                   *string
	SubmittedAt              *time.Time
	SubmittedBy              *uuid.UUID
	RecipientEmail           *string
	RecipientAddress         *string
	PortalURL                *string
	PortalConfirmationNumber *string
	CourierService           *string
	TrackingNumber           *string
	EstimatedDelivery        *time.Time
	DeliveredAt              *time.Time
	DeliveryProofURL         *string
	EmailMessageID           *string
	EmailOpened              *bool
	EmailOpenedAt            *time.Time
	Documents                []map[string]any
	TotalPages               *int
	CopyCount                *int
	Notes                    *string
	RejectionReason          *string
	Metadata                 map[string]any
}

// CreateSubmission creates a new tender submission record.
func (s *Service) CreateSubmission(ctx context.Context, params SubmissionCreateParams) (*ent.TenderSubmission, error) {
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

	builder := s.db.TenderSubmission.Create().
		SetTenantID(params.TenantID).
		SetTenderID(params.TenderID)

	if params.SubmissionType != "" {
		builder.SetSubmissionType(params.SubmissionType)
	}
	if params.RecipientEmail != "" {
		builder.SetRecipientEmail(params.RecipientEmail)
	}
	if params.RecipientAddress != "" {
		builder.SetRecipientAddress(params.RecipientAddress)
	}
	if params.PortalURL != "" {
		builder.SetPortalURL(params.PortalURL)
	}
	if params.CourierService != "" {
		builder.SetCourierService(params.CourierService)
	}
	if len(params.Documents) > 0 {
		builder.SetDocuments(params.Documents)
	}
	if params.TotalPages != nil {
		builder.SetTotalPages(*params.TotalPages)
	}
	if params.CopyCount != nil {
		builder.SetCopyCount(*params.CopyCount)
	}
	if params.Notes != "" {
		builder.SetNotes(params.Notes)
	}
	if params.Metadata != nil {
		builder.SetMetadata(params.Metadata)
	}

	submission, err := builder.Save(ctx)
	if err != nil {
		s.logger.Error("failed to create tender submission",
			zap.String("tender_id", params.TenderID.String()),
			zap.Error(err),
		)
		return nil, fmt.Errorf("failed to create tender submission: %w", err)
	}

	s.logger.Info("tender submission created",
		zap.String("submission_id", submission.ID.String()),
		zap.String("tender_id", params.TenderID.String()),
	)

	return submission, nil
}

// GetSubmission retrieves a submission by ID.
func (s *Service) GetSubmission(ctx context.Context, tenantID, submissionID uuid.UUID) (*ent.TenderSubmission, error) {
	submission, err := s.db.TenderSubmission.Query().
		Where(
			tendersubmission.ID(submissionID),
			tendersubmission.TenantID(tenantID),
		).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrSubmissionNotFound
		}
		return nil, fmt.Errorf("failed to get tender submission: %w", err)
	}
	return submission, nil
}

// ListSubmissions retrieves submissions for a tender.
func (s *Service) ListSubmissions(ctx context.Context, tenantID, tenderID uuid.UUID) ([]*ent.TenderSubmission, error) {
	submissions, err := s.db.TenderSubmission.Query().
		Where(
			tendersubmission.TenantID(tenantID),
			tendersubmission.TenderID(tenderID),
		).
		Order(ent.Desc(tendersubmission.FieldCreatedAt)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list submissions: %w", err)
	}
	return submissions, nil
}

// UpdateSubmission updates a tender submission.
func (s *Service) UpdateSubmission(ctx context.Context, tenantID, submissionID uuid.UUID, params SubmissionUpdateParams) (*ent.TenderSubmission, error) {
	_, err := s.GetSubmission(ctx, tenantID, submissionID)
	if err != nil {
		return nil, err
	}

	builder := s.db.TenderSubmission.UpdateOneID(submissionID)

	if params.Status != nil {
		builder.SetStatus(*params.Status)
	}
	if params.SubmittedAt != nil {
		builder.SetSubmittedAt(*params.SubmittedAt)
	}
	if params.SubmittedBy != nil {
		builder.SetSubmittedBy(*params.SubmittedBy)
	}
	if params.RecipientEmail != nil {
		builder.SetRecipientEmail(*params.RecipientEmail)
	}
	if params.RecipientAddress != nil {
		builder.SetRecipientAddress(*params.RecipientAddress)
	}
	if params.PortalURL != nil {
		builder.SetPortalURL(*params.PortalURL)
	}
	if params.PortalConfirmationNumber != nil {
		builder.SetPortalConfirmationNumber(*params.PortalConfirmationNumber)
	}
	if params.CourierService != nil {
		builder.SetCourierService(*params.CourierService)
	}
	if params.TrackingNumber != nil {
		builder.SetTrackingNumber(*params.TrackingNumber)
	}
	if params.EstimatedDelivery != nil {
		builder.SetEstimatedDelivery(*params.EstimatedDelivery)
	}
	if params.DeliveredAt != nil {
		builder.SetDeliveredAt(*params.DeliveredAt)
	}
	if params.DeliveryProofURL != nil {
		builder.SetDeliveryProofURL(*params.DeliveryProofURL)
	}
	if params.EmailMessageID != nil {
		builder.SetEmailMessageID(*params.EmailMessageID)
	}
	if params.EmailOpened != nil {
		builder.SetEmailOpened(*params.EmailOpened)
	}
	if params.EmailOpenedAt != nil {
		builder.SetEmailOpenedAt(*params.EmailOpenedAt)
	}
	if params.Documents != nil {
		builder.SetDocuments(params.Documents)
	}
	if params.TotalPages != nil {
		builder.SetTotalPages(*params.TotalPages)
	}
	if params.CopyCount != nil {
		builder.SetCopyCount(*params.CopyCount)
	}
	if params.Notes != nil {
		builder.SetNotes(*params.Notes)
	}
	if params.RejectionReason != nil {
		builder.SetRejectionReason(*params.RejectionReason)
	}
	if params.Metadata != nil {
		builder.SetMetadata(params.Metadata)
	}

	submission, err := builder.Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to update tender submission: %w", err)
	}

	return submission, nil
}

// SubmitTender marks a submission as submitted and updates the tender status.
func (s *Service) SubmitTender(ctx context.Context, tenantID, submissionID uuid.UUID, submittedBy uuid.UUID) (*ent.TenderSubmission, error) {
	submission, err := s.GetSubmission(ctx, tenantID, submissionID)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	status := "submitted"

	// Update submission
	submission, err = s.UpdateSubmission(ctx, tenantID, submissionID, SubmissionUpdateParams{
		Status:      &status,
		SubmittedAt: &now,
		SubmittedBy: &submittedBy,
	})
	if err != nil {
		return nil, err
	}

	// Update tender status
	_, err = s.UpdateStatus(ctx, tenantID, submission.TenderID, "submitted")
	if err != nil {
		s.logger.Warn("failed to update tender status after submission",
			zap.String("tender_id", submission.TenderID.String()),
			zap.Error(err),
		)
	}

	s.logger.Info("tender submitted",
		zap.String("submission_id", submissionID.String()),
		zap.String("tender_id", submission.TenderID.String()),
	)

	return submission, nil
}

// ConfirmDelivery marks a physical submission as delivered.
func (s *Service) ConfirmDelivery(ctx context.Context, tenantID, submissionID uuid.UUID, deliveryProofURL string) (*ent.TenderSubmission, error) {
	now := time.Now()
	status := "confirmed"

	return s.UpdateSubmission(ctx, tenantID, submissionID, SubmissionUpdateParams{
		Status:           &status,
		DeliveredAt:      &now,
		DeliveryProofURL: &deliveryProofURL,
	})
}

// RecordEmailTracking records email tracking information.
func (s *Service) RecordEmailTracking(ctx context.Context, tenantID, submissionID uuid.UUID, messageID string, opened bool) (*ent.TenderSubmission, error) {
	params := SubmissionUpdateParams{
		EmailMessageID: &messageID,
		EmailOpened:    &opened,
	}

	if opened {
		now := time.Now()
		params.EmailOpenedAt = &now
	}

	return s.UpdateSubmission(ctx, tenantID, submissionID, params)
}

// DeleteSubmission removes a submission record.
func (s *Service) DeleteSubmission(ctx context.Context, tenantID, submissionID uuid.UUID) error {
	_, err := s.GetSubmission(ctx, tenantID, submissionID)
	if err != nil {
		return err
	}

	err = s.db.TenderSubmission.DeleteOneID(submissionID).Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to delete submission: %w", err)
	}

	s.logger.Info("tender submission deleted",
		zap.String("submission_id", submissionID.String()),
	)

	return nil
}
