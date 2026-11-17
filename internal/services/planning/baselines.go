package planning

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/bengobox/projects-service/internal/ent"
	entmilestone "github.com/bengobox/projects-service/internal/ent/milestone"
	entbaseline "github.com/bengobox/projects-service/internal/ent/projectbaseline"
	enttask "github.com/bengobox/projects-service/internal/ent/task"
	"github.com/bengobox/projects-service/internal/planmodel"
	"github.com/bengobox/projects-service/internal/platform/events"
	"github.com/bengobox/projects-service/internal/services/activity"
	"github.com/bengobox/projects-service/internal/services/people"
	"github.com/bengobox/projects-service/internal/services/txn"
)

// Baselines (sprint 16): a saved copy of the plan (task dates and estimates, milestone dates),
// one JSON snapshot per save in project_baselines.
const (
	// baselineTaskLimit matches the Gantt chart's cap: a bigger project keeps its oldest tasks.
	baselineTaskLimit      = 2000
	baselineMilestoneLimit = 500
	// MaxBaselines bounds one project's saved baselines.
	MaxBaselines = 50
)

// Baseline events (subject project.baseline.<type>).
const (
	EventBaselineSaved   = "baseline.saved"
	EventBaselineDeleted = "baseline.deleted"
)

// BaselineSummary is a baseline without its snapshot (the list).
type BaselineSummary struct {
	ID            uuid.UUID `json:"id"`
	ProjectID     uuid.UUID `json:"project_id"`
	Name          string    `json:"name"`
	Note          string    `json:"note"`
	TaskCount     int       `json:"task_count"`
	CreatedBy     uuid.UUID `json:"created_by"`
	CreatedByName string    `json:"created_by_name"`
	CreatedAt     time.Time `json:"created_at"`
}

func summaryOf(b *ent.ProjectBaseline, names map[uuid.UUID]people.Person) BaselineSummary {
	return BaselineSummary{ID: b.ID, ProjectID: b.ProjectID, Name: b.Name, Note: b.Note, TaskCount: b.TaskCount,
		CreatedBy: b.CreatedBy, CreatedByName: names[b.CreatedBy].Label(), CreatedAt: b.CreatedAt}
}

func optTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

// SaveBaseline snapshots the project's plan: one query for the tasks, one for the milestones.
func (s *Service) SaveBaseline(ctx context.Context, tenantID, projectID uuid.UUID, name, note string) (BaselineSummary, error) {
	name, note = strings.TrimSpace(name), strings.TrimSpace(note)
	if name == "" {
		name = "Baseline " + time.Now().UTC().Format("2006-01-02 15:04")
	}
	if len([]rune(name)) > 120 || len([]rune(note)) > 1000 {
		return BaselineSummary{}, ValidationError("the name is at most 120 characters and the note at most 1000")
	}
	var out *ent.ProjectBaseline
	err := txn.Run(ctx, s.client, func(tx *ent.Tx) error {
		p, err := s.project(ctx, tx.Project, tenantID, projectID)
		if err != nil {
			return err
		}
		n, err := tx.ProjectBaseline.Query().Where(entbaseline.TenantID(tenantID), entbaseline.ProjectID(projectID)).Count(ctx)
		if err != nil {
			return fmt.Errorf("count baselines: %w", err)
		}
		if n >= MaxBaselines {
			return ValidationError(fmt.Sprintf("a project keeps at most %d baselines; delete an old one first", MaxBaselines))
		}
		rows, err := tx.Task.Query().Where(enttask.TenantID(tenantID), enttask.ProjectID(projectID)).
			Select(enttask.FieldID, enttask.FieldTitle, enttask.FieldStatus, enttask.FieldStartDate, enttask.FieldDueDate,
				enttask.FieldEstimatedHours, enttask.FieldParentID).
			Order(ent.Asc(enttask.FieldCreatedAt), ent.Asc(enttask.FieldID)).Limit(baselineTaskLimit + 1).All(ctx)
		if err != nil {
			return fmt.Errorf("load tasks: %w", err)
		}
		snap := planmodel.Snapshot{ProjectStart: optTime(p.StartDate), ProjectEnd: optTime(p.EndDate),
			Tasks: make([]planmodel.BaselineTask, 0, len(rows)), Milestones: []planmodel.BaselineMilestone{}}
		if len(rows) > baselineTaskLimit {
			rows, snap.Truncated = rows[:baselineTaskLimit], true
		}
		for _, t := range rows {
			bt := planmodel.BaselineTask{ID: t.ID, Title: t.Title, Status: t.Status, StartDate: t.StartDate,
				DueDate: optTime(t.DueDate), EstimatedHours: t.EstimatedHours}
			if t.ParentID != uuid.Nil {
				pid := t.ParentID
				bt.ParentID = &pid
			}
			snap.Tasks = append(snap.Tasks, bt)
		}
		ms, err := tx.Milestone.Query().Where(entmilestone.TenantID(tenantID), entmilestone.ProjectID(projectID)).
			Select(entmilestone.FieldID, entmilestone.FieldName, entmilestone.FieldTargetDate).
			Order(ent.Asc(entmilestone.FieldTargetDate)).Limit(baselineMilestoneLimit).All(ctx)
		if err != nil {
			return fmt.Errorf("load milestones: %w", err)
		}
		for _, m := range ms {
			snap.Milestones = append(snap.Milestones, planmodel.BaselineMilestone{ID: m.ID, Name: m.Name, TargetDate: optTime(m.TargetDate)})
		}
		if out, err = tx.ProjectBaseline.Create().SetTenantID(tenantID).SetProjectID(projectID).SetName(name).SetNote(note).
			SetSnapshot(snap).SetTaskCount(len(snap.Tasks)).SetCreatedBy(activity.ActorFrom(ctx)).Save(ctx); err != nil {
			return fmt.Errorf("save baseline: %w", err)
		}
		if err := record(ctx, tx, tenantID, projectID, "baseline", out.ID, activity.ActionCreated,
			map[string]any{"name": name, "task_count": out.TaskCount}); err != nil {
			return err
		}
		return events.Write(ctx, tx, events.AggregateProject, EventBaselineSaved, projectID, tenantID, map[string]any{
			"project_id": projectID.String(), "baseline_id": out.ID.String(), "name": name,
			"task_count": out.TaskCount, "milestone_count": len(snap.Milestones), "truncated": snap.Truncated, "actor_id": actor(ctx),
		})
	})
	if err != nil {
		return BaselineSummary{}, err
	}
	names, err := people.Lookup(ctx, s.client.ProjectsUser, tenantID, []uuid.UUID{out.CreatedBy})
	if err != nil {
		return BaselineSummary{}, err
	}
	return summaryOf(out, names), nil
}

// ListBaselines lists a project's baselines newest first (at most MaxBaselines), without snapshots.
func (s *Service) ListBaselines(ctx context.Context, tenantID, projectID uuid.UUID) ([]BaselineSummary, error) {
	rows, err := s.client.ProjectBaseline.Query().Where(entbaseline.TenantID(tenantID), entbaseline.ProjectID(projectID)).
		Select(entbaseline.FieldID, entbaseline.FieldProjectID, entbaseline.FieldName, entbaseline.FieldNote,
			entbaseline.FieldTaskCount, entbaseline.FieldCreatedBy, entbaseline.FieldCreatedAt).
		Order(ent.Desc(entbaseline.FieldCreatedAt)).Limit(MaxBaselines).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list baselines: %w", err)
	}
	ids := make([]uuid.UUID, len(rows))
	for i, b := range rows {
		ids[i] = b.CreatedBy
	}
	names, err := people.Lookup(ctx, s.client.ProjectsUser, tenantID, ids)
	if err != nil {
		return nil, err
	}
	out := make([]BaselineSummary, len(rows))
	for i, b := range rows {
		out[i] = summaryOf(b, names)
	}
	return out, nil
}

// GetBaseline returns one baseline with its snapshot; a nil id is the latest (ErrNotFound when the
// project has none).
func (s *Service) GetBaseline(ctx context.Context, tenantID, projectID uuid.UUID, id *uuid.UUID) (*ent.ProjectBaseline, error) {
	q := s.client.ProjectBaseline.Query().Where(entbaseline.TenantID(tenantID), entbaseline.ProjectID(projectID))
	if id != nil {
		q = q.Where(entbaseline.ID(*id))
	}
	b, err := q.Order(ent.Desc(entbaseline.FieldCreatedAt)).First(ctx)
	if ent.IsNotFound(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load baseline: %w", err)
	}
	return b, nil
}

// DeleteBaseline removes one baseline.
func (s *Service) DeleteBaseline(ctx context.Context, tenantID, projectID, id uuid.UUID) error {
	return txn.Run(ctx, s.client, func(tx *ent.Tx) error {
		n, err := tx.ProjectBaseline.Delete().Where(entbaseline.ID(id), entbaseline.TenantID(tenantID), entbaseline.ProjectID(projectID)).Exec(ctx)
		if err != nil {
			return fmt.Errorf("delete baseline: %w", err)
		}
		if n == 0 {
			return ErrNotFound
		}
		if err := record(ctx, tx, tenantID, projectID, "baseline", id, activity.ActionDeleted, nil); err != nil {
			return err
		}
		return events.Write(ctx, tx, events.AggregateProject, EventBaselineDeleted, projectID, tenantID, map[string]any{
			"project_id": projectID.String(), "baseline_id": id.String(), "actor_id": actor(ctx),
		})
	})
}

// GanttBaseline is what the Gantt chart draws under the live bars: per task id the baseline
// dates and estimate, and the milestones' baseline dates.
type GanttBaseline struct {
	ID         uuid.UUID                            `json:"id"`
	Name       string                               `json:"name"`
	CreatedAt  time.Time                            `json:"created_at"`
	Tasks      map[uuid.UUID]planmodel.BaselineTask `json:"tasks"`
	Milestones []planmodel.BaselineMilestone        `json:"milestones"`
	Truncated  bool                                 `json:"truncated"`
}

// ForGantt returns the baseline for the chart (nil id: the latest; nil result when the project
// has none and no id was asked for).
func (s *Service) ForGantt(ctx context.Context, tenantID, projectID uuid.UUID, id *uuid.UUID) (*GanttBaseline, error) {
	b, err := s.GetBaseline(ctx, tenantID, projectID, id)
	if err == ErrNotFound && id == nil {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	out := &GanttBaseline{ID: b.ID, Name: b.Name, CreatedAt: b.CreatedAt, Tasks: make(map[uuid.UUID]planmodel.BaselineTask, len(b.Snapshot.Tasks)),
		Milestones: b.Snapshot.Milestones, Truncated: b.Snapshot.Truncated}
	for _, t := range b.Snapshot.Tasks {
		out.Tasks[t.ID] = t
	}
	if out.Milestones == nil {
		out.Milestones = []planmodel.BaselineMilestone{}
	}
	return out, nil
}

// Slip is one task (or milestone) whose finish moved since the baseline.
type Slip struct {
	Kind           string    `json:"kind"` // task | milestone
	ID             uuid.UUID `json:"id"`
	Title          string    `json:"title"`
	Status         string    `json:"status,omitempty"`
	BaselineStart  *string   `json:"baseline_start,omitempty"`
	CurrentStart   *string   `json:"current_start,omitempty"`
	BaselineFinish *string   `json:"baseline_finish"`
	CurrentFinish  *string   `json:"current_finish"`
	StartSlipDays  int       `json:"start_slip_days"`
	FinishSlipDays int       `json:"finish_slip_days"`
	EstimateChange float64   `json:"estimate_change_hours"`
}

// SlipReport compares the live plan with a baseline.
type SlipReport struct {
	Baseline       BaselineSummary `json:"baseline"`
	Slips          []Slip          `json:"slips"`
	Compared       int             `json:"tasks_compared"`
	Slipped        int             `json:"slipped"`
	PulledIn       int             `json:"pulled_in"`
	MaxSlipDays    int             `json:"max_slip_days"`
	Removed        int             `json:"removed_since"`
	Added          int             `json:"added_since"`
	MilestonesLate int             `json:"milestones_slipped"`
}

func dateStr(t *time.Time) *string {
	if t == nil || t.IsZero() {
		return nil
	}
	s := t.UTC().Format(time.DateOnly)
	return &s
}

// DaysBetween is b minus a in whole calendar days (UTC dates).
func DaysBetween(a, b time.Time) int {
	da := time.Date(a.UTC().Year(), a.UTC().Month(), a.UTC().Day(), 0, 0, 0, 0, time.UTC)
	db := time.Date(b.UTC().Year(), b.UTC().Month(), b.UTC().Day(), 0, 0, 0, 0, time.UTC)
	return int(math.Round(db.Sub(da).Hours() / 24))
}

// CurrentTask is the live side of a slip comparison.
type CurrentTask struct {
	ID       uuid.UUID
	Title    string
	Status   string
	Start    *time.Time
	Due      *time.Time
	Estimate *float64
}

// CompareSlips lists the tasks and milestones whose finish moved (or that gained or lost a finish
// date), latest slip first, with the totals. Tasks deleted since the baseline count as removed,
// tasks created since as added.
func CompareSlips(snap planmodel.Snapshot, current []CurrentTask, milestones map[uuid.UUID]*time.Time, addedSince int) ([]Slip, SlipReport) {
	byID := make(map[uuid.UUID]CurrentTask, len(current))
	for _, c := range current {
		byID[c.ID] = c
	}
	var rep SlipReport
	rep.Added = addedSince
	slips := []Slip{}
	for _, b := range snap.Tasks {
		c, ok := byID[b.ID]
		if !ok {
			rep.Removed++
			continue
		}
		rep.Compared++
		sl := Slip{Kind: "task", ID: b.ID, Title: c.Title, Status: c.Status, BaselineStart: dateStr(b.StartDate),
			CurrentStart: dateStr(c.Start), BaselineFinish: dateStr(b.DueDate), CurrentFinish: dateStr(c.Due)}
		if b.StartDate != nil && c.Start != nil {
			sl.StartSlipDays = DaysBetween(*b.StartDate, *c.Start)
		}
		moved := false
		switch {
		case b.DueDate != nil && c.Due != nil:
			sl.FinishSlipDays = DaysBetween(*b.DueDate, *c.Due)
			moved = sl.FinishSlipDays != 0
		case (b.DueDate == nil) != (c.Due == nil):
			moved = true
		}
		if b.EstimatedHours != nil || c.Estimate != nil {
			var was, now float64
			if b.EstimatedHours != nil {
				was = *b.EstimatedHours
			}
			if c.Estimate != nil {
				now = *c.Estimate
			}
			sl.EstimateChange = round2(now - was)
		}
		if !moved {
			continue
		}
		if sl.FinishSlipDays > 0 {
			rep.Slipped++
		} else if sl.FinishSlipDays < 0 {
			rep.PulledIn++
		}
		if sl.FinishSlipDays > rep.MaxSlipDays {
			rep.MaxSlipDays = sl.FinishSlipDays
		}
		slips = append(slips, sl)
	}
	for _, m := range snap.Milestones {
		cur, ok := milestones[m.ID]
		if !ok || m.TargetDate == nil || cur == nil {
			continue
		}
		d := DaysBetween(*m.TargetDate, *cur)
		if d == 0 {
			continue
		}
		if d > 0 {
			rep.MilestonesLate++
		}
		slips = append(slips, Slip{Kind: "milestone", ID: m.ID, Title: m.Name, BaselineFinish: dateStr(m.TargetDate),
			CurrentFinish: dateStr(cur), FinishSlipDays: d})
	}
	sort.SliceStable(slips, func(i, j int) bool {
		if slips[i].FinishSlipDays != slips[j].FinishSlipDays {
			return slips[i].FinishSlipDays > slips[j].FinishSlipDays
		}
		return slips[i].Title < slips[j].Title
	})
	return slips, rep
}

// Slips compares the live plan with a baseline (nil id: the latest): one query for the tasks the
// baseline holds, one for the milestones, one count of tasks created since.
func (s *Service) Slips(ctx context.Context, tenantID, projectID uuid.UUID, id *uuid.UUID) (SlipReport, error) {
	b, err := s.GetBaseline(ctx, tenantID, projectID, id)
	if err != nil {
		return SlipReport{}, err
	}
	ids := make([]uuid.UUID, len(b.Snapshot.Tasks))
	for i, t := range b.Snapshot.Tasks {
		ids[i] = t.ID
	}
	var current []CurrentTask
	if len(ids) > 0 {
		rows, err := s.client.Task.Query().Where(enttask.TenantID(tenantID), enttask.ProjectID(projectID), enttask.IDIn(ids...)).
			Select(enttask.FieldID, enttask.FieldTitle, enttask.FieldStatus, enttask.FieldStartDate, enttask.FieldDueDate, enttask.FieldEstimatedHours).All(ctx)
		if err != nil {
			return SlipReport{}, fmt.Errorf("load tasks: %w", err)
		}
		for _, t := range rows {
			current = append(current, CurrentTask{ID: t.ID, Title: t.Title, Status: t.Status, Start: t.StartDate, Due: optTime(t.DueDate), Estimate: t.EstimatedHours})
		}
	}
	ms, err := s.client.Milestone.Query().Where(entmilestone.TenantID(tenantID), entmilestone.ProjectID(projectID)).
		Select(entmilestone.FieldID, entmilestone.FieldTargetDate).Limit(baselineMilestoneLimit).All(ctx)
	if err != nil {
		return SlipReport{}, fmt.Errorf("load milestones: %w", err)
	}
	mdates := make(map[uuid.UUID]*time.Time, len(ms))
	for _, m := range ms {
		mdates[m.ID] = optTime(m.TargetDate)
	}
	added, err := s.client.Task.Query().Where(enttask.TenantID(tenantID), enttask.ProjectID(projectID), enttask.CreatedAtGT(b.CreatedAt)).Count(ctx)
	if err != nil {
		return SlipReport{}, fmt.Errorf("count new tasks: %w", err)
	}
	slips, rep := CompareSlips(b.Snapshot, current, mdates, added)
	names, err := people.Lookup(ctx, s.client.ProjectsUser, tenantID, []uuid.UUID{b.CreatedBy})
	if err != nil {
		return SlipReport{}, err
	}
	rep.Baseline, rep.Slips = summaryOf(b, names), slips
	return rep, nil
}
