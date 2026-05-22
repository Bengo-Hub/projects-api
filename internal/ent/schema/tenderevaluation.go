package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

// TenderEvaluation holds the schema definition for the TenderEvaluation entity.
type TenderEvaluation struct{ ent.Schema }

func (TenderEvaluation) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		field.UUID("tender_id", uuid.UUID{}),
		field.UUID("tenant_id", uuid.UUID{}),
		field.UUID("evaluator_id", uuid.UUID{}),
		field.Float("score"),
		field.Text("notes").Optional(),
		field.String("criteria").Optional(),
		field.Time("evaluated_at").Default(time.Now).Immutable(),
	}
}

func (TenderEvaluation) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("tender", Tender.Type).Ref("evaluations").Field("tender_id").Unique().Required(),
	}
}
