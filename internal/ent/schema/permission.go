package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// Permission is the global permission catalogue ({service}.{module}.{action}), seeded from the
// registry in internal/modules/rbac on every start. Never tenant scoped.
type Permission struct {
	ent.Schema
}

// Fields of the Permission.
func (Permission) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		field.String("permission_code").NotEmpty().Unique(),
		field.String("name").NotEmpty(),
		field.String("module").NotEmpty(),
		field.String("action").NotEmpty(),
		field.String("resource").Optional(),
		field.Text("description").Optional(),
		field.Time("created_at").Default(time.Now).Immutable(),
	}
}

// Edges of the Permission.
func (Permission) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("roles", Role.Type).Ref("permissions").Through("role_permissions", RolePermission.Type),
	}
}

// Indexes of the Permission.
func (Permission) Indexes() []ent.Index {
	return []ent.Index{index.Fields("module", "action")}
}
