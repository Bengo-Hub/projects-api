package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// TenderEvaluation holds the schema definition for the TenderEvaluation entity.
type TenderEvaluation struct {
	ent.Schema
}

// Fields of the TenderEvaluation.
func (TenderEvaluation) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New).
			Immutable(),
		field.UUID("tenant_id", uuid.UUID{}),
		field.UUID("tender_id", uuid.UUID{}),
		field.UUID("evaluator_id", uuid.UUID{}).
			Comment("User ID of the evaluator"),
		field.UUID("committee_id", uuid.UUID{}).
			Optional().
			Comment("Committee this evaluation belongs to"),
		field.Float("technical_score").
			Optional().
			Comment("Technical capability score (0-100)"),
		field.Float("financial_score").
			Optional().
			Comment("Financial viability score (0-100)"),
		field.Float("resource_score").
			Optional().
			Comment("Resource availability score (0-100)"),
		field.Float("risk_score").
			Optional().
			Comment("Risk assessment score (0-100, lower is better)"),
		field.Float("overall_score").
			Optional().
			Comment("Weighted overall score"),
		field.String("vote").
			Optional().
			Comment("go, no_go, abstain"),
		field.String("vote_comment").
			Optional(),
		field.JSON("strengths", []string{}).
			Optional().
			Comment("SWOT: Strengths identified"),
		field.JSON("weaknesses", []string{}).
			Optional().
			Comment("SWOT: Weaknesses identified"),
		field.JSON("opportunities", []string{}).
			Optional().
			Comment("SWOT: Opportunities identified"),
		field.JSON("threats", []string{}).
			Optional().
			Comment("SWOT: Threats identified"),
		field.String("recommendation").
			Optional().
			Comment("Evaluator's recommendation text"),
		field.Bool("is_final").
			Default(false).
			Comment("Whether this is the final evaluation"),
		field.Time("submitted_at").
			Optional(),
		field.Time("created_at").
			Default(time.Now).
			Immutable(),
		field.Time("updated_at").
			Default(time.Now).
			UpdateDefault(time.Now),
		field.JSON("metadata", map[string]any{}).
			Optional(),
	}
}

// Edges of the TenderEvaluation.
func (TenderEvaluation) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("tender", Tender.Type).
			Ref("evaluations").
			Field("tender_id").
			Unique().
			Required(),
	}
}

// Indexes of the TenderEvaluation.
func (TenderEvaluation) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "tender_id"),
		index.Fields("tender_id", "evaluator_id").
			Unique(),
		index.Fields("tender_id", "is_final"),
	}
}
