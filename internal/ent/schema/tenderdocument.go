package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

// TenderDocument holds the schema definition for the TenderDocument entity.
type TenderDocument struct{ ent.Schema }

func (TenderDocument) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		field.UUID("tender_id", uuid.UUID{}),
		field.UUID("tenant_id", uuid.UUID{}),
		field.String("file_url"),
		field.String("file_name"),
		field.Int64("file_size").Optional(),
		field.String("mime_type").Optional(),
		field.UUID("uploaded_by", uuid.UUID{}),
		field.Time("uploaded_at").Default(time.Now).Immutable(),
	}
}

func (TenderDocument) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("tender", Tender.Type).Ref("documents").Field("tender_id").Unique().Required(),
	}
}
