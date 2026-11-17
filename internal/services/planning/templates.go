package planning

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"entgo.io/ent/dialect/sql"
	"github.com/google/uuid"

	"github.com/bengobox/projects-service/internal/ent"
	entmilestone "github.com/bengobox/projects-service/internal/ent/milestone"
	entmember "github.com/bengobox/projects-service/internal/ent/projectmember"
	enttemplate "github.com/bengobox/projects-service/internal/ent/projecttemplate"
	enttask "github.com/bengobox/projects-service/internal/ent/task"
	entassignee "github.com/bengobox/projects-service/internal/ent/taskassignee"
	enttaskdep "github.com/bengobox/projects-service/internal/ent/taskdependency"
	"github.com/bengobox/projects-service/internal/modules/rbac"
	"github.com/bengobox/projects-service/internal/planmodel"
	"github.com/bengobox/projects-service/internal/platform/events"
	"github.com/bengobox/projects-service/internal/services/activity"
	"github.com/bengobox/projects-service/internal/services/projects"
	"github.com/bengobox/projects-service/internal/services/tasks"
	"github.com/bengobox/projects-service/internal/services/txn"
)

// Project templates (sprint 16). A template is a plan without dates: phases (milestones) and
// tasks placed by day offsets from the project start with a duration, task parents and
// dependencies by reference, the roles the work needs, and the board statuses and status rules.
// Starting a project from a template creates the project and its whole plan in one transaction.
const (
	MaxTemplateTasks    = 500
	maxTemplatePhases   = 50
	maxTemplateRoles    = 30
	MaxTenantTemplates  = 200
	maxTemplateOffset   = 3650
	maxTemplateDuration = 730
)

// Template events (subject project.template.<type>).
const (
	EventTemplateSaved   = "template.saved"
	EventTemplateDeleted = "template.deleted"
	EventTemplateApplied = "template.applied"
)

// TemplateSummary is a template without its definition (the list).
type TemplateSummary struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Category    string    `json:"category"`
	System      bool      `json:"system"`
	TaskCount   int       `json:"task_count"`
	PhaseCount  int       `json:"phase_count"`
	Duration    int       `json:"duration_days"`
	CreatedAt   time.Time `json:"created_at"`
}

// Template is a template with its definition.
type Template struct {
	TemplateSummary
	Definition planmodel.TemplateDefinition `json:"definition"`
}

func templateOf(t *ent.ProjectTemplate) Template {
	return Template{TemplateSummary: TemplateSummary{ID: t.ID, Name: t.Name, Description: t.Description, Category: t.Category,
		System: t.TenantID == nil, TaskCount: t.TaskCount, PhaseCount: len(t.Definition.Phases),
		Duration: t.Definition.DurationDays, CreatedAt: t.CreatedAt}, Definition: t.Definition}
}

// ValidateDefinition checks a template plan: sizes, unique refs, refs that resolve, offsets and
// durations in range, no dependency on itself. It fills DurationDays.
func ValidateDefinition(d *planmodel.TemplateDefinition) error {
	if len(d.Tasks) == 0 {
		return ValidationError("a template needs at least one task")
	}
	if len(d.Tasks) > MaxTemplateTasks || len(d.Phases) > maxTemplatePhases || len(d.Roles) > maxTemplateRoles {
		return ValidationError(fmt.Sprintf("a template holds at most %d tasks, %d phases and %d roles", MaxTemplateTasks, maxTemplatePhases, maxTemplateRoles))
	}
	phases := map[string]bool{}
	for _, p := range d.Phases {
		if p.Ref == "" || strings.TrimSpace(p.Name) == "" || phases[p.Ref] {
			return ValidationError("every phase needs a unique ref and a name")
		}
		if p.OffsetDays < 0 || p.OffsetDays > maxTemplateOffset {
			return ValidationError(fmt.Sprintf("phase %q: offset_days must be 0-%d", p.Name, maxTemplateOffset))
		}
		phases[p.Ref] = true
	}
	refs := map[string]bool{}
	for _, t := range d.Tasks {
		if t.Ref == "" || strings.TrimSpace(t.Title) == "" || refs[t.Ref] {
			return ValidationError("every task needs a unique ref and a title")
		}
		refs[t.Ref] = true
	}
	end := 0
	for _, p := range d.Phases {
		end = max(end, p.OffsetDays+1)
	}
	for _, t := range d.Tasks {
		if t.OffsetDays < 0 || t.OffsetDays > maxTemplateOffset || t.DurationDays < 1 || t.DurationDays > maxTemplateDuration {
			return ValidationError(fmt.Sprintf("task %q: offset_days 0-%d and duration_days 1-%d", t.Title, maxTemplateOffset, maxTemplateDuration))
		}
		if t.PhaseRef != "" && !phases[t.PhaseRef] {
			return ValidationError(fmt.Sprintf("task %q: unknown phase %q", t.Title, t.PhaseRef))
		}
		if t.ParentRef != "" && (!refs[t.ParentRef] || t.ParentRef == t.Ref) {
			return ValidationError(fmt.Sprintf("task %q: unknown parent %q", t.Title, t.ParentRef))
		}
		for _, dep := range t.DependsOn {
			if !refs[dep] || dep == t.Ref {
				return ValidationError(fmt.Sprintf("task %q: unknown dependency %q", t.Title, dep))
			}
		}
		if t.EstimatedHours != nil && (*t.EstimatedHours < 0 || *t.EstimatedHours > 10000) {
			return ValidationError(fmt.Sprintf("task %q: estimated_hours 0-10000", t.Title))
		}
		end = max(end, t.OffsetDays+t.DurationDays)
	}
	for _, r := range d.Roles {
		if r.Code == "" || r.Count < 0 {
			return ValidationError("every role needs a code and a count of zero or more")
		}
	}
	d.DurationDays = end
	return nil
}

// ListTemplates returns one page of the system templates and the tenant's, system first then by
// name (?q matches the name).
func (s *Service) ListTemplates(ctx context.Context, tenantID uuid.UUID, q string, limit, offset int) ([]TemplateSummary, int, error) {
	query := s.client.ProjectTemplate.Query().Where(enttemplate.Or(enttemplate.TenantIDIsNil(), enttemplate.TenantID(tenantID)))
	if q = strings.TrimSpace(q); q != "" {
		query = query.Where(enttemplate.NameContainsFold(q))
	}
	total, err := query.Clone().Count(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("count templates: %w", err)
	}
	if limit <= 0 {
		limit = 20
	}
	rows, err := query.Order(enttemplate.ByTenantID(sql.OrderNullsFirst()), ent.Asc(enttemplate.FieldName), ent.Asc(enttemplate.FieldID)).
		Offset(offset).Limit(limit).All(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("list templates: %w", err)
	}
	out := make([]TemplateSummary, len(rows))
	for i, t := range rows {
		out[i] = templateOf(t).TemplateSummary
	}
	return out, total, nil
}

// GetTemplate returns a system template or one of the tenant's.
func (s *Service) GetTemplate(ctx context.Context, tenantID, id uuid.UUID) (Template, error) {
	t, err := s.client.ProjectTemplate.Query().
		Where(enttemplate.ID(id), enttemplate.Or(enttemplate.TenantIDIsNil(), enttemplate.TenantID(tenantID))).Only(ctx)
	if ent.IsNotFound(err) {
		return Template{}, ErrNotFound
	}
	if err != nil {
		return Template{}, fmt.Errorf("load template: %w", err)
	}
	return templateOf(t), nil
}

// DeleteTemplate removes one of the tenant's templates (system ones cannot be removed).
func (s *Service) DeleteTemplate(ctx context.Context, tenantID, id uuid.UUID) error {
	return txn.Run(ctx, s.client, func(tx *ent.Tx) error {
		t, err := tx.ProjectTemplate.Query().
			Where(enttemplate.ID(id), enttemplate.Or(enttemplate.TenantIDIsNil(), enttemplate.TenantID(tenantID))).Only(ctx)
		if ent.IsNotFound(err) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("load template: %w", err)
		}
		if t.TenantID == nil {
			return ErrSystemTemplate
		}
		if err := tx.ProjectTemplate.DeleteOne(t).Exec(ctx); err != nil {
			return fmt.Errorf("delete template: %w", err)
		}
		return events.Write(ctx, tx, events.AggregateProject, EventTemplateDeleted, id, tenantID, map[string]any{
			"template_id": id.String(), "name": t.Name, "actor_id": actor(ctx),
		})
	})
}

// SaveAsTemplateInput names the template a project is saved as.
type SaveAsTemplateInput struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Category    string `json:"category"`
}

// BuildDefinition turns a project's plan into a template plan, offsets counted from start: the
// milestones become phases, the tasks keep their offset, length, estimate, parent, dependencies
// and the role of their primary assignee; roles count the team by role.
func BuildDefinition(start time.Time, ms []*ent.Milestone, ts []*ent.Task, deps []*ent.TaskDependency, taskRole map[uuid.UUID]string, roleCounts map[string]int) planmodel.TemplateDefinition {
	d := planmodel.TemplateDefinition{Phases: []planmodel.TemplatePhase{}, Tasks: []planmodel.TemplateTask{}, Roles: []planmodel.TemplateRole{}}
	offset := func(t time.Time) int { return max(0, DaysBetween(start, t)) }
	for i, m := range ms {
		ph := planmodel.TemplatePhase{Ref: fmt.Sprintf("p%d", i+1), Name: m.Name, Description: m.Description}
		if !m.TargetDate.IsZero() {
			ph.OffsetDays = offset(m.TargetDate)
		}
		d.Phases = append(d.Phases, ph)
	}
	refs := make(map[uuid.UUID]string, len(ts))
	for i, t := range ts {
		refs[t.ID] = fmt.Sprintf("t%d", i+1)
	}
	depsOf := map[uuid.UUID][]string{}
	for _, dep := range deps {
		if r, ok := refs[dep.DependsOnTaskID]; ok {
			depsOf[dep.TaskID] = append(depsOf[dep.TaskID], r)
		}
	}
	for _, t := range ts {
		tt := planmodel.TemplateTask{Ref: refs[t.ID], Title: t.Title, Description: t.Description, Priority: t.Priority,
			EstimatedHours: t.EstimatedHours, DependsOn: depsOf[t.ID], Role: taskRole[t.ID], WbsCode: t.WbsCode, DurationDays: 1}
		switch {
		case t.StartDate != nil && !t.StartDate.IsZero():
			tt.OffsetDays = offset(*t.StartDate)
			if !t.DueDate.IsZero() {
				tt.DurationDays = max(1, DaysBetween(*t.StartDate, t.DueDate)+1)
			}
		case !t.DueDate.IsZero():
			tt.OffsetDays = offset(t.DueDate)
		}
		if t.ParentID != uuid.Nil {
			tt.ParentRef = refs[t.ParentID]
		}
		d.Tasks = append(d.Tasks, tt)
	}
	codes := make([]string, 0, len(roleCounts))
	for c := range roleCounts {
		codes = append(codes, c)
	}
	sort.Strings(codes)
	for _, c := range codes {
		d.Roles = append(d.Roles, planmodel.TemplateRole{Code: c, Label: roleLabel(c), Count: roleCounts[c]})
	}
	return d
}

func roleLabel(code string) string {
	for _, r := range rbac.Roles {
		if r.Code == code {
			return r.Name
		}
	}
	return strings.ReplaceAll(code, "_", " ")
}

// SaveAsTemplate saves a project's plan as a tenant template (at most MaxTemplateTasks tasks,
// oldest first). Five batched reads: project, milestones, tasks, dependencies, assignees and team.
func (s *Service) SaveAsTemplate(ctx context.Context, tenantID, projectID uuid.UUID, in SaveAsTemplateInput) (Template, error) {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || len([]rune(in.Name)) > 120 {
		return Template{}, ValidationError("name is required (at most 120 characters)")
	}
	var out *ent.ProjectTemplate
	err := txn.Run(ctx, s.client, func(tx *ent.Tx) error {
		p, err := s.project(ctx, tx.Project, tenantID, projectID)
		if err != nil {
			return err
		}
		n, err := tx.ProjectTemplate.Query().Where(enttemplate.TenantID(tenantID)).Count(ctx)
		if err != nil {
			return fmt.Errorf("count templates: %w", err)
		}
		if n >= MaxTenantTemplates {
			return ValidationError(fmt.Sprintf("an organisation keeps at most %d templates", MaxTenantTemplates))
		}
		ts, err := tx.Task.Query().Where(enttask.TenantID(tenantID), enttask.ProjectID(projectID)).
			Order(ent.Asc(enttask.FieldCreatedAt), ent.Asc(enttask.FieldID)).Limit(MaxTemplateTasks + 1).All(ctx)
		if err != nil {
			return fmt.Errorf("load tasks: %w", err)
		}
		if len(ts) == 0 {
			return ValidationError("the project has no tasks to save")
		}
		if len(ts) > MaxTemplateTasks {
			return ValidationError(fmt.Sprintf("a template holds at most %d tasks", MaxTemplateTasks))
		}
		ms, err := tx.Milestone.Query().Where(entmilestone.TenantID(tenantID), entmilestone.ProjectID(projectID)).
			Order(ent.Asc(entmilestone.FieldTargetDate)).Limit(maxTemplatePhases).All(ctx)
		if err != nil {
			return fmt.Errorf("load milestones: %w", err)
		}
		ids := make([]uuid.UUID, len(ts))
		for i, t := range ts {
			ids[i] = t.ID
		}
		deps, err := tx.TaskDependency.Query().Where(enttaskdep.TaskIDIn(ids...)).All(ctx)
		if err != nil {
			return fmt.Errorf("load dependencies: %w", err)
		}
		primaries, err := tx.TaskAssignee.Query().Where(entassignee.TenantID(tenantID), entassignee.TaskIDIn(ids...), entassignee.IsPrimary(true)).All(ctx)
		if err != nil {
			return fmt.Errorf("load assignees: %w", err)
		}
		members, err := tx.ProjectMember.Query().Where(entmember.TenantID(tenantID), entmember.ProjectID(projectID),
			entmember.LeftAtIsNil(), entmember.RoleCodeNEQ(rbac.RoleClient)).All(ctx)
		if err != nil {
			return fmt.Errorf("load team: %w", err)
		}
		roleOf := map[uuid.UUID]string{p.OwnerID: rbac.RoleProjectManager}
		counts := map[string]int{rbac.RoleProjectManager: 1}
		for _, m := range members {
			if m.UserID == p.OwnerID {
				continue
			}
			roleOf[m.UserID] = m.RoleCode
			counts[m.RoleCode]++
		}
		taskRole := map[uuid.UUID]string{}
		for _, a := range primaries {
			taskRole[a.TaskID] = roleOf[a.UserID]
		}
		start := p.StartDate
		if start.IsZero() {
			start = earliest(ts, p.CreatedAt)
		}
		def := BuildDefinition(start, ms, ts, deps, taskRole, counts)
		if raw, ok := p.Metadata[tasks.MetaStatuses]; ok {
			def.Statuses, _ = json.Marshal(raw)
		}
		if raw, ok := p.Metadata[tasks.MetaStatusRules]; ok {
			def.StatusRules, _ = json.Marshal(raw)
		}
		if err := ValidateDefinition(&def); err != nil {
			return err
		}
		actorID := activity.ActorFrom(ctx)
		c := tx.ProjectTemplate.Create().SetTenantID(tenantID).SetName(in.Name).SetDescription(in.Description).
			SetCategory(strings.TrimSpace(in.Category)).SetDefinition(def).SetTaskCount(len(def.Tasks)).SetSourceProjectID(projectID)
		if actorID != uuid.Nil {
			c.SetCreatedBy(actorID)
		}
		if out, err = c.Save(ctx); err != nil {
			return fmt.Errorf("save template: %w", err)
		}
		if err := record(ctx, tx, tenantID, projectID, "template", out.ID, activity.ActionCreated,
			map[string]any{"name": out.Name, "tasks": out.TaskCount}); err != nil {
			return err
		}
		return events.Write(ctx, tx, events.AggregateProject, EventTemplateSaved, out.ID, tenantID, map[string]any{
			"template_id": out.ID.String(), "name": out.Name, "source_project_id": projectID.String(),
			"task_count": out.TaskCount, "phase_count": len(def.Phases), "actor_id": actor(ctx),
		})
	})
	if err != nil {
		return Template{}, err
	}
	return templateOf(out), nil
}

// earliest is the first start or due date among the tasks, else fallback.
func earliest(ts []*ent.Task, fallback time.Time) time.Time {
	var out time.Time
	for _, t := range ts {
		for _, d := range []*time.Time{t.StartDate, optTime(t.DueDate)} {
			if d != nil && !d.IsZero() && (out.IsZero() || d.Before(out)) {
				out = *d
			}
		}
	}
	if out.IsZero() {
		return fallback
	}
	return out
}

// FromTemplateInput starts a project from a template.
type FromTemplateInput struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	StartDate   tasks.Date     `json:"start_date"`
	Currency    string         `json:"currency"`
	Metadata    map[string]any `json:"metadata"`
}

// FromTemplateResult is the new project with what was created.
type FromTemplateResult struct {
	Project    *ent.Project             `json:"project"`
	Tasks      int                      `json:"task_count"`
	Milestones int                      `json:"milestone_count"`
	Roles      []planmodel.TemplateRole `json:"roles"`
}

// CreateFromTemplate creates a project owned by the signed-in user with the template's plan laid
// out from the start date, its board statuses and status rules, in one transaction.
func (s *Service) CreateFromTemplate(ctx context.Context, tenantID, templateID uuid.UUID, in FromTemplateInput) (FromTemplateResult, error) {
	owner := activity.ActorFrom(ctx)
	if owner == uuid.Nil {
		return FromTemplateResult{}, ValidationError("a project needs a signed-in owner")
	}
	if in.StartDate.IsZero() {
		return FromTemplateResult{}, ValidationError("start_date is required")
	}
	tpl, err := s.GetTemplate(ctx, tenantID, templateID)
	if err != nil {
		return FromTemplateResult{}, err
	}
	def := tpl.Definition
	if err := ValidateDefinition(&def); err != nil {
		return FromTemplateResult{}, err
	}
	start := time.Date(in.StartDate.Year(), in.StartDate.Month(), in.StartDate.Day(), 0, 0, 0, 0, time.UTC)
	end := TemplateEnd(def, start)
	name := strings.TrimSpace(in.Name)
	if name == "" {
		name = tpl.Name
	}
	var res FromTemplateResult
	err = txn.Run(ctx, s.client, func(tx *ent.Tx) error {
		p, err := projects.CreateProjectTx(ctx, tx, tenantID, projects.CreateProjectInput{
			Name: name, Description: in.Description, StartDate: &start, EndDate: &end, Currency: in.Currency,
			OwnerID: owner, Metadata: in.Metadata,
		})
		if err != nil {
			return err
		}
		tpl.Definition = def
		res, err = ApplyTemplateTx(ctx, tx, tenantID, p, tpl, start)
		return err
	})
	if res.Roles == nil {
		res.Roles = []planmodel.TemplateRole{}
	}
	return res, err
}

// TemplateEnd is the last day of a template's plan laid out from start (tpl.Definition must have
// passed ValidateDefinition).
func TemplateEnd(def planmodel.TemplateDefinition, start time.Time) time.Time {
	return start.AddDate(0, 0, max(def.DurationDays, 1)-1)
}

// ApplyTemplateTx lays a template's plan out on project p from start inside the caller's
// transaction: the board statuses and status rules on the project, the phases as milestones, the
// tasks with their parents and dependencies, one activity entry and project.template.applied.
// tpl.Definition must have passed ValidateDefinition. A tender conversion (sprint 2) uses it
// on the project it has just created.
func ApplyTemplateTx(ctx context.Context, tx *ent.Tx, tenantID uuid.UUID, p *ent.Project, tpl Template, start time.Time) (FromTemplateResult, error) {
	def := tpl.Definition
	var res FromTemplateResult
	err := func() error {
		var err error
		meta := map[string]any{}
		for k, v := range p.Metadata {
			meta[k] = v
		}
		meta["template_id"] = tpl.ID.String()
		if len(def.Statuses) > 0 {
			var list []tasks.Status
			if json.Unmarshal(def.Statuses, &list) == nil {
				if valid, verr := tasks.ValidateStatuses(list); verr == nil {
					meta[tasks.MetaStatuses] = valid
				}
			}
		}
		if len(def.StatusRules) > 0 {
			var rules map[string]any
			if json.Unmarshal(def.StatusRules, &rules) == nil {
				meta[tasks.MetaStatusRules] = rules
			}
		}
		if p, err = p.Update().SetMetadata(meta).Save(ctx); err != nil {
			return fmt.Errorf("save project plan settings: %w", err)
		}
		firstStatus := "todo"
		if list, ok := meta[tasks.MetaStatuses].([]tasks.Status); ok && len(list) > 0 {
			firstStatus = list[0].Key
		}
		mbulk := make([]*ent.MilestoneCreate, len(def.Phases))
		for i, ph := range def.Phases {
			mbulk[i] = tx.Milestone.Create().SetTenantID(tenantID).SetProjectID(p.ID).SetName(ph.Name).
				SetDescription(ph.Description).SetTargetDate(start.AddDate(0, 0, ph.OffsetDays))
		}
		if len(mbulk) > 0 {
			if _, err := tx.Milestone.CreateBulk(mbulk...).Save(ctx); err != nil {
				return fmt.Errorf("create milestones: %w", err)
			}
		}
		ids := make(map[string]uuid.UUID, len(def.Tasks))
		for _, t := range def.Tasks {
			ids[t.Ref] = uuid.New()
		}
		tbulk := make([]*ent.TaskCreate, len(def.Tasks))
		for i, t := range def.Tasks {
			priority := t.Priority
			if priority == "" {
				priority = "medium"
			}
			ts := start.AddDate(0, 0, t.OffsetDays)
			c := tx.Task.Create().SetID(ids[t.Ref]).SetTenantID(tenantID).SetProjectID(p.ID).SetTitle(t.Title).
				SetDescription(t.Description).SetStatus(firstStatus).SetPriority(priority).
				SetStartDate(ts).SetDueDate(ts.AddDate(0, 0, t.DurationDays-1))
			if t.EstimatedHours != nil {
				c.SetEstimatedHours(*t.EstimatedHours)
			}
			if t.ParentRef != "" {
				c.SetParentID(ids[t.ParentRef])
			}
			if t.WbsCode != "" {
				c.SetWbsCode(t.WbsCode)
			}
			if t.Role != "" {
				c.SetMetadata(map[string]any{"template_role": t.Role})
			}
			tbulk[i] = c
		}
		if _, err := tx.Task.CreateBulk(tbulk...).Save(ctx); err != nil {
			return fmt.Errorf("create tasks: %w", err)
		}
		var dbulk []*ent.TaskDependencyCreate
		for _, t := range def.Tasks {
			for _, dep := range t.DependsOn {
				dbulk = append(dbulk, tx.TaskDependency.Create().SetTaskID(ids[t.Ref]).SetDependsOnTaskID(ids[dep]).SetDependencyType("FS"))
			}
		}
		if len(dbulk) > 0 {
			if _, err := tx.TaskDependency.CreateBulk(dbulk...).Save(ctx); err != nil {
				return fmt.Errorf("create dependencies: %w", err)
			}
		}
		if err := record(ctx, tx, tenantID, p.ID, "project", p.ID, activity.ActionUpdated,
			map[string]any{"template_id": tpl.ID.String(), "tasks": len(def.Tasks), "milestones": len(def.Phases)}); err != nil {
			return err
		}
		res = FromTemplateResult{Project: p, Tasks: len(def.Tasks), Milestones: len(def.Phases), Roles: def.Roles}
		return events.Write(ctx, tx, events.AggregateProject, EventTemplateApplied, p.ID, tenantID, map[string]any{
			"project_id": p.ID.String(), "template_id": tpl.ID.String(), "template_name": tpl.Name,
			"task_count": len(def.Tasks), "milestone_count": len(def.Phases), "start_date": start.Format(time.DateOnly),
			"actor_id": actor(ctx),
		})
	}()
	if res.Roles == nil {
		res.Roles = []planmodel.TemplateRole{}
	}
	return res, err
}
