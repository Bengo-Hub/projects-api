package tenders

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/bengobox/projects-service/internal/ent"
	enttender "github.com/bengobox/projects-service/internal/ent/tender"
	enttenderdoc "github.com/bengobox/projects-service/internal/ent/tenderdocument"
	enttendersection "github.com/bengobox/projects-service/internal/ent/tendersection"
)

// Section statuses (US-1.7, US-1.8).
const (
	SectionNotStarted       = "not_started"
	SectionInProgress       = "in_progress"
	SectionReview           = "review"
	SectionChangesRequested = "changes_requested"
	SectionApproved         = "approved"
)

var sectionStatuses = []string{SectionNotStarted, SectionInProgress, SectionReview, SectionChangesRequested, SectionApproved}

// CreateSectionInput adds a section to a tender.
type CreateSectionInput struct {
	Title       string     `json:"title"`
	Description string     `json:"description"`
	AssigneeID  *uuid.UUID `json:"assignee_id"`
	ReviewerID  *uuid.UUID `json:"reviewer_id"`
	DueDate     *time.Time `json:"due_date"`
	SortOrder   *int       `json:"sort_order"`
}

// UpdateSectionInput edits a section. Status here only moves between not_started and
// in_progress; review outcomes go through Submit, Approve and RequestChanges.
type UpdateSectionInput struct {
	Title       *string    `json:"title"`
	Description *string    `json:"description"`
	AssigneeID  *uuid.UUID `json:"assignee_id"`
	ReviewerID  *uuid.UUID `json:"reviewer_id"`
	DueDate     *time.Time `json:"due_date"`
	SortOrder   *int       `json:"sort_order"`
	Status      *string    `json:"status"`
	DocumentURL *string    `json:"document_url"`
}

// SubmitSectionInput hands a section to its reviewer, with the document link if not set yet.
type SubmitSectionInput struct {
	DocumentURL string `json:"document_url"`
}

// ReviewInput carries the reviewer's comments.
type ReviewInput struct {
	Comments   string    `json:"comments"`
	ReviewedBy uuid.UUID `json:"-"`
}

// SectionSummary counts a tender's sections by status.
type SectionSummary struct {
	Total       int            `json:"total"`
	ByStatus    map[string]int `json:"by_status"`
	AllApproved bool           `json:"all_approved"`
	Overdue     int            `json:"overdue"`
}

func summarizeSections(items []*ent.TenderSection, now time.Time) SectionSummary {
	sum := SectionSummary{Total: len(items), ByStatus: map[string]int{}}
	for _, st := range sectionStatuses {
		sum.ByStatus[st] = 0
	}
	for _, sec := range items {
		sum.ByStatus[sec.Status]++
		if sec.Status != SectionApproved && sec.DueDate != nil && sec.DueDate.Before(now) {
			sum.Overdue++
		}
	}
	sum.AllApproved = sum.Total > 0 && sum.ByStatus[SectionApproved] == sum.Total
	return sum
}

func (s *Service) sectionOf(ctx context.Context, tenantID, tenderID, id uuid.UUID) (*ent.TenderSection, error) {
	sec, err := s.client.TenderSection.Query().
		Where(enttendersection.ID(id), enttendersection.TenantID(tenantID), enttendersection.TenderID(tenderID)).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("fetch section: %w", err)
	}
	return sec, nil
}

func (s *Service) sectionPayload(sec *ent.TenderSection) map[string]any {
	p := map[string]any{
		"tender_id":  sec.TenderID.String(),
		"section_id": sec.ID.String(),
		"title":      sec.Title,
		"status":     sec.Status,
	}
	if sec.AssigneeID != nil {
		p["assignee_id"] = sec.AssigneeID.String()
	}
	if sec.ReviewerID != nil {
		p["reviewer_id"] = sec.ReviewerID.String()
	}
	if sec.DueDate != nil {
		p["due_date"] = *sec.DueDate
	}
	if sec.ReviewComments != "" {
		p["review_comments"] = sec.ReviewComments
	}
	return p
}

// CreateSection adds a section to a tender. Assigning someone emits tender.section.assigned.
func (s *Service) CreateSection(ctx context.Context, tenantID, tenderID uuid.UUID, input CreateSectionInput) (*ent.TenderSection, error) {
	if strings.TrimSpace(input.Title) == "" {
		return nil, ErrValidation("title is required")
	}
	if _, err := s.tenderOf(ctx, tenantID, tenderID); err != nil {
		return nil, err
	}
	c := s.client.TenderSection.Create().
		SetTenantID(tenantID).
		SetTenderID(tenderID).
		SetTitle(strings.TrimSpace(input.Title)).
		SetNillableAssigneeID(input.AssigneeID).
		SetNillableReviewerID(input.ReviewerID).
		SetNillableDueDate(input.DueDate)
	if input.Description != "" {
		c = c.SetDescription(input.Description)
	}
	if input.SortOrder != nil {
		c = c.SetSortOrder(*input.SortOrder)
	} else {
		n, err := s.client.TenderSection.Query().
			Where(enttendersection.TenderID(tenderID), enttendersection.TenantID(tenantID)).Count(ctx)
		if err != nil {
			return nil, fmt.Errorf("count sections: %w", err)
		}
		c = c.SetSortOrder(n + 1)
	}
	sec, err := c.Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("create section: %w", err)
	}
	if sec.AssigneeID != nil {
		s.emit(ctx, tenantID, tenderID, "tender.section.assigned", s.sectionPayload(sec))
	}
	return sec, nil
}

// ListSections returns a tender's sections in order with a status summary.
func (s *Service) ListSections(ctx context.Context, tenantID, tenderID uuid.UUID) ([]*ent.TenderSection, SectionSummary, error) {
	if _, err := s.tenderOf(ctx, tenantID, tenderID); err != nil {
		return nil, SectionSummary{}, err
	}
	items, err := s.client.TenderSection.Query().
		Where(enttendersection.TenderID(tenderID), enttendersection.TenantID(tenantID)).
		Order(ent.Asc(enttendersection.FieldSortOrder), ent.Asc(enttendersection.FieldCreatedAt)).
		Limit(subListLimit).All(ctx)
	if err != nil {
		return nil, SectionSummary{}, fmt.Errorf("list sections: %w", err)
	}
	return items, summarizeSections(items, time.Now()), nil
}

// UpdateSection edits a section. A new assignee emits tender.section.assigned. Editing an
// approved section is refused: request changes first.
func (s *Service) UpdateSection(ctx context.Context, tenantID, tenderID, id uuid.UUID, input UpdateSectionInput) (*ent.TenderSection, error) {
	sec, err := s.sectionOf(ctx, tenantID, tenderID, id)
	if err != nil {
		return nil, err
	}
	if sec.Status == SectionApproved {
		return nil, ErrValidation("section is approved; request changes before editing it")
	}
	u := sec.Update()
	if input.Title != nil {
		if strings.TrimSpace(*input.Title) == "" {
			return nil, ErrValidation("title cannot be empty")
		}
		u = u.SetTitle(strings.TrimSpace(*input.Title))
	}
	if input.Description != nil {
		u = u.SetDescription(*input.Description)
	}
	reassigned := false
	if input.AssigneeID != nil {
		reassigned = sec.AssigneeID == nil || *sec.AssigneeID != *input.AssigneeID
		u = u.SetAssigneeID(*input.AssigneeID)
	}
	if input.ReviewerID != nil {
		u = u.SetReviewerID(*input.ReviewerID)
	}
	if input.DueDate != nil {
		u = u.SetDueDate(*input.DueDate)
	}
	if input.SortOrder != nil {
		u = u.SetSortOrder(*input.SortOrder)
	}
	if input.DocumentURL != nil {
		u = u.SetDocumentURL(strings.TrimSpace(*input.DocumentURL))
	}
	if input.Status != nil {
		switch *input.Status {
		case SectionNotStarted, SectionInProgress:
			if sec.Status == SectionReview {
				return nil, ErrTransition{From: sec.Status, To: *input.Status}
			}
			u = u.SetStatus(*input.Status)
		default:
			return nil, ErrValidation("status can only be set to not_started or in_progress; use submit, approve or request-changes")
		}
	}
	updated, err := u.Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("update section: %w", err)
	}
	if reassigned {
		s.emit(ctx, tenantID, tenderID, "tender.section.assigned", s.sectionPayload(updated))
	}
	return updated, nil
}

// SubmitSection sends a section for review. It needs a document link (given here or earlier).
func (s *Service) SubmitSection(ctx context.Context, tenantID, tenderID, id uuid.UUID, input SubmitSectionInput) (*ent.TenderSection, error) {
	sec, err := s.sectionOf(ctx, tenantID, tenderID, id)
	if err != nil {
		return nil, err
	}
	switch sec.Status {
	case SectionNotStarted, SectionInProgress, SectionChangesRequested:
	default:
		return nil, ErrTransition{From: sec.Status, To: SectionReview}
	}
	doc := strings.TrimSpace(input.DocumentURL)
	if doc == "" {
		doc = sec.DocumentURL
	}
	if doc == "" {
		return nil, ErrValidation("document_url is required to submit a section")
	}
	updated, err := sec.Update().
		SetStatus(SectionReview).
		SetDocumentURL(doc).
		SetSubmittedAt(time.Now()).
		Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("submit section: %w", err)
	}
	s.emit(ctx, tenantID, tenderID, "tender.section.submitted", s.sectionPayload(updated))
	return updated, nil
}

func (s *Service) review(ctx context.Context, tenantID, tenderID, id uuid.UUID, to string, input ReviewInput) (*ent.TenderSection, error) {
	sec, err := s.sectionOf(ctx, tenantID, tenderID, id)
	if err != nil {
		return nil, err
	}
	if sec.Status != SectionReview && !(to == SectionChangesRequested && sec.Status == SectionApproved) {
		return nil, ErrTransition{From: sec.Status, To: to}
	}
	u := sec.Update().SetStatus(to).SetReviewedAt(time.Now()).SetReviewComments(input.Comments)
	if input.ReviewedBy != uuid.Nil {
		u = u.SetReviewedBy(input.ReviewedBy)
	}
	updated, err := u.Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("review section: %w", err)
	}
	// A section that is no longer approved makes the compiled document stale.
	if to == SectionChangesRequested {
		if err := s.client.Tender.Update().
			Where(enttender.ID(tenderID), enttender.TenantID(tenantID)).
			SetReadyForSubmission(false).Exec(ctx); err != nil {
			return nil, fmt.Errorf("clear ready flag: %w", err)
		}
	}
	return updated, nil
}

// ApproveSection approves a section under review.
func (s *Service) ApproveSection(ctx context.Context, tenantID, tenderID, id uuid.UUID, input ReviewInput) (*ent.TenderSection, error) {
	sec, err := s.review(ctx, tenantID, tenderID, id, SectionApproved, input)
	if err != nil {
		return nil, err
	}
	s.emit(ctx, tenantID, tenderID, "tender.section.approved", s.sectionPayload(sec))
	return sec, nil
}

// RequestSectionChanges sends a section back to its writer with comments (also allowed on an
// approved section, which reopens it).
func (s *Service) RequestSectionChanges(ctx context.Context, tenantID, tenderID, id uuid.UUID, input ReviewInput) (*ent.TenderSection, error) {
	if strings.TrimSpace(input.Comments) == "" {
		return nil, ErrValidation("comments are required when requesting changes")
	}
	sec, err := s.review(ctx, tenantID, tenderID, id, SectionChangesRequested, input)
	if err != nil {
		return nil, err
	}
	s.emit(ctx, tenantID, tenderID, "tender.section.changes_requested", s.sectionPayload(sec))
	return sec, nil
}

// DeleteSection removes a section.
func (s *Service) DeleteSection(ctx context.Context, tenantID, tenderID, id uuid.UUID) error {
	n, err := s.client.TenderSection.Delete().
		Where(enttendersection.ID(id), enttendersection.TenantID(tenantID), enttendersection.TenderID(tenderID)).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("delete section: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// --- Final document (US-1.9) ---

// FinalDocumentInput is a compiled tender document (a link to the file).
type FinalDocumentInput struct {
	FileURL    string    `json:"file_url"`
	FileName   string    `json:"file_name"`
	FileSize   *int64    `json:"file_size"`
	MimeType   string    `json:"mime_type"`
	UploadedBy uuid.UUID `json:"-"`
}

// AddFinalDocument stores a new version of the compiled tender document. A new version clears
// ready_for_submission so it is checked again.
func (s *Service) AddFinalDocument(ctx context.Context, tenantID, tenderID uuid.UUID, input FinalDocumentInput) (*ent.TenderDocument, error) {
	if strings.TrimSpace(input.FileURL) == "" {
		return nil, ErrValidation("file_url is required")
	}
	t, err := s.tenderOf(ctx, tenantID, tenderID)
	if err != nil {
		return nil, err
	}
	name := strings.TrimSpace(input.FileName)
	if name == "" {
		name = fmt.Sprintf("%s final v%d", t.Number, t.FinalDocumentVersion+1)
	}
	tx, err := s.client.Tx(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin: %w", err)
	}
	version := t.FinalDocumentVersion + 1
	c := tx.TenderDocument.Create().
		SetTenderID(tenderID).
		SetTenantID(tenantID).
		SetFileURL(strings.TrimSpace(input.FileURL)).
		SetFileName(name).
		SetUploadedBy(input.UploadedBy).
		SetKind("final").
		SetVersion(version).
		SetNillableFileSize(input.FileSize)
	if input.MimeType != "" {
		c = c.SetMimeType(input.MimeType)
	}
	doc, err := c.Save(ctx)
	if err != nil {
		_ = tx.Rollback()
		return nil, fmt.Errorf("add final document: %w", err)
	}
	// The version guard makes two concurrent uploads fail one instead of sharing a number.
	n, err := tx.Tender.Update().
		Where(enttender.ID(tenderID), enttender.TenantID(tenantID), enttender.FinalDocumentVersion(t.FinalDocumentVersion)).
		SetFinalDocumentVersion(version).
		SetReadyForSubmission(false).
		Save(ctx)
	if err != nil {
		_ = tx.Rollback()
		return nil, fmt.Errorf("bump document version: %w", err)
	}
	if n == 0 {
		_ = tx.Rollback()
		return nil, ErrValidation("another final document was added at the same time; retry")
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}
	return doc, nil
}

// latestFinalDocument returns the newest final document, or nil.
func (s *Service) latestFinalDocument(ctx context.Context, tenantID, tenderID uuid.UUID) (*ent.TenderDocument, error) {
	doc, err := s.client.TenderDocument.Query().
		Where(enttenderdoc.TenderID(tenderID), enttenderdoc.TenantID(tenantID), enttenderdoc.Kind("final")).
		Order(ent.Desc(enttenderdoc.FieldVersion)).
		First(ctx)
	if ent.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("latest final document: %w", err)
	}
	return doc, nil
}

// SetReady marks the tender ready (or not) for submission. Ready needs a final document and,
// when the tender has sections, every section approved.
func (s *Service) SetReady(ctx context.Context, tenantID, tenderID uuid.UUID, ready bool) (*ent.Tender, error) {
	t, err := s.tenderOf(ctx, tenantID, tenderID)
	if err != nil {
		return nil, err
	}
	if ready {
		sections, sum, err := s.ListSections(ctx, tenantID, tenderID)
		if err != nil {
			return nil, err
		}
		if len(sections) > 0 && !sum.AllApproved {
			return nil, ErrValidation(fmt.Sprintf("%d of %d sections are approved; approve all sections first",
				sum.ByStatus[SectionApproved], sum.Total))
		}
		doc, err := s.latestFinalDocument(ctx, tenantID, tenderID)
		if err != nil {
			return nil, err
		}
		if doc == nil {
			return nil, ErrValidation("add the final tender document first")
		}
	}
	updated, err := t.Update().SetReadyForSubmission(ready).Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("set ready: %w", err)
	}
	return updated, nil
}
