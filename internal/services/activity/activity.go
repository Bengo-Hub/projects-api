// Package activity writes and reads the project activity feed. Every mutating service method
// calls Record inside its own transaction, so an entry exists if and only if the change committed.
package activity

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/bengobox/projects-service/internal/ent"
	entactivity "github.com/bengobox/projects-service/internal/ent/activity"
)

// Actions recorded in the feed.
const (
	ActionCreated = "created"
	ActionUpdated = "updated"
	ActionDeleted = "deleted"
)

type actorKey struct{}

// WithActor returns ctx carrying the auth-api id of the user making the change. The access
// middleware sets it for every signed-in request.
func WithActor(ctx context.Context, userID uuid.UUID) context.Context {
	return context.WithValue(ctx, actorKey{}, userID)
}

// ActorFrom returns the acting user, or uuid.Nil for service-to-service calls.
func ActorFrom(ctx context.Context) uuid.UUID {
	id, _ := ctx.Value(actorKey{}).(uuid.UUID)
	return id
}

// Entry is one feed row.
type Entry struct {
	TenantID   uuid.UUID
	ProjectID  *uuid.UUID
	TaskID     *uuid.UUID
	EntityType string // project, task, milestone, member, comment, tender
	EntityID   uuid.UUID
	Action     string
	// Changes is a small summary: the fields that changed with their new value, or a few
	// identifying fields on create and delete. Never whole descriptions or metadata.
	Changes map[string]any
}

// Record writes e inside tx, with the actor taken from ctx. activity_type is
// "{entity_type}.{action}", for example task.updated.
func Record(ctx context.Context, tx *ent.Tx, e Entry) error {
	payload := map[string]any{
		"entity_type": e.EntityType,
		"entity_id":   e.EntityID.String(),
		"action":      e.Action,
	}
	if len(e.Changes) > 0 {
		payload["changes"] = e.Changes
	}
	c := tx.Activity.Create().
		SetTenantID(e.TenantID).
		SetUserID(ActorFrom(ctx)).
		SetActivityType(e.EntityType + "." + e.Action).
		SetPayload(payload)
	if e.ProjectID != nil {
		c.SetProjectID(*e.ProjectID)
	}
	if e.TaskID != nil {
		c.SetTaskID(*e.TaskID)
	}
	if err := c.Exec(ctx); err != nil {
		return fmt.Errorf("record activity %s.%s: %w", e.EntityType, e.Action, err)
	}
	return nil
}

// Ref returns a pointer to id, for Entry.ProjectID and Entry.TaskID.
func Ref(id uuid.UUID) *uuid.UUID { return &id }

// feedLimit caps one feed page.
const feedLimit = 100

// Service reads the feed.
type Service struct {
	client *ent.Client
}

// NewService creates the feed reader.
func NewService(client *ent.Client) *Service { return &Service{client: client} }

// ListByProject returns the project's latest entries, newest first (at most 100).
func (s *Service) ListByProject(ctx context.Context, tenantID, projectID uuid.UUID) ([]*ent.Activity, error) {
	return s.client.Activity.Query().
		Where(entactivity.TenantID(tenantID), entactivity.ProjectID(projectID)).
		Order(ent.Desc(entactivity.FieldOccurredAt)).Limit(feedLimit).All(ctx)
}

// ListByTask returns the task's latest entries, newest first (at most 100). The task must belong
// to the project.
func (s *Service) ListByTask(ctx context.Context, tenantID, projectID, taskID uuid.UUID) ([]*ent.Activity, error) {
	return s.client.Activity.Query().
		Where(entactivity.TenantID(tenantID), entactivity.TaskID(taskID), entactivity.ProjectID(projectID)).
		Order(ent.Desc(entactivity.FieldOccurredAt)).Limit(feedLimit).All(ctx)
}
