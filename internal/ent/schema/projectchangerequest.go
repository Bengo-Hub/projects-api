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

// ProjectChangeRequest is a scope, cost or schedule change waiting for sign-off (sprint 16). The
// approval is local to projects (a non-financial decision); a cost change is pushed to treasury
// as a project budget after approval, where treasury's own approval runs. Metadata holds the
// date shifts, the proposed budget body and treasury's answer, never a money total of record.
type ProjectChangeRequest struct {
	ent.Schema
}

func (ProjectChangeRequest) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "project_change_requests"}}
}

func (ProjectChangeRequest) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		field.UUID("tenant_id", uuid.UUID{}),
		field.UUID("project_id", uuid.UUID{}),
		field.String("title").NotEmpty(),
		field.String("description").Optional(),
		field.String("kind").Default("scope").Comment("scope | cost | schedule"),
		field.String("status").Default("pending").Comment("pending | approved | rejected | withdrawn"),
		field.UUID("requested_by", uuid.UUID{}),
		field.UUID("decided_by", uuid.UUID{}).Optional().Nillable(),
		field.Time("decided_at").Optional().Nillable(),
		field.String("decision_comment").Optional(),
		field.JSON("metadata", map[string]any{}).Optional(),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

// Indexes: a project's requests by status, newest first, and the tenant's pending queue.
func (ProjectChangeRequest) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "project_id", "status", "created_at"),
		index.Fields("tenant_id", "status", "created_at"),
	}
}
