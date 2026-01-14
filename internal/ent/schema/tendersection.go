package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// TenderSection holds the schema definition for the TenderSection entity.
type TenderSection struct {
	ent.Schema
}

// Fields of the TenderSection.
func (TenderSection) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New).
			Immutable(),
		field.UUID("tenant_id", uuid.UUID{}),
		field.UUID("tender_id", uuid.UUID{}),
		field.UUID("parent_id", uuid.UUID{}).
			Optional().
			Comment("Parent section for hierarchical structure"),
		field.String("title").
			NotEmpty(),
		field.String("description").
			Optional(),
		field.String("section_number").
			Optional().
			Comment("Section numbering e.g., 1.2.3"),
		field.Int("sort_order").
			Default(0),
		field.String("section_type").
			Default("content").
			Comment("content, technical, financial, legal, executive_summary, appendix"),
		field.UUID("assignee_id", uuid.UUID{}).
			Optional(),
		field.String("status").
			Default("not_started").
			Comment("not_started, in_progress, draft, review, approved, rejected"),
		field.Time("due_date").
			Optional(),
		field.String("content").
			Optional().
			Comment("Section content/response"),
		field.Int("word_count").
			Optional(),
		field.Int("page_limit").
			Optional().
			Comment("Maximum pages allowed for this section"),
		field.String("review_status").
			Optional().
			Comment("pending_technical, pending_financial, pending_legal, pending_final, approved"),
		field.UUID("reviewer_id", uuid.UUID{}).
			Optional(),
		field.String("reviewer_comments").
			Optional(),
		field.Time("reviewed_at").
			Optional(),
		field.JSON("compliance_checklist", []map[string]any{}).
			Optional().
			Comment("Compliance items for this section"),
		field.Bool("is_compliant").
			Optional(),
		field.Time("started_at").
			Optional(),
		field.Time("completed_at").
			Optional(),
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

// Edges of the TenderSection.
func (TenderSection) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("tender", Tender.Type).
			Ref("sections").
			Field("tender_id").
			Unique().
			Required(),
		edge.To("children", TenderSection.Type).
			From("parent").
			Field("parent_id").
			Unique(),
	}
}

// Indexes of the TenderSection.
func (TenderSection) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "tender_id"),
		index.Fields("tender_id", "assignee_id"),
		index.Fields("tender_id", "status"),
		index.Fields("tender_id", "sort_order"),
	}
}
