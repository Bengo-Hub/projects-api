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
)

// ErrNotFound is returned when a comment is not found.
var ErrNotFound = errors.New("not found")

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
	UserID    uuid.UUID      `json:"user_id"`
	Content   string         `json:"content"`
	Metadata  map[string]any `json:"metadata"`
}

// UpdateCommentInput holds data for updating a comment.
type UpdateCommentInput struct {
	Content string `json:"content"`
}

// ListCommentsByProject returns all comments for a project.
func (s *Service) ListCommentsByProject(ctx context.Context, tenantID, projectID uuid.UUID) ([]*ent.Comment, error) {
	items, err := s.client.Comment.Query().
		Where(entcomment.TenantID(tenantID), entcomment.ProjectID(projectID)).
		Order(ent.Asc(entcomment.FieldCreatedAt)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list comments by project: %w", err)
	}
	return items, nil
}

// ListCommentsByTask returns all comments for a task.
func (s *Service) ListCommentsByTask(ctx context.Context, tenantID, taskID uuid.UUID) ([]*ent.Comment, error) {
	items, err := s.client.Comment.Query().
		Where(entcomment.TenantID(tenantID), entcomment.TaskID(taskID)).
		Order(ent.Asc(entcomment.FieldCreatedAt)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list comments by task: %w", err)
	}
	return items, nil
}

// CreateProjectComment creates a comment on a project.
func (s *Service) CreateProjectComment(ctx context.Context, tenantID, projectID uuid.UUID, input CreateCommentInput) (*ent.Comment, error) {
	if input.Content == "" {
		return nil, fmt.Errorf("content is required")
	}
	c := s.client.Comment.Create().
		SetTenantID(tenantID).
		SetProjectID(projectID).
		SetUserID(input.UserID).
		SetContent(input.Content)
	if input.Metadata != nil {
		c = c.SetMetadata(input.Metadata)
	}
	comment, err := c.Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("create project comment: %w", err)
	}
	return comment, nil
}

// CreateTaskComment creates a comment on a task.
func (s *Service) CreateTaskComment(ctx context.Context, tenantID, taskID uuid.UUID, input CreateCommentInput) (*ent.Comment, error) {
	if input.Content == "" {
		return nil, fmt.Errorf("content is required")
	}
	c := s.client.Comment.Create().
		SetTenantID(tenantID).
		SetTaskID(taskID).
		SetUserID(input.UserID).
		SetContent(input.Content)
	if input.Metadata != nil {
		c = c.SetMetadata(input.Metadata)
	}
	comment, err := c.Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("create task comment: %w", err)
	}
	return comment, nil
}

// UpdateComment updates the content of a comment.
func (s *Service) UpdateComment(ctx context.Context, tenantID, id uuid.UUID, input UpdateCommentInput) (*ent.Comment, error) {
	c, err := s.client.Comment.Query().
		Where(entcomment.ID(id), entcomment.TenantID(tenantID)).Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("fetch comment: %w", err)
	}
	updated, err := c.Update().SetContent(input.Content).Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("update comment: %w", err)
	}
	return updated, nil
}

// DeleteComment deletes a comment.
func (s *Service) DeleteComment(ctx context.Context, tenantID, id uuid.UUID) error {
	n, err := s.client.Comment.Delete().
		Where(entcomment.ID(id), entcomment.TenantID(tenantID)).Exec(ctx)
	if err != nil {
		return fmt.Errorf("delete comment: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
