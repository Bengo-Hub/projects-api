package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// TenderCommitteeMember holds the schema definition for the TenderCommitteeMember entity.
type TenderCommitteeMember struct {
	ent.Schema
}

// Fields of the TenderCommitteeMember.
func (TenderCommitteeMember) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New).
			Immutable(),
		field.UUID("tenant_id", uuid.UUID{}),
		field.UUID("committee_id", uuid.UUID{}),
		field.UUID("user_id", uuid.UUID{}),
		field.String("role").
			Default("member").
			Comment("chair, secretary, member, observer"),
		field.String("expertise").
			Optional().
			Comment("Member's area of expertise: technical, financial, legal, domain"),
		field.Bool("is_active").
			Default(true),
		field.Time("joined_at").
			Default(time.Now),
		field.Time("left_at").
			Optional(),
		field.UUID("added_by", uuid.UUID{}),
		field.JSON("metadata", map[string]any{}).
			Optional(),
	}
}

// Edges of the TenderCommitteeMember.
func (TenderCommitteeMember) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("committee", TenderCommittee.Type).
			Ref("members").
			Field("committee_id").
			Unique().
			Required(),
	}
}

// Indexes of the TenderCommitteeMember.
func (TenderCommitteeMember) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "committee_id"),
		index.Fields("committee_id", "user_id").
			Unique(),
	}
}
