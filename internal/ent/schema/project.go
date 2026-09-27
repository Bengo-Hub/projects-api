package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

// Project holds the schema definition for the Project entity.
type Project struct {
	ent.Schema
}

// Fields of the Project.
func (Project) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New).
			Immutable(),
		field.UUID("tenant_id", uuid.UUID{}),
		field.String("name").
			NotEmpty(),
		field.String("description").
			Optional(),
		field.String("status").
			Default("active"),
		field.Time("start_date").
			Optional(),
		field.Time("end_date").
			Optional(),
		field.Float("budget").
			Optional(),
		field.String("currency").
			Default("USD"),
		field.UUID("owner_id", uuid.UUID{}),
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

// Edges of the Project.
func (Project) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("tasks", Task.Type),
		edge.To("members", ProjectMember.Type),
		edge.To("milestones", Milestone.Type),
		edge.To("comments", Comment.Type),
		edge.To("activities", Activity.Type),
		edge.To("attachments", Attachment.Type),
	}
}

