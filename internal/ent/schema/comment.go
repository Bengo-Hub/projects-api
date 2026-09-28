package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// Comment holds the schema definition for the Comment entity.
type Comment struct {
	ent.Schema
}

// Fields of the Comment.
func (Comment) Fields() []ent.Field {
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
		field.Text("content").
			NotEmpty(),
		field.Time("created_at").
			Default(time.Now).
			Immutable(),
		field.Time("updated_at").
			Default(time.Now).
			UpdateDefault(time.Now),
		field.JSON("metadata", map[string]any{}).
			Optional(),
	}
}

// Indexes of the Comment: threads are read per project or per task in posting order.
func (Comment) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "project_id", "created_at"),
		index.Fields("tenant_id", "task_id", "created_at"),
	}
}

// Edges of the Comment.
func (Comment) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("project", Project.Type).
			Ref("comments").
			Field("project_id").
			Unique(),
		edge.From("task", Task.Type).
			Ref("comments").
			Field("task_id").
			Unique(),
	}
}

