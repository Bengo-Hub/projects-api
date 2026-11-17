package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// RBACAuditLog records who changed a role or a person's access in a tenant (sprint 17): a role
// created, copied, customised, renamed, deleted or given new permissions, a role given or taken
// away, a person invited, deactivated or reactivated. Rows are written once and never changed.
type RBACAuditLog struct {
	ent.Schema
}

func (RBACAuditLog) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "rbac_audit_logs"}}
}

func (RBACAuditLog) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		field.UUID("tenant_id", uuid.UUID{}).Immutable(),
		field.UUID("actor_user_id", uuid.UUID{}).Immutable().Comment("auth-api user id of whoever made the change"),
		field.String("actor_email").Optional().MaxLen(254).Immutable(),
		// action: role.created, role.cloned, role.customized, role.updated, role.deleted,
		// role.permissions_updated, role.assigned, role.revoked, user.invited, user.deactivated,
		// user.reactivated.
		field.String("action").MaxLen(64).Immutable(),
		// resource_type: role or user.
		field.String("resource_type").MaxLen(16).Immutable(),
		field.String("resource_id").MaxLen(64).Immutable(),
		field.JSON("changes", map[string]any{}).Optional().Immutable(),
		field.String("ip_address").Optional().MaxLen(64).Immutable(),
		field.Time("created_at").Default(time.Now).Immutable(),
	}
}

func (RBACAuditLog) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "created_at"),
	}
}
