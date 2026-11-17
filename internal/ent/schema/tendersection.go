package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// TenderSection is one part of a bid document (sprint 1, US-1.7 to US-1.9): assigned to a person
// with a deadline and a reviewer, and taken through not_started, in_progress, in_review,
// changes_requested and approved. A real table rather than tender metadata because sections are
// edited by different people at the same time (one JSONB blob would lose updates), are listed per
// tender in order and carry their own review trail. Like the sprint 16 tables it carries no
// foreign key; deleting a tender deletes its sections in the same transaction.
type TenderSection struct{ ent.Schema }

func (TenderSection) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		field.UUID("tenant_id", uuid.UUID{}),
		field.UUID("tender_id", uuid.UUID{}),
		field.String("title").NotEmpty(),
		field.Text("description").Optional(),
		field.Int("position").Default(0),
		field.UUID("assignee_id", uuid.UUID{}).Optional().Nillable(),
		field.UUID("reviewer_id", uuid.UUID{}).Optional().Nillable(),
		field.Time("due_date").Optional().Nillable(),
		field.String("status").Default("not_started"),
		field.UUID("document_id", uuid.UUID{}).Optional().Nillable().Comment("a tender document of the same tender: the section's latest draft"),
		field.Time("submitted_at").Optional().Nillable(),
		field.UUID("reviewed_by", uuid.UUID{}).Optional().Nillable(),
		field.Time("reviewed_at").Optional().Nillable(),
		field.Text("review_comment").Optional(),
		field.UUID("created_by", uuid.UUID{}).Optional().Nillable(),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

// Indexes: sections are always read per tender in their order.
func (TenderSection) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "tender_id", "position"),
	}
}
