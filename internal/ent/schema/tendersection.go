package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// TenderSection is one part of a tender document, assigned to a writer and a reviewer
// (US-1.7 to US-1.9). Status flow: not_started -> in_progress -> review -> approved, or
// review -> changes_requested -> review again.
type TenderSection struct{ ent.Schema }

func (TenderSection) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		field.UUID("tenant_id", uuid.UUID{}),
		field.UUID("tender_id", uuid.UUID{}),
		field.String("title").NotEmpty(),
		field.Text("description").Optional(),
		field.UUID("assignee_id", uuid.UUID{}).Optional().Nillable(),
		field.UUID("reviewer_id", uuid.UUID{}).Optional().Nillable(),
		field.Time("due_date").Optional().Nillable(),
		field.String("status").Default("not_started"),
		field.String("document_url").Optional(),
		field.Text("review_comments").Optional(),
		field.Int("sort_order").Default(0),
		field.Time("submitted_at").Optional().Nillable(),
		field.UUID("reviewed_by", uuid.UUID{}).Optional().Nillable(),
		field.Time("reviewed_at").Optional().Nillable(),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (TenderSection) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("tender", Tender.Type).Ref("sections").Field("tender_id").Unique().Required(),
	}
}

func (TenderSection) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "tender_id"),
	}
}
