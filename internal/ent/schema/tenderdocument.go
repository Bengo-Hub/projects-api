package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// TenderDocument holds the schema definition for the TenderDocument entity.
type TenderDocument struct {
	ent.Schema
}

// Fields of the TenderDocument.
func (TenderDocument) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New).
			Immutable(),
		field.UUID("tenant_id", uuid.UUID{}),
		field.UUID("tender_id", uuid.UUID{}),
		field.String("name").
			NotEmpty(),
		field.String("description").
			Optional(),
		field.String("document_type").
			Default("other").
			Comment("rfp, rfq, tor, specification, evaluation_criteria, contract_template, addendum, clarification, response, other"),
		field.String("file_url").
			NotEmpty(),
		field.String("file_name").
			NotEmpty(),
		field.Int64("file_size"),
		field.String("mime_type").
			Optional(),
		field.Int("version").
			Default(1),
		field.Bool("is_latest").
			Default(true),
		field.UUID("previous_version_id", uuid.UUID{}).
			Optional().
			Comment("Reference to previous version if this is a revision"),
		field.UUID("uploaded_by", uuid.UUID{}),
		field.Time("uploaded_at").
			Default(time.Now),
		field.JSON("metadata", map[string]any{}).
			Optional(),
	}
}

// Edges of the TenderDocument.
func (TenderDocument) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("tender", Tender.Type).
			Ref("documents").
			Field("tender_id").
			Unique().
			Required(),
	}
}

// Indexes of the TenderDocument.
func (TenderDocument) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "tender_id"),
		index.Fields("tender_id", "document_type"),
	}
}
