package tender

import (
	"context"
	"fmt"
	"time"

	"github.com/bengobox/projects-service/internal/ent"
	"github.com/bengobox/projects-service/internal/ent/tender"
	"github.com/bengobox/projects-service/internal/ent/tendersection"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// SectionCreateParams holds parameters for creating a tender section.
type SectionCreateParams struct {
	TenantID            uuid.UUID
	TenderID            uuid.UUID
	ParentID            *uuid.UUID
	Title               string
	Description         string
	SectionNumber       string
	SortOrder           int
	SectionType         string
	AssigneeID          *uuid.UUID
	DueDate             *time.Time
	PageLimit           *int
	ComplianceChecklist []map[string]any
	Metadata            map[string]any
}

// SectionUpdateParams holds parameters for updating a section.
type SectionUpdateParams struct {
	Title               *string
	Description         *string
	SectionNumber       *string
	SortOrder           *int
	SectionType         *string
	AssigneeID          *uuid.UUID
	Status              *string
	DueDate             *time.Time
	Content             *string
	WordCount           *int
	PageLimit           *int
	ReviewStatus        *string
	ReviewerID          *uuid.UUID
	ReviewerComments    *string
	ComplianceChecklist []map[string]any
	IsCompliant         *bool
	Metadata            map[string]any
}

// SectionListParams holds parameters for listing sections.
type SectionListParams struct {
	TenantID   uuid.UUID
	TenderID   uuid.UUID
	AssigneeID *uuid.UUID
	Status     string
	ParentOnly bool
	Limit      int
	Offset     int
}

// CreateSection creates a new tender section.
func (s *Service) CreateSection(ctx context.Context, params SectionCreateParams) (*ent.TenderSection, error) {
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

	builder := s.db.TenderSection.Create().
		SetTenantID(params.TenantID).
		SetTenderID(params.TenderID).
		SetTitle(params.Title)

	if params.ParentID != nil {
		builder.SetParentID(*params.ParentID)
	}
	if params.Description != "" {
		builder.SetDescription(params.Description)
	}
	if params.SectionNumber != "" {
		builder.SetSectionNumber(params.SectionNumber)
	}
	if params.SortOrder > 0 {
		builder.SetSortOrder(params.SortOrder)
	}
	if params.SectionType != "" {
		builder.SetSectionType(params.SectionType)
	}
	if params.AssigneeID != nil {
		builder.SetAssigneeID(*params.AssigneeID)
	}
	if params.DueDate != nil {
		builder.SetDueDate(*params.DueDate)
	}
	if params.PageLimit != nil {
		builder.SetPageLimit(*params.PageLimit)
	}
	if len(params.ComplianceChecklist) > 0 {
		builder.SetComplianceChecklist(params.ComplianceChecklist)
	}
	if params.Metadata != nil {
		builder.SetMetadata(params.Metadata)
	}

	section, err := builder.Save(ctx)
	if err != nil {
		s.logger.Error("failed to create tender section",
			zap.String("tender_id", params.TenderID.String()),
			zap.String("title", params.Title),
			zap.Error(err),
		)
		return nil, fmt.Errorf("failed to create tender section: %w", err)
	}

	s.logger.Info("tender section created",
		zap.String("section_id", section.ID.String()),
		zap.String("tender_id", params.TenderID.String()),
	)

	return section, nil
}

// GetSection retrieves a section by ID.
func (s *Service) GetSection(ctx context.Context, tenantID, sectionID uuid.UUID) (*ent.TenderSection, error) {
	section, err := s.db.TenderSection.Query().
		Where(
			tendersection.ID(sectionID),
			tendersection.TenantID(tenantID),
		).
		WithChildren().
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrSectionNotFound
		}
		return nil, fmt.Errorf("failed to get tender section: %w", err)
	}
	return section, nil
}

// ListSections retrieves sections for a tender.
func (s *Service) ListSections(ctx context.Context, params SectionListParams) ([]*ent.TenderSection, int, error) {
	query := s.db.TenderSection.Query().
		Where(
			tendersection.TenantID(params.TenantID),
			tendersection.TenderID(params.TenderID),
		)

	if params.AssigneeID != nil {
		query = query.Where(tendersection.AssigneeID(*params.AssigneeID))
	}
	if params.Status != "" {
		query = query.Where(tendersection.Status(params.Status))
	}
	if params.ParentOnly {
		query = query.Where(tendersection.ParentIDIsNil())
	}

	total, err := query.Clone().Count(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to count sections: %w", err)
	}

	if params.Limit > 0 {
		query = query.Limit(params.Limit)
	}
	if params.Offset > 0 {
		query = query.Offset(params.Offset)
	}

	query = query.
		WithChildren().
		Order(ent.Asc(tendersection.FieldSortOrder))

	sections, err := query.All(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to list sections: %w", err)
	}

	return sections, total, nil
}

// UpdateSection updates a tender section.
func (s *Service) UpdateSection(ctx context.Context, tenantID, sectionID uuid.UUID, params SectionUpdateParams) (*ent.TenderSection, error) {
	existing, err := s.GetSection(ctx, tenantID, sectionID)
	if err != nil {
		return nil, err
	}

	builder := s.db.TenderSection.UpdateOneID(sectionID)

	if params.Title != nil {
		builder.SetTitle(*params.Title)
	}
	if params.Description != nil {
		builder.SetDescription(*params.Description)
	}
	if params.SectionNumber != nil {
		builder.SetSectionNumber(*params.SectionNumber)
	}
	if params.SortOrder != nil {
		builder.SetSortOrder(*params.SortOrder)
	}
	if params.SectionType != nil {
		builder.SetSectionType(*params.SectionType)
	}
	if params.AssigneeID != nil {
		builder.SetAssigneeID(*params.AssigneeID)
	}
	if params.Status != nil {
		builder.SetStatus(*params.Status)
		// Track status transitions
		if *params.Status == "in_progress" && existing.StartedAt.IsZero() {
			builder.SetStartedAt(time.Now())
		}
		if *params.Status == "approved" && existing.CompletedAt.IsZero() {
			builder.SetCompletedAt(time.Now())
		}
	}
	if params.DueDate != nil {
		builder.SetDueDate(*params.DueDate)
	}
	if params.Content != nil {
		builder.SetContent(*params.Content)
		// Auto-calculate word count
		wordCount := len([]rune(*params.Content)) / 5 // Rough estimate
		builder.SetWordCount(wordCount)
	}
	if params.WordCount != nil {
		builder.SetWordCount(*params.WordCount)
	}
	if params.PageLimit != nil {
		builder.SetPageLimit(*params.PageLimit)
	}
	if params.ReviewStatus != nil {
		builder.SetReviewStatus(*params.ReviewStatus)
	}
	if params.ReviewerID != nil {
		builder.SetReviewerID(*params.ReviewerID)
	}
	if params.ReviewerComments != nil {
		builder.SetReviewerComments(*params.ReviewerComments)
		builder.SetReviewedAt(time.Now())
	}
	if params.ComplianceChecklist != nil {
		builder.SetComplianceChecklist(params.ComplianceChecklist)
	}
	if params.IsCompliant != nil {
		builder.SetIsCompliant(*params.IsCompliant)
	}
	if params.Metadata != nil {
		builder.SetMetadata(params.Metadata)
	}

	section, err := builder.Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to update tender section: %w", err)
	}

	return section, nil
}

// AssignSection assigns a section to a user.
func (s *Service) AssignSection(ctx context.Context, tenantID, sectionID, assigneeID uuid.UUID) (*ent.TenderSection, error) {
	return s.UpdateSection(ctx, tenantID, sectionID, SectionUpdateParams{
		AssigneeID: &assigneeID,
	})
}

// SubmitSectionForReview marks a section as ready for review.
func (s *Service) SubmitSectionForReview(ctx context.Context, tenantID, sectionID uuid.UUID, reviewerID uuid.UUID) (*ent.TenderSection, error) {
	status := "review"
	reviewStatus := "pending_technical"
	return s.UpdateSection(ctx, tenantID, sectionID, SectionUpdateParams{
		Status:       &status,
		ReviewStatus: &reviewStatus,
		ReviewerID:   &reviewerID,
	})
}

// ApproveSection approves a section after review.
func (s *Service) ApproveSection(ctx context.Context, tenantID, sectionID uuid.UUID, reviewerID uuid.UUID, comments string) (*ent.TenderSection, error) {
	status := "approved"
	reviewStatus := "approved"
	return s.UpdateSection(ctx, tenantID, sectionID, SectionUpdateParams{
		Status:           &status,
		ReviewStatus:     &reviewStatus,
		ReviewerID:       &reviewerID,
		ReviewerComments: &comments,
	})
}

// RejectSection rejects a section and sends it back for revisions.
func (s *Service) RejectSection(ctx context.Context, tenantID, sectionID uuid.UUID, reviewerID uuid.UUID, comments string) (*ent.TenderSection, error) {
	status := "rejected"
	return s.UpdateSection(ctx, tenantID, sectionID, SectionUpdateParams{
		Status:           &status,
		ReviewerID:       &reviewerID,
		ReviewerComments: &comments,
	})
}

// DeleteSection removes a section.
func (s *Service) DeleteSection(ctx context.Context, tenantID, sectionID uuid.UUID) error {
	_, err := s.GetSection(ctx, tenantID, sectionID)
	if err != nil {
		return err
	}

	err = s.db.TenderSection.DeleteOneID(sectionID).Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to delete section: %w", err)
	}

	s.logger.Info("tender section deleted",
		zap.String("section_id", sectionID.String()),
	)

	return nil
}

// GetSectionProgress returns progress summary for tender sections.
func (s *Service) GetSectionProgress(ctx context.Context, tenantID, tenderID uuid.UUID) (*SectionProgress, error) {
	sections, _, err := s.ListSections(ctx, SectionListParams{
		TenantID: tenantID,
		TenderID: tenderID,
		Limit:    1000,
	})
	if err != nil {
		return nil, err
	}

	progress := &SectionProgress{
		Total:       len(sections),
		ByStatus:    make(map[string]int),
		ByAssignee:  make(map[string]int),
		Overdue:     0,
		Unassigned:  0,
	}

	now := time.Now()
	for _, section := range sections {
		progress.ByStatus[section.Status]++

		if section.AssigneeID != uuid.Nil {
			progress.ByAssignee[section.AssigneeID.String()]++
		} else {
			progress.Unassigned++
		}

		if !section.DueDate.IsZero() && section.DueDate.Before(now) && section.Status != "approved" {
			progress.Overdue++
		}

		if section.Status == "approved" {
			progress.Completed++
		}
	}

	if progress.Total > 0 {
		progress.CompletionPercent = float64(progress.Completed) / float64(progress.Total) * 100
	}

	return progress, nil
}

// SectionProgress holds progress summary for sections.
type SectionProgress struct {
	Total             int            `json:"total"`
	Completed         int            `json:"completed"`
	CompletionPercent float64        `json:"completion_percent"`
	ByStatus          map[string]int `json:"by_status"`
	ByAssignee        map[string]int `json:"by_assignee"`
	Overdue           int            `json:"overdue"`
	Unassigned        int            `json:"unassigned"`
}
