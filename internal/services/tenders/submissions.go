package tenders

import (
	"context"
	"fmt"
	"net/mail"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/bengobox/projects-service/internal/ent"
	enttendersection "github.com/bengobox/projects-service/internal/ent/tendersection"
	enttendersubmission "github.com/bengobox/projects-service/internal/ent/tendersubmission"
	"github.com/bengobox/projects-service/internal/services/activity"
	"github.com/bengobox/projects-service/internal/services/people"
	"github.com/bengobox/projects-service/internal/services/txn"
)

// Tender submissions (sprint 1, US-1.10 and US-1.11). A submission records how the bid went out
// (online portal, email or physical delivery), when, to whom, the confirmation or tracking
// reference, the documents sent and the proof of receipt. The first one needs a go decision
// (status preparing) and every section approved, unless the caller overrides the sections; it
// moves the tender to submitted. Later ones (an amended bid, a clarification) are recorded while
// the bid is out and leave the status alone. projects-api does not send the email itself: the
// tender.submitted event carries the details for a notifications consumer (an open follow-up), and
// the record keeps the reference of the email the team sent.

// SubmissionInput records one submission.
type SubmissionInput struct {
	Method          string     `json:"method"`
	SubmittedAt     *time.Time `json:"submitted_at"`
	Recipient       string     `json:"recipient"`
	Reference       string     `json:"reference"`
	Courier         string     `json:"courier"`
	Notes           string     `json:"notes"`
	DocumentID      *uuid.UUID `json:"document_id"`
	ProofDocumentID *uuid.UUID `json:"proof_document_id"`
	EmailSubject    string     `json:"email_subject"`
	// OverrideSections submits although some sections are not approved (the activity records it).
	OverrideSections bool `json:"override_sections"`
}

// Submission is a submission with the submitter's name.
type Submission struct {
	*ent.TenderSubmission
	SubmittedByName string `json:"submitted_by_name"`
}

// CheckSubmission validates a submission's fields for its method: an email address for email, an
// address for physical delivery, and a time that is not in the future.
func CheckSubmission(in *SubmissionInput, now time.Time) error {
	in.Method = strings.TrimSpace(in.Method)
	in.Recipient, in.Reference = strings.TrimSpace(in.Recipient), strings.TrimSpace(in.Reference)
	in.Courier, in.Notes, in.EmailSubject = strings.TrimSpace(in.Courier), strings.TrimSpace(in.Notes), strings.TrimSpace(in.EmailSubject)
	if !slices.Contains(SubmissionMethods, in.Method) {
		return ErrValidation("method must be online, email or physical")
	}
	switch in.Method {
	case "email":
		addr, err := mail.ParseAddress(in.Recipient)
		if err != nil || addr.Name != "" {
			return ErrValidation("an email submission needs the address it went to")
		}
	case "physical":
		if in.Recipient == "" {
			return ErrValidation("a physical submission needs the address it was delivered to")
		}
	}
	for name, v := range map[string]string{"recipient": in.Recipient, "reference": in.Reference, "courier": in.Courier, "email_subject": in.EmailSubject} {
		if err := checkText(name, v, 500); err != nil {
			return err
		}
	}
	if err := checkText("notes", in.Notes, 4000); err != nil {
		return err
	}
	if in.SubmittedAt != nil && in.SubmittedAt.After(now.Add(5*time.Minute)) {
		return ErrValidation("submitted_at cannot be in the future")
	}
	return nil
}

func (s *Service) namedSubmissions(ctx context.Context, tenantID uuid.UUID, rows []*ent.TenderSubmission) ([]Submission, error) {
	ids := make([]uuid.UUID, len(rows))
	for i, r := range rows {
		ids[i] = r.SubmittedBy
	}
	names, err := people.Lookup(ctx, s.client.ProjectsUser, tenantID, ids)
	if err != nil {
		return nil, err
	}
	out := make([]Submission, len(rows))
	for i, r := range rows {
		out[i] = Submission{TenderSubmission: r, SubmittedByName: names[r.SubmittedBy].Label()}
	}
	return out, nil
}

// Submit records a submission as the signed-in user (see the package note above).
func (s *Service) Submit(ctx context.Context, tenantID, tenderID uuid.UUID, in SubmissionInput) (Submission, error) {
	actor := activity.ActorFrom(ctx)
	if actor == uuid.Nil {
		return Submission{}, ErrValidation("a submission needs a signed-in user")
	}
	now := time.Now().UTC()
	if err := CheckSubmission(&in, now); err != nil {
		return Submission{}, err
	}
	at := now
	if in.SubmittedAt != nil {
		at = in.SubmittedAt.UTC()
	}
	var sub *ent.TenderSubmission
	err := txn.Run(ctx, s.client, func(tx *ent.Tx) error {
		t, err := lockTender(ctx, tx, tenantID, tenderID)
		if err != nil {
			return err
		}
		if !CanSubmit(t.Status) {
			if CanDecide(t.Status) {
				return stateErr("tender_no_go_decision", "make the go decision before submitting the bid")
			}
			return stateErr("tender_closed", "a "+t.Status+" tender takes no more submissions")
		}
		first := t.Status == StatusPreparing
		if first {
			statuses, err := tx.TenderSection.Query().Where(enttendersection.TenantID(tenantID), enttendersection.TenderID(tenderID)).
				Select(enttendersection.FieldStatus).Strings(ctx)
			if err != nil {
				return fmt.Errorf("check sections: %w", err)
			}
			if sum := Summarize(statuses); sum.Total > 0 && !sum.Ready && !in.OverrideSections {
				return stateErr("sections_not_approved", fmt.Sprintf("%d of %d sections are approved", sum.ByStatus[SectionApproved], sum.Total))
			}
		}
		for _, id := range []*uuid.UUID{in.DocumentID, in.ProofDocumentID} {
			if id != nil {
				if err := documentOf(ctx, tx, tenantID, tenderID, *id); err != nil {
					return err
				}
			}
		}
		meta := map[string]any{"resubmission": !first}
		if in.EmailSubject != "" {
			meta["email_subject"] = in.EmailSubject
		}
		if in.OverrideSections && first {
			meta["sections_overridden"] = true
		}
		c := tx.TenderSubmission.Create().SetTenantID(tenantID).SetTenderID(tenderID).SetMethod(in.Method).
			SetSubmittedAt(at).SetSubmittedBy(actor).SetRecipient(in.Recipient).SetReference(in.Reference).
			SetCourier(in.Courier).SetNotes(in.Notes).SetNillableDocumentID(in.DocumentID).
			SetNillableProofDocumentID(in.ProofDocumentID).SetMetadata(meta)
		if sub, err = c.Save(ctx); err != nil {
			return fmt.Errorf("record submission: %w", err)
		}
		prev := t.Status
		u := t.Update().SetSubmissionType(in.Method)
		if first {
			u.SetStatus(StatusSubmitted).SetSubmittedAt(at)
		}
		if t, err = u.Save(ctx); err != nil {
			return fmt.Errorf("mark tender submitted: %w", err)
		}
		changes := map[string]any{"submission": in.Method, "submission_id": sub.ID.String()}
		if first {
			changes["status"] = map[string]any{"from": prev, "to": StatusSubmitted}
		}
		if meta["sections_overridden"] == true {
			changes["sections_overridden"] = true
		}
		if err := record(ctx, tx, tenantID, tenderID, activity.ActionUpdated, changes); err != nil {
			return err
		}
		extra := map[string]any{
			"submission_id": sub.ID.String(), "method": in.Method, "submitted_at": at.Format(time.RFC3339),
			"submitted_by": actor.String(), "recipient": in.Recipient, "reference": in.Reference,
			"resubmission": !first, "previous_status": prev,
		}
		if in.EmailSubject != "" {
			extra["email_subject"] = in.EmailSubject
		}
		return emit(ctx, tx, t, EventSubmitted, extra)
	})
	if err != nil {
		return Submission{}, err
	}
	out, err := s.namedSubmissions(ctx, tenantID, []*ent.TenderSubmission{sub})
	if err != nil {
		return Submission{}, err
	}
	return out[0], nil
}

// ListSubmissions returns a tender's submissions, newest first (bounded by subListLimit).
func (s *Service) ListSubmissions(ctx context.Context, tenantID, tenderID uuid.UUID) ([]Submission, error) {
	if err := s.tenderExists(ctx, tenantID, tenderID); err != nil {
		return nil, err
	}
	rows, err := s.client.TenderSubmission.Query().
		Where(enttendersubmission.TenantID(tenantID), enttendersubmission.TenderID(tenderID)).
		Order(ent.Desc(enttendersubmission.FieldSubmittedAt), ent.Desc(enttendersubmission.FieldID)).
		Limit(subListLimit).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list submissions: %w", err)
	}
	return s.namedSubmissions(ctx, tenantID, rows)
}
