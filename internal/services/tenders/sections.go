package tenders

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/bengobox/projects-service/internal/ent"
	enttendersection "github.com/bengobox/projects-service/internal/ent/tendersection"
	"github.com/bengobox/projects-service/internal/services/activity"
	"github.com/bengobox/projects-service/internal/services/people"
	"github.com/bengobox/projects-service/internal/services/tasks"
	"github.com/bengobox/projects-service/internal/services/txn"
)

// Tender sections (sprint 1, US-1.7 to US-1.9). A tender manager breaks the bid into sections,
// each assigned to a person with a deadline and a reviewer. The assignee works on it (in_progress,
// a draft document) and submits it for review; the reviewer, or a tender manager, approves it or
// asks for changes with a comment. Nobody reviews a section assigned to them. The bid is ready
// to submit when every section is approved.
const (
	SectionNotStarted       = "not_started"
	SectionInProgress       = "in_progress"
	SectionInReview         = "in_review"
	SectionChangesRequested = "changes_requested"
	SectionApproved         = "approved"
	maxSections             = 200
)

// SectionStatuses is every section status in order.
var SectionStatuses = []string{SectionNotStarted, SectionInProgress, SectionInReview, SectionChangesRequested, SectionApproved}

// SectionInput creates or edits a section. On an edit absent fields stay; an empty assignee_id,
// reviewer_id or document_id clears it and an empty due_date clears the deadline.
type SectionInput struct {
	Title       *string     `json:"title"`
	Description *string     `json:"description"`
	Position    *int        `json:"position"`
	AssigneeID  *string     `json:"assignee_id"`
	ReviewerID  *string     `json:"reviewer_id"`
	DueDate     *tasks.Date `json:"due_date"`
	DocumentID  *string     `json:"document_id"`
	// Status may only move between not_started and in_progress here; review has its own routes.
	Status *string `json:"status"`
}

// Section is a section with its people's names.
type Section struct {
	*ent.TenderSection
	AssigneeName string `json:"assignee_name"`
	ReviewerName string `json:"reviewer_name"`
}

// SectionSummary counts a tender's sections by status; Ready is true when there is at least one
// section and every one is approved.
type SectionSummary struct {
	Total    int            `json:"total"`
	ByStatus map[string]int `json:"by_status"`
	Ready    bool           `json:"ready"`
}

// Summarize counts sections by status.
func Summarize(statuses []string) SectionSummary {
	out := SectionSummary{Total: len(statuses), ByStatus: make(map[string]int, len(SectionStatuses))}
	for _, st := range SectionStatuses {
		out.ByStatus[st] = 0
	}
	for _, st := range statuses {
		out.ByStatus[st]++
	}
	out.Ready = out.Total > 0 && out.ByStatus[SectionApproved] == out.Total
	return out
}

func optionalUUID(v *string, field string) (*uuid.UUID, bool, error) {
	if v == nil {
		return nil, false, nil
	}
	s := strings.TrimSpace(*v)
	if s == "" {
		return nil, true, nil
	}
	id, err := uuid.Parse(s)
	if err != nil {
		return nil, true, ErrValidation(field + " must be a UUID")
	}
	return &id, true, nil
}

func (s *Service) named(ctx context.Context, tenantID uuid.UUID, rows []*ent.TenderSection) ([]Section, error) {
	ids := make([]uuid.UUID, 0, 2*len(rows))
	for _, r := range rows {
		if r.AssigneeID != nil {
			ids = append(ids, *r.AssigneeID)
		}
		if r.ReviewerID != nil {
			ids = append(ids, *r.ReviewerID)
		}
	}
	names, err := people.Lookup(ctx, s.client.ProjectsUser, tenantID, ids)
	if err != nil {
		return nil, err
	}
	out := make([]Section, len(rows))
	for i, r := range rows {
		out[i] = Section{TenderSection: r}
		if r.AssigneeID != nil {
			out[i].AssigneeName = names[*r.AssigneeID].Label()
		}
		if r.ReviewerID != nil {
			out[i].ReviewerName = names[*r.ReviewerID].Label()
		}
	}
	return out, nil
}

// ListSections returns a tender's sections in order (bounded by maxSections) with the summary.
func (s *Service) ListSections(ctx context.Context, tenantID, tenderID uuid.UUID) ([]Section, SectionSummary, error) {
	if err := s.tenderExists(ctx, tenantID, tenderID); err != nil {
		return nil, SectionSummary{}, err
	}
	rows, err := s.client.TenderSection.Query().
		Where(enttendersection.TenantID(tenantID), enttendersection.TenderID(tenderID)).
		Order(ent.Asc(enttendersection.FieldPosition), ent.Asc(enttendersection.FieldCreatedAt)).
		Limit(maxSections).All(ctx)
	if err != nil {
		return nil, SectionSummary{}, fmt.Errorf("list sections: %w", err)
	}
	statuses := make([]string, len(rows))
	for i, r := range rows {
		statuses[i] = r.Status
	}
	out, err := s.named(ctx, tenantID, rows)
	return out, Summarize(statuses), err
}

func sectionRecord(ctx context.Context, tx *ent.Tx, sec *ent.TenderSection, action string, changes map[string]any) error {
	changes["section_id"], changes["section"] = sec.ID.String(), sec.Title
	return record(ctx, tx, sec.TenantID, sec.TenderID, action, changes)
}

// sectionsOpen refuses section changes on a tender whose bid is no longer being prepared.
func sectionsOpen(t *ent.Tender) error {
	switch t.Status {
	case StatusAwarded, StatusLost, StatusCancelled, StatusNoGo:
		return stateErr("tender_closed", "the sections of a "+t.Status+" tender are closed")
	}
	return nil
}

// CreateSection adds a section to a tender (tender managers).
func (s *Service) CreateSection(ctx context.Context, tenantID, tenderID uuid.UUID, in SectionInput) (Section, error) {
	if in.Title == nil || strings.TrimSpace(*in.Title) == "" || len([]rune(*in.Title)) > 200 {
		return Section{}, ErrValidation("title is required (at most 200 characters)")
	}
	assignee, _, err := optionalUUID(in.AssigneeID, "assignee_id")
	if err != nil {
		return Section{}, err
	}
	reviewer, _, err := optionalUUID(in.ReviewerID, "reviewer_id")
	if err != nil {
		return Section{}, err
	}
	if assignee != nil && reviewer != nil && *assignee == *reviewer {
		return Section{}, ErrValidation("the reviewer must be someone other than the assignee")
	}
	doc, _, err := optionalUUID(in.DocumentID, "document_id")
	if err != nil {
		return Section{}, err
	}
	var sec *ent.TenderSection
	err = txn.Run(ctx, s.client, func(tx *ent.Tx) error {
		t, err := lockTender(ctx, tx, tenantID, tenderID)
		if err != nil {
			return err
		}
		if err := sectionsOpen(t); err != nil {
			return err
		}
		n, err := tx.TenderSection.Query().Where(enttendersection.TenantID(tenantID), enttendersection.TenderID(tenderID)).Count(ctx)
		if err != nil {
			return fmt.Errorf("count sections: %w", err)
		}
		if n >= maxSections {
			return ErrValidation(fmt.Sprintf("a tender has at most %d sections", maxSections))
		}
		if doc != nil {
			if err := documentOf(ctx, tx, tenantID, tenderID, *doc); err != nil {
				return err
			}
		}
		pos := n
		if in.Position != nil {
			pos = *in.Position
		}
		c := tx.TenderSection.Create().SetTenantID(tenantID).SetTenderID(tenderID).SetTitle(strings.TrimSpace(*in.Title)).
			SetPosition(pos).SetStatus(SectionNotStarted).SetNillableAssigneeID(assignee).SetNillableReviewerID(reviewer).
			SetNillableDocumentID(doc)
		if actor := activity.ActorFrom(ctx); actor != uuid.Nil {
			c.SetCreatedBy(actor)
		}
		if in.Description != nil {
			c.SetDescription(*in.Description)
		}
		if in.DueDate != nil && !in.DueDate.IsZero() {
			c.SetDueDate(in.DueDate.Time)
		}
		if sec, err = c.Save(ctx); err != nil {
			return fmt.Errorf("create section: %w", err)
		}
		changes := map[string]any{"section_added": sec.Title}
		if assignee != nil {
			changes["assignee_id"] = assignee.String()
		}
		return sectionRecord(ctx, tx, sec, activity.ActionUpdated, changes)
	})
	if err != nil {
		return Section{}, err
	}
	out, err := s.named(ctx, tenantID, []*ent.TenderSection{sec})
	if err != nil {
		return Section{}, err
	}
	return out[0], nil
}

// lockSection locks one section of the tender for the rest of tx and loads it.
func lockSection(ctx context.Context, tx *ent.Tx, tenantID, tenderID, id uuid.UUID) (*ent.TenderSection, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id FROM tender_sections WHERE id = $1 AND tenant_id = $2 AND tender_id = $3 FOR UPDATE`,
		id, tenantID, tenderID)
	if err != nil {
		return nil, fmt.Errorf("lock section: %w", err)
	}
	found := rows.Next()
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if !found {
		return nil, ErrNotFound
	}
	return tx.TenderSection.Get(ctx, id)
}

// UpdateSection edits a section. A tender manager edits everything (status only between
// not_started and in_progress); the assignee may only move their own section between not_started
// and in_progress and set its draft document.
func (s *Service) UpdateSection(ctx context.Context, tenantID, tenderID, id uuid.UUID, in SectionInput, canManage bool) (Section, error) {
	actor := activity.ActorFrom(ctx)
	assignee, setAssignee, err := optionalUUID(in.AssigneeID, "assignee_id")
	if err != nil {
		return Section{}, err
	}
	reviewer, setReviewer, err := optionalUUID(in.ReviewerID, "reviewer_id")
	if err != nil {
		return Section{}, err
	}
	doc, setDoc, err := optionalUUID(in.DocumentID, "document_id")
	if err != nil {
		return Section{}, err
	}
	if in.Status != nil && *in.Status != SectionNotStarted && *in.Status != SectionInProgress {
		return Section{}, ErrValidation("status moves here only between not_started and in_progress; submit, approve and request-changes handle review")
	}
	if in.Title != nil && (strings.TrimSpace(*in.Title) == "" || len([]rune(*in.Title)) > 200) {
		return Section{}, ErrValidation("title is required (at most 200 characters)")
	}
	var sec *ent.TenderSection
	err = txn.Run(ctx, s.client, func(tx *ent.Tx) error {
		t, err := lockTender(ctx, tx, tenantID, tenderID)
		if err != nil {
			return err
		}
		if err := sectionsOpen(t); err != nil {
			return err
		}
		cur, err := lockSection(ctx, tx, tenantID, tenderID, id)
		if err != nil {
			return err
		}
		mine := cur.AssigneeID != nil && *cur.AssigneeID == actor
		if !canManage {
			if !mine {
				return ErrNotYours
			}
			if in.Title != nil || in.Description != nil || in.Position != nil || setAssignee || setReviewer || in.DueDate != nil {
				return ErrNotYours
			}
		}
		if in.Status != nil && (cur.Status == SectionInReview || cur.Status == SectionApproved) {
			return stateErr("section_in_review", "a section in review or approved changes status through review")
		}
		newAssignee, newReviewer := cur.AssigneeID, cur.ReviewerID
		if setAssignee {
			newAssignee = assignee
		}
		if setReviewer {
			newReviewer = reviewer
		}
		if newAssignee != nil && newReviewer != nil && *newAssignee == *newReviewer {
			return ErrValidation("the reviewer must be someone other than the assignee")
		}
		changes := map[string]any{}
		u := cur.Update()
		if in.Title != nil {
			u.SetTitle(strings.TrimSpace(*in.Title))
			changes["title"] = strings.TrimSpace(*in.Title)
		}
		if in.Description != nil {
			u.SetDescription(*in.Description)
			changes["description"] = "changed"
		}
		if in.Position != nil {
			u.SetPosition(*in.Position)
		}
		if setAssignee {
			u.SetNillableAssigneeID(assignee)
			if assignee == nil {
				u.ClearAssigneeID()
			}
			changes["assignee_id"] = strings.TrimSpace(*in.AssigneeID)
		}
		if setReviewer {
			u.SetNillableReviewerID(reviewer)
			if reviewer == nil {
				u.ClearReviewerID()
			}
			changes["reviewer_id"] = strings.TrimSpace(*in.ReviewerID)
		}
		if in.DueDate != nil {
			if in.DueDate.IsZero() {
				u.ClearDueDate()
			} else {
				u.SetDueDate(in.DueDate.Time)
			}
			changes["due_date"] = "changed"
		}
		if setDoc {
			if doc == nil {
				u.ClearDocumentID()
			} else {
				if err := documentOf(ctx, tx, tenantID, tenderID, *doc); err != nil {
					return err
				}
				u.SetDocumentID(*doc)
			}
			changes["document"] = "changed"
		}
		if in.Status != nil && *in.Status != cur.Status {
			u.SetStatus(*in.Status)
			changes["status"] = map[string]any{"from": cur.Status, "to": *in.Status}
		}
		if sec, err = u.Save(ctx); err != nil {
			return fmt.Errorf("update section: %w", err)
		}
		return sectionRecord(ctx, tx, sec, activity.ActionUpdated, changes)
	})
	if err != nil {
		return Section{}, err
	}
	out, err := s.named(ctx, tenantID, []*ent.TenderSection{sec})
	if err != nil {
		return Section{}, err
	}
	return out[0], nil
}

// DeleteSection removes a section (tender managers).
func (s *Service) DeleteSection(ctx context.Context, tenantID, tenderID, id uuid.UUID) error {
	return txn.Run(ctx, s.client, func(tx *ent.Tx) error {
		t, err := lockTender(ctx, tx, tenantID, tenderID)
		if err != nil {
			return err
		}
		if err := sectionsOpen(t); err != nil {
			return err
		}
		cur, err := lockSection(ctx, tx, tenantID, tenderID, id)
		if err != nil {
			return err
		}
		if err := tx.TenderSection.DeleteOne(cur).Exec(ctx); err != nil {
			return fmt.Errorf("delete section: %w", err)
		}
		return sectionRecord(ctx, tx, cur, activity.ActionUpdated, map[string]any{"section_removed": cur.Title})
	})
}

// SubmitSection sends a section for review: the assignee, or a tender manager. It needs a
// reviewer or at least one tender manager to review it; the reviewer is told through the
// tender.section.review_requested event.
func (s *Service) SubmitSection(ctx context.Context, tenantID, tenderID, id uuid.UUID, canManage bool) (Section, error) {
	actor := activity.ActorFrom(ctx)
	var sec *ent.TenderSection
	err := txn.Run(ctx, s.client, func(tx *ent.Tx) error {
		t, err := lockTender(ctx, tx, tenantID, tenderID)
		if err != nil {
			return err
		}
		if err := sectionsOpen(t); err != nil {
			return err
		}
		cur, err := lockSection(ctx, tx, tenantID, tenderID, id)
		if err != nil {
			return err
		}
		if !canManage && (cur.AssigneeID == nil || *cur.AssigneeID != actor) {
			return ErrNotYours
		}
		if !slices.Contains([]string{SectionNotStarted, SectionInProgress, SectionChangesRequested}, cur.Status) {
			return stateErr("section_not_open", "a section "+cur.Status+" cannot be submitted for review")
		}
		now := time.Now().UTC()
		if sec, err = cur.Update().SetStatus(SectionInReview).SetSubmittedAt(now).Save(ctx); err != nil {
			return fmt.Errorf("submit section: %w", err)
		}
		if err := sectionRecord(ctx, tx, sec, activity.ActionUpdated, map[string]any{"status": map[string]any{"from": cur.Status, "to": SectionInReview}}); err != nil {
			return err
		}
		extra := map[string]any{"section_id": sec.ID.String(), "section": sec.Title, "submitted_by": actor.String()}
		if sec.ReviewerID != nil {
			extra["reviewer_id"] = sec.ReviewerID.String()
		}
		return emit(ctx, tx, t, EventSectionReview, extra)
	})
	if err != nil {
		return Section{}, err
	}
	out, err := s.named(ctx, tenantID, []*ent.TenderSection{sec})
	if err != nil {
		return Section{}, err
	}
	return out[0], nil
}

// ReviewSection approves a section in review or asks for changes (comment required) as the
// signed-in user: its reviewer, or a tender manager, never its assignee.
func (s *Service) ReviewSection(ctx context.Context, tenantID, tenderID, id uuid.UUID, approve bool, comment string, canManage bool) (Section, error) {
	actor := activity.ActorFrom(ctx)
	comment = strings.TrimSpace(comment)
	if !approve && comment == "" {
		return Section{}, ErrValidation("say what needs to change")
	}
	if len([]rune(comment)) > 2000 {
		return Section{}, ErrValidation("the comment is at most 2000 characters")
	}
	var sec *ent.TenderSection
	err := txn.Run(ctx, s.client, func(tx *ent.Tx) error {
		t, err := lockTender(ctx, tx, tenantID, tenderID)
		if err != nil {
			return err
		}
		if err := sectionsOpen(t); err != nil {
			return err
		}
		cur, err := lockSection(ctx, tx, tenantID, tenderID, id)
		if err != nil {
			return err
		}
		if actor == uuid.Nil || (cur.AssigneeID != nil && *cur.AssigneeID == actor) {
			return ErrOwnSection
		}
		if !canManage && (cur.ReviewerID == nil || *cur.ReviewerID != actor) {
			return ErrNotReviewer
		}
		if cur.Status != SectionInReview {
			return stateErr("section_not_in_review", "only a section in review can be reviewed")
		}
		status := SectionChangesRequested
		if approve {
			status = SectionApproved
		}
		if sec, err = cur.Update().SetStatus(status).SetReviewedBy(actor).SetReviewedAt(time.Now().UTC()).
			SetReviewComment(comment).Save(ctx); err != nil {
			return fmt.Errorf("review section: %w", err)
		}
		if err := sectionRecord(ctx, tx, sec, activity.ActionUpdated, map[string]any{
			"status": map[string]any{"from": cur.Status, "to": status}, "comment": comment,
		}); err != nil {
			return err
		}
		extra := map[string]any{"section_id": sec.ID.String(), "section": sec.Title, "decision": status, "reviewed_by": actor.String(), "comment": comment}
		if sec.AssigneeID != nil {
			extra["assignee_id"] = sec.AssigneeID.String()
		}
		return emit(ctx, tx, t, EventSectionReviewed, extra)
	})
	if err != nil {
		return Section{}, err
	}
	out, err := s.named(ctx, tenantID, []*ent.TenderSection{sec})
	if err != nil {
		return Section{}, err
	}
	return out[0], nil
}
