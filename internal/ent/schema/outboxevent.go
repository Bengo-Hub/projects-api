package schema

import (
	"encoding/json"
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// OutboxEvent matches the shared-events SQL outbox repository column layout: services write rows
// inside their domain transaction and the shared publisher claims, publishes and deletes them.
type OutboxEvent struct {
	ent.Schema
}

// Fields of the OutboxEvent.
func (OutboxEvent) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		field.UUID("tenant_id", uuid.UUID{}),
		field.String("aggregate_type").NotEmpty(),
		field.UUID("aggregate_id", uuid.UUID{}),
		field.String("event_type").NotEmpty(),
		field.JSON("payload", json.RawMessage{}).Optional().Comment("The whole shared-events envelope, published as is"),
		field.String("status").Default("PENDING"),
		field.Int("attempts").Default(0),
		field.Time("last_attempt_at").Optional().Nillable(),
		field.Time("published_at").Optional().Nillable(),
		field.Text("error_message").Optional().Nillable(),
		field.Time("created_at").Default(time.Now).Immutable(),
	}
}

// Indexes of the OutboxEvent: the shared-events worker polls pending rows oldest first every
// few seconds.
func (OutboxEvent) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("status", "created_at"),
	}
}
