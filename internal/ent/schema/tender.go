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
		// Go/no-go decision (US-1.6): "go" moves the tender to preparing, "no_go" closes it.
		field.String("decision").Optional().Comment("go or no_go"),
		field.Text("decision_rationale").Optional(),
		field.UUID("decided_by", uuid.UUID{}).Optional().Nillable(),
		field.Time("decided_at").Optional().Nillable(),
		// Outcome (US-1.13): award date, value and contract details, or loss reason and
		// competitor, plus lessons learned.
		field.JSON("outcome", map[string]any{}).Optional(),
		// Every status change with who made it, when and why (US-1.12).
		field.JSON("status_history", []map[string]any{}).Optional(),
		// Final document (US-1.9): the latest compiled version and whether it may be submitted.
		field.Int("final_document_version").Default(0),
		field.Bool("ready_for_submission").Default(false),
	}
}

func (Tender) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("documents", TenderDocument.Type),
		edge.To("committees", TenderCommittee.Type),
		edge.To("evaluations", TenderEvaluation.Type),
		edge.To("meetings", TenderMeeting.Type),
		edge.To("sections", TenderSection.Type),
		edge.To("submissions", TenderSubmission.Type),
	}
}

func (Tender) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "status"),
		index.Fields("tenant_id", "deadline"),
	}
}
