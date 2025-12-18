package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

// Activity holds the schema definition for the Activity entity.
type Activity struct {
	ent.Schema
}

// Fields of the Activity.
func (Activity) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New).
			Immutable(),
		field.UUID("tenant_id", uuid.UUID{}),
		field.UUID("project_id", uuid.UUID{}).
			Optional(),
		field.UUID("task_id", uuid.UUID{}).
			Optional(),
		field.UUID("user_id", uuid.UUID{}),
		field.String("activity_type").
			NotEmpty(),
		field.JSON("payload", map[string]any{}).
			Optional(),
		field.Time("occurred_at").
			Default(time.Now),
	}
}

// Edges of the Activity.
func (Activity) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("project", Project.Type).
			Ref("activities").
			Field("project_id").
			Unique(),
		edge.From("task", Task.Type).
			Ref("activities").
			Field("task_id").
			Unique(),
	}
}

