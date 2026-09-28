package tenders

import (
	"context"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/bengobox/projects-service/internal/ent"
	enttendersub "github.com/bengobox/projects-service/internal/ent/tendersubmission"
)

// SubmitTenderInput records a tender hand-in (US-1.10, US-1.11).
//
// email:    recipient_email and email_subject are required; the event carries the message and
//
//	the final document link so notifications-api can send it. The record starts "queued".
//
// physical: address is required; courier, tracking_number and proof_url are optional.
// online:   confirmation_number (the portal reference) is recommended.
type SubmitTenderInput struct {
	Method             string     `json:"method"`
	SubmittedAt        *time.Time `json:"submitted_at"`
	RecipientEmail     string     `json:"recipient_email"`
	EmailSubject       string     `json:"email_subject"`
	EmailBody          string     `json:"email_body"`
	DocumentURL        string     `json:"document_url"`
	Courier            string     `json:"courier"`
	TrackingNumber     string     `json:"tracking_number"`
	Address            string     `json:"address"`
	ProofURL           string     `json:"proof_url"`
	ConfirmationNumber string     `json:"confirmation_number"`
	Notes              string     `json:"notes"`
	SubmittedBy        uuid.UUID  `json:"-"`
}

// validateSubmission checks the fields each method needs.
func validateSubmission(in *SubmitTenderInput) error {
	in.Method = strings.ToLower(strings.TrimSpace(in.Method))
	switch in.Method {
	case "email":
		if _, err := mail.ParseAddress(in.RecipientEmail); err != nil {
			return ErrValidation("a valid recipient_email is required for email submission")
		}
		if strings.TrimSpace(in.EmailSubject) == "" {
			return ErrValidation("email_subject is required for email submission")
		}
	case "physical":
		if strings.TrimSpace(in.Address) == "" {
			return ErrValidation("address is required for physical submission")
		}
	case "online":
	default:
		return ErrValidation(`method must be "email", "physical" or "online"`)
	}
	if in.SubmittedAt != nil && in.SubmittedAt.After(time.Now().Add(5*time.Minute)) {
		return ErrValidation("submitted_at cannot be in the future")
	}
	return nil
}

// SubmitTender records the submission and moves the tender to submitted. A tender that is
// already submitted (or further along) can take more records, for example a physical copy after
// the email, without changing status. The final document defaults to the latest version.
func (s *Service) SubmitTender(ctx context.Context, tenantID, tenderID uuid.UUID, input SubmitTenderInput) (*ent.TenderSubmission, error) {
	if err := validateSubmission(&input); err != nil {
		return nil, err
	}
	t, err := s.tenderOf(ctx, tenantID, tenderID)
	if err != nil {
		return nil, err
	}
	firstSubmission := CanTransition(t.Status, StatusSubmitted)
	switch {
	case firstSubmission:
	case t.Status == StatusSubmitted || t.Status == StatusUnderReview || t.Status == StatusShortlisted || t.Status == StatusInterview:
	default:
		return nil, ErrTransition{From: t.Status, To: StatusSubmitted}
	}
	now := time.Now()
	at := now
	if input.SubmittedAt != nil {
		at = *input.SubmittedAt
	}
	docURL := strings.TrimSpace(input.DocumentURL)
	if docURL == "" {
		if doc, err := s.latestFinalDocument(ctx, tenantID, tenderID); err == nil && doc != nil {
			docURL = doc.FileURL
		}
	}
	status := "recorded"
	if input.Method == "email" {
		status = "queued"
	}

	tx, err := s.client.Tx(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin: %w", err)
	}
	sub, err := tx.TenderSubmission.Create().
		SetTenantID(tenantID).
		SetTenderID(tenderID).
		SetMethod(input.Method).
		SetSubmittedAt(at).
		SetSubmittedBy(input.SubmittedBy).
		SetRecipientEmail(strings.TrimSpace(input.RecipientEmail)).
		SetEmailSubject(input.EmailSubject).
		SetEmailBody(input.EmailBody).
		SetDocumentURL(docURL).
		SetCourier(input.Courier).
		SetTrackingNumber(input.TrackingNumber).
		SetAddress(input.Address).
		SetProofURL(input.ProofURL).
		SetConfirmationNumber(input.ConfirmationNumber).
		SetDeliveryStatus(status).
		SetNotes(input.Notes).
		Save(ctx)
	if err != nil {
		_ = tx.Rollback()
		return nil, fmt.Errorf("record submission: %w", err)
	}
	var moved *ent.Tender
	if firstSubmission {
		history, err := nextHistory(t, StatusSubmitted, "submitted by "+input.Method, input.SubmittedBy, now)
		if err != nil {
			_ = tx.Rollback()
			return nil, err
		}
		moved, err = tx.Tender.UpdateOneID(t.ID).
			SetStatus(StatusSubmitted).
			SetStatusHistory(history).
			SetSubmittedAt(at).
			SetSubmissionType(input.Method).
			Save(ctx)
		if err != nil {
			_ = tx.Rollback()
			return nil, fmt.Errorf("mark submitted: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}

	payload := s.tenderPayload(t)
	if moved != nil {
		payload = s.tenderPayload(moved)
	}
	payload["submission_id"] = sub.ID.String()
	payload["method"] = sub.Method
	payload["submitted_at"] = sub.SubmittedAt
	payload["document_url"] = sub.DocumentURL
	if sub.Method == "email" {
		// notifications-api sends this email and reports delivery back on the submission.
		payload["recipient_email"] = sub.RecipientEmail
		payload["email_subject"] = sub.EmailSubject
		payload["email_body"] = sub.EmailBody
	} else {
		payload["courier"] = sub.Courier
		payload["tracking_number"] = sub.TrackingNumber
		payload["confirmation_number"] = sub.ConfirmationNumber
	}
	s.emit(ctx, tenantID, tenderID, "tender.submitted", payload)
	if moved != nil {
		s.emitStatusChanged(ctx, t.Status, moved, "submitted by "+input.Method)
	}
	return sub, nil
}

// ListSubmissions returns a tender's submission records, newest first.
func (s *Service) ListSubmissions(ctx context.Context, tenantID, tenderID uuid.UUID) ([]*ent.TenderSubmission, error) {
	if _, err := s.tenderOf(ctx, tenantID, tenderID); err != nil {
		return nil, err
	}
	items, err := s.client.TenderSubmission.Query().
		Where(enttendersub.TenderID(tenderID), enttendersub.TenantID(tenantID)).
		Order(ent.Desc(enttendersub.FieldSubmittedAt)).
		Limit(subListLimit).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list submissions: %w", err)
	}
	return items, nil
}
