package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// Tender holds the schema definition for the Tender entity.
type Tender struct{ ent.Schema }

func (Tender) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		field.UUID("tenant_id", uuid.UUID{}),
		field.String("number").Unique(),
		field.String("title").NotEmpty(),
		field.String("client_name").NotEmpty(),
		field.String("source").Optional(),
		field.String("status").Default("draft"),
		field.String("priority").Default("medium"),
		field.Float("estimated_value").Optional(),
		field.String("currency").Default("KES"),
		field.Time("deadline").Optional(),
		field.Text("description").Optional(),
		field.String("submission_type").Default("physical"),
		field.Time("submitted_at").Optional(),
		field.JSON("metadata", map[string]any{}).Optional(),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
		field.UUID("created_by", uuid.UUID{}),
	}
}

func (Tender) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("documents", TenderDocument.Type),
		edge.To("committees", TenderCommittee.Type),
		edge.To("evaluations", TenderEvaluation.Type),
		edge.To("meetings", TenderMeeting.Type),
	}
}

func (Tender) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "status"),
		index.Fields("tenant_id", "deadline"),
	}
}
