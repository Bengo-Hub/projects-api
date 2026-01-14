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
type TenderCommittee struct {
	ent.Schema
}

// Fields of the TenderCommittee.
func (TenderCommittee) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New).
			Immutable(),
		field.UUID("tenant_id", uuid.UUID{}),
		field.UUID("tender_id", uuid.UUID{}),
		field.String("name").
			NotEmpty().
			Comment("Committee name, e.g., Technical Evaluation Committee"),
		field.String("committee_type").
			Default("evaluation").
			Comment("evaluation, technical, financial, legal"),
		field.String("status").
			Default("active").
			Comment("active, dissolved"),
		field.UUID("chair_id", uuid.UUID{}).
			Optional().
			Comment("User ID of committee chair"),
		field.Time("formed_at").
			Default(time.Now),
		field.Time("dissolved_at").
			Optional(),
		field.String("mandate").
			Optional().
			Comment("Committee's terms of reference"),
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

// Edges of the TenderCommittee.
func (TenderCommittee) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("tender", Tender.Type).
			Ref("committees").
			Field("tender_id").
			Unique().
			Required(),
		edge.To("members", TenderCommitteeMember.Type),
		edge.To("meetings", TenderMeeting.Type),
	}
}

// Indexes of the TenderCommittee.
func (TenderCommittee) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "tender_id"),
		index.Fields("tender_id", "committee_type"),
	}
}
