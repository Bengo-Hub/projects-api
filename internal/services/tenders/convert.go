package tenders

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/projects-service/internal/ent"
	entproject "github.com/bengobox/projects-service/internal/ent/project"
	enttendercommittee "github.com/bengobox/projects-service/internal/ent/tendercommittee"
	enttendercommitteemember "github.com/bengobox/projects-service/internal/ent/tendercommitteemember"
	enttenderdoc "github.com/bengobox/projects-service/internal/ent/tenderdocument"
	"github.com/bengobox/projects-service/internal/platform/treasury"
	"github.com/bengobox/projects-service/internal/services/activity"
	"github.com/bengobox/projects-service/internal/services/attachments"
	"github.com/bengobox/projects-service/internal/services/planning"
	"github.com/bengobox/projects-service/internal/services/projects"
	"github.com/bengobox/projects-service/internal/services/tasks"
	"github.com/bengobox/projects-service/internal/services/txn"
)

// Tender to project conversion (sprint 2, US-2.1). A won (awarded) tender becomes a project once,
// in one transaction: the name and description, the client (the marketflow CRM contact), the award
// as the contract value with a billing type, the dates (the contract dates from the outcome, else
// today, or what the caller gives), the team from the tender's committees, the tender documents
// linked as project files (the same stored files, never copied), and optionally a project template
// laid out from the start date. The tender keeps metadata.conversion and the project
// metadata.tender_id. Converting again returns the same project; if that project was deleted, the
// tender can be converted again.
//
// When the tender carries a bid budget, a draft project budget is created in treasury after the
// commit (treasury owns budgets; projects never keeps a ledger), as the signed-in user and only for
// a caller with projects.financials.manage on a plan with budget_tracking. The push state lives in
// metadata.conversion.budget; a failed push can be retried, and the budget id is kept as soon as
// treasury answers so a retry never creates a second budget. It is created, not submitted: the
// project manager reviews it in Financials and submits it for treasury's approval.

// Budget push states (metadata.conversion.budget.status).
const (
	BudgetNotNeeded  = "not_needed"
	BudgetNotAllowed = "not_allowed"
	BudgetSkipped    = "skipped"
	BudgetPending    = "pending"
	BudgetPushing    = "pushing"
	BudgetCreated    = "created"
	BudgetFailed     = "failed"
	budgetPushStale  = 2 * time.Minute
)

// TemplateSource loads a project template (the planning service).
type TemplateSource interface {
	GetTemplate(ctx context.Context, tenantID, id uuid.UUID) (planning.Template, error)
}

// WithConversion sets the template source and the treasury client the conversion uses.
func (s *Service) WithConversion(templates TemplateSource, tc *treasury.Client) *Service {
	s.templates, s.treasury = templates, tc
	return s
}

// ConvertInput converts a won tender. Everything is optional: the defaults come from the tender.
type ConvertInput struct {
	Name           string      `json:"name"`
	BillingType    string      `json:"billing_type"`
	ContractValue  *float64    `json:"contract_value"`
	StartDate      *tasks.Date `json:"start_date"`
	EndDate        *tasks.Date `json:"end_date"`
	OwnerID        *uuid.UUID  `json:"owner_id"`
	TemplateID     *uuid.UUID  `json:"template_id"`
	CRMContactID   string      `json:"crm_contact_id"`
	CRMContactName string      `json:"crm_contact_name"`
	SkipTeam       bool        `json:"skip_team"`
	SkipBudget     bool        `json:"skip_budget"`
}

// ConvertAccess is what the caller may do, decided by the handler.
type ConvertAccess struct {
	// BudgetAllowed: projects.financials.manage on a plan with budget_tracking.
	BudgetAllowed bool
	// CanSeeProject applies membership scoping to an already converted tender's project.
	CanSeeProject func(ctx context.Context, projectID uuid.UUID) (bool, error)
}

// ConvertedTemplate names the template a conversion applied.
type ConvertedTemplate struct {
	ID         uuid.UUID `json:"id"`
	Name       string    `json:"name"`
	Tasks      int       `json:"task_count"`
	Milestones int       `json:"milestone_count"`
}

// ConvertResult is the project and what the conversion did.
type ConvertResult struct {
	Project          *ent.Project       `json:"project"`
	Tender           *ent.Tender        `json:"tender"`
	AlreadyConverted bool               `json:"already_converted"`
	MembersAdded     int                `json:"members_added"`
	DocumentsLinked  int                `json:"documents_linked"`
	Template         *ConvertedTemplate `json:"template"`
	Budget           map[string]any     `json:"budget"`
}

// teamRole maps a committee role to a project team role: the chair leads (manager), an observer
// follows (viewer), everyone else works on it (member).
func teamRole(committeeRole string) string {
	switch strings.ToLower(committeeRole) {
	case "chair":
		return "manager"
	case "observer":
		return "viewer"
	}
	return "member"
}

var roleRank = map[string]int{"owner": 0, "manager": 1, "member": 2, "viewer": 3}

// TeamFromCommittee merges committee seats into one project role per person (the strongest seat
// wins), leaving out the owner.
func TeamFromCommittee(seats []*ent.TenderCommitteeMember, owner uuid.UUID) map[uuid.UUID]string {
	out := map[uuid.UUID]string{}
	for _, m := range seats {
		if m.UserID == owner || m.UserID == uuid.Nil {
			continue
		}
		role := teamRole(m.Role)
		if cur, ok := out[m.UserID]; !ok || roleRank[role] < roleRank[cur] {
			out[m.UserID] = role
		}
	}
	return out
}

func dateOnly(t time.Time) time.Time { return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC) }

func outcomeDate(meta map[string]any, key string) *time.Time {
	o, _ := meta[MetaOutcome].(map[string]any)
	s, _ := o[key].(string)
	if s == "" {
		return nil
	}
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		return nil
	}
	return &t
}

func outcomeValue(meta map[string]any) (float64, bool) {
	o, _ := meta[MetaOutcome].(map[string]any)
	v, ok := o["award_value"].(float64)
	return v, ok
}

// conversionOf reads metadata.conversion.
func conversionOf(meta map[string]any) map[string]any {
	c, _ := meta[MetaConversion].(map[string]any)
	return c
}

// ConvertedProjectID is the project a tender became (uuid.Nil when none).
func ConvertedProjectID(meta map[string]any) uuid.UUID {
	s, _ := conversionOf(meta)["project_id"].(string)
	id, _ := uuid.Parse(s)
	return id
}

// BudgetBody is the treasury BudgetInput for a converted tender's bid budget: a monthly project
// budget over the project's dates (a year from the start when the project has no end).
func BudgetBody(t *ent.Tender, projectID uuid.UUID, name, currency string, start time.Time, end *time.Time) map[string]any {
	last := start.AddDate(1, 0, -1)
	if end != nil {
		last = *end
	}
	b, _ := t.Metadata[MetaBidBudget].(map[string]any)
	lines, _ := b["lines"].([]any)
	return map[string]any{
		"name":        name + " budget (tender " + t.Number + ")",
		"fiscal_year": strconv.Itoa(start.Year()),
		"budget_type": "project",
		"project_id":  projectID.String(),
		"period_type": "monthly",
		"start_date":  start.Format(time.DateOnly),
		"end_date":    last.Format(time.DateOnly),
		"currency":    currency,
		"lines":       lines,
	}
}

// Convert converts a won tender into a project (see the note at the top of this file).
func (s *Service) Convert(ctx context.Context, tenantID, tenderID uuid.UUID, in ConvertInput, access ConvertAccess) (ConvertResult, error) {
	actor := activity.ActorFrom(ctx)
	if actor == uuid.Nil {
		return ConvertResult{}, ErrValidation("a conversion needs a signed-in user")
	}
	in.Name = strings.TrimSpace(in.Name)
	if len([]rune(in.Name)) > 200 {
		return ConvertResult{}, ErrValidation("name is at most 200 characters")
	}
	if in.BillingType == "" {
		in.BillingType = projects.BillingFixed
	}
	if in.ContractValue != nil && *in.ContractValue < 0 {
		return ConvertResult{}, ErrValidation("contract_value must be zero or more")
	}
	var tpl *planning.Template
	if in.TemplateID != nil && *in.TemplateID != uuid.Nil {
		if s.templates == nil {
			return ConvertResult{}, ErrValidation("project templates are not available")
		}
		got, err := s.templates.GetTemplate(ctx, tenantID, *in.TemplateID)
		if errors.Is(err, planning.ErrNotFound) {
			return ConvertResult{}, ErrValidation("template_id is not one of your templates")
		}
		if err != nil {
			return ConvertResult{}, err
		}
		if err := planning.ValidateDefinition(&got.Definition); err != nil {
			return ConvertResult{}, ErrValidation(err.Error())
		}
		tpl = &got
	}

	var res ConvertResult
	push := false
	err := txn.Run(ctx, s.client, func(tx *ent.Tx) error {
		t, err := lockTender(ctx, tx, tenantID, tenderID)
		if err != nil {
			return err
		}
		if pid := ConvertedProjectID(t.Metadata); pid != uuid.Nil {
			p, err := tx.Project.Query().Where(entproject.ID(pid), entproject.TenantID(tenantID)).Only(ctx)
			if err == nil {
				conv := conversionOf(t.Metadata)
				budget, _ := conv["budget"].(map[string]any)
				res = ConvertResult{Project: p, Tender: t, AlreadyConverted: true, Budget: budget}
				return nil
			}
			if !ent.IsNotFound(err) {
				return fmt.Errorf("load converted project: %w", err)
			}
			// The project was deleted: the tender converts again.
		}
		if t.Status != StatusAwarded {
			return stateErr("tender_not_won", "only a won (awarded) tender becomes a project (status "+t.Status+")")
		}

		// Dates: the caller's, else the contract dates from the outcome, else today.
		start := dateOnly(time.Now().UTC())
		if d := outcomeDate(t.Metadata, "contract_start"); d != nil {
			start = *d
		}
		if in.StartDate != nil && !in.StartDate.IsZero() {
			start = dateOnly(in.StartDate.Time)
		}
		var end *time.Time
		if d := outcomeDate(t.Metadata, "contract_end"); d != nil {
			end = d
		}
		if in.EndDate != nil && !in.EndDate.IsZero() {
			e := dateOnly(in.EndDate.Time)
			end = &e
		}
		if end == nil && tpl != nil {
			e := planning.TemplateEnd(tpl.Definition, start)
			end = &e
		}
		if end != nil && end.Before(start) {
			return ErrValidation("the end date must be on or after the start date")
		}

		// The committee: who sits on this tender's committees, and the owner.
		seats, err := tx.TenderCommitteeMember.Query().
			Where(enttendercommitteemember.TenantID(tenantID),
				enttendercommitteemember.HasCommitteeWith(enttendercommittee.TenderID(tenderID), enttendercommittee.TenantID(tenantID))).
			Limit(subListLimit).All(ctx)
		if err != nil {
			return fmt.Errorf("load committee: %w", err)
		}
		owner := actor
		if in.OwnerID != nil && *in.OwnerID != uuid.Nil && *in.OwnerID != actor {
			onCommittee := false
			for _, m := range seats {
				onCommittee = onCommittee || m.UserID == *in.OwnerID
			}
			if !onCommittee {
				return ErrValidation("the owner must be you or a member of the tender's committee")
			}
			owner = *in.OwnerID
		}

		// The project, with its commercial terms and the client from the CRM.
		name := in.Name
		if name == "" {
			name = t.Title
		}
		meta := map[string]any{
			projects.MetaBillingType: in.BillingType,
			"tender_id":              t.ID.String(),
			"tender_number":          t.Number,
		}
		value := t.EstimatedValue
		if v, ok := outcomeValue(t.Metadata); ok {
			value = v
		}
		if in.ContractValue != nil {
			value = *in.ContractValue
		}
		if value > 0 {
			meta[projects.MetaContractValue] = value
		}
		contactID, contactName := strings.TrimSpace(in.CRMContactID), strings.TrimSpace(in.CRMContactName)
		if contactID == "" {
			contactID, _ = t.Metadata[MetaCRMContactID].(string)
			contactName, _ = t.Metadata[MetaCRMContactName].(string)
		}
		if contactID != "" {
			meta[projects.MetaCRMContactID] = contactID
			if contactName == "" {
				contactName = t.ClientName
			}
			meta[projects.MetaCRMContactName] = contactName
		}
		p, err := projects.CreateProjectTx(ctx, tx, tenantID, projects.CreateProjectInput{
			Name: name, Description: t.Description, StartDate: &start, EndDate: end, Currency: t.Currency,
			OwnerID: owner, Metadata: meta,
		})
		var pverr projects.ValidationError
		if errors.As(err, &pverr) {
			return ErrValidation(pverr.Error())
		}
		if err != nil {
			return err
		}

		// The team: committee seats (the strongest seat per person), and the converter as a
		// manager when someone else owns the project, so they keep sight of it.
		team := map[uuid.UUID]string{}
		if !in.SkipTeam {
			team = TeamFromCommittee(seats, owner)
		}
		if actor != owner {
			if cur, ok := team[actor]; !ok || roleRank["manager"] < roleRank[cur] {
				team[actor] = "manager"
			}
		}
		if len(team) > 0 {
			bulk := make([]*ent.ProjectMemberCreate, 0, len(team))
			for uid, role := range team {
				bulk = append(bulk, tx.ProjectMember.Create().SetTenantID(tenantID).SetProjectID(p.ID).SetUserID(uid).SetRoleCode(role).
					SetMetadata(map[string]any{"source": "tender", "tender_id": t.ID.String()}))
			}
			if _, err := tx.ProjectMember.CreateBulk(bulk...).Save(ctx); err != nil {
				return fmt.Errorf("add team: %w", err)
			}
		}

		// The documents: linked, not copied (the attachment points at the same stored file and
		// never deletes it).
		docs, err := tx.TenderDocument.Query().Where(enttenderdoc.TenantID(tenantID), enttenderdoc.TenderID(tenderID)).
			Order(ent.Asc(enttenderdoc.FieldUploadedAt)).Limit(subListLimit).All(ctx)
		if err != nil {
			return fmt.Errorf("load documents: %w", err)
		}
		if len(docs) > 0 {
			bulk := make([]*ent.AttachmentCreate, len(docs))
			for i, d := range docs {
				am := map[string]any{attachments.MetaLinkedFrom: "tender_document", "tender_id": t.ID.String(), "tender_document_id": d.ID.String()}
				if storedKey(d.FileURL) {
					am["storage"] = "objectstore"
				}
				c := tx.Attachment.Create().SetTenantID(tenantID).SetProjectID(p.ID).SetFileURL(d.FileURL).SetFileName(d.FileName).
					SetFileSize(d.FileSize).SetUploadedBy(d.UploadedBy).SetUploadedAt(d.UploadedAt).SetMetadata(am)
				if d.MimeType != "" {
					c.SetMimeType(d.MimeType)
				}
				bulk[i] = c
			}
			if _, err := tx.Attachment.CreateBulk(bulk...).Save(ctx); err != nil {
				return fmt.Errorf("link documents: %w", err)
			}
		}

		// The template's plan.
		var applied *ConvertedTemplate
		if tpl != nil {
			r, err := planning.ApplyTemplateTx(ctx, tx, tenantID, p, *tpl, start)
			if err != nil {
				return err
			}
			p = r.Project
			applied = &ConvertedTemplate{ID: tpl.ID, Name: tpl.Name, Tasks: r.Tasks, Milestones: r.Milestones}
		}

		// The bid budget goes to treasury after the commit.
		budget := map[string]any{"status": BudgetNotNeeded}
		switch {
		case !HasBidBudget(t.Metadata):
		case in.SkipBudget:
			budget["status"] = BudgetSkipped
		case !access.BudgetAllowed:
			budget["status"] = BudgetNotAllowed
			budget["error"] = "creating the project budget needs project financials (budget tracking) and permission to manage budgets"
		default:
			budget = map[string]any{"status": BudgetPending, "attempts": 0, "request": BudgetBody(t, p.ID, p.Name, p.Currency, start, end)}
			push = true
		}

		now := time.Now().UTC()
		conv := map[string]any{
			"project_id": p.ID.String(), "project_name": p.Name, "converted_at": now.Format(time.RFC3339),
			"converted_by": actor.String(), "members_added": len(team), "documents_linked": len(docs), "budget": budget,
		}
		if applied != nil {
			conv["template_id"], conv["template_name"] = applied.ID.String(), applied.Name
		}
		tmeta := copyMeta(t.Metadata)
		tmeta[MetaConversion] = conv
		if t, err = t.Update().SetMetadata(tmeta).Save(ctx); err != nil {
			return fmt.Errorf("save conversion: %w", err)
		}
		if err := record(ctx, tx, tenantID, tenderID, activity.ActionUpdated, map[string]any{
			"converted_to_project": p.ID.String(), "project_name": p.Name,
		}); err != nil {
			return err
		}
		if err := activity.Record(ctx, tx, activity.Entry{
			TenantID: tenantID, ProjectID: activity.Ref(p.ID), EntityType: "project", EntityID: p.ID, Action: activity.ActionUpdated,
			Changes: map[string]any{"from_tender": t.Number, "members_added": len(team), "documents_linked": len(docs)},
		}); err != nil {
			return err
		}
		extra := map[string]any{
			"project_id": p.ID.String(), "project_name": p.Name, "owner_id": owner.String(), "converted_by": actor.String(),
			"members_added": len(team), "documents_linked": len(docs), "budget_status": budget["status"],
		}
		if v, ok := meta[projects.MetaContractValue]; ok {
			extra["contract_value"] = v
		}
		if applied != nil {
			extra["template_id"] = applied.ID.String()
		}
		if err := emit(ctx, tx, t, EventConverted, extra); err != nil {
			return err
		}
		res = ConvertResult{Project: p, Tender: t, MembersAdded: len(team), DocumentsLinked: len(docs), Template: applied, Budget: budget}
		return nil
	})
	if err != nil {
		return ConvertResult{}, err
	}
	if res.AlreadyConverted {
		if access.CanSeeProject != nil {
			ok, err := access.CanSeeProject(ctx, res.Project.ID)
			if err != nil {
				return ConvertResult{}, err
			}
			if !ok {
				return ConvertResult{}, stateErr("tender_already_converted", "this tender is already a project you are not on")
			}
		}
		return res, nil
	}
	if push {
		if state, perr := s.PushConversionBudget(ctx, tenantID, tenderID); perr == nil {
			res.Budget = state
		} else {
			s.log.Warn("conversion budget push not run", zap.String("tender_id", tenderID.String()), zap.Error(perr))
		}
	}
	return res, nil
}

// ConvertedProject returns the project a tender became (uuid.Nil when it has not been converted).
func (s *Service) ConvertedProject(ctx context.Context, tenantID, tenderID uuid.UUID) (uuid.UUID, error) {
	t, err := s.client.Tender.Query().Where(tenderOf(tenantID, tenderID)...).Only(ctx)
	if ent.IsNotFound(err) {
		return uuid.Nil, ErrNotFound
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("fetch tender: %w", err)
	}
	return ConvertedProjectID(t.Metadata), nil
}

// saveBudgetState writes metadata.conversion.budget (outside any other transaction).
func (s *Service) saveBudgetState(ctx context.Context, tenantID, tenderID uuid.UUID, state map[string]any, final bool) error {
	return txn.Run(ctx, s.client, func(tx *ent.Tx) error {
		t, err := lockTender(ctx, tx, tenantID, tenderID)
		if err != nil {
			return err
		}
		meta := copyMeta(t.Metadata)
		conv := copyMeta(conversionOf(meta))
		conv["budget"] = state
		meta[MetaConversion] = conv
		if err := t.Update().SetMetadata(meta).Exec(ctx); err != nil {
			return fmt.Errorf("save budget state: %w", err)
		}
		if !final {
			return nil
		}
		return record(ctx, tx, tenantID, tenderID, activity.ActionUpdated, map[string]any{
			"project_budget": state["status"], "budget_id": state["budget_id"],
		})
	})
}

// PushConversionBudget creates the converted project's draft budget in treasury as the signed-in
// user. Only a pending or failed push (or one stuck pushing for two minutes) runs; anything else is
// a StateError. It returns the new state.
func (s *Service) PushConversionBudget(ctx context.Context, tenantID, tenderID uuid.UUID) (map[string]any, error) {
	userID := activity.ActorFrom(ctx)
	var state map[string]any
	err := txn.Run(ctx, s.client, func(tx *ent.Tx) error {
		t, err := lockTender(ctx, tx, tenantID, tenderID)
		if err != nil {
			return err
		}
		conv := conversionOf(t.Metadata)
		cur, _ := conv["budget"].(map[string]any)
		if cur == nil {
			return stateErr("budget_push_not_needed", "this tender has no project budget to send")
		}
		state = copyMeta(cur)
		st, _ := state["status"].(string)
		since, _ := time.Parse(time.RFC3339, fmt.Sprint(state["pushing_since"]))
		switch {
		case st == BudgetPending || st == BudgetFailed:
		case st == BudgetPushing && time.Since(since) > budgetPushStale:
		case st == BudgetPushing:
			return stateErr("budget_push_in_progress", "the budget is being sent to treasury right now")
		case st == BudgetCreated:
			return stateErr("budget_push_done", "the budget is already in treasury")
		default:
			return stateErr("budget_push_not_needed", "this tender has no project budget to send")
		}
		attempts, _ := state["attempts"].(float64)
		if n, ok := state["attempts"].(int); ok {
			attempts = float64(n)
		}
		state["status"], state["pushing_since"], state["attempts"] = BudgetPushing, time.Now().UTC().Format(time.RFC3339), int(attempts)+1
		delete(state, "error")
		meta := copyMeta(t.Metadata)
		c := copyMeta(conv)
		c["budget"] = state
		meta[MetaConversion] = c
		return t.Update().SetMetadata(meta).Exec(ctx)
	})
	if err != nil {
		return nil, err
	}
	budgetID, perr := s.createBudget(ctx, tenantID, userID, state)
	delete(state, "pushing_since")
	state["attempted_at"] = time.Now().UTC().Format(time.RFC3339)
	if budgetID != "" {
		state["budget_id"] = budgetID
	}
	if perr != nil {
		state["status"], state["error"] = BudgetFailed, treasuryMessage(perr)
		s.log.Warn("conversion budget push failed", zap.String("tender_id", tenderID.String()), zap.Error(perr))
	} else {
		state["status"] = BudgetCreated
	}
	if err := s.saveBudgetState(context.WithoutCancel(ctx), tenantID, tenderID, state, true); err != nil {
		return nil, err
	}
	return state, nil
}

func (s *Service) createBudget(ctx context.Context, tenantID, userID uuid.UUID, state map[string]any) (string, error) {
	if id, _ := state["budget_id"].(string); id != "" {
		return id, nil
	}
	if !s.treasury.Enabled() {
		return "", treasury.ErrNotConfigured
	}
	body, _ := state["request"].(map[string]any)
	if body == nil {
		return "", fmt.Errorf("the budget request is missing")
	}
	raw, err := s.treasury.CreateBudget(ctx, tenantID, userID, body)
	if err != nil {
		return "", err
	}
	var created struct {
		ID uuid.UUID `json:"id"`
	}
	if json.Unmarshal(raw, &created) != nil || created.ID == uuid.Nil {
		return "", fmt.Errorf("treasury budget create returned no id")
	}
	return created.ID.String(), nil
}

// treasuryMessage is treasury's reason for a refusal, or a plain one.
func treasuryMessage(err error) string {
	var te *treasury.Error
	if errors.As(err, &te) {
		var body struct {
			Error   string `json:"error"`
			Message string `json:"message"`
		}
		_ = json.Unmarshal(te.Body, &body)
		if body.Error != "" {
			return body.Error
		}
		if body.Message != "" {
			return body.Message
		}
		return fmt.Sprintf("treasury answered %d", te.Status)
	}
	if errors.Is(err, treasury.ErrNotConfigured) {
		return "treasury is not configured"
	}
	return "treasury could not be reached"
}
