package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

// Task holds the schema definition for the Task entity.
type Task struct {
	ent.Schema
}

// Fields of the Task.
func (Task) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New).
			Immutable(),
		field.UUID("tenant_id", uuid.UUID{}),
		field.UUID("project_id", uuid.UUID{}),
		field.String("title").
			NotEmpty(),
		field.String("description").
			Optional(),
		field.String("status").
			Default("todo"),
		field.String("priority").
			Default("medium"),
		field.UUID("assignee_id", uuid.UUID{}).
			Optional(),
		field.Time("due_date").
			Optional(),
		field.Time("completed_at").
			Optional(),
		field.Time("created_at").
			Default(time.Now).
			Immutable(),
		field.Time("updated_at").
			Default(time.Now).
			UpdateDefault(time.Now),
		field.JSON("metadata", map[string]any{}).
			Optional(),
		field.UUID("parent_id", uuid.UUID{}).
			Optional(),
		field.String("wbs_code").
			Optional(),
	}
}

// Edges of the Task.
func (Task) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("project", Project.Type).
			Ref("tasks").
			Field("project_id").
			Unique().
			Required(),
		edge.To("dependencies", TaskDependency.Type),
		edge.To("comments", Comment.Type),
		edge.To("activities", Activity.Type),
		edge.To("attachments", Attachment.Type),
	}
}

