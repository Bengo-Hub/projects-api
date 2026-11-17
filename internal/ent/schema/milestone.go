package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// Milestone holds the schema definition for the Milestone entity.
type Milestone struct {
	ent.Schema
}

// Fields of the Milestone.
func (Milestone) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New).
			Immutable(),
		field.UUID("tenant_id", uuid.UUID{}),
		field.UUID("project_id", uuid.UUID{}),
		field.String("name").
			NotEmpty(),
		field.String("description").
			Optional(),
		field.Time("target_date").
			Optional(),
		field.String("status").
			Default("pending"),
		field.Time("completed_at").
			Optional(),
		// Billing (sprint 15), read and never filtered: amount (decimal string) or amount_pct (share
		// of the project's contract value), and the treasury invoice once one is created
		// (invoice_id, invoice_number). Treasury owns the invoice itself.
		field.JSON("metadata", map[string]any{}).
			Optional(),
		field.Time("created_at").
			Default(time.Now).
			Immutable(),
		field.Time("updated_at").
			Default(time.Now).
			UpdateDefault(time.Now),
	}
}

// Indexes of the Milestone: listed per project by target date.
func (Milestone) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "project_id", "target_date"),
	}
}

// Edges of the Milestone.
func (Milestone) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("project", Project.Type).
			Ref("milestones").
			Field("project_id").
			Unique().
			Required(),
	}
}

