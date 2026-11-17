package portal

import (
	"context"
	"fmt"
	"time"

	"entgo.io/ent/dialect/sql"
	"entgo.io/ent/dialect/sql/sqljson"
	"github.com/google/uuid"

	"github.com/bengobox/projects-service/internal/ent"
	entmilestone "github.com/bengobox/projects-service/internal/ent/milestone"
	"github.com/bengobox/projects-service/internal/ent/predicate"
	entproject "github.com/bengobox/projects-service/internal/ent/project"
	entmember "github.com/bengobox/projects-service/internal/ent/projectmember"
	enttask "github.com/bengobox/projects-service/internal/ent/task"
	"github.com/bengobox/projects-service/internal/services/milestones"
	"github.com/bengobox/projects-service/internal/services/tasks"
)

// What a client reads is built field by field from the rows, never by returning a row: no
// budget, currency, cost, hours, estimates, margins, assignees, metadata or internal comments
// ever leave through these types.

// ProjectCard is a project in the client's list.
type ProjectCard struct {
	ID        uuid.UUID  `json:"id"`
	Name      string     `json:"name"`
	Status    string     `json:"status"`
	StartDate *time.Time `json:"start_date"`
	EndDate   *time.Time `json:"end_date"`
}

func datePtr(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

func card(p *ent.Project) ProjectCard {
	return ProjectCard{ID: p.ID, Name: p.Name, Status: p.Status, StartDate: datePtr(p.StartDate), EndDate: datePtr(p.EndDate)}
}

// clientOf matches projects where userID is an active client.
func clientOf(tenantID, userID uuid.UUID) predicate.Project {
	return entproject.HasMembersWith(clientRow(tenantID), entmember.UserID(userID))
}

// Progress is how far a project is, the same counts on the landing card and the project page:
// every task counts, shared or not (only the counts leave), and a milestone is done when it is
// completed or the client approved it. AwaitingReview counts the milestones waiting for the
// client's decision, so the landing card can say so without a summary per card.
type Progress struct {
	TasksTotal      int `json:"tasks_total"`
	TasksDone       int `json:"tasks_done"`
	Percent         int `json:"percent"`
	MilestonesTotal int `json:"milestones_total"`
	MilestonesDone  int `json:"milestones_done"`
	AwaitingReview  int `json:"awaiting_review"`
}

// ProjectListItem is a project in the client's list with its progress.
type ProjectListItem struct {
	ProjectCard
	Progress Progress `json:"progress"`
}

// Two grouped queries for a whole page of projects. tasks is read through its
// (tenant_id, project_id, status) index alone; milestones through (tenant_id, project_id,
// target_date).
const (
	taskProgressSQL = `
SELECT project_id, COUNT(*), COUNT(*) FILTER (WHERE status = $3)
  FROM tasks
 WHERE tenant_id = $1 AND project_id = ANY($2::uuid[])
 GROUP BY project_id`
	milestoneProgressSQL = `
SELECT project_id, COUNT(*), COUNT(*) FILTER (WHERE status = ANY($3::text[])), COUNT(*) FILTER (WHERE status = $4)
  FROM milestones
 WHERE tenant_id = $1 AND project_id = ANY($2::uuid[])
 GROUP BY project_id`
)

// progressOf computes the progress of each project in ids in two grouped queries, never one per
// project. A project with no tasks or milestones answers zeros.
func (s *Service) progressOf(ctx context.Context, tenantID uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]Progress, error) {
	out := make(map[uuid.UUID]Progress, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	list := make([]string, len(ids))
	for i, id := range ids {
		list[i] = id.String()
		out[id] = Progress{}
	}
	// run executes one grouped query (tenant and project ids first, then args) and hands each row's
	// scan to row.
	run := func(query string, args []any, row func(scan func(dest ...any) error) error) error {
		rows, err := s.client.QueryContext(ctx, query, append([]any{tenantID, list}, args...)...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			if err := row(rows.Scan); err != nil {
				return err
			}
		}
		return rows.Err()
	}
	if err := run(taskProgressSQL, []any{tasks.StatusDone}, func(scan func(...any) error) error {
		var id uuid.UUID
		var total, done int
		if err := scan(&id, &total, &done); err != nil {
			return err
		}
		p := out[id]
		p.TasksTotal, p.TasksDone = total, done
		if total > 0 {
			p.Percent = done * 100 / total
		}
		out[id] = p
		return nil
	}); err != nil {
		return nil, fmt.Errorf("count tasks: %w", err)
	}
	if err := run(milestoneProgressSQL, []any{doneMilestoneStatuses, milestones.StatusReadyForReview}, func(scan func(...any) error) error {
		var id uuid.UUID
		var total, done, awaiting int
		if err := scan(&id, &total, &done, &awaiting); err != nil {
			return err
		}
		p := out[id]
		p.MilestonesTotal, p.MilestonesDone, p.AwaitingReview = total, done, awaiting
		out[id] = p
		return nil
	}); err != nil {
		return nil, fmt.Errorf("count milestones: %w", err)
	}
	return out, nil
}

// ListProjects returns one page of the tenant's projects where userID is an active client, newest
// first, each with its progress, and the total.
func (s *Service) ListProjects(ctx context.Context, tenantID, userID uuid.UUID, limit, offset int) ([]ProjectListItem, int, error) {
	if limit <= 0 {
		limit = 20
	}
	q := s.client.Project.Query().Where(entproject.TenantID(tenantID), clientOf(tenantID, userID))
	total, err := q.Clone().Count(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("count client projects: %w", err)
	}
	rows, err := q.Order(ent.Desc(entproject.FieldCreatedAt), ent.Desc(entproject.FieldID)).Offset(offset).Limit(limit).All(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("list client projects: %w", err)
	}
	ids := make([]uuid.UUID, len(rows))
	for i, p := range rows {
		ids[i] = p.ID
	}
	progress, err := s.progressOf(ctx, tenantID, ids)
	if err != nil {
		return nil, 0, err
	}
	out := make([]ProjectListItem, len(rows))
	for i, p := range rows {
		out[i] = ProjectListItem{ProjectCard: card(p), Progress: progress[p.ID]}
	}
	return out, total, nil
}

// MilestoneBrief is the next milestone on the progress page.
type MilestoneBrief struct {
	ID         uuid.UUID  `json:"id"`
	Name       string     `json:"name"`
	Status     string     `json:"status"`
	TargetDate *time.Time `json:"target_date"`
}

// Summary is the client's progress page of one project.
type Summary struct {
	ProjectCard
	Progress            int             `json:"progress"`
	TotalTasks          int             `json:"total_tasks"`
	CompletedTasks      int             `json:"completed_tasks"`
	TotalMilestones     int             `json:"total_milestones"`
	CompletedMilestones int             `json:"completed_milestones"`
	AwaitingReview      int             `json:"awaiting_review"`
	NextMilestone       *MilestoneBrief `json:"next_milestone"`
}

// doneMilestoneStatuses are the statuses of a milestone that needs nothing more: completed, or
// approved by the client.
var doneMilestoneStatuses = []string{"completed", milestones.StatusApproved}

// ProjectSummary is the progress of a project the caller is a client of: status, dates, the same
// counts as its card in ListProjects (progressOf), how many milestones wait for review and the
// next open milestone.
func (s *Service) ProjectSummary(ctx context.Context, tenantID, projectID uuid.UUID) (Summary, error) {
	p, err := s.client.Project.Query().Where(entproject.ID(projectID), entproject.TenantID(tenantID)).Only(ctx)
	if ent.IsNotFound(err) {
		return Summary{}, ErrNotFound
	}
	if err != nil {
		return Summary{}, fmt.Errorf("load project: %w", err)
	}
	progress, err := s.progressOf(ctx, tenantID, []uuid.UUID{projectID})
	if err != nil {
		return Summary{}, err
	}
	pr := progress[projectID]
	out := Summary{
		ProjectCard: card(p), Progress: pr.Percent, TotalTasks: pr.TasksTotal, CompletedTasks: pr.TasksDone,
		TotalMilestones: pr.MilestonesTotal, CompletedMilestones: pr.MilestonesDone, AwaitingReview: pr.AwaitingReview,
	}
	next, err := s.client.Milestone.Query().
		Where(entmilestone.TenantID(tenantID), entmilestone.ProjectID(projectID), entmilestone.StatusNotIn(doneMilestoneStatuses...)).
		Order(ent.Asc(entmilestone.FieldTargetDate), ent.Asc(entmilestone.FieldID)).First(ctx)
	if err != nil && !ent.IsNotFound(err) {
		return Summary{}, fmt.Errorf("load next milestone: %w", err)
	}
	if next != nil {
		out.NextMilestone = &MilestoneBrief{ID: next.ID, Name: next.Name, Status: next.Status, TargetDate: datePtr(next.TargetDate)}
	}
	return out, nil
}

// MilestoneView is a milestone as a client sees it.
type MilestoneView struct {
	ID          uuid.UUID  `json:"id"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Status      string     `json:"status"`
	TargetDate  *time.Time `json:"target_date"`
	CompletedAt *time.Time `json:"completed_at"`
	// CanDecide is true while the milestone waits for the client's review.
	CanDecide    bool                 `json:"can_decide"`
	LastDecision *milestones.Decision `json:"last_decision"`
}

// Milestones returns the project's milestones by target date, each with the client's latest
// decision (milestones.LatestDecisions, the same reader as the team's milestone list).
func (s *Service) Milestones(ctx context.Context, tenantID, projectID uuid.UUID) ([]MilestoneView, error) {
	ms, err := s.client.Milestone.Query().Where(entmilestone.TenantID(tenantID), entmilestone.ProjectID(projectID)).
		Order(ent.Asc(entmilestone.FieldTargetDate), ent.Asc(entmilestone.FieldID)).Limit(500).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list milestones: %w", err)
	}
	decisions, err := milestones.LatestDecisions(ctx, s.client, tenantID, projectID)
	if err != nil {
		return nil, err
	}
	out := make([]MilestoneView, len(ms))
	for i, m := range ms {
		out[i] = MilestoneView{
			ID: m.ID, Name: m.Name, Description: m.Description, Status: m.Status,
			TargetDate: datePtr(m.TargetDate), CompletedAt: datePtr(m.CompletedAt),
			CanDecide: m.Status == milestones.StatusReadyForReview, LastDecision: decisions[m.ID],
		}
	}
	return out, nil
}

// SharedTask is a task the team shared with the client.
type SharedTask struct {
	ID          uuid.UUID  `json:"id"`
	Title       string     `json:"title"`
	Description string     `json:"description"`
	Status      string     `json:"status"`
	StatusLabel string     `json:"status_label"`
	StartDate   *time.Time `json:"start_date"`
	DueDate     *time.Time `json:"due_date"`
	Progress    int        `json:"progress"`
	CompletedAt *time.Time `json:"completed_at"`
}

// sharedTask matches tasks whose metadata shares them with the client.
func sharedTask() predicate.Task {
	return predicate.Task(func(s *sql.Selector) {
		s.Where(sqljson.ValueEQ(s.C(enttask.FieldMetadata), true, sqljson.Path(tasks.MetaClientVisible)))
	})
}

// SharedTasks returns one page of the project's tasks shared with its clients, by due date, with
// the total.
func (s *Service) SharedTasks(ctx context.Context, tenantID, projectID uuid.UUID, limit, offset int) ([]SharedTask, int, error) {
	p, err := s.client.Project.Query().Where(entproject.ID(projectID), entproject.TenantID(tenantID)).Only(ctx)
	if ent.IsNotFound(err) {
		return nil, 0, ErrNotFound
	}
	if err != nil {
		return nil, 0, fmt.Errorf("load project: %w", err)
	}
	if limit <= 0 {
		limit = 20
	}
	q := s.client.Task.Query().Where(enttask.TenantID(tenantID), enttask.ProjectID(projectID), sharedTask())
	total, err := q.Clone().Count(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("count shared tasks: %w", err)
	}
	rows, err := q.Order(ent.Asc(enttask.FieldDueDate), ent.Asc(enttask.FieldID)).Offset(offset).Limit(limit).All(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("list shared tasks: %w", err)
	}
	out := make([]SharedTask, len(rows))
	for i, t := range rows {
		st := SharedTask{
			ID: t.ID, Title: t.Title, Description: t.Description, Status: t.Status,
			StatusLabel: tasks.StatusLabel(p, t.Status), DueDate: datePtr(t.DueDate),
			Progress: t.ProgressPct, CompletedAt: datePtr(t.CompletedAt),
		}
		if t.StartDate != nil {
			st.StartDate = datePtr(*t.StartDate)
		}
		if t.Status == tasks.StatusDone {
			st.Progress = 100
		}
		out[i] = st
	}
	return out, total, nil
}
