package portal

import (
	"context"
	"fmt"
	"time"

	"entgo.io/ent/dialect/sql"
	"entgo.io/ent/dialect/sql/sqljson"
	"github.com/google/uuid"

	"github.com/bengobox/projects-service/internal/ent"
	entactivity "github.com/bengobox/projects-service/internal/ent/activity"
	entmilestone "github.com/bengobox/projects-service/internal/ent/milestone"
	"github.com/bengobox/projects-service/internal/ent/predicate"
	entproject "github.com/bengobox/projects-service/internal/ent/project"
	entmember "github.com/bengobox/projects-service/internal/ent/projectmember"
	enttask "github.com/bengobox/projects-service/internal/ent/task"
	"github.com/bengobox/projects-service/internal/services/milestones"
	"github.com/bengobox/projects-service/internal/services/people"
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

// ListProjects returns one page of the tenant's projects where userID is an active client, newest
// first, with the total.
func (s *Service) ListProjects(ctx context.Context, tenantID, userID uuid.UUID, limit, offset int) ([]ProjectCard, int, error) {
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
	out := make([]ProjectCard, len(rows))
	for i, p := range rows {
		out[i] = card(p)
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

// milestoneDone reports a milestone that needs nothing more: completed, or approved by the client.
func milestoneDone(status string) bool {
	return status == "completed" || status == milestones.StatusApproved
}

// ProjectSummary is the progress of a project the caller is a client of: status, dates, percent
// of tasks done (every task counts, shared or not; only the counts leave), milestone counts and
// the next open milestone.
func (s *Service) ProjectSummary(ctx context.Context, tenantID, projectID uuid.UUID) (Summary, error) {
	p, err := s.client.Project.Query().Where(entproject.ID(projectID), entproject.TenantID(tenantID)).Only(ctx)
	if ent.IsNotFound(err) {
		return Summary{}, ErrNotFound
	}
	if err != nil {
		return Summary{}, fmt.Errorf("load project: %w", err)
	}
	out := Summary{ProjectCard: card(p)}
	taskQ := s.client.Task.Query().Where(enttask.TenantID(tenantID), enttask.ProjectID(projectID))
	if out.TotalTasks, err = taskQ.Clone().Count(ctx); err != nil {
		return Summary{}, fmt.Errorf("count tasks: %w", err)
	}
	if out.CompletedTasks, err = taskQ.Where(enttask.Status(tasks.StatusDone)).Count(ctx); err != nil {
		return Summary{}, fmt.Errorf("count done tasks: %w", err)
	}
	if out.TotalTasks > 0 {
		out.Progress = out.CompletedTasks * 100 / out.TotalTasks
	}
	ms, err := s.client.Milestone.Query().Where(entmilestone.TenantID(tenantID), entmilestone.ProjectID(projectID)).
		Order(ent.Asc(entmilestone.FieldTargetDate)).All(ctx)
	if err != nil {
		return Summary{}, fmt.Errorf("load milestones: %w", err)
	}
	out.TotalMilestones = len(ms)
	for _, m := range ms {
		switch {
		case milestoneDone(m.Status):
			out.CompletedMilestones++
		case m.Status == milestones.StatusReadyForReview:
			out.AwaitingReview++
		}
		if out.NextMilestone == nil && !milestoneDone(m.Status) {
			out.NextMilestone = &MilestoneBrief{ID: m.ID, Name: m.Name, Status: m.Status, TargetDate: datePtr(m.TargetDate)}
		}
	}
	return out, nil
}

// Decision is a client's latest verdict on a milestone.
type Decision struct {
	Decision      string `json:"decision"`
	Comment       string `json:"comment"`
	DecidedAt     string `json:"decided_at"`
	DecidedByName string `json:"decided_by_name"`
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
	CanDecide    bool      `json:"can_decide"`
	LastDecision *Decision `json:"last_decision"`
}

// Milestones returns the project's milestones by target date, each with the client's latest
// decision (from the activity entries Decide writes).
func (s *Service) Milestones(ctx context.Context, tenantID, projectID uuid.UUID) ([]MilestoneView, error) {
	ms, err := s.client.Milestone.Query().Where(entmilestone.TenantID(tenantID), entmilestone.ProjectID(projectID)).
		Order(ent.Asc(entmilestone.FieldTargetDate), ent.Asc(entmilestone.FieldID)).Limit(500).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list milestones: %w", err)
	}
	decisions, err := s.latestDecisions(ctx, tenantID, projectID)
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

// latestDecisions reads the project's client decision entries (milestone.updated with a
// client_decision change), newest first, and keeps the newest per milestone. Served by the
// (tenant_id, project_id, occurred_at) index.
func (s *Service) latestDecisions(ctx context.Context, tenantID, projectID uuid.UUID) (map[uuid.UUID]*Decision, error) {
	rows, err := s.client.Activity.Query().
		Where(entactivity.TenantID(tenantID), entactivity.ProjectID(projectID), entactivity.ActivityType("milestone.updated"),
			predicate.Activity(func(s *sql.Selector) {
				s.Where(sqljson.HasKey(s.C(entactivity.FieldPayload), sqljson.DotPath("changes."+milestones.ChangeClientDecision)))
			})).
		Order(ent.Desc(entactivity.FieldOccurredAt)).Limit(1000).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("load decisions: %w", err)
	}
	ids := make([]uuid.UUID, 0, len(rows))
	for _, a := range rows {
		ids = append(ids, a.UserID)
	}
	who, err := people.Lookup(ctx, s.client.ProjectsUser, tenantID, ids)
	if err != nil {
		return nil, err
	}
	out := map[uuid.UUID]*Decision{}
	for _, a := range rows {
		id, err := uuid.Parse(str(a.Payload, "entity_id"))
		if err != nil || out[id] != nil {
			continue
		}
		changes, _ := a.Payload["changes"].(map[string]any)
		out[id] = &Decision{
			Decision: str(changes, milestones.ChangeClientDecision), Comment: str(changes, "comment"),
			DecidedAt: str(changes, "decided_at"), DecidedByName: who[a.UserID].Label(),
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
