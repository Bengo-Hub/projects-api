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

// QATemplate is a reusable acceptance checklist: its criteria and the pass rule a session starts
// from. A null tenant_id is a platform template (Codevertex services, Hadia included), managed by
// the platform owner and readable by every tenant.
type QATemplate struct {
	ent.Schema
}

func (QATemplate) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "qa_templates"}}
}

func (QATemplate) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		field.UUID("tenant_id", uuid.UUID{}).Optional().Nillable().Immutable(),
		field.String("name").NotEmpty().MaxLen(200),
		field.String("kind").Default(qamodel.KindSoftware),
		// service_tag ties a platform template to a catalogue service (for example "hadia").
		field.String("service_tag").Optional().MaxLen(64),
		field.Int("version").Default(1),
		field.String("status").Default(qamodel.TemplateDraft),
		// needs_review is set when a catalogue feature one of its criteria checks has changed.
		field.Bool("needs_review").Default(false),
		field.JSON("pass_rule", qamodel.PassRule{}).Optional(),
		field.JSON("metadata", map[string]any{}).Optional(),
		field.UUID("created_by", uuid.UUID{}).Optional().Nillable(),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (QATemplate) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("criteria", QACriterion.Type),
	}
}

func (QATemplate) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "created_at"),
		index.Fields("service_tag"),
	}
}
