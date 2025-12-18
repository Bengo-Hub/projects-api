package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

// RolePermission holds the schema definition for the RolePermission entity.
type RolePermission struct {
	ent.Schema
}

// Fields of the RolePermission.
func (RolePermission) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New).
			Immutable(),
		field.String("role_code"),
		field.UUID("permission_id", uuid.UUID{}),
		field.Time("assigned_at").
			Default(time.Now),
	}
}

// Edges of the RolePermission.
func (RolePermission) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("role", Role.Type).
			Ref("role_permissions").
			Field("role_code").
			Unique().
			Required(),
		edge.From("permission", Permission.Type).
			Ref("role_permissions").
			Field("permission_id").
			Unique().
			Required(),
	}
}

