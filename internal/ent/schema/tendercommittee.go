package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
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

// Indexes: committees are always read per tender.
func (TenderCommittee) Indexes() []ent.Index {
	return []ent.Index{index.Fields("tender_id")}
}

func (TenderCommittee) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("tender", Tender.Type).Ref("committees").Field("tender_id").Unique().Required(),
		edge.To("members", TenderCommitteeMember.Type),
	}
}
