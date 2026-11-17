package comments

import (
	"context"
	"fmt"
	"time"

	"entgo.io/ent/dialect/sql"
	"entgo.io/ent/dialect/sql/sqljson"
	"github.com/Bengo-Hub/httpware/richtext"
	"github.com/google/uuid"

	"github.com/bengobox/projects-service/internal/ent"
	entcomment "github.com/bengobox/projects-service/internal/ent/comment"
	"github.com/bengobox/projects-service/internal/ent/predicate"
	entproject "github.com/bengobox/projects-service/internal/ent/project"
	"github.com/bengobox/projects-service/internal/services/activity"
	"github.com/bengobox/projects-service/internal/services/notify"
	"github.com/bengobox/projects-service/internal/services/txn"
)

// The client thread (sprint 14) is one conversation per project between the team and the
// project's clients. Its comments are project comments (task_id empty) whose server-owned
// metadata.thread is "client". Internal lists never show them and the portal never shows anything
// else, so internal talk cannot leak and client talk cannot mix into the team's thread.
const (
	// MetaThread is the server-owned comment metadata key naming the thread; only
	// CreateClientThreadComment writes it.
	MetaThread = "thread"
	// ThreadClient is MetaThread's value on the client thread.
	ThreadClient = "client"
)

// IsClientThread reports whether a comment's metadata places it on the client thread.
func IsClientThread(meta map[string]any) bool {
	v, _ := meta[MetaThread].(string)
	return v == ThreadClient
}

// clientThread matches comments of the client thread: metadata ->> 'thread' = 'client'.
func clientThread() predicate.Comment {
	return predicate.Comment(func(s *sql.Selector) {
		s.Where(sqljson.ValueEQ(s.C(entcomment.FieldMetadata), ThreadClient, sqljson.Path(MetaThread)))
	})
}

// internalThread matches every comment that is not on the client thread, including comments with
// no metadata at all (a bare NOT would drop those, since a missing key compares as NULL).
func internalThread() predicate.Comment {
	return predicate.Comment(func(s *sql.Selector) {
		col := s.C(entcomment.FieldMetadata)
		s.Where(sql.Or(
			sql.IsNull(col),
			sql.Not(sqljson.HasKey(col, sqljson.Path(MetaThread))),
			sqljson.ValueNEQ(col, ThreadClient, sqljson.Path(MetaThread)),
		))
	})
}

// Author kinds on the client thread. MetaAuthorKind holds it on the comment: server owned, written
// when the comment is posted from the client or the team side, so a later change to who is a
// client (an invite revoked) never rewrites who said what.
const (
	AuthorClient   = "client"
	AuthorTeam     = "team"
	MetaAuthorKind = "author_kind"
)

// ThreadComment is what the client thread shows of a comment: the text, who wrote it (name, and
// whether a client or the team) and when. No metadata, so mention lists and idempotency keys stay
// internal.
type ThreadComment struct {
	ID         uuid.UUID `json:"id"`
	Content    string    `json:"content"`
	AuthorID   uuid.UUID `json:"author_id"`
	AuthorName string    `json:"author_name"`
	AuthorKind string    `json:"author_kind"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// threadViews builds the thread entries of one page. The author kind is the one stored on each
// comment when it was posted (MetaAuthorKind), so no lookup is needed.
func threadViews(items []Authored) []ThreadComment {
	out := make([]ThreadComment, len(items))
	for i, a := range items {
		kind, _ := a.Metadata[MetaAuthorKind].(string)
		if kind != AuthorClient {
			kind = AuthorTeam
		}
		out[i] = ThreadComment{
			ID: a.ID, Content: a.Content, AuthorID: a.UserID, AuthorName: a.AuthorName, AuthorKind: kind,
			CreatedAt: a.CreatedAt, UpdatedAt: a.UpdatedAt,
		}
	}
	return out
}

// ListClientThread returns one page of the project's client thread, oldest first, with the total.
// Staff (comments.view) and the project's clients (portal) read the same list.
func (s *Service) ListClientThread(ctx context.Context, tenantID, projectID uuid.UUID, limit, offset int) ([]ThreadComment, int, error) {
	items, total, err := s.page(ctx, tenantID, s.client.Comment.Query().
		Where(entcomment.TenantID(tenantID), entcomment.ProjectID(projectID), clientThread()), limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list client thread: %w", err)
	}
	return threadViews(items), total, nil
}

// CreateClientThreadComment posts to the project's client thread as input.UserID, who writes as
// authorKind: AuthorClient from the portal route, AuthorTeam from the team route (the caller has
// checked which the user may do). The kind is stored on the comment. The body is sanitised like
// any comment. The post's notices to the other side (notifyThread) are written in the same
// transaction. Mentions notify nobody here: a client must never be able to page a teammate with
// text that lands in an internal notice, and staff reach clients through the thread itself. An
// Idempotency-Key replays only a comment of this same thread; the key on an internal comment
// answers ErrKeyReused.
func (s *Service) CreateClientThreadComment(ctx context.Context, tenantID, projectID uuid.UUID, input CreateCommentInput, authorKind string) (*ThreadComment, bool, error) {
	if authorKind != AuthorClient && authorKind != AuthorTeam {
		return nil, false, fmt.Errorf("client thread: unknown author kind %q", authorKind)
	}
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
		p, err := tx.Project.Query().Where(entproject.ID(projectID), entproject.TenantID(tenantID)).
			Select(entproject.FieldID, entproject.FieldTenantID, entproject.FieldName).Only(ctx)
		if ent.IsNotFound(err) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("validate project: %w", err)
		}
		meta := map[string]any{MetaThread: ThreadClient, MetaAuthorKind: authorKind}
		if key != "" {
			prior, err := priorComment(ctx, tx, tenantID, input.UserID, key)
			if err != nil {
				return err
			}
			if prior != nil {
				if prior.ProjectID != projectID || prior.TaskID != uuid.Nil || !IsClientThread(prior.Metadata) {
					return ErrKeyReused
				}
				out, replayed = prior, true
				return nil
			}
			meta[metaIdempotencyKey] = key
		}
		if out, err = tx.Comment.Create().SetTenantID(tenantID).SetProjectID(projectID).
			SetUserID(input.UserID).SetContent(content).SetMetadata(meta).Save(ctx); err != nil {
			return fmt.Errorf("create client thread comment: %w", err)
		}
		if err := activity.Record(ctx, tx, activity.Entry{
			TenantID: tenantID, ProjectID: activity.Ref(projectID), EntityType: "comment", EntityID: out.ID,
			Action: activity.ActionCreated, Changes: map[string]any{"thread": ThreadClient, "length": len([]rune(content))},
		}); err != nil {
			return err
		}
		return notifyThread(ctx, tx, p, out, authorKind)
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
	views := threadViews([]Authored{*a})
	return &views[0], replayed, nil
}

// notifyThread tells the other side of the client thread about a new post, in the post's
// transaction, one event per recipient (never the poster):
//
//   - a client's post: project.client_thread.posted to the project's owner and its owner and
//     manager members, never a client; the link opens the team's Client thread tab.
//   - a teammate's post: project.client_thread.replied to each signed-in client of the project; the
//     link opens the portal project's Messages tab. A client still at the invite stage has no
//     portal account to read the reply with, so the invite stays their way in.
//
// The notice carries the post as plain text (comment_excerpt, at most notify.ExcerptLength runes).
// Mentions in the post still notify nobody.
func notifyThread(ctx context.Context, tx *ent.Tx, p *ent.Project, c *ent.Comment, authorKind string) error {
	var (
		event, path string
		recipients  []uuid.UUID
		err         error
	)
	if authorKind == AuthorClient {
		event, path = notify.EventClientThreadPosted, notify.ClientThreadPath(ctx, p.ID)
		recipients, err = notify.ProjectLeadsNotClients(ctx, tx, p.TenantID, p.ID)
	} else {
		event, path = notify.EventClientThreadReplied, notify.PortalThreadPath(ctx, p.ID)
		recipients, err = notify.ProjectClients(ctx, tx, p.TenantID, p.ID)
	}
	if err != nil || len(recipients) == 0 {
		return err
	}
	actor, err := notify.Actor(ctx, tx, p.TenantID, c.UserID)
	if err != nil {
		return err
	}
	if actor.Label() == "" {
		// DisplayName's own fallback says "A teammate", which is wrong for a client and vague for
		// the team as a client sees it.
		actor.Name = "Your project team"
		if authorKind == AuthorClient {
			actor.Name = "Your client"
		}
	}
	return notify.Write(ctx, tx, event, notify.Notice{
		TenantID: p.TenantID, ProjectID: p.ID, ProjectName: p.Name, Actor: actor,
		CommentExcerpt: richtext.Excerpt(c.Content, notify.ExcerptLength),
		ActionPath:     path,
		Extra:          map[string]any{"comment_id": c.ID.String(), "author_kind": authorKind},
	}, recipients)
}
