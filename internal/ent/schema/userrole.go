package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

// UserRole holds the schema definition for the UserRole entity.
type UserRole struct {
	ent.Schema
}

// Fields of the UserRole.
func (UserRole) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New).
			Immutable(),
		field.UUID("tenant_id", uuid.UUID{}),
		field.UUID("user_id", uuid.UUID{}),
		field.String("role_code"),
		field.Time("assigned_at").
			Default(time.Now),
		field.UUID("assigned_by", uuid.UUID{}).
			Optional(),
		field.JSON("metadata", map[string]any{}).
			Optional(),
	}
}

// Edges of the UserRole.
func (UserRole) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("role", Role.Type).
			Ref("user_roles").
			Field("role_code").
			Unique().
			Required(),
	}
}

