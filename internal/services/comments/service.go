package comments

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"go.uber.org/zap"

	sharedcache "github.com/Bengo-Hub/cache"
	"github.com/bengobox/projects-service/internal/ent"
	entcomment "github.com/bengobox/projects-service/internal/ent/comment"
	"github.com/bengobox/projects-service/internal/ent/predicate"
	entproject "github.com/bengobox/projects-service/internal/ent/project"
	enttask "github.com/bengobox/projects-service/internal/ent/task"
	"github.com/bengobox/projects-service/internal/services/activity"
	"github.com/bengobox/projects-service/internal/services/txn"
)

// ErrNotFound is returned when a comment (or its project or task) is not found.
var ErrNotFound = errors.New("not found")

// ErrNotOwner is returned when a caller limited to their own comments edits someone else's.
var ErrNotOwner = errors.New("you can only change your own comments")

// ValidationError is a refused input, answered 400.
type ValidationError string

func (e ValidationError) Error() string { return string(e) }

// Service handles comment operations.
type Service struct {
	client *ent.Client
	cache  *sharedcache.Aside
	log    *zap.Logger
}

// NewService creates a new comments service.
func NewService(client *ent.Client, cache *sharedcache.Aside, log *zap.Logger) *Service {
	return &Service{client: client, cache: cache, log: log.Named("comments.svc")}
}

// CreateCommentInput holds data for creating a comment.
type CreateCommentInput struct {
	UserID   uuid.UUID      `json:"-"`
	Content  string         `json:"content"`
	Metadata map[string]any `json:"metadata"`
}

// UpdateCommentInput holds data for updating a comment.
type UpdateCommentInput struct {
	Content string `json:"content"`
}

// inProject matches comments on the project itself or on one of its tasks.
func inProject(projectID uuid.UUID) predicate.Comment {
	return entcomment.Or(entcomment.ProjectID(projectID), entcomment.HasTaskWith(enttask.ProjectID(projectID)))
}

func page(ctx context.Context, q *ent.CommentQuery, limit, offset int) ([]*ent.Comment, int, error) {
	if limit <= 0 || limit > subListLimit {
		limit = subListLimit
	}
	total, err := q.Clone().Count(ctx)
	if err != nil {
		return nil, 0, err
	}
	items, err := q.Order(ent.Asc(entcomment.FieldCreatedAt), ent.Asc(entcomment.FieldID)).Offset(offset).Limit(limit).All(ctx)
	return items, total, err
}

// ListCommentsByProject returns one page of a project's own comments, oldest first, with the total.
func (s *Service) ListCommentsByProject(ctx context.Context, tenantID, projectID uuid.UUID, limit, offset int) ([]*ent.Comment, int, error) {
	items, total, err := page(ctx, s.client.Comment.Query().
		Where(entcomment.TenantID(tenantID), entcomment.ProjectID(projectID)), limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list comments by project: %w", err)
	}
	return items, total, nil
}

// ListCommentsByTask returns one page of a task's comments, oldest first, with the total. The task
// must belong to the project.
func (s *Service) ListCommentsByTask(ctx context.Context, tenantID, projectID, taskID uuid.UUID, limit, offset int) ([]*ent.Comment, int, error) {
	items, total, err := page(ctx, s.client.Comment.Query().
		Where(entcomment.TenantID(tenantID), entcomment.TaskID(taskID), entcomment.HasTaskWith(enttask.ProjectID(projectID))), limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list comments by task: %w", err)
	}
	return items, total, nil
}

func record(ctx context.Context, tx *ent.Tx, c *ent.Comment, projectID uuid.UUID, action string) error {
	e := activity.Entry{TenantID: c.TenantID, ProjectID: activity.Ref(projectID), EntityType: "comment", EntityID: c.ID, Action: action}
	if c.TaskID != uuid.Nil {
		e.TaskID = activity.Ref(c.TaskID)
	}
	if action != activity.ActionDeleted {
		n := len([]rune(c.Content))
		e.Changes = map[string]any{"length": n}
	}
	return activity.Record(ctx, tx, e)
}

// CreateProjectComment creates a comment on a project of the tenant.
func (s *Service) CreateProjectComment(ctx context.Context, tenantID, projectID uuid.UUID, input CreateCommentInput) (*ent.Comment, error) {
	if input.Content == "" {
		return nil, ValidationError("content is required")
	}
	var out *ent.Comment
	err := txn.Run(ctx, s.client, func(tx *ent.Tx) error {
		ok, err := tx.Project.Query().Where(entproject.ID(projectID), entproject.TenantID(tenantID)).Exist(ctx)
		if err != nil {
			return fmt.Errorf("validate project: %w", err)
		}
		if !ok {
			return ErrNotFound
		}
		c := tx.Comment.Create().SetTenantID(tenantID).SetProjectID(projectID).SetUserID(input.UserID).SetContent(input.Content)
		if input.Metadata != nil {
			c = c.SetMetadata(input.Metadata)
		}
		if out, err = c.Save(ctx); err != nil {
			return fmt.Errorf("create project comment: %w", err)
		}
		return record(ctx, tx, out, projectID, activity.ActionCreated)
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// CreateTaskComment creates a comment on a task of the project.
func (s *Service) CreateTaskComment(ctx context.Context, tenantID, projectID, taskID uuid.UUID, input CreateCommentInput) (*ent.Comment, error) {
	if input.Content == "" {
		return nil, ValidationError("content is required")
	}
	var out *ent.Comment
	err := txn.Run(ctx, s.client, func(tx *ent.Tx) error {
		ok, err := tx.Task.Query().Where(enttask.ID(taskID), enttask.TenantID(tenantID), enttask.ProjectID(projectID)).Exist(ctx)
		if err != nil {
			return fmt.Errorf("validate task: %w", err)
		}
		if !ok {
			return ErrNotFound
		}
		c := tx.Comment.Create().SetTenantID(tenantID).SetTaskID(taskID).SetUserID(input.UserID).SetContent(input.Content)
		if input.Metadata != nil {
			c = c.SetMetadata(input.Metadata)
		}
		if out, err = c.Save(ctx); err != nil {
			return fmt.Errorf("create task comment: %w", err)
		}
		return record(ctx, tx, out, projectID, activity.ActionCreated)
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// loadForChange loads a comment of the project; ownerOnly (when set) must be its author.
func loadForChange(ctx context.Context, tx *ent.Tx, tenantID, projectID, id uuid.UUID, ownerOnly *uuid.UUID) (*ent.Comment, error) {
	c, err := tx.Comment.Query().Where(entcomment.ID(id), entcomment.TenantID(tenantID), inProject(projectID)).Only(ctx)
	if ent.IsNotFound(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("fetch comment: %w", err)
	}
	if ownerOnly != nil && c.UserID != *ownerOnly {
		return nil, ErrNotOwner
	}
	return c, nil
}

// UpdateComment updates the content of a comment of the project. ownerOnly limits the change to
// the caller's own comment (callers with comments.manage pass nil).
func (s *Service) UpdateComment(ctx context.Context, tenantID, projectID, id uuid.UUID, input UpdateCommentInput, ownerOnly *uuid.UUID) (*ent.Comment, error) {
	if input.Content == "" {
		return nil, ValidationError("content is required")
	}
	var updated *ent.Comment
	err := txn.Run(ctx, s.client, func(tx *ent.Tx) error {
		c, err := loadForChange(ctx, tx, tenantID, projectID, id, ownerOnly)
		if err != nil {
			return err
		}
		if updated, err = c.Update().SetContent(input.Content).Save(ctx); err != nil {
			return fmt.Errorf("update comment: %w", err)
		}
		return record(ctx, tx, updated, projectID, activity.ActionUpdated)
	})
	if err != nil {
		return nil, err
	}
	return updated, nil
}

// DeleteComment deletes a comment of the project; ownerOnly as for UpdateComment.
func (s *Service) DeleteComment(ctx context.Context, tenantID, projectID, id uuid.UUID, ownerOnly *uuid.UUID) error {
	return txn.Run(ctx, s.client, func(tx *ent.Tx) error {
		c, err := loadForChange(ctx, tx, tenantID, projectID, id, ownerOnly)
		if err != nil {
			return err
		}
		if err := tx.Comment.DeleteOne(c).Exec(ctx); err != nil {
			return fmt.Errorf("delete comment: %w", err)
		}
		return record(ctx, tx, c, projectID, activity.ActionDeleted)
	})
}
