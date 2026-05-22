package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

// Budget holds the schema definition for the Budget entity.
type Budget struct{ ent.Schema }

func (Budget) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		field.UUID("project_id", uuid.UUID{}).Unique(),
		field.UUID("tenant_id", uuid.UUID{}),
		field.Float("total_amount").Default(0),
		field.Float("spent_amount").Default(0),
		field.String("currency").Default("KES"),
		field.String("status").Default("draft"),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (Budget) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("project", Project.Type).Ref("project_budget").Field("project_id").Unique().Required(),
		edge.To("expenses", Expense.Type),
	}
}
