package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

// TenderCommitteeMember holds the schema definition for the TenderCommitteeMember entity.
type TenderCommitteeMember struct{ ent.Schema }

func (TenderCommitteeMember) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		field.UUID("committee_id", uuid.UUID{}),
		field.UUID("tenant_id", uuid.UUID{}),
		field.UUID("user_id", uuid.UUID{}),
		field.String("role").Default("member"),
		field.Time("joined_at").Default(time.Now).Immutable(),
	}
}

func (TenderCommitteeMember) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("committee", TenderCommittee.Type).Ref("members").Field("committee_id").Unique().Required(),
	}
}
