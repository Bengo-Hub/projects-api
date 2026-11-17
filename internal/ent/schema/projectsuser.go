package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// ProjectsUser is the local reference to an auth-api user within one tenant, provisioned just in
// time on the first authenticated request. auth-api owns identity; this keeps only what role
// assignments and the role screens need.
type ProjectsUser struct {
	ent.Schema
}

// Fields of the ProjectsUser.
func (ProjectsUser) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		field.UUID("tenant_id", uuid.UUID{}),
		field.UUID("auth_service_user_id", uuid.UUID{}).Comment("auth-api user id, unique per tenant"),
		field.String("email").Optional(),
		field.String("name").Optional(),
		// status: active or inactive. Only an admin's deactivation sets inactive; sign-in never
		// changes it back.
		field.String("status").Default("active"),
		field.Time("deactivated_at").Optional().Nillable(),
		field.UUID("deactivated_by", uuid.UUID{}).Optional().Nillable().Comment("auth-api user id of whoever deactivated them"),
		field.String("sync_status").Default("synced"),
		field.Time("last_sync_at").Optional().Nillable(),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

// Edges of the ProjectsUser.
func (ProjectsUser) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("role_assignments", UserRoleAssignment.Type).Ref("user"),
	}
}

// Indexes of the ProjectsUser.
func (ProjectsUser) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "auth_service_user_id").Unique(),
		index.Fields("auth_service_user_id"),
	}
}
