package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

// Expense holds the schema definition for the Expense entity.
type Expense struct{ ent.Schema }

func (Expense) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		field.UUID("budget_id", uuid.UUID{}),
		field.UUID("project_id", uuid.UUID{}),
		field.UUID("tenant_id", uuid.UUID{}),
		field.String("description").NotEmpty(),
		field.Float("amount"),
		field.String("currency").Default("KES"),
		field.String("category").Optional(),
		field.UUID("incurred_by", uuid.UUID{}),
		field.Time("incurred_at"),
		field.String("receipt_url").Optional(),
		field.String("status").Default("pending"),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (Expense) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("budget", Budget.Type).Ref("expenses").Field("budget_id").Unique().Required(),
	}
}
