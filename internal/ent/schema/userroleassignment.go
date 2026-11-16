package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// UserRoleAssignment gives a local user a role within a tenant. assigned_by equal to the user's
// own auth id marks the default role sign-in gave them (JIT); anything else was granted by an admin.
type UserRoleAssignment struct {
	ent.Schema
}

// Fields of the UserRoleAssignment.
func (UserRoleAssignment) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		field.UUID("tenant_id", uuid.UUID{}),
		field.UUID("user_id", uuid.UUID{}).Comment("Local ProjectsUser.ID"),
		field.UUID("role_id", uuid.UUID{}),
		field.UUID("assigned_by", uuid.UUID{}).Comment("auth-api user id of whoever granted it"),
		field.Time("assigned_at").Default(time.Now).Immutable(),
		field.Time("expires_at").Optional().Nillable(),
	}
}

// Edges of the UserRoleAssignment.
func (UserRoleAssignment) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("user", ProjectsUser.Type).Field("user_id").Required().Unique(),
		edge.To("role", Role.Type).Field("role_id").Required().Unique(),
	}
}

// Indexes of the UserRoleAssignment.
func (UserRoleAssignment) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "user_id", "role_id").Unique(),
		index.Fields("role_id"),
	}
}
