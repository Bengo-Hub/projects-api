package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

// TenderCommittee holds the schema definition for the TenderCommittee entity.
type TenderCommittee struct{ ent.Schema }

func (TenderCommittee) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		field.UUID("tender_id", uuid.UUID{}),
		field.UUID("tenant_id", uuid.UUID{}),
		field.String("name").NotEmpty(),
		field.Time("created_at").Default(time.Now).Immutable(),
	}
}

func (TenderCommittee) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("tender", Tender.Type).Ref("committees").Field("tender_id").Unique().Required(),
		edge.To("members", TenderCommitteeMember.Type),
	}
}
