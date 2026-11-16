// Package notify writes the person-addressed task events notifications-api turns into email and
// WhatsApp notices: project.task.assigned, project.task.mentioned and project.task.status_changed.
// Each event is written to the outbox inside the caller's transaction (one event per recipient),
// so a notice exists if and only if the change committed. Recipient email and name come from
// projects_users, the just-in-time copy of the SSO identity.
package notify

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/bengobox/projects-service/internal/ent"
	entproject "github.com/bengobox/projects-service/internal/ent/project"
	entmember "github.com/bengobox/projects-service/internal/ent/projectmember"
	entuser "github.com/bengobox/projects-service/internal/ent/projectsuser"
	"github.com/bengobox/projects-service/internal/platform/events"
)

// Event types (subject project.<type>).
const (
	EventTaskAssigned      = "task.assigned"
	EventTaskMentioned     = "task.mentioned"
	EventTaskStatusChanged = "task.status_changed"
)

// ExcerptLength is the most characters of a comment a mention notice carries.
const ExcerptLength = 200

// Person is a local copy of an SSO user.
type Person struct {
	ID    uuid.UUID
	Email string
	Name  string
}

// DisplayName is the name, else the email, else a neutral word.
func (p Person) DisplayName() string {
	switch {
	case p.Name != "":
		return p.Name
	case p.Email != "":
		return p.Email
	}
	return "A teammate"
}

// People returns the tenant's local users among ids, keyed by auth-api user id. An id without a
// local row (never signed in to projects) is absent.
func People(ctx context.Context, tx *ent.Tx, tenantID uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]Person, error) {
	out := make(map[uuid.UUID]Person, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := tx.ProjectsUser.Query().
		Where(entuser.TenantID(tenantID), entuser.AuthServiceUserIDIn(ids...)).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("load people: %w", err)
	}
	for _, u := range rows {
		out[u.AuthServiceUserID] = Person{ID: u.AuthServiceUserID, Email: u.Email, Name: u.Name}
	}
	return out, nil
}

// Members returns which of ids own or actively belong to the project (the same rule as
// membership scoping): only they may be assigned or mentioned.
func Members(ctx context.Context, tx *ent.Tx, tenantID, projectID uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]bool, error) {
	out := make(map[uuid.UUID]bool, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	p, err := tx.Project.Query().Where(entproject.ID(projectID), entproject.TenantID(tenantID)).
		Select(entproject.FieldOwnerID).Only(ctx)
	if err != nil {
		return nil, fmt.Errorf("load project owner: %w", err)
	}
	for _, id := range ids {
		if id == p.OwnerID {
			out[id] = true
		}
	}
	members, err := tx.ProjectMember.Query().
		Where(entmember.TenantID(tenantID), entmember.ProjectID(projectID), entmember.UserIDIn(ids...), entmember.LeftAtIsNil()).
		Select(entmember.FieldUserID).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("load project members: %w", err)
	}
	for _, m := range members {
		out[m.UserID] = true
	}
	return out, nil
}

// Notice is what every person-addressed task event carries besides its recipient.
type Notice struct {
	TenantID    uuid.UUID
	ProjectID   uuid.UUID
	ProjectName string
	// TaskID is uuid.Nil for a mention in a project comment.
	TaskID    uuid.UUID
	TaskTitle string
	Actor     Person
	// CommentExcerpt is set on mentions: the comment as plain text, at most ExcerptLength.
	CommentExcerpt string
	// ActionPath is the UI path the notice links to, e.g. /{org}/projects/{id}/tasks?task={id}.
	ActionPath string
	// Extra holds event-specific fields (status_changed: from_status, to_status).
	Extra map[string]any
}

type orgKey struct{}

// WithOrgSlug returns ctx carrying the tenant slug the UI links start with. The handlers set it
// from the caller's token.
func WithOrgSlug(ctx context.Context, slug string) context.Context {
	return context.WithValue(ctx, orgKey{}, slug)
}

// OrgSlug returns the slug set by WithOrgSlug ("" when none).
func OrgSlug(ctx context.Context) string {
	s, _ := ctx.Value(orgKey{}).(string)
	return s
}

// ActionPath builds the UI link of a task (or of the project's tasks page without a task):
// /{org}/projects/{project}/tasks?task={task}, the org taken from ctx.
func ActionPath(ctx context.Context, projectID, taskID uuid.UUID) string {
	p := "/projects/" + projectID.String() + "/tasks"
	if org := OrgSlug(ctx); org != "" {
		p = "/" + org + p
	}
	if taskID != uuid.Nil {
		p += "?task=" + taskID.String()
	}
	return p
}

// Write writes one eventType event per recipient, never to the actor. Recipients without a local
// user row still get an event with an empty email and name; notifications-api resolves them.
func Write(ctx context.Context, tx *ent.Tx, eventType string, n Notice, recipients []uuid.UUID) error {
	if len(recipients) == 0 {
		return nil
	}
	people, err := People(ctx, tx, n.TenantID, recipients)
	if err != nil {
		return err
	}
	aggregate := n.TaskID
	if aggregate == uuid.Nil {
		aggregate = n.ProjectID
	}
	seen := map[uuid.UUID]bool{}
	for _, id := range recipients {
		if id == uuid.Nil || id == n.Actor.ID || seen[id] {
			continue
		}
		seen[id] = true
		who := people[id]
		payload := map[string]any{
			"project_id":        n.ProjectID.String(),
			"project_name":      n.ProjectName,
			"task_id":           "",
			"task_title":        n.TaskTitle,
			"actor_id":          n.Actor.ID.String(),
			"actor_name":        n.Actor.DisplayName(),
			"recipient_user_id": id.String(),
			"recipient_email":   who.Email,
			"recipient_name":    who.Name,
			"comment_excerpt":   n.CommentExcerpt,
			"action_path":       n.ActionPath,
		}
		if n.TaskID != uuid.Nil {
			payload["task_id"] = n.TaskID.String()
		}
		for k, v := range n.Extra {
			payload[k] = v
		}
		if err := events.Write(ctx, tx, events.AggregateProject, eventType, aggregate, n.TenantID, payload); err != nil {
			return err
		}
	}
	return nil
}

// Actor loads the acting user's local copy (empty name and email when they have no row).
func Actor(ctx context.Context, tx *ent.Tx, tenantID, actorID uuid.UUID) (Person, error) {
	if actorID == uuid.Nil {
		return Person{}, nil
	}
	people, err := People(ctx, tx, tenantID, []uuid.UUID{actorID})
	if err != nil {
		return Person{}, err
	}
	p, ok := people[actorID]
	if !ok {
		p = Person{ID: actorID}
	}
	return p, nil
}
