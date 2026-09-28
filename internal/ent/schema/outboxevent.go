package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// OutboxEvent holds the schema definition for the OutboxEvent entity.
type OutboxEvent struct {
	ent.Schema
}

// Fields of the OutboxEvent.
func (OutboxEvent) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New).
			Immutable(),
		field.UUID("tenant_id", uuid.UUID{}),
		field.String("aggregate_type").
			NotEmpty(),
		field.UUID("aggregate_id", uuid.UUID{}),
		field.String("event_type").
			NotEmpty(),
		field.JSON("payload", map[string]any{}).
			Optional(),
		field.String("status").
			Default("pending"),
		field.Int("attempts").
			Default(0),
		field.Time("last_attempt_at").
			Optional(),
		field.Time("created_at").
			Default(time.Now).
			Immutable(),
	}
}

// Indexes of the OutboxEvent: the shared-events worker polls pending rows oldest first every
// few seconds, while published rows keep accumulating.
func (OutboxEvent) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("status", "created_at"),
	}
}
