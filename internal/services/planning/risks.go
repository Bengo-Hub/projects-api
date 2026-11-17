package planning

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/bengobox/projects-service/internal/ent"
	entrisk "github.com/bengobox/projects-service/internal/ent/projectrisk"
	"github.com/bengobox/projects-service/internal/platform/events"
	"github.com/bengobox/projects-service/internal/services/activity"
	"github.com/bengobox/projects-service/internal/services/notify"
	"github.com/bengobox/projects-service/internal/services/people"
	"github.com/bengobox/projects-service/internal/services/txn"
)

// Risk register (sprint 16): likelihood and impact on a 1-5 scale, score = likelihood x impact.

// Risk statuses; closed and occurred risks drop off the heat map by default.
var RiskStatuses = []string{"open", "monitoring", "mitigated", "occurred", "closed"}

// Risk events (subject project.risk.<type>).
const (
	EventRiskCreated = "risk.created"
	EventRiskUpdated = "risk.updated"
	EventRiskDeleted = "risk.deleted"
)

// MaxRisks bounds one project's register.
const MaxRisks = 1000

// RiskInput creates a risk or, as a patch, changes one (nil fields stay; an empty owner_id or
// due_date clears it).
type RiskInput struct {
	Title       *string `json:"title"`
	Description *string `json:"description"`
	Category    *string `json:"category"`
	Likelihood  *int    `json:"likelihood"`
	Impact      *int    `json:"impact"`
	OwnerID     *string `json:"owner_id"`
	Mitigation  *string `json:"mitigation"`
	Status      *string `json:"status"`
	DueDate     *string `json:"due_date"`
}

// Risk is a register row with the owner's name.
type Risk struct {
	*ent.ProjectRisk
	OwnerName string `json:"owner_name"`
}

// Score is likelihood times impact.
func Score(likelihood, impact int) int { return likelihood * impact }

func scale(name string, v int) error {
	if v < 1 || v > 5 {
		return ValidationError(name + " must be between 1 and 5")
	}
	return nil
}

func (s *Service) checkOwner(ctx context.Context, tx *ent.Tx, tenantID, projectID uuid.UUID, raw string) (*uuid.UUID, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		return nil, ValidationError("owner_id must be a user id")
	}
	members, err := notify.Members(ctx, tx, tenantID, projectID, []uuid.UUID{id})
	if err != nil {
		return nil, err
	}
	if !members[id] {
		return nil, ValidationError("the risk owner must be on the project's team")
	}
	return &id, nil
}

func parseDue(raw string) (*time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	t, err := time.Parse(time.DateOnly, raw)
	if err != nil {
		return nil, ValidationError("due_date must be YYYY-MM-DD")
	}
	return &t, nil
}

func riskPayload(r *ent.ProjectRisk) map[string]any {
	out := map[string]any{
		"project_id": r.ProjectID.String(), "risk_id": r.ID.String(), "title": r.Title, "likelihood": r.Likelihood,
		"impact": r.Impact, "score": r.Score, "status": r.Status, "owner_id": "",
	}
	if r.OwnerID != nil {
		out["owner_id"] = r.OwnerID.String()
	}
	if r.DueDate != nil {
		out["due_date"] = r.DueDate.Format(time.DateOnly)
	}
	return out
}

func (s *Service) withOwners(ctx context.Context, tenantID uuid.UUID, rows []*ent.ProjectRisk) ([]Risk, error) {
	ids := make([]uuid.UUID, 0, len(rows))
	for _, r := range rows {
		if r.OwnerID != nil {
			ids = append(ids, *r.OwnerID)
		}
	}
	names, err := people.Lookup(ctx, s.client.ProjectsUser, tenantID, ids)
	if err != nil {
		return nil, err
	}
	out := make([]Risk, len(rows))
	for i, r := range rows {
		out[i] = Risk{ProjectRisk: r}
		if r.OwnerID != nil {
			out[i].OwnerName = names[*r.OwnerID].Label()
		}
	}
	return out, nil
}

// CreateRisk adds a risk to the register.
func (s *Service) CreateRisk(ctx context.Context, tenantID, projectID uuid.UUID, in RiskInput) (Risk, error) {
	if in.Title == nil || strings.TrimSpace(*in.Title) == "" {
		return Risk{}, ValidationError("title is required")
	}
	if in.Likelihood == nil || in.Impact == nil {
		return Risk{}, ValidationError("likelihood and impact are required (1-5)")
	}
	if err := scale("likelihood", *in.Likelihood); err != nil {
		return Risk{}, err
	}
	if err := scale("impact", *in.Impact); err != nil {
		return Risk{}, err
	}
	status := "open"
	if in.Status != nil && *in.Status != "" {
		status = *in.Status
	}
	if !slices.Contains(RiskStatuses, status) {
		return Risk{}, ValidationError("status must be open, monitoring, mitigated, occurred or closed")
	}
	var r *ent.ProjectRisk
	err := txn.Run(ctx, s.client, func(tx *ent.Tx) error {
		if _, err := s.project(ctx, tx.Project, tenantID, projectID); err != nil {
			return err
		}
		n, err := tx.ProjectRisk.Query().Where(entrisk.TenantID(tenantID), entrisk.ProjectID(projectID)).Count(ctx)
		if err != nil {
			return fmt.Errorf("count risks: %w", err)
		}
		if n >= MaxRisks {
			return ValidationError(fmt.Sprintf("a project's register holds at most %d risks", MaxRisks))
		}
		c := tx.ProjectRisk.Create().SetTenantID(tenantID).SetProjectID(projectID).SetTitle(strings.TrimSpace(*in.Title)).
			SetLikelihood(*in.Likelihood).SetImpact(*in.Impact).SetScore(Score(*in.Likelihood, *in.Impact)).
			SetStatus(status).SetCreatedBy(activity.ActorFrom(ctx))
		if in.Description != nil {
			c.SetDescription(*in.Description)
		}
		if in.Category != nil {
			c.SetCategory(strings.TrimSpace(*in.Category))
		}
		if in.Mitigation != nil {
			c.SetMitigation(*in.Mitigation)
		}
		if in.OwnerID != nil {
			owner, err := s.checkOwner(ctx, tx, tenantID, projectID, *in.OwnerID)
			if err != nil {
				return err
			}
			c.SetNillableOwnerID(owner)
		}
		if in.DueDate != nil {
			due, err := parseDue(*in.DueDate)
			if err != nil {
				return err
			}
			c.SetNillableDueDate(due)
		}
		if r, err = c.Save(ctx); err != nil {
			return fmt.Errorf("create risk: %w", err)
		}
		if err := record(ctx, tx, tenantID, projectID, "risk", r.ID, activity.ActionCreated,
			map[string]any{"title": r.Title, "score": r.Score, "status": r.Status}); err != nil {
			return err
		}
		return events.Write(ctx, tx, events.AggregateProject, EventRiskCreated, projectID, tenantID, riskPayload(r))
	})
	if err != nil {
		return Risk{}, err
	}
	out, err := s.withOwners(ctx, tenantID, []*ent.ProjectRisk{r})
	if err != nil {
		return Risk{}, err
	}
	return out[0], nil
}

// UpdateRisk applies a patch to a risk.
func (s *Service) UpdateRisk(ctx context.Context, tenantID, projectID, id uuid.UUID, in RiskInput) (Risk, error) {
	var r *ent.ProjectRisk
	err := txn.Run(ctx, s.client, func(tx *ent.Tx) error {
		cur, err := tx.ProjectRisk.Query().Where(entrisk.ID(id), entrisk.TenantID(tenantID), entrisk.ProjectID(projectID)).Only(ctx)
		if ent.IsNotFound(err) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("load risk: %w", err)
		}
		u := cur.Update()
		changes := map[string]any{}
		if in.Title != nil {
			t := strings.TrimSpace(*in.Title)
			if t == "" {
				return ValidationError("title cannot be empty")
			}
			u.SetTitle(t)
			changes["title"] = t
		}
		if in.Description != nil {
			u.SetDescription(*in.Description)
			changes["description"] = "changed"
		}
		if in.Category != nil {
			u.SetCategory(strings.TrimSpace(*in.Category))
			changes["category"] = strings.TrimSpace(*in.Category)
		}
		if in.Mitigation != nil {
			u.SetMitigation(*in.Mitigation)
			changes["mitigation"] = "changed"
		}
		l, i := cur.Likelihood, cur.Impact
		if in.Likelihood != nil {
			if err := scale("likelihood", *in.Likelihood); err != nil {
				return err
			}
			l = *in.Likelihood
		}
		if in.Impact != nil {
			if err := scale("impact", *in.Impact); err != nil {
				return err
			}
			i = *in.Impact
		}
		if l != cur.Likelihood || i != cur.Impact {
			u.SetLikelihood(l).SetImpact(i).SetScore(Score(l, i))
			changes["score"] = map[string]any{"from": cur.Score, "to": Score(l, i)}
		}
		if in.Status != nil {
			if !slices.Contains(RiskStatuses, *in.Status) {
				return ValidationError("status must be open, monitoring, mitigated, occurred or closed")
			}
			if *in.Status != cur.Status {
				u.SetStatus(*in.Status)
				changes["status"] = map[string]any{"from": cur.Status, "to": *in.Status}
			}
		}
		if in.OwnerID != nil {
			owner, err := s.checkOwner(ctx, tx, tenantID, projectID, *in.OwnerID)
			if err != nil {
				return err
			}
			if owner == nil {
				u.ClearOwnerID()
				changes["owner_id"] = nil
			} else {
				u.SetOwnerID(*owner)
				changes["owner_id"] = owner.String()
			}
		}
		if in.DueDate != nil {
			due, err := parseDue(*in.DueDate)
			if err != nil {
				return err
			}
			if due == nil {
				u.ClearDueDate()
				changes["due_date"] = nil
			} else {
				u.SetDueDate(*due)
				changes["due_date"] = due.Format(time.DateOnly)
			}
		}
		if r, err = u.Save(ctx); err != nil {
			return fmt.Errorf("update risk: %w", err)
		}
		if err := record(ctx, tx, tenantID, projectID, "risk", id, activity.ActionUpdated, changes); err != nil {
			return err
		}
		payload := riskPayload(r)
		payload["changes"] = changes
		return events.Write(ctx, tx, events.AggregateProject, EventRiskUpdated, projectID, tenantID, payload)
	})
	if err != nil {
		return Risk{}, err
	}
	out, err := s.withOwners(ctx, tenantID, []*ent.ProjectRisk{r})
	if err != nil {
		return Risk{}, err
	}
	return out[0], nil
}

// DeleteRisk removes a risk.
func (s *Service) DeleteRisk(ctx context.Context, tenantID, projectID, id uuid.UUID) error {
	return txn.Run(ctx, s.client, func(tx *ent.Tx) error {
		r, err := tx.ProjectRisk.Query().Where(entrisk.ID(id), entrisk.TenantID(tenantID), entrisk.ProjectID(projectID)).Only(ctx)
		if ent.IsNotFound(err) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("load risk: %w", err)
		}
		if err := tx.ProjectRisk.DeleteOne(r).Exec(ctx); err != nil {
			return fmt.Errorf("delete risk: %w", err)
		}
		if err := record(ctx, tx, tenantID, projectID, "risk", id, activity.ActionDeleted, map[string]any{"title": r.Title}); err != nil {
			return err
		}
		return events.Write(ctx, tx, events.AggregateProject, EventRiskDeleted, projectID, tenantID, riskPayload(r))
	})
}

// RiskFilter narrows the register list.
type RiskFilter struct {
	Status   string
	MinScore int
	OwnerID  *uuid.UUID
	Limit    int
	Offset   int
}

// ListRisks returns one page of the register, highest score first, then soonest due.
func (s *Service) ListRisks(ctx context.Context, tenantID, projectID uuid.UUID, f RiskFilter) ([]Risk, int, error) {
	q := s.client.ProjectRisk.Query().Where(entrisk.TenantID(tenantID), entrisk.ProjectID(projectID))
	if f.Status != "" {
		q = q.Where(entrisk.Status(f.Status))
	}
	if f.MinScore > 0 {
		q = q.Where(entrisk.ScoreGTE(f.MinScore))
	}
	if f.OwnerID != nil {
		q = q.Where(entrisk.OwnerID(*f.OwnerID))
	}
	total, err := q.Clone().Count(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("count risks: %w", err)
	}
	if f.Limit <= 0 {
		f.Limit = 20
	}
	rows, err := q.Order(ent.Desc(entrisk.FieldScore), ent.Asc(entrisk.FieldDueDate), ent.Asc(entrisk.FieldID)).
		Offset(f.Offset).Limit(f.Limit).All(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("list risks: %w", err)
	}
	out, err := s.withOwners(ctx, tenantID, rows)
	return out, total, err
}

// GetRisk returns one risk.
func (s *Service) GetRisk(ctx context.Context, tenantID, projectID, id uuid.UUID) (Risk, error) {
	r, err := s.client.ProjectRisk.Query().Where(entrisk.ID(id), entrisk.TenantID(tenantID), entrisk.ProjectID(projectID)).Only(ctx)
	if ent.IsNotFound(err) {
		return Risk{}, ErrNotFound
	}
	if err != nil {
		return Risk{}, fmt.Errorf("load risk: %w", err)
	}
	out, err := s.withOwners(ctx, tenantID, []*ent.ProjectRisk{r})
	if err != nil {
		return Risk{}, err
	}
	return out[0], nil
}

// HeatCell is one (likelihood, impact) count.
type HeatCell struct {
	Likelihood int `json:"likelihood"`
	Impact     int `json:"impact"`
	Count      int `json:"count"`
}

// HeatMap is the 5x5 grid: Grid[likelihood-1][impact-1].
type HeatMap struct {
	Grid  [5][5]int  `json:"grid"`
	Cells []HeatCell `json:"cells"`
	Total int        `json:"total"`
}

// BuildHeatMap places grouped counts on the grid, ignoring anything off the 1-5 scale.
func BuildHeatMap(cells []HeatCell) HeatMap {
	var h HeatMap
	h.Cells = []HeatCell{}
	for _, c := range cells {
		if c.Likelihood < 1 || c.Likelihood > 5 || c.Impact < 1 || c.Impact > 5 || c.Count <= 0 {
			continue
		}
		h.Grid[c.Likelihood-1][c.Impact-1] += c.Count
		h.Total += c.Count
	}
	for l := 1; l <= 5; l++ {
		for i := 1; i <= 5; i++ {
			if n := h.Grid[l-1][i-1]; n > 0 {
				h.Cells = append(h.Cells, HeatCell{Likelihood: l, Impact: i, Count: n})
			}
		}
	}
	return h
}

// RiskHeatMap counts the project's risks per likelihood and impact in one grouped query. Closed
// and occurred risks are left out unless all is set.
func (s *Service) RiskHeatMap(ctx context.Context, tenantID, projectID uuid.UUID, all bool) (HeatMap, error) {
	q := s.client.ProjectRisk.Query().Where(entrisk.TenantID(tenantID), entrisk.ProjectID(projectID))
	if !all {
		q = q.Where(entrisk.StatusNotIn("closed", "occurred"))
	}
	var cells []HeatCell
	if err := q.GroupBy(entrisk.FieldLikelihood, entrisk.FieldImpact).Aggregate(ent.As(ent.Count(), "count")).Scan(ctx, &cells); err != nil {
		return HeatMap{}, fmt.Errorf("risk heat map: %w", err)
	}
	return BuildHeatMap(cells), nil
}
