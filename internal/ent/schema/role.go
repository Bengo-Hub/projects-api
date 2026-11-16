package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// Role is the projects role catalogue (Layer 3, local to this service). Seeded system roles are
// global (tenant_id null); the tenant_id column leaves room for a tenant's own roles.
type Role struct {
	ent.Schema
}

// Fields of the Role.
func (Role) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		field.UUID("tenant_id", uuid.UUID{}).Optional().Nillable(),
		field.String("role_code").NotEmpty(),
		field.String("name").NotEmpty(),
		field.Text("description").Optional(),
		field.Bool("is_system_role").Default(false),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

// Edges of the Role.
func (Role) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("permissions", Permission.Type).Through("role_permissions", RolePermission.Type),
		edge.From("user_assignments", UserRoleAssignment.Type).Ref("role"),
	}
}

// Indexes of the Role: a code is unique among the global roles and within one tenant.
func (Role) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id"),
		index.Fields("role_code").Unique().Annotations(entsql.IndexWhere("tenant_id IS NULL")),
		index.Fields("tenant_id", "role_code").Unique().Annotations(entsql.IndexWhere("tenant_id IS NOT NULL")),
	}
}
