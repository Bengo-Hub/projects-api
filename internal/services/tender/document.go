package tender

import (
	"context"
	"fmt"

	"github.com/bengobox/projects-service/internal/ent"
	"github.com/bengobox/projects-service/internal/ent/tender"
	"github.com/bengobox/projects-service/internal/ent/tenderdocument"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// DocumentCreateParams holds parameters for creating a tender document.
type DocumentCreateParams struct {
	TenantID     uuid.UUID
	TenderID     uuid.UUID
	Name         string
	Description  string
	DocumentType string
	FileURL      string
	FileName     string
	FileSize     int64
	MimeType     string
	UploadedBy   uuid.UUID
	Metadata     map[string]any
}

// DocumentUpdateParams holds parameters for updating a tender document.
type DocumentUpdateParams struct {
	Name         *string
	Description  *string
	DocumentType *string
	Metadata     map[string]any
}

// DocumentListParams holds parameters for listing tender documents.
type DocumentListParams struct {
	TenantID     uuid.UUID
	TenderID     uuid.UUID
	DocumentType string
	LatestOnly   bool
	Limit        int
	Offset       int
}

// CreateDocument creates a new tender document.
func (s *Service) CreateDocument(ctx context.Context, params DocumentCreateParams) (*ent.TenderDocument, error) {
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

	builder := s.db.TenderDocument.Create().
		SetTenantID(params.TenantID).
		SetTenderID(params.TenderID).
		SetName(params.Name).
		SetFileURL(params.FileURL).
		SetFileName(params.FileName).
		SetFileSize(params.FileSize).
		SetUploadedBy(params.UploadedBy)

	if params.Description != "" {
		builder.SetDescription(params.Description)
	}
	if params.DocumentType != "" {
		builder.SetDocumentType(params.DocumentType)
	}
	if params.MimeType != "" {
		builder.SetMimeType(params.MimeType)
	}
	if params.Metadata != nil {
		builder.SetMetadata(params.Metadata)
	}

	doc, err := builder.Save(ctx)
	if err != nil {
		s.logger.Error("failed to create tender document",
			zap.String("tender_id", params.TenderID.String()),
			zap.String("name", params.Name),
			zap.Error(err),
		)
		return nil, fmt.Errorf("failed to create tender document: %w", err)
	}

	s.logger.Info("tender document created",
		zap.String("document_id", doc.ID.String()),
		zap.String("tender_id", params.TenderID.String()),
	)

	return doc, nil
}

// GetDocument retrieves a document by ID.
func (s *Service) GetDocument(ctx context.Context, tenantID, documentID uuid.UUID) (*ent.TenderDocument, error) {
	doc, err := s.db.TenderDocument.Query().
		Where(
			tenderdocument.ID(documentID),
			tenderdocument.TenantID(tenantID),
		).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrDocumentNotFound
		}
		return nil, fmt.Errorf("failed to get tender document: %w", err)
	}
	return doc, nil
}

// ListDocuments retrieves documents for a tender.
func (s *Service) ListDocuments(ctx context.Context, params DocumentListParams) ([]*ent.TenderDocument, int, error) {
	query := s.db.TenderDocument.Query().
		Where(
			tenderdocument.TenantID(params.TenantID),
			tenderdocument.TenderID(params.TenderID),
		)

	if params.DocumentType != "" {
		query = query.Where(tenderdocument.DocumentType(params.DocumentType))
	}
	if params.LatestOnly {
		query = query.Where(tenderdocument.IsLatest(true))
	}

	total, err := query.Clone().Count(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to count documents: %w", err)
	}

	if params.Limit > 0 {
		query = query.Limit(params.Limit)
	}
	if params.Offset > 0 {
		query = query.Offset(params.Offset)
	}

	query = query.Order(ent.Desc(tenderdocument.FieldUploadedAt))

	docs, err := query.All(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to list documents: %w", err)
	}

	return docs, total, nil
}

// UpdateDocument updates a tender document.
func (s *Service) UpdateDocument(ctx context.Context, tenantID, documentID uuid.UUID, params DocumentUpdateParams) (*ent.TenderDocument, error) {
	_, err := s.GetDocument(ctx, tenantID, documentID)
	if err != nil {
		return nil, err
	}

	builder := s.db.TenderDocument.UpdateOneID(documentID)

	if params.Name != nil {
		builder.SetName(*params.Name)
	}
	if params.Description != nil {
		builder.SetDescription(*params.Description)
	}
	if params.DocumentType != nil {
		builder.SetDocumentType(*params.DocumentType)
	}
	if params.Metadata != nil {
		builder.SetMetadata(params.Metadata)
	}

	doc, err := builder.Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to update tender document: %w", err)
	}

	return doc, nil
}

// DeleteDocument removes a tender document.
func (s *Service) DeleteDocument(ctx context.Context, tenantID, documentID uuid.UUID) error {
	_, err := s.GetDocument(ctx, tenantID, documentID)
	if err != nil {
		return err
	}

	err = s.db.TenderDocument.DeleteOneID(documentID).Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to delete tender document: %w", err)
	}

	s.logger.Info("tender document deleted",
		zap.String("document_id", documentID.String()),
	)

	return nil
}

// CreateDocumentVersion creates a new version of an existing document.
func (s *Service) CreateDocumentVersion(ctx context.Context, tenantID, documentID uuid.UUID, params DocumentCreateParams) (*ent.TenderDocument, error) {
	// Get existing document
	existing, err := s.GetDocument(ctx, tenantID, documentID)
	if err != nil {
		return nil, err
	}

	// Mark existing as not latest
	_, err = s.db.TenderDocument.UpdateOneID(documentID).
		SetIsLatest(false).
		Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to update existing document: %w", err)
	}

	// Create new version
	builder := s.db.TenderDocument.Create().
		SetTenantID(params.TenantID).
		SetTenderID(existing.TenderID).
		SetName(params.Name).
		SetFileURL(params.FileURL).
		SetFileName(params.FileName).
		SetFileSize(params.FileSize).
		SetUploadedBy(params.UploadedBy).
		SetVersion(existing.Version + 1).
		SetIsLatest(true).
		SetPreviousVersionID(documentID)

	if params.Description != "" {
		builder.SetDescription(params.Description)
	}
	if params.DocumentType != "" {
		builder.SetDocumentType(params.DocumentType)
	} else {
		builder.SetDocumentType(existing.DocumentType)
	}
	if params.MimeType != "" {
		builder.SetMimeType(params.MimeType)
	}
	if params.Metadata != nil {
		builder.SetMetadata(params.Metadata)
	}

	doc, err := builder.Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create document version: %w", err)
	}

	s.logger.Info("tender document version created",
		zap.String("document_id", doc.ID.String()),
		zap.Int("version", doc.Version),
	)

	return doc, nil
}
