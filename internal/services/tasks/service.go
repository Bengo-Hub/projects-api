package tasks

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	sharedcache "github.com/Bengo-Hub/cache"
	"github.com/bengobox/projects-service/internal/ent"
	entcomment "github.com/bengobox/projects-service/internal/ent/comment"
	enttask "github.com/bengobox/projects-service/internal/ent/task"
	entassignee "github.com/bengobox/projects-service/internal/ent/taskassignee"
	enttaskdep "github.com/bengobox/projects-service/internal/ent/taskdependency"
	"github.com/bengobox/projects-service/internal/platform/realtime"
	"github.com/bengobox/projects-service/internal/services/activity"
	"github.com/bengobox/projects-service/internal/services/comments"
	"github.com/bengobox/projects-service/internal/services/notify"
	"github.com/bengobox/projects-service/internal/services/txn"
)

// Service handles task CRUD, assignees, board statuses, scheduling and dependency operations.
type Service struct {
	client *ent.Client
	cache  *sharedcache.Aside
	log    *zap.Logger
	live   realtime.Publisher
}

// NewService creates a new tasks service.
func NewService(client *ent.Client, cache *sharedcache.Aside, log *zap.Logger) *Service {
	return &Service{client: client, cache: cache, log: log.Named("tasks.svc")}
}

// WithRealtime sets where change hints go after each commit (the project SSE streams).
func (s *Service) WithRealtime(p realtime.Publisher) *Service {
	s.live = p
	return s
}

// emit sends a change hint for a task after its transaction committed.
func (s *Service) emit(ctx context.Context, tenantID uuid.UUID, typ string, t *ent.Task) {
	realtime.Emit(s.live, tenantID, realtime.Event{
		Type: typ, ProjectID: t.ProjectID.String(), TaskID: t.ID.String(), Status: t.Status,
		ActorID: actorString(ctx),
	})
}

func actorString(ctx context.Context) string {
	if id := activity.ActorFrom(ctx); id != uuid.Nil {
		return id.String()
	}
	return ""
}

// CreateTaskInput holds the data needed to create a task. AssigneeIDs lists the assignees, the
// first being the primary.
type CreateTaskInput struct {
	Title          string         `json:"title"`
	Description    string         `json:"description"`
	Status         string         `json:"status"`
	Priority       string         `json:"priority"`
	AssigneeIDs    []uuid.UUID    `json:"assignee_ids"`
	DueDate        *Date          `json:"due_date"`
	StartDate      *Date          `json:"start_date"`
	EstimatedHours *float64       `json:"estimated_hours"`
	ProgressPct    *int           `json:"progress_pct"`
	ParentID       *uuid.UUID     `json:"parent_id"`
	WbsCode        string         `json:"wbs_code"`
	Metadata       map[string]any `json:"metadata"`
}

// UpdateTaskInput holds the data needed to update a task. AssigneeIDs, when present, replaces the
// assignees (first is primary; an empty list unassigns everyone).
type UpdateTaskInput struct {
	Title          *string        `json:"title"`
	Description    *string        `json:"description"`
	Status         *string        `json:"status"`
	Priority       *string        `json:"priority"`
	AssigneeIDs    *[]uuid.UUID   `json:"assignee_ids"`
	DueDate        *Date          `json:"due_date"`
	StartDate      *Date          `json:"start_date"`
	EstimatedHours *float64       `json:"estimated_hours"`
	ProgressPct    *int           `json:"progress_pct"`
	ParentID       *uuid.UUID     `json:"parent_id"`
	WbsCode        *string        `json:"wbs_code"`
	Metadata       map[string]any `json:"metadata"`
}

// ListTasksFilter holds filter options for listing tasks. Limit and Offset come from the shared
// pagination parser. Assignee matches any assignee, primary or not. DueFrom and DueTo bound the due
// date (both inclusive) for the calendar.
type ListTasksFilter struct {
	Status   string
	Priority string
	Assignee *uuid.UUID
	// Parent lists one task's subtasks (served by the (tenant_id, parent_id) index).
	Parent  *uuid.UUID
	DueFrom *time.Time
	DueTo   *time.Time
	Limit   int
	Offset  int
}

// AddDependencyInput holds data to add a task dependency.
type AddDependencyInput struct {
	DependsOnTaskID uuid.UUID `json:"depends_on_task_id"`
	DependencyType  string    `json:"dependency_type"`
}

// ListTasks returns one page of the project's tasks with the total, each with its assignees. A
// status filter gives one board column; each column pages on its own.
func (s *Service) ListTasks(ctx context.Context, tenantID, projectID uuid.UUID, filter ListTasksFilter) ([]*ent.Task, int, error) {
	if filter.Limit <= 0 {
		filter.Limit = 20
	}
	q := s.client.Task.Query().Where(enttask.TenantID(tenantID), enttask.ProjectID(projectID))
	if filter.Status != "" {
		q = q.Where(enttask.Status(filter.Status))
	}
	if filter.Priority != "" {
		q = q.Where(enttask.Priority(filter.Priority))
	}
	if filter.Assignee != nil {
		q = q.Where(assignedTo(tenantID, *filter.Assignee))
	}
	if filter.Parent != nil {
		q = q.Where(enttask.ParentID(*filter.Parent))
	}
	q = q.Where(dueRange(filter.DueFrom, filter.DueTo)...)
	total, err := q.Clone().Count(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("count tasks: %w", err)
	}
	if filter.DueFrom != nil || filter.DueTo != nil {
		q = q.Order(enttask.ByDueDate(), enttask.ByID())
	} else {
		q = q.Order(enttask.ByCreatedAt(), enttask.ByID())
	}
	items, err := q.WithAssignees(assigneeOrder).Offset(filter.Offset).Limit(filter.Limit).All(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("list tasks: %w", err)
	}
	return items, total, nil
}

// TaskDetail is a task with its dependencies and assignees (under edges), its latest comments
// oldest first and the task's comment count. Older comments come from the paged task comments route.
type TaskDetail struct {
	*ent.Task
	Comments      []comments.Authored `json:"comments"`
	CommentsTotal int                 `json:"comments_total"`
}

// GetTask fetches a single task with its dependencies, assignees and its latest 50 comments.
func (s *Service) GetTask(ctx context.Context, tenantID, projectID, taskID uuid.UUID) (*TaskDetail, error) {
	t, err := s.client.Task.Query().
		Where(enttask.ID(taskID), enttask.TenantID(tenantID), enttask.ProjectID(projectID)).
		WithDependencies().
		WithAssignees(assigneeOrder).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get task: %w", err)
	}
	cq := s.client.Comment.Query().Where(entcomment.TenantID(tenantID), entcomment.TaskID(taskID))
	total, err := cq.Clone().Count(ctx)
	if err != nil {
		return nil, fmt.Errorf("count task comments: %w", err)
	}
	latest, err := cq.Order(ent.Desc(entcomment.FieldCreatedAt)).Limit(taskCommentsLimit).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("task comments: %w", err)
	}
	slices.Reverse(latest)
	withNames, err := comments.WithAuthors(ctx, s.client, tenantID, latest)
	if err != nil {
		return nil, err
	}
	return &TaskDetail{Task: t, Comments: withNames, CommentsTotal: total}, nil
}

// notice builds the common part of a person-addressed event for a task.
func notice(ctx context.Context, tx *ent.Tx, p *ent.Project, t *ent.Task) (notify.Notice, error) {
	actor, err := notify.Actor(ctx, tx, p.TenantID, activity.ActorFrom(ctx))
	if err != nil {
		return notify.Notice{}, err
	}
	return notify.Notice{
		TenantID: p.TenantID, ProjectID: p.ID, ProjectName: p.Name, TaskID: t.ID, TaskTitle: t.Title,
		Actor: actor, ActionPath: notify.ActionPath(ctx, p.ID, t.ID),
	}, nil
}

// CreateTask creates a new task in the project: the status must be one of the project's, every
// assignee a project member. New assignees other than the author get project.task.assigned.
func (s *Service) CreateTask(ctx context.Context, tenantID, projectID uuid.UUID, input CreateTaskInput) (*ent.Task, error) {
	if input.Title == "" {
		return nil, ErrValidation("title is required")
	}
	status := input.Status
	if status == "" {
		status = "todo"
	}
	priority := input.Priority
	if priority == "" {
		priority = "medium"
	}
	if input.StartDate != nil && input.DueDate != nil && !input.StartDate.IsZero() && !input.DueDate.IsZero() &&
		input.DueDate.Before(input.StartDate.Time) {
		return nil, ErrValidation("due date cannot be before the start date")
	}
	if input.Metadata != nil {
		if err := normalizeRepeat(input.Metadata); err != nil {
			return nil, err
		}
	}
	var task *ent.Task
	err := txn.Run(ctx, s.client, func(tx *ent.Tx) error {
		p, err := projectForTask(ctx, tx, tenantID, projectID)
		if err != nil {
			return err
		}
		if input.Status == "" && !hasStatus(statusesOf(p.Metadata), status) {
			status = statusesOf(p.Metadata)[0].Key
		}
		if err := checkStatus(p, status); err != nil {
			return err
		}
		if input.ParentID != nil {
			ok, err := tx.Task.Query().Where(enttask.ID(*input.ParentID), enttask.TenantID(tenantID), enttask.ProjectID(projectID)).Exist(ctx)
			if err != nil {
				return fmt.Errorf("validate parent: %w", err)
			}
			if !ok {
				return ErrValidation("parent task not found in this project")
			}
		}
		c := tx.Task.Create().
			SetTenantID(tenantID).
			SetProjectID(projectID).
			SetTitle(input.Title).
			SetDescription(input.Description).
			SetStatus(status).
			SetPriority(priority)
		if input.DueDate != nil && !input.DueDate.IsZero() {
			c = c.SetDueDate(input.DueDate.Time)
		}
		if input.StartDate != nil && !input.StartDate.IsZero() {
			c = c.SetStartDate(input.StartDate.Time)
		}
		if input.EstimatedHours != nil {
			c = c.SetEstimatedHours(*input.EstimatedHours)
		}
		if input.ProgressPct != nil {
			c = c.SetProgressPct(clampPct(*input.ProgressPct))
		}
		if status == StatusDone {
			c = c.SetProgressPct(100).SetCompletedAt(time.Now())
		}
		if input.ParentID != nil {
			c = c.SetParentID(*input.ParentID)
		}
		if input.WbsCode != "" {
			c = c.SetWbsCode(input.WbsCode)
		}
		if input.Metadata != nil {
			c = c.SetMetadata(clientMetadata(nil, input.Metadata))
		}
		if task, err = c.Save(ctx); err != nil {
			return fmt.Errorf("create task: %w", err)
		}
		changes := map[string]any{"title": task.Title, "status": task.Status}
		if len(input.AssigneeIDs) > 0 {
			rows, added, err := setAssignees(ctx, tx, tenantID, projectID, task.ID, input.AssigneeIDs)
			if err != nil {
				return err
			}
			task.Edges.Assignees = rows
			changes["assignee_ids"] = idStrings(assigneeIDs(rows))
			n, err := notice(ctx, tx, p, task)
			if err != nil {
				return err
			}
			if err := notify.Write(ctx, tx, notify.EventTaskAssigned, n, added); err != nil {
				return err
			}
		}
		return activity.Record(ctx, tx, activity.Entry{
			TenantID: tenantID, ProjectID: activity.Ref(projectID), TaskID: activity.Ref(task.ID),
			EntityType: "task", EntityID: task.ID, Action: activity.ActionCreated, Changes: changes,
		})
	})
	if err != nil {
		return nil, err
	}
	if task.Edges.Assignees == nil {
		task.Edges.Assignees = []*ent.TaskAssignee{}
	}
	s.emit(ctx, tenantID, realtime.TaskCreated, task)
	return task, nil
}

// applyStatus moves t to status on u, keeping completion in step: entering done completes the task
// (progress 100, completed_at), leaving it reopens it (completed_at cleared). It records the change
// in changes and writes project.task.status_changed to the task's assignees.
//
// facts is the task as it will be after the whole change (nil: as it is now); the project's
// status rules are checked against it and a refused move is *StatusRuleBlocked.
func applyStatus(ctx context.Context, tx *ent.Tx, p *ent.Project, t *ent.Task, u *ent.TaskUpdateOne, status string, changes map[string]any, facts *MoveFacts) error {
	if err := checkStatus(p, status); err != nil {
		return err
	}
	if status == t.Status {
		return nil
	}
	if err := checkStatusRules(ctx, tx, p, t, status, facts); err != nil {
		return err
	}
	u.SetStatus(status)
	changes["status"] = map[string]any{"from": t.Status, "to": status}
	switch {
	case status == StatusDone:
		u.SetCompletedAt(time.Now()).SetProgressPct(100)
	case t.Status == StatusDone:
		u.ClearCompletedAt()
	}
	assignees, err := tx.TaskAssignee.Query().Where(entassignee.TenantID(t.TenantID), entassignee.TaskID(t.ID)).All(ctx)
	if err != nil {
		return fmt.Errorf("load assignees: %w", err)
	}
	n, err := notice(ctx, tx, p, t)
	if err != nil {
		return err
	}
	n.Extra = map[string]any{"from_status": t.Status, "to_status": status, "to_status_label": StatusLabel(p, status)}
	return notify.Write(ctx, tx, notify.EventTaskStatusChanged, n, assigneeIDs(assignees))
}

// updatedFacts is the move facts of t with an update's fields applied, so a request that fills a
// required field and moves the task in one go passes.
func updatedFacts(t *ent.Task, in UpdateTaskInput) MoveFacts {
	f := factsOf(t)
	if in.EstimatedHours != nil {
		f.EstimatedHours = in.EstimatedHours
	}
	if in.StartDate != nil {
		f.StartDate = nil
		if !in.StartDate.IsZero() {
			v := in.StartDate.Time
			f.StartDate = &v
		}
	}
	if in.DueDate != nil {
		f.DueDate = in.DueDate.Time
	}
	if in.Description != nil {
		f.Description = *in.Description
	}
	if in.Metadata != nil {
		f.Metadata = in.Metadata
	}
	if in.AssigneeIDs != nil {
		ids, _ := normalizeAssignees(*in.AssigneeIDs)
		f.Assignees = len(ids)
	}
	return f
}

// factsOf is the move facts of a task as stored. Assignees is -1 (load when a rule needs it).
func factsOf(t *ent.Task) MoveFacts {
	return MoveFacts{Assignees: -1, EstimatedHours: t.EstimatedHours, StartDate: t.StartDate, DueDate: t.DueDate,
		Description: t.Description, Metadata: t.Metadata}
}

// checkStatusRules refuses a move the project's status rules forbid (dormant without the
// project_automation feature). Assignee and comment counts are read only when a requirement of
// this move needs them.
func checkStatusRules(ctx context.Context, tx *ent.Tx, p *ent.Project, t *ent.Task, to string, facts *MoveFacts) error {
	if !planningRulesOn(ctx) {
		return nil
	}
	rules := statusRulesOf(p.Metadata)
	if rules.Empty() {
		return nil
	}
	f := factsOf(t)
	if facts != nil {
		f = *facts
	}
	if rules.NeedsCounts(t.Status, to) {
		if f.Assignees < 0 {
			n, err := tx.TaskAssignee.Query().Where(entassignee.TenantID(t.TenantID), entassignee.TaskID(t.ID)).Count(ctx)
			if err != nil {
				return fmt.Errorf("count assignees: %w", err)
			}
			f.Assignees = n
		}
		n, err := tx.Comment.Query().Where(entcomment.TenantID(t.TenantID), entcomment.TaskID(t.ID)).Count(ctx)
		if err != nil {
			return fmt.Errorf("count comments: %w", err)
		}
		f.Comments = n
	}
	if blocked := EvaluateMove(rules, t.Status, to, f, statusLabels(statusesOf(p.Metadata))); blocked != nil {
		return blocked
	}
	return nil
}

// StatusLabel is the project's label for a status key (the key itself when the project has no
// such status).
func StatusLabel(p *ent.Project, key string) string {
	for _, st := range statusesOf(p.Metadata) {
		if st.Key == key {
			return st.Label
		}
	}
	return key
}

// UpdateTask updates an existing task. A status must be one of the project's statuses; assignees
// must be project members, and people newly assigned get project.task.assigned.
func (s *Service) UpdateTask(ctx context.Context, tenantID, projectID, taskID uuid.UUID, input UpdateTaskInput) (*ent.Task, error) {
	if input.ParentID != nil && *input.ParentID == taskID {
		return nil, ErrValidation("a task cannot be its own parent")
	}
	if input.Metadata != nil {
		if err := normalizeRepeat(input.Metadata); err != nil {
			return nil, err
		}
	}
	var updated *ent.Task
	statusChanged := false
	err := txn.Run(ctx, s.client, func(tx *ent.Tx) error {
		// The row lock keeps this read-modify-write of metadata from undoing a checklist tick, a
		// status idempotency key or a timer written meanwhile.
		if err := lockTask(ctx, tx, tenantID, projectID, taskID); err != nil {
			return err
		}
		t, err := tx.Task.Query().Where(enttask.ID(taskID)).Only(ctx)
		if err != nil {
			return fmt.Errorf("fetch task: %w", err)
		}
		p, err := projectForTask(ctx, tx, tenantID, projectID)
		if err != nil {
			return err
		}
		changes := map[string]any{}
		u := t.Update()
		if input.Title != nil {
			u = u.SetTitle(*input.Title)
			changes["title"] = *input.Title
		}
		if input.Description != nil {
			u = u.SetDescription(*input.Description)
			changes["description"] = "changed"
		}
		fromStatus := t.Status
		if input.Status != nil {
			facts := updatedFacts(t, input)
			if err := applyStatus(ctx, tx, p, t, u, *input.Status, changes, &facts); err != nil {
				return err
			}
			statusChanged = *input.Status != t.Status
		}
		if input.Priority != nil {
			u = u.SetPriority(*input.Priority)
			changes["priority"] = *input.Priority
		}
		if input.DueDate != nil {
			if input.DueDate.IsZero() {
				u = u.ClearDueDate()
				changes["due_date"] = nil
			} else {
				u = u.SetDueDate(input.DueDate.Time)
				changes["due_date"] = input.DueDate.Format(time.DateOnly)
			}
		}
		if input.StartDate != nil {
			if input.StartDate.IsZero() {
				u = u.ClearStartDate()
				changes["start_date"] = nil
			} else {
				u = u.SetStartDate(input.StartDate.Time)
				changes["start_date"] = input.StartDate.Format(time.DateOnly)
			}
		}
		if input.EstimatedHours != nil {
			u = u.SetEstimatedHours(*input.EstimatedHours)
			changes["estimated_hours"] = *input.EstimatedHours
		}
		if input.ProgressPct != nil {
			u = u.SetProgressPct(clampPct(*input.ProgressPct))
			changes["progress_pct"] = clampPct(*input.ProgressPct)
		}
		if input.ParentID != nil {
			u = u.SetParentID(*input.ParentID)
			changes["parent_id"] = input.ParentID.String()
		}
		if input.WbsCode != nil {
			u = u.SetWbsCode(*input.WbsCode)
			changes["wbs_code"] = *input.WbsCode
		}
		if input.Metadata != nil {
			u = u.SetMetadata(clientMetadata(t.Metadata, input.Metadata))
			changes["metadata"] = "changed"
		}
		if updated, err = u.Save(ctx); err != nil {
			return fmt.Errorf("update task: %w", err)
		}
		if input.AssigneeIDs != nil {
			rows, added, err := setAssignees(ctx, tx, tenantID, projectID, taskID, *input.AssigneeIDs)
			if err != nil {
				return err
			}
			updated.Edges.Assignees = rows
			changes["assignee_ids"] = idStrings(assigneeIDs(rows))
			n, err := notice(ctx, tx, p, updated)
			if err != nil {
				return err
			}
			if err := notify.Write(ctx, tx, notify.EventTaskAssigned, n, added); err != nil {
				return err
			}
		} else if updated.Edges.Assignees, err = tx.TaskAssignee.Query().
			Where(entassignee.TenantID(tenantID), entassignee.TaskID(taskID)).Order(assigneeOrderBy()...).All(ctx); err != nil {
			return fmt.Errorf("load assignees: %w", err)
		}
		if statusChanged {
			if updated, err = runAutomations(ctx, tx, p, updated, fromStatus, changes); err != nil {
				return err
			}
		}
		return activity.Record(ctx, tx, activity.Entry{
			TenantID: tenantID, ProjectID: activity.Ref(projectID), TaskID: activity.Ref(taskID),
			EntityType: "task", EntityID: taskID, Action: activity.ActionUpdated, Changes: changes,
		})
	})
	if err != nil {
		return nil, err
	}
	if statusChanged {
		s.emit(ctx, tenantID, realtime.TaskMoved, updated)
	} else {
		s.emit(ctx, tenantID, realtime.TaskUpdated, updated)
	}
	return updated, nil
}

// DeleteTask deletes a task by ID with its dependencies and assignees.
func (s *Service) DeleteTask(ctx context.Context, tenantID, projectID, taskID uuid.UUID) error {
	var deleted *ent.Task
	err := txn.Run(ctx, s.client, func(tx *ent.Tx) error {
		t, err := tx.Task.Query().
			Where(enttask.ID(taskID), enttask.TenantID(tenantID), enttask.ProjectID(projectID)).Only(ctx)
		if ent.IsNotFound(err) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("fetch task: %w", err)
		}
		if _, err := tx.TaskDependency.Delete().
			Where(enttaskdep.Or(enttaskdep.TaskID(taskID), enttaskdep.DependsOnTaskID(taskID))).Exec(ctx); err != nil {
			return fmt.Errorf("delete task dependencies: %w", err)
		}
		if _, err := tx.TaskAssignee.Delete().Where(entassignee.TenantID(tenantID), entassignee.TaskID(taskID)).Exec(ctx); err != nil {
			return fmt.Errorf("delete task assignees: %w", err)
		}
		if err := tx.Task.DeleteOne(t).Exec(ctx); err != nil {
			return fmt.Errorf("delete task: %w", err)
		}
		deleted = t
		return activity.Record(ctx, tx, activity.Entry{
			TenantID: tenantID, ProjectID: activity.Ref(projectID), EntityType: "task", EntityID: taskID,
			Action: activity.ActionDeleted, Changes: map[string]any{"title": t.Title},
		})
	})
	if err != nil {
		return err
	}
	s.emit(ctx, tenantID, realtime.TaskDeleted, deleted)
	return nil
}

// AddDependency records that taskID (in the project) depends on dependsOnID (any task of the
// tenant), refusing a dependency that would close a cycle.
func (s *Service) AddDependency(ctx context.Context, tenantID, projectID, taskID, dependsOnID uuid.UUID, depType string) error {
	if depType == "" {
		depType = "FS"
	}
	if taskID == dependsOnID {
		return ErrCircularDependency
	}
	err := txn.Run(ctx, s.client, func(tx *ent.Tx) error {
		inProject, err := tx.Task.Query().Where(enttask.ID(taskID), enttask.TenantID(tenantID), enttask.ProjectID(projectID)).Exist(ctx)
		if err != nil {
			return fmt.Errorf("check task: %w", err)
		}
		other, err := tx.Task.Query().Where(enttask.ID(dependsOnID), enttask.TenantID(tenantID)).Exist(ctx)
		if err != nil {
			return fmt.Errorf("check task: %w", err)
		}
		if !inProject || !other {
			return ErrNotFound
		}
		// Adding "taskID depends on dependsOnID" closes a cycle when dependsOnID already depends,
		// directly or through others, on taskID.
		cycle, err := reachesInDB(ctx, tx, tenantID, dependsOnID, taskID)
		if err != nil {
			return err
		}
		if cycle {
			return ErrCircularDependency
		}
		if _, err := tx.TaskDependency.Create().
			SetTaskID(taskID).SetDependsOnTaskID(dependsOnID).SetDependencyType(depType).Save(ctx); err != nil {
			return fmt.Errorf("add dependency: %w", err)
		}
		return activity.Record(ctx, tx, activity.Entry{
			TenantID: tenantID, ProjectID: activity.Ref(projectID), TaskID: activity.Ref(taskID), EntityType: "task",
			EntityID: taskID, Action: activity.ActionUpdated,
			Changes: map[string]any{"dependency_added": dependsOnID.String(), "dependency_type": depType},
		})
	})
	if err == nil {
		realtime.Emit(s.live, tenantID, realtime.Event{Type: realtime.TaskUpdated, ProjectID: projectID.String(), TaskID: taskID.String(), ActorID: actorString(ctx)})
	}
	return err
}

// reachesQuery walks the dependency graph from one task only (never the whole tenant), as one
// recursive CTE: $1 start, $2 target, $3 tenant, $4 depth bound. UNION drops rows already seen,
// so a cycle already in the data cannot loop, and EXISTS stops at the first hit.
const reachesQuery = `
WITH RECURSIVE reach(id, depth) AS (
	SELECT d.depends_on_task_id, 1
	FROM task_dependencies d
	JOIN tasks t ON t.id = d.task_id AND t.tenant_id = $3
	WHERE d.task_id = $1
	UNION
	SELECT d.depends_on_task_id, r.depth + 1
	FROM reach r
	JOIN task_dependencies d ON d.task_id = r.id
	JOIN tasks t ON t.id = d.task_id AND t.tenant_id = $3
	WHERE r.depth < $4
)
SELECT EXISTS (SELECT 1 FROM reach WHERE id = $2)`

// reachesInDB reports whether target is reachable from start along dependency edges.
func reachesInDB(ctx context.Context, tx *ent.Tx, tenantID, start, target uuid.UUID) (bool, error) {
	rows, err := tx.QueryContext(ctx, reachesQuery, start, target, tenantID, maxDependencyDepth)
	if err != nil {
		return false, fmt.Errorf("cycle check: %w", err)
	}
	defer rows.Close()
	var found bool
	if rows.Next() {
		if err := rows.Scan(&found); err != nil {
			return false, fmt.Errorf("cycle check: %w", err)
		}
	}
	return found, rows.Err()
}

// RemoveDependency removes a dependency of a task in the project.
func (s *Service) RemoveDependency(ctx context.Context, tenantID, projectID, taskID, dependsOnID uuid.UUID) error {
	err := txn.Run(ctx, s.client, func(tx *ent.Tx) error {
		n, err := tx.TaskDependency.Delete().
			Where(enttaskdep.TaskID(taskID), enttaskdep.DependsOnTaskID(dependsOnID),
				enttaskdep.HasTaskWith(enttask.TenantID(tenantID), enttask.ProjectID(projectID))).Exec(ctx)
		if err != nil {
			return fmt.Errorf("remove dependency: %w", err)
		}
		if n == 0 {
			return ErrNotFound
		}
		return activity.Record(ctx, tx, activity.Entry{
			TenantID: tenantID, ProjectID: activity.Ref(projectID), TaskID: activity.Ref(taskID), EntityType: "task",
			EntityID: taskID, Action: activity.ActionUpdated, Changes: map[string]any{"dependency_removed": dependsOnID.String()},
		})
	})
	if err == nil {
		realtime.Emit(s.live, tenantID, realtime.Event{Type: realtime.TaskUpdated, ProjectID: projectID.String(), TaskID: taskID.String(), ActorID: actorString(ctx)})
	}
	return err
}

// clampPct keeps a progress percentage within 0-100.
func clampPct(p int) int {
	if p < 0 {
		return 0
	}
	if p > 100 {
		return 100
	}
	return p
}
