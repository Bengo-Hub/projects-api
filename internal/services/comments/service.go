package comments

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"entgo.io/ent/dialect/sql"
	"entgo.io/ent/dialect/sql/sqljson"
	"github.com/google/uuid"
	"go.uber.org/zap"

	sharedcache "github.com/Bengo-Hub/cache"
	"github.com/Bengo-Hub/httpware/richtext"
	"github.com/bengobox/projects-service/internal/ent"
	entcomment "github.com/bengobox/projects-service/internal/ent/comment"
	"github.com/bengobox/projects-service/internal/ent/predicate"
	entproject "github.com/bengobox/projects-service/internal/ent/project"
	enttask "github.com/bengobox/projects-service/internal/ent/task"
	"github.com/bengobox/projects-service/internal/platform/realtime"
	"github.com/bengobox/projects-service/internal/services/activity"
	"github.com/bengobox/projects-service/internal/services/notify"
	"github.com/bengobox/projects-service/internal/services/people"
	"github.com/bengobox/projects-service/internal/services/txn"
)

// ErrNotFound is returned when a comment (or its project or task) is not found.
var ErrNotFound = errors.New("not found")

// ErrNotOwner is returned when a caller limited to their own comments edits someone else's.
var ErrNotOwner = errors.New("you can only change your own comments")

// ValidationError is a refused input, answered 400.
type ValidationError string

func (e ValidationError) Error() string { return string(e) }

// metaMentions is the server-owned comment metadata key listing the project members the comment
// mentions (auth-api user ids). Clients never write it.
const metaMentions = "mentions"

// Service handles comment operations.
type Service struct {
	client *ent.Client
	cache  *sharedcache.Aside
	log    *zap.Logger
	live   realtime.Publisher
}

// NewService creates a new comments service.
func NewService(client *ent.Client, cache *sharedcache.Aside, log *zap.Logger) *Service {
	return &Service{client: client, cache: cache, log: log.Named("comments.svc")}
}

// WithRealtime sets where comment.created hints go after a commit.
func (s *Service) WithRealtime(p realtime.Publisher) *Service {
	s.live = p
	return s
}

// CreateCommentInput holds data for creating a comment. Content is rich text from the shared
// editor (or plain text); it is sanitised before it is stored.
type CreateCommentInput struct {
	UserID   uuid.UUID      `json:"-"`
	Content  string         `json:"content"`
	Metadata map[string]any `json:"metadata"`
	// IdempotencyKey is the optional Idempotency-Key header (see priorComment).
	IdempotencyKey string `json:"-"`
}

// UpdateCommentInput holds data for updating a comment.
type UpdateCommentInput struct {
	Content string `json:"content"`
}

// inProject matches comments on the project itself or on one of its tasks.
func inProject(projectID uuid.UUID) predicate.Comment {
	return entcomment.Or(entcomment.ProjectID(projectID), entcomment.HasTaskWith(enttask.ProjectID(projectID)))
}

// Authored is a comment with its author's name from projects_users ("" for someone who has not
// signed in to projects; the UI shows a fallback).
type Authored struct {
	*ent.Comment
	AuthorName string `json:"author_name"`
}

// WithAuthors adds the author names to a page of comments in one batched query.
func WithAuthors(ctx context.Context, client *ent.Client, tenantID uuid.UUID, list []*ent.Comment) ([]Authored, error) {
	ids := make([]uuid.UUID, len(list))
	for i, c := range list {
		ids[i] = c.UserID
	}
	known, err := people.Lookup(ctx, client.ProjectsUser, tenantID, ids)
	if err != nil {
		return nil, err
	}
	out := make([]Authored, len(list))
	for i, c := range list {
		out[i] = Authored{Comment: c, AuthorName: known[c.UserID].Label()}
	}
	return out, nil
}

// authored adds the author's name to one comment.
func (s *Service) authored(ctx context.Context, c *ent.Comment) (*Authored, error) {
	list, err := WithAuthors(ctx, s.client, c.TenantID, []*ent.Comment{c})
	if err != nil {
		return nil, err
	}
	return &list[0], nil
}

func (s *Service) page(ctx context.Context, tenantID uuid.UUID, q *ent.CommentQuery, limit, offset int) ([]Authored, int, error) {
	if limit <= 0 || limit > subListLimit {
		limit = subListLimit
	}
	total, err := q.Clone().Count(ctx)
	if err != nil {
		return nil, 0, err
	}
	items, err := q.Order(ent.Asc(entcomment.FieldCreatedAt), ent.Asc(entcomment.FieldID)).Offset(offset).Limit(limit).All(ctx)
	if err != nil {
		return nil, 0, err
	}
	out, err := WithAuthors(ctx, s.client, tenantID, items)
	return out, total, err
}

// ListCommentsByProject returns one page of a project's own internal comments, oldest first, with
// the total. The client thread is never part of it (see ListClientThread).
func (s *Service) ListCommentsByProject(ctx context.Context, tenantID, projectID uuid.UUID, limit, offset int) ([]Authored, int, error) {
	items, total, err := s.page(ctx, tenantID, s.client.Comment.Query().
		Where(entcomment.TenantID(tenantID), entcomment.ProjectID(projectID), internalThread()), limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list comments by project: %w", err)
	}
	return items, total, nil
}

// ListCommentsByTask returns one page of a task's comments, oldest first, with the total. The task
// must belong to the project.
func (s *Service) ListCommentsByTask(ctx context.Context, tenantID, projectID, taskID uuid.UUID, limit, offset int) ([]Authored, int, error) {
	items, total, err := s.page(ctx, tenantID, s.client.Comment.Query().
		Where(entcomment.TenantID(tenantID), entcomment.TaskID(taskID), entcomment.HasTaskWith(enttask.ProjectID(projectID))), limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list comments by task: %w", err)
	}
	return items, total, nil
}

func record(ctx context.Context, tx *ent.Tx, c *ent.Comment, projectID uuid.UUID, action string, mentioned int) error {
	e := activity.Entry{TenantID: c.TenantID, ProjectID: activity.Ref(projectID), EntityType: "comment", EntityID: c.ID, Action: action}
	if c.TaskID != uuid.Nil {
		e.TaskID = activity.Ref(c.TaskID)
	}
	if action != activity.ActionDeleted {
		e.Changes = map[string]any{"length": len([]rune(c.Content))}
		if mentioned > 0 {
			e.Changes["mentions"] = mentioned
		}
	}
	return activity.Record(ctx, tx, e)
}

// clean sanitises a comment body; an empty result is refused.
func clean(content string) (string, error) {
	out := richtext.Sanitize(content)
	if out == "" {
		return "", ValidationError("content is required")
	}
	return out, nil
}

// clientMetadata drops the server-owned keys from a client's metadata.
func clientMetadata(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		if k != metaMentions && k != metaIdempotencyKey && k != MetaThread {
			out[k] = v
		}
	}
	return out
}

// mentionsOf reads the stored mention list of a comment.
func mentionsOf(meta map[string]any) []uuid.UUID {
	var raw []any
	switch v := meta[metaMentions].(type) {
	case []any:
		raw = v
	case []string:
		for _, s := range v {
			raw = append(raw, s)
		}
	}
	out := make([]uuid.UUID, 0, len(raw))
	for _, v := range raw {
		if s, ok := v.(string); ok {
			if id, err := uuid.Parse(s); err == nil {
				out = append(out, id)
			}
		}
	}
	return out
}

// mentioned returns the project members content mentions, never the author, in mention order.
func mentioned(ctx context.Context, tx *ent.Tx, tenantID, projectID, author uuid.UUID, content string) ([]uuid.UUID, error) {
	ids := richtext.Mentions(content)
	if len(ids) == 0 {
		return nil, nil
	}
	members, err := notify.Members(ctx, tx, tenantID, projectID, ids)
	if err != nil {
		return nil, err
	}
	out := make([]uuid.UUID, 0, len(ids))
	for _, id := range ids {
		if id != author && members[id] {
			out = append(out, id)
		}
	}
	return out, nil
}

// notifyMentions writes one project.task.mentioned per newly mentioned person.
func notifyMentions(ctx context.Context, tx *ent.Tx, tenantID, projectID uuid.UUID, task *ent.Task, c *ent.Comment, recipients []uuid.UUID) error {
	if len(recipients) == 0 {
		return nil
	}
	p, err := tx.Project.Query().Where(entproject.ID(projectID), entproject.TenantID(tenantID)).Only(ctx)
	if err != nil {
		return fmt.Errorf("load project: %w", err)
	}
	actor, err := notify.Actor(ctx, tx, tenantID, c.UserID)
	if err != nil {
		return err
	}
	n := notify.Notice{
		TenantID: tenantID, ProjectID: projectID, ProjectName: p.Name, Actor: actor,
		CommentExcerpt: richtext.Excerpt(c.Content, notify.ExcerptLength),
		ActionPath:     notify.ActionPath(ctx, projectID, uuid.Nil),
		Extra:          map[string]any{"comment_id": c.ID.String()},
	}
	if task != nil {
		n.TaskID, n.TaskTitle = task.ID, task.Title
		n.ActionPath = notify.ActionPath(ctx, projectID, task.ID)
	}
	return notify.Write(ctx, tx, notify.EventTaskMentioned, n, recipients)
}

func withMentions(meta map[string]any, ids []uuid.UUID) map[string]any {
	out := make(map[string]any, len(meta)+1)
	for k, v := range meta {
		out[k] = v
	}
	list := make([]string, len(ids))
	for i, id := range ids {
		list[i] = id.String()
	}
	out[metaMentions] = list
	return out
}

func (s *Service) emitCreated(ctx context.Context, c *ent.Comment, projectID uuid.UUID) {
	ev := realtime.Event{Type: realtime.CommentCreated, ProjectID: projectID.String(), ID: c.ID.String()}
	if c.TaskID != uuid.Nil {
		ev.TaskID = c.TaskID.String()
	}
	if c.UserID != uuid.Nil {
		ev.ActorID = c.UserID.String()
	}
	realtime.Emit(s.live, c.TenantID, ev)
}

// CreateProjectComment creates a comment on a project of the tenant. Mentioned project members
// get project.task.mentioned (without a task). replayed reports an Idempotency-Key repeat that
// answered the earlier comment and changed nothing.
func (s *Service) CreateProjectComment(ctx context.Context, tenantID, projectID uuid.UUID, input CreateCommentInput) (*Authored, bool, error) {
	return s.create(ctx, tenantID, projectID, uuid.Nil, input)
}

// CreateTaskComment creates a comment on a task of the project. Mentioned project members get
// project.task.mentioned, one event each, never the author. replayed as for CreateProjectComment.
func (s *Service) CreateTaskComment(ctx context.Context, tenantID, projectID, taskID uuid.UUID, input CreateCommentInput) (*Authored, bool, error) {
	if taskID == uuid.Nil {
		return nil, false, ErrNotFound
	}
	return s.create(ctx, tenantID, projectID, taskID, input)
}

// create writes a comment on the project (taskID uuid.Nil) or on one of its tasks.
func (s *Service) create(ctx context.Context, tenantID, projectID, taskID uuid.UUID, input CreateCommentInput) (*Authored, bool, error) {
	content, err := clean(input.Content)
	if err != nil {
		return nil, false, err
	}
	key, err := idempotencyKey(input.IdempotencyKey)
	if err != nil {
		return nil, false, err
	}
	var out *ent.Comment
	replayed := false
	err = txn.Run(ctx, s.client, func(tx *ent.Tx) error {
		var task *ent.Task
		if taskID == uuid.Nil {
			ok, err := tx.Project.Query().Where(entproject.ID(projectID), entproject.TenantID(tenantID)).Exist(ctx)
			if err != nil {
				return fmt.Errorf("validate project: %w", err)
			}
			if !ok {
				return ErrNotFound
			}
		} else {
			task, err = tx.Task.Query().Where(enttask.ID(taskID), enttask.TenantID(tenantID), enttask.ProjectID(projectID)).Only(ctx)
			if ent.IsNotFound(err) {
				return ErrNotFound
			}
			if err != nil {
				return fmt.Errorf("validate task: %w", err)
			}
		}
		if key != "" {
			prior, err := priorComment(ctx, tx, tenantID, input.UserID, key)
			if err != nil {
				return err
			}
			if prior != nil {
				if prior.TaskID != taskID || (taskID == uuid.Nil && prior.ProjectID != projectID) {
					return ErrKeyReused
				}
				out, replayed = prior, true
				return nil
			}
		}
		ids, err := mentioned(ctx, tx, tenantID, projectID, input.UserID, content)
		if err != nil {
			return err
		}
		meta := withMentions(clientMetadata(input.Metadata), ids)
		if key != "" {
			meta[metaIdempotencyKey] = key
		}
		c := tx.Comment.Create().SetTenantID(tenantID).SetUserID(input.UserID).SetContent(content).SetMetadata(meta)
		if task != nil {
			c.SetTaskID(taskID)
		} else {
			c.SetProjectID(projectID)
		}
		if out, err = c.Save(ctx); err != nil {
			return fmt.Errorf("create comment: %w", err)
		}
		if err := notifyMentions(ctx, tx, tenantID, projectID, task, out, ids); err != nil {
			return err
		}
		return record(ctx, tx, out, projectID, activity.ActionCreated, len(ids))
	})
	if err != nil {
		return nil, false, err
	}
	if !replayed {
		s.emitCreated(ctx, out, projectID)
	}
	a, err := s.authored(ctx, out)
	if err != nil {
		return nil, false, err
	}
	return a, replayed, nil
}

// Idempotent comment creation: an offline client replays its queued comment with the same
// Idempotency-Key. The key is stored in the comment's metadata (idempotency_key, server-owned)
// and is unique per tenant and author: a repeat within idempotencyWindow answers the comment the
// first request made, with no second row, notice, activity entry or hint. A transaction-scoped
// advisory lock on (tenant, author, key) makes two replays racing on different pods apply once.
const (
	metaIdempotencyKey = "idempotency_key"
	idempotencyWindow  = 24 * time.Hour
	maxKeyLength       = 128
)

// ErrKeyReused is a key the author already used for a comment somewhere else (another task or
// project) within the window, answered 409.
var ErrKeyReused = errors.New("this Idempotency-Key was already used for another comment")

// idempotencyKey trims and bounds an Idempotency-Key header ("" when none).
func idempotencyKey(raw string) (string, error) {
	key := strings.TrimSpace(raw)
	if len(key) > maxKeyLength {
		return "", ValidationError(fmt.Sprintf("Idempotency-Key is at most %d characters", maxKeyLength))
	}
	return key, nil
}

// priorComment locks (tenant, author, key) for the rest of the transaction and returns the
// comment the author made with key within the window, nil when there is none. The read is served
// by the (tenant_id, user_id, created_at) index and filters the author's last day on the key.
func priorComment(ctx context.Context, tx *ent.Tx, tenantID, author uuid.UUID, key string) (*ent.Comment, error) {
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`,
		"comment:"+tenantID.String()+":"+author.String()+":"+key); err != nil {
		return nil, fmt.Errorf("lock idempotency key: %w", err)
	}
	c, err := tx.Comment.Query().Where(
		entcomment.TenantID(tenantID), entcomment.UserID(author),
		entcomment.CreatedAtGTE(time.Now().Add(-idempotencyWindow)),
		predicate.Comment(func(s *sql.Selector) {
			s.Where(sqljson.ValueEQ(s.C(entcomment.FieldMetadata), key, sqljson.Path(metaIdempotencyKey)))
		}),
	).Order(ent.Asc(entcomment.FieldCreatedAt)).First(ctx)
	if ent.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find replayed comment: %w", err)
	}
	return c, nil
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
// the caller's own comment (callers with comments.manage pass nil). Only people the edit newly
// mentions are notified.
func (s *Service) UpdateComment(ctx context.Context, tenantID, projectID, id uuid.UUID, input UpdateCommentInput, ownerOnly *uuid.UUID) (*Authored, error) {
	content, err := clean(input.Content)
	if err != nil {
		return nil, err
	}
	var updated *ent.Comment
	err = txn.Run(ctx, s.client, func(tx *ent.Tx) error {
		c, err := loadForChange(ctx, tx, tenantID, projectID, id, ownerOnly)
		if err != nil {
			return err
		}
		var ids []uuid.UUID
		if !IsClientThread(c.Metadata) {
			// The client thread notifies nobody by mention (see CreateClientThreadComment).
			if ids, err = mentioned(ctx, tx, tenantID, projectID, c.UserID, content); err != nil {
				return err
			}
		}
		before := map[uuid.UUID]bool{}
		for _, m := range mentionsOf(c.Metadata) {
			before[m] = true
		}
		var fresh []uuid.UUID
		for _, m := range ids {
			if !before[m] {
				fresh = append(fresh, m)
			}
		}
		if updated, err = c.Update().SetContent(content).SetMetadata(withMentions(c.Metadata, ids)).Save(ctx); err != nil {
			return fmt.Errorf("update comment: %w", err)
		}
		var task *ent.Task
		if len(fresh) > 0 && updated.TaskID != uuid.Nil {
			if task, err = tx.Task.Get(ctx, updated.TaskID); err != nil {
				return fmt.Errorf("load task: %w", err)
			}
		}
		if err := notifyMentions(ctx, tx, tenantID, projectID, task, updated, fresh); err != nil {
			return err
		}
		return record(ctx, tx, updated, projectID, activity.ActionUpdated, len(fresh))
	})
	if err != nil {
		return nil, err
	}
	return s.authored(ctx, updated)
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
		return record(ctx, tx, c, projectID, activity.ActionDeleted, 0)
	})
}
