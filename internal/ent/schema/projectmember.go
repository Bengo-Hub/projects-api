package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// ProjectMember holds the schema definition for the ProjectMember entity.
type ProjectMember struct {
	ent.Schema
}

// Fields of the ProjectMember.
func (ProjectMember) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New).
			Immutable(),
		field.UUID("tenant_id", uuid.UUID{}),
		field.UUID("project_id", uuid.UUID{}),
		field.UUID("user_id", uuid.UUID{}),
		field.String("role_code"),
		field.Time("joined_at").
			Default(time.Now),
		field.Time("left_at").
			Optional(),
		field.JSON("metadata", map[string]any{}).
			Optional(),
	}
}

// Indexes of the ProjectMember: listed per project and looked up per project and user.
func (ProjectMember) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "project_id", "user_id"),
	}
}

// Edges of the ProjectMember.
func (ProjectMember) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("project", Project.Type).
			Ref("members").
			Field("project_id").
			Unique().
			Required(),
	}
}

