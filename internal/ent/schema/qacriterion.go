package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"

	"github.com/bengobox/projects-service/internal/qamodel"
)

// QACriterion is one acceptance check on a template. Its subject says what it checks: a catalogue
// feature code (subscriptions-api owns the catalogue), a milestone or task id here, or nothing.
type QACriterion struct {
	ent.Schema
}

func (QACriterion) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "qa_criteria"}}
}

func (QACriterion) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		field.UUID("template_id", uuid.UUID{}),
		// Copied from the template, so the tenant purge removes a tenant's criteria too.
		field.UUID("tenant_id", uuid.UUID{}).Optional().Nillable().Immutable(),
		field.String("subject_kind").Default(qamodel.SubjectNone),
		field.String("subject_ref").Optional().MaxLen(200),
		// area_code groups criteria in the report, for example OWN or REG in Hadia's plan.
		field.String("area_code").Optional().MaxLen(32),
		field.String("title").NotEmpty().MaxLen(300),
		field.String("requirement_ref").Optional().MaxLen(100),
		field.Text("preconditions").Optional(),
		field.Text("steps").Optional(),
		field.Text("expected_result").Optional(),
		field.String("weight").Default(qamodel.WeightMust),
		// automated_ref names an automated test that covers the criterion, when one exists.
		field.String("automated_ref").Optional().MaxLen(300),
		field.Int("sort_order").Default(0),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (QACriterion) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("template", QATemplate.Type).Ref("criteria").Field("template_id").Unique().Required(),
	}
}

func (QACriterion) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("template_id", "sort_order"),
		// The catalogue change consumer finds templates by the feature codes their criteria check.
		index.Fields("subject_kind", "subject_ref"),
	}
}
