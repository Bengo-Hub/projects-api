package tenders

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	entsql "entgo.io/ent/dialect/sql"
	"github.com/google/uuid"
	"go.uber.org/zap"

	sharedcache "github.com/Bengo-Hub/cache"
	"github.com/bengobox/projects-service/internal/ent"
	"github.com/bengobox/projects-service/internal/ent/predicate"
	entproject "github.com/bengobox/projects-service/internal/ent/project"
	enttender "github.com/bengobox/projects-service/internal/ent/tender"
	enttendercommittee "github.com/bengobox/projects-service/internal/ent/tendercommittee"
	enttendercommitteemember "github.com/bengobox/projects-service/internal/ent/tendercommitteemember"
	enttenderdoc "github.com/bengobox/projects-service/internal/ent/tenderdocument"
	enttendereval "github.com/bengobox/projects-service/internal/ent/tenderevaluation"
	enttendermeeting "github.com/bengobox/projects-service/internal/ent/tendermeeting"
	enttendersection "github.com/bengobox/projects-service/internal/ent/tendersection"
	enttendersubmission "github.com/bengobox/projects-service/internal/ent/tendersubmission"
	"github.com/bengobox/projects-service/internal/platform/treasury"
	"github.com/bengobox/projects-service/internal/services/activity"
	"github.com/bengobox/projects-service/internal/services/people"
	"github.com/bengobox/projects-service/internal/services/sequence"
	"github.com/bengobox/projects-service/internal/services/tasks"
	"github.com/bengobox/projects-service/internal/services/txn"
)

// Service handles tender management operations.
type Service struct {
	client *ent.Client
	cache  *sharedcache.Aside
	log    *zap.Logger
	// store and limits keep tender documents (WithStorage); templates and treasury serve the
	// conversion to a project (WithConversion).
	store     Store
	limits    Limits
	templates TemplateSource
	treasury  *treasury.Client
}

// tenderOf selects one tender of the tenant.
func tenderOf(tenantID, id uuid.UUID) []predicate.Tender {
	return []predicate.Tender{enttender.ID(id), enttender.TenantID(tenantID)}
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
	Deadline       *tasks.Date     `json:"deadline"`
	Description    string         `json:"description"`
	SubmissionType string         `json:"submission_type"`
	Metadata       map[string]any `json:"metadata"`
	CreatedBy      uuid.UUID      `json:"-"`
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
	Deadline       *tasks.Date     `json:"deadline"`
	Description    *string        `json:"description"`
	SubmissionType *string        `json:"submission_type"`
	Metadata       map[string]any `json:"metadata"`
}

// ListTendersFilter holds filter options for listing tenders. Limit and Offset come from the
// shared pagination parser. Q searches the title, client and number; Sort is deadline,
// estimated_value or created_at, with a leading "-" for descending (default -created_at).
type ListTendersFilter struct {
	Status         string
	Priority       string
	Q              string
	Sort           string
	DeadlineBefore *time.Time
	DeadlineAfter  *time.Time
	Limit          int
	Offset         int
}

// SortOrders are the list orders ListTenders accepts.
var SortOrders = []string{"deadline", "-deadline", "estimated_value", "-estimated_value", "created_at", "-created_at"}

// tenderOrder is the ORDER BY of a sort, always ending on the id so pages never overlap. Missing
// deadlines and values sort last either way.
func tenderOrder(sort string) []enttender.OrderOption {
	desc := strings.HasPrefix(sort, "-")
	field := strings.TrimPrefix(sort, "-")
	opts := []entsql.OrderTermOption{entsql.OrderNullsLast()}
	if desc {
		opts = append(opts, entsql.OrderDesc())
	}
	switch field {
	case "deadline":
		return []enttender.OrderOption{enttender.ByDeadline(opts...), enttender.ByID()}
	case "estimated_value":
		return []enttender.OrderOption{enttender.ByEstimatedValue(opts...), enttender.ByID()}
	case "created_at":
		if !desc {
			return []enttender.OrderOption{enttender.ByCreatedAt(), enttender.ByID()}
		}
	}
	return []enttender.OrderOption{enttender.ByCreatedAt(entsql.OrderDesc()), enttender.ByID(entsql.OrderDesc())}
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
	Title       string    `json:"title"`
	ScheduledAt time.Time `json:"scheduled_at"`
	Platform    string    `json:"platform"`
	MeetingURL  string    `json:"meeting_url"`
	Notes       string    `json:"notes"`
	CreatedBy   uuid.UUID `json:"-"`
}

// TenderNumber formats a tender number: TND-{year}-{seq, at least 4 digits}.
func TenderNumber(year int, seq int64) string { return fmt.Sprintf("TND-%d-%04d", year, seq) }

func record(ctx context.Context, tx *ent.Tx, tenantID, tenderID uuid.UUID, action string, changes map[string]any) error {
	return activity.Record(ctx, tx, activity.Entry{
		TenantID: tenantID, EntityType: "tender", EntityID: tenderID, Action: action, Changes: changes,
	})
}

// CreateTender creates a new tender numbered from the tenant's tender sequence, in one transaction.
func (s *Service) CreateTender(ctx context.Context, tenantID uuid.UUID, input CreateTenderInput) (*ent.Tender, error) {
	input.Title, input.ClientName = strings.TrimSpace(input.Title), strings.TrimSpace(input.ClientName)
	if input.Title == "" || len([]rune(input.Title)) > 300 {
		return nil, ErrValidation("title is required (at most 300 characters)")
	}
	if input.ClientName == "" || len([]rune(input.ClientName)) > 200 {
		return nil, ErrValidation("client_name is required (at most 200 characters)")
	}
	status := input.Status
	if status == "" {
		status = StatusDraft
	}
	if !validCreateStatus(status) {
		return nil, ErrValidation("a new tender starts as draft or evaluating")
	}
	priority := input.Priority
	if priority == "" {
		priority = "medium"
	}
	if !slices.Contains(Priorities, priority) {
		return nil, ErrValidation("priority must be low, medium, high or critical")
	}
	if input.EstimatedValue != nil && *input.EstimatedValue < 0 {
		return nil, ErrValidation("estimated_value must be zero or more")
	}
	if err := normalizeInputMeta(input.Metadata); err != nil {
		return nil, err
	}
	if input.Metadata != nil {
		input.Metadata = mergeMeta(nil, input.Metadata)
	}
	currency := input.Currency
	if currency == "" {
		currency = "KES"
	}
	submType := input.SubmissionType
	if submType == "" {
		submType = "physical"
	}
	var t *ent.Tender
	err := txn.Run(ctx, s.client, func(tx *ent.Tx) error {
		seq, err := sequence.Next(ctx, tx, tenantID, sequence.KindTender)
		if err != nil {
			return err
		}
		c := tx.Tender.Create().
			SetTenantID(tenantID).
			SetNumber(TenderNumber(time.Now().Year(), seq)).
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
		if input.Deadline != nil && !input.Deadline.IsZero() {
			c = c.SetDeadline(input.Deadline.Time)
		}
		if input.Description != "" {
			c = c.SetDescription(input.Description)
		}
		if input.Metadata != nil {
			c = c.SetMetadata(input.Metadata)
		}
		if t, err = c.Save(ctx); err != nil {
			return fmt.Errorf("create tender: %w", err)
		}
		return record(ctx, tx, tenantID, t.ID, activity.ActionCreated, map[string]any{"number": t.Number, "title": t.Title})
	})
	if err != nil {
		return nil, err
	}
	s.log.Info("tender created", zap.String("id", t.ID.String()), zap.String("number", t.Number))
	return t, nil
}

// ListTenders returns one page of the tenant's tenders with the total.
func (s *Service) ListTenders(ctx context.Context, tenantID uuid.UUID, filter ListTendersFilter) ([]*ent.Tender, int, error) {
	if filter.Limit <= 0 {
		filter.Limit = 20
	}
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
	if term := strings.TrimSpace(filter.Q); term != "" {
		q = q.Where(enttender.Or(enttender.TitleContainsFold(term), enttender.ClientNameContainsFold(term), enttender.NumberContainsFold(term)))
	}
	total, err := q.Clone().Count(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("count tenders: %w", err)
	}
	items, err := q.Order(tenderOrder(filter.Sort)...).
		Offset(filter.Offset).Limit(filter.Limit).All(ctx)
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

// UpdateTender updates an existing tender. A status change here is limited to the manual moves
// (CanMove); the decision, submit and outcome routes make the others. Metadata is a partial
// update (null removes a key) and never touches the server-owned keys.
func (s *Service) UpdateTender(ctx context.Context, tenantID, id uuid.UUID, input UpdateTenderInput) (*ent.Tender, error) {
	if input.Title != nil {
		*input.Title = strings.TrimSpace(*input.Title)
		if *input.Title == "" || len([]rune(*input.Title)) > 300 {
			return nil, ErrValidation("title is required (at most 300 characters)")
		}
	}
	if input.ClientName != nil {
		*input.ClientName = strings.TrimSpace(*input.ClientName)
		if *input.ClientName == "" || len([]rune(*input.ClientName)) > 200 {
			return nil, ErrValidation("client_name is required (at most 200 characters)")
		}
	}
	if input.Priority != nil && !slices.Contains(Priorities, *input.Priority) {
		return nil, ErrValidation("priority must be low, medium, high or critical")
	}
	if input.EstimatedValue != nil && *input.EstimatedValue < 0 {
		return nil, ErrValidation("estimated_value must be zero or more")
	}
	if err := normalizeInputMeta(input.Metadata); err != nil {
		return nil, err
	}
	var updated *ent.Tender
	err := txn.Run(ctx, s.client, func(tx *ent.Tx) error {
		t, err := lockTender(ctx, tx, tenantID, id)
		if err != nil {
			return err
		}
		changes := map[string]any{}
		u := t.Update()
		if input.Title != nil {
			u = u.SetTitle(*input.Title)
			changes["title"] = *input.Title
		}
		if input.ClientName != nil {
			u = u.SetClientName(*input.ClientName)
			changes["client_name"] = *input.ClientName
		}
		if input.Source != nil {
			u = u.SetSource(*input.Source)
			changes["source"] = *input.Source
		}
		prevStatus := t.Status
		statusChanged := input.Status != nil && *input.Status != t.Status
		if statusChanged {
			if !CanMove(t.Status, *input.Status) {
				return stateErr("tender_status_move", fmt.Sprintf("a %s tender cannot be moved to %s here: use the decision, submit or outcome step", t.Status, *input.Status))
			}
			u = u.SetStatus(*input.Status)
			changes["status"] = map[string]any{"from": t.Status, "to": *input.Status}
		}
		if input.Priority != nil {
			u = u.SetPriority(*input.Priority)
			changes["priority"] = *input.Priority
		}
		if input.EstimatedValue != nil {
			u = u.SetEstimatedValue(*input.EstimatedValue)
			changes["estimated_value"] = *input.EstimatedValue
		}
		if input.Currency != nil {
			u = u.SetCurrency(*input.Currency)
			changes["currency"] = *input.Currency
		}
		if input.Deadline != nil {
			if input.Deadline.IsZero() {
				u = u.ClearDeadline()
				changes["deadline"] = nil
			} else {
				u = u.SetDeadline(input.Deadline.Time)
				changes["deadline"] = input.Deadline.Format(time.RFC3339)
			}
		}
		if input.Description != nil {
			u = u.SetDescription(*input.Description)
			changes["description"] = "changed"
		}
		if input.SubmissionType != nil {
			u = u.SetSubmissionType(*input.SubmissionType)
			changes["submission_type"] = *input.SubmissionType
		}
		if input.Metadata != nil {
			u = u.SetMetadata(mergeMeta(t.Metadata, input.Metadata))
			changes["metadata"] = "changed"
		}
		if updated, err = u.Save(ctx); err != nil {
			return fmt.Errorf("update tender: %w", err)
		}
		if err := record(ctx, tx, tenantID, id, activity.ActionUpdated, changes); err != nil {
			return err
		}
		if statusChanged {
			return emit(ctx, tx, updated, EventStatusChanged, map[string]any{"previous_status": prevStatus})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return updated, nil
}

// DeleteTender deletes a tender with its committees, evaluations, meetings, sections, submissions
// and documents. A converted tender stays (its project links its documents): 409 tender_converted.
// Stored files go after the commit unless a project attachment still links them.
func (s *Service) DeleteTender(ctx context.Context, tenantID, id uuid.UUID) error {
	var files []string
	err := txn.Run(ctx, s.client, func(tx *ent.Tx) error {
		t, err := lockTender(ctx, tx, tenantID, id)
		if err != nil {
			return err
		}
		if pid := ConvertedProjectID(t.Metadata); pid != uuid.Nil {
			exists, err := tx.Project.Query().Where(entproject.ID(pid), entproject.TenantID(tenantID)).Exist(ctx)
			if err != nil {
				return fmt.Errorf("check converted project: %w", err)
			}
			if exists {
				return stateErr("tender_converted", "this tender became a project, so it stays as its record")
			}
		}
		if _, err := tx.TenderSection.Delete().Where(enttendersection.TenantID(tenantID), enttendersection.TenderID(id)).Exec(ctx); err != nil {
			return fmt.Errorf("delete tender sections: %w", err)
		}
		if _, err := tx.TenderSubmission.Delete().Where(enttendersubmission.TenantID(tenantID), enttendersubmission.TenderID(id)).Exec(ctx); err != nil {
			return fmt.Errorf("delete tender submissions: %w", err)
		}
		if files, err = tx.TenderDocument.Query().Where(enttenderdoc.TenderID(id)).Select(enttenderdoc.FieldFileURL).Strings(ctx); err != nil {
			return fmt.Errorf("list tender files: %w", err)
		}
		committees, err := tx.TenderCommittee.Query().Where(enttendercommittee.TenderID(id)).IDs(ctx)
		if err != nil {
			return err
		}
		if len(committees) > 0 {
			if _, err := tx.TenderCommitteeMember.Delete().Where(enttendercommitteemember.CommitteeIDIn(committees...)).Exec(ctx); err != nil {
				return err
			}
		}
		if _, err := tx.TenderCommittee.Delete().Where(enttendercommittee.TenderID(id)).Exec(ctx); err != nil {
			return fmt.Errorf("delete tender committees: %w", err)
		}
		if _, err := tx.TenderEvaluation.Delete().Where(enttendereval.TenderID(id)).Exec(ctx); err != nil {
			return fmt.Errorf("delete tender evaluations: %w", err)
		}
		if _, err := tx.TenderMeeting.Delete().Where(enttendermeeting.TenderID(id)).Exec(ctx); err != nil {
			return fmt.Errorf("delete tender meetings: %w", err)
		}
		if _, err := tx.TenderDocument.Delete().Where(enttenderdoc.TenderID(id)).Exec(ctx); err != nil {
			return fmt.Errorf("delete tender documents: %w", err)
		}
		if err := tx.Tender.DeleteOne(t).Exec(ctx); err != nil {
			return fmt.Errorf("delete tender: %w", err)
		}
		return record(ctx, tx, tenantID, id, activity.ActionDeleted, map[string]any{"number": t.Number})
	})
	if err != nil {
		return err
	}
	s.removeStored(ctx, tenantID, files)
	return nil
}

// tenderStatuses are always reported (zero when a tenant has none) so the UI cards never read a
// missing key.
var tenderStatuses = Statuses

// StatusMetric is one status bucket of the tender pipeline.
type StatusMetric struct {
	Count int     `json:"count"`
	Value float64 `json:"value"`
}

// GetTenderMetrics returns the tender pipeline by status in ONE grouped query: count and total
// estimated value per status, the overall totals, and the win rate (awarded out of decided,
// i.e. awarded + lost).
func (s *Service) GetTenderMetrics(ctx context.Context, tenantID uuid.UUID) (map[string]any, error) {
	var rows []struct {
		Status string  `json:"status"`
		Count  int     `json:"count"`
		Value  float64 `json:"value"`
	}
	err := s.client.Tender.Query().
		Where(enttender.TenantID(tenantID)).
		GroupBy(enttender.FieldStatus).
		Aggregate(
			ent.Count(),
			func(sel *entsql.Selector) string {
				return entsql.As(fmt.Sprintf("COALESCE(SUM(%s), 0)", sel.C(enttender.FieldEstimatedValue)), "value")
			},
		).
		Scan(ctx, &rows)
	if err != nil {
		return nil, fmt.Errorf("tender metrics: %w", err)
	}
	byStatus := make(map[string]StatusMetric, len(tenderStatuses))
	for _, st := range tenderStatuses {
		byStatus[st] = StatusMetric{}
	}
	total, totalValue := 0, 0.0
	for _, r := range rows {
		st := r.Status
		if !IsKnownStatus(st) {
			st = StatusDraft // a status from before the current vocabulary counts as draft
		}
		cur := byStatus[st]
		byStatus[st] = StatusMetric{Count: cur.Count + r.Count, Value: cur.Value + r.Value}
		total += r.Count
		totalValue += r.Value
	}
	winRate := 0.0
	if decided := byStatus["awarded"].Count + byStatus["lost"].Count; decided > 0 {
		winRate = float64(byStatus["awarded"].Count) / float64(decided) * 100
	}
	pipeline := 0.0
	for _, st := range openStatuses {
		pipeline += byStatus[st].Value
	}
	return map[string]any{
		"total":                 total,
		"total_estimated_value": totalValue,
		"by_status":             byStatus,
		"win_rate":              winRate,
		"awarded_value":         byStatus["awarded"].Value,
		"pipeline_value":        pipeline,
	}, nil
}

// onTender runs fn in a transaction after checking the tender belongs to the tenant, then records
// one tender activity entry with changes.
func (s *Service) onTender(ctx context.Context, tenantID, tenderID uuid.UUID, fn func(tx *ent.Tx) (map[string]any, error)) error {
	return txn.Run(ctx, s.client, func(tx *ent.Tx) error {
		ok, err := tx.Tender.Query().Where(enttender.ID(tenderID), enttender.TenantID(tenantID)).Exist(ctx)
		if err != nil {
			return fmt.Errorf("fetch tender: %w", err)
		}
		if !ok {
			return ErrNotFound
		}
		changes, err := fn(tx)
		if err != nil {
			return err
		}
		return record(ctx, tx, tenantID, tenderID, activity.ActionUpdated, changes)
	})
}

// committeeOf checks the committee belongs to the tender and tenant.
func committeeOf(ctx context.Context, tx *ent.Tx, tenantID, tenderID, committeeID uuid.UUID) error {
	ok, err := tx.TenderCommittee.Query().Where(enttendercommittee.ID(committeeID), enttendercommittee.TenderID(tenderID),
		enttendercommittee.TenantID(tenantID)).Exist(ctx)
	if err != nil {
		return fmt.Errorf("fetch committee: %w", err)
	}
	if !ok {
		return ErrNotFound
	}
	return nil
}

// --- Committee operations ---

// CreateCommittee creates a new committee for a tender.
func (s *Service) CreateCommittee(ctx context.Context, tenantID, tenderID uuid.UUID, input CreateCommitteeInput) (*ent.TenderCommittee, error) {
	if input.Name == "" {
		return nil, ErrValidation("name is required")
	}
	var c *ent.TenderCommittee
	err := s.onTender(ctx, tenantID, tenderID, func(tx *ent.Tx) (map[string]any, error) {
		var err error
		c, err = tx.TenderCommittee.Create().SetTenderID(tenderID).SetTenantID(tenantID).SetName(input.Name).Save(ctx)
		if err != nil {
			return nil, fmt.Errorf("create committee: %w", err)
		}
		return map[string]any{"committee_added": c.Name}, nil
	})
	if err != nil {
		return nil, err
	}
	return c, nil
}

// CommitteeMember is a committee member row with the person's name and email from projects_users
// (empty for someone who has never signed in to projects), so a reader never needs the people
// search to tell who sits on a committee.
type CommitteeMember struct {
	*ent.TenderCommitteeMember
	UserName string `json:"user_name"`
	Email    string `json:"email"`
}

// CommitteeEdges replaces the ent edges in the committee list: the members with their names.
type CommitteeEdges struct {
	Members []CommitteeMember `json:"members,omitempty"`
}

// Committee is a committee as the list answers it: every ent field as before, and edges.members
// carrying user_name and email beside the existing member fields.
type Committee struct {
	*ent.TenderCommittee
	Edges CommitteeEdges `json:"edges"`
}

// ListCommittees lists committees for a tender with their members. The names and emails of every
// member of every committee come from projects_users in one batched lookup, never one per row.
func (s *Service) ListCommittees(ctx context.Context, tenantID, tenderID uuid.UUID) ([]Committee, error) {
	items, err := s.client.TenderCommittee.Query().
		Where(enttendercommittee.TenderID(tenderID), enttendercommittee.TenantID(tenantID)).
		WithMembers().
		Limit(subListLimit).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list committees: %w", err)
	}
	var ids []uuid.UUID
	for _, c := range items {
		for _, m := range c.Edges.Members {
			ids = append(ids, m.UserID)
		}
	}
	known, err := people.Lookup(ctx, s.client.ProjectsUser, tenantID, ids)
	if err != nil {
		return nil, err
	}
	out := make([]Committee, len(items))
	for i, c := range items {
		members := make([]CommitteeMember, len(c.Edges.Members))
		for j, m := range c.Edges.Members {
			p := known[m.UserID]
			members[j] = CommitteeMember{TenderCommitteeMember: m, UserName: p.Name, Email: p.Email}
		}
		out[i] = Committee{TenderCommittee: c, Edges: CommitteeEdges{Members: members}}
	}
	return out, nil
}

// AddCommitteeMember adds a user to a committee of the tender.
func (s *Service) AddCommitteeMember(ctx context.Context, tenantID, tenderID, committeeID uuid.UUID, input AddCommitteeMemberInput) (*ent.TenderCommitteeMember, error) {
	if input.UserID == uuid.Nil {
		return nil, ErrValidation("user_id is required")
	}
	role := input.Role
	if role == "" {
		role = "member"
	}
	var m *ent.TenderCommitteeMember
	err := s.onTender(ctx, tenantID, tenderID, func(tx *ent.Tx) (map[string]any, error) {
		if err := committeeOf(ctx, tx, tenantID, tenderID, committeeID); err != nil {
			return nil, err
		}
		var err error
		m, err = tx.TenderCommitteeMember.Create().SetCommitteeID(committeeID).SetTenantID(tenantID).
			SetUserID(input.UserID).SetRole(role).Save(ctx)
		if err != nil {
			return nil, fmt.Errorf("add committee member: %w", err)
		}
		return map[string]any{"committee_member_added": input.UserID.String(), "role": role}, nil
	})
	if err != nil {
		return nil, err
	}
	return m, nil
}

// RemoveCommitteeMember removes a user from a committee of the tender.
func (s *Service) RemoveCommitteeMember(ctx context.Context, tenantID, tenderID, committeeID, userID uuid.UUID) error {
	return s.onTender(ctx, tenantID, tenderID, func(tx *ent.Tx) (map[string]any, error) {
		if err := committeeOf(ctx, tx, tenantID, tenderID, committeeID); err != nil {
			return nil, err
		}
		n, err := tx.TenderCommitteeMember.Delete().
			Where(enttendercommitteemember.CommitteeID(committeeID), enttendercommitteemember.TenantID(tenantID),
				enttendercommitteemember.UserID(userID)).Exec(ctx)
		if err != nil {
			return nil, fmt.Errorf("remove committee member: %w", err)
		}
		if n == 0 {
			return nil, ErrNotFound
		}
		return map[string]any{"committee_member_removed": userID.String()}, nil
	})
}

// --- Evaluation operations ---

// SubmitEvaluation submits an evaluation for a tender.
func (s *Service) SubmitEvaluation(ctx context.Context, tenantID, tenderID uuid.UUID, input SubmitEvaluationInput) (*ent.TenderEvaluation, error) {
	var e *ent.TenderEvaluation
	err := s.onTender(ctx, tenantID, tenderID, func(tx *ent.Tx) (map[string]any, error) {
		c := tx.TenderEvaluation.Create().SetTenderID(tenderID).SetTenantID(tenantID).
			SetEvaluatorID(input.EvaluatorID).SetScore(input.Score)
		if input.Notes != "" {
			c = c.SetNotes(input.Notes)
		}
		if input.Criteria != "" {
			c = c.SetCriteria(input.Criteria)
		}
		var err error
		if e, err = c.Save(ctx); err != nil {
			return nil, fmt.Errorf("submit evaluation: %w", err)
		}
		return map[string]any{"evaluation_added": e.ID.String(), "score": e.Score}, nil
	})
	if err != nil {
		return nil, err
	}
	return e, nil
}

// ListEvaluations lists evaluations for a tender.
func (s *Service) ListEvaluations(ctx context.Context, tenantID, tenderID uuid.UUID) ([]*ent.TenderEvaluation, error) {
	items, err := s.client.TenderEvaluation.Query().
		Where(enttendereval.TenderID(tenderID), enttendereval.TenantID(tenantID)).
		Order(ent.Desc(enttendereval.FieldEvaluatedAt)).
		Limit(subListLimit).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list evaluations: %w", err)
	}
	return items, nil
}

// --- Meeting operations ---

// ScheduleMeeting schedules a meeting for a tender.
func (s *Service) ScheduleMeeting(ctx context.Context, tenantID, tenderID uuid.UUID, input ScheduleMeetingInput) (*ent.TenderMeeting, error) {
	if input.Title == "" {
		return nil, ErrValidation("title is required")
	}
	platform := input.Platform
	if platform == "" {
		platform = "physical"
	}
	var m *ent.TenderMeeting
	err := s.onTender(ctx, tenantID, tenderID, func(tx *ent.Tx) (map[string]any, error) {
		c := tx.TenderMeeting.Create().SetTenderID(tenderID).SetTenantID(tenantID).SetTitle(input.Title).
			SetScheduledAt(input.ScheduledAt).SetPlatform(platform).SetCreatedBy(input.CreatedBy)
		if input.MeetingURL != "" {
			c = c.SetMeetingURL(input.MeetingURL)
		}
		if input.Notes != "" {
			c = c.SetNotes(input.Notes)
		}
		var err error
		if m, err = c.Save(ctx); err != nil {
			return nil, fmt.Errorf("schedule meeting: %w", err)
		}
		return map[string]any{"meeting_scheduled": m.Title, "scheduled_at": m.ScheduledAt.Format(time.RFC3339)}, nil
	})
	if err != nil {
		return nil, err
	}
	return m, nil
}

// ListMeetings lists meetings for a tender.
func (s *Service) ListMeetings(ctx context.Context, tenantID, tenderID uuid.UUID) ([]*ent.TenderMeeting, error) {
	items, err := s.client.TenderMeeting.Query().
		Where(enttendermeeting.TenderID(tenderID), enttendermeeting.TenantID(tenantID)).
		Order(ent.Asc(enttendermeeting.FieldScheduledAt)).
		Limit(subListLimit).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list meetings: %w", err)
	}
	return items, nil
}
