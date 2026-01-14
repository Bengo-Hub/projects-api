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
type Tender struct {
	ent.Schema
}

// Fields of the Tender.
func (Tender) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New).
			Immutable(),
		field.UUID("tenant_id", uuid.UUID{}),
		field.String("tender_number").
			NotEmpty().
			Comment("Auto-generated unique tender reference number"),
		field.String("title").
			NotEmpty(),
		field.String("description").
			Optional(),
		field.String("client_name").
			NotEmpty(),
		field.String("client_contact").
			Optional().
			Comment("Client contact person or department"),
		field.String("client_email").
			Optional(),
		field.String("source").
			Default("manual").
			Comment("Source: manual, government_portal, referral, website"),
		field.String("source_url").
			Optional().
			Comment("URL where tender was found"),
		field.String("status").
			Default("identified").
			Comment("identified, evaluating, preparing, submitted, under_review, shortlisted, awarded, lost, withdrawn"),
		field.String("priority").
			Default("medium").
			Comment("low, medium, high, critical"),
		field.Float("estimated_value").
			Optional().
			Comment("Estimated contract value"),
		field.String("currency").
			Default("USD"),
		field.Time("publication_date").
			Optional().
			Comment("Date tender was published"),
		field.Time("deadline").
			Comment("Submission deadline"),
		field.Time("clarification_deadline").
			Optional().
			Comment("Deadline for submitting clarification questions"),
		field.String("submission_method").
			Optional().
			Comment("email, physical, portal, mixed"),
		field.String("submission_address").
			Optional().
			Comment("Physical or email address for submission"),
		field.JSON("categories", []string{}).
			Optional().
			Comment("Tender categories/tags"),
		field.JSON("requirements_summary", map[string]any{}).
			Optional().
			Comment("Key requirements extracted from tender"),
		field.String("decision").
			Optional().
			Comment("go, no_go - evaluation committee decision"),
		field.String("decision_rationale").
			Optional(),
		field.Time("decision_date").
			Optional(),
		field.UUID("decided_by", uuid.UUID{}).
			Optional(),
		field.UUID("project_id", uuid.UUID{}).
			Optional().
			Comment("Linked project if tender is awarded"),
		field.UUID("created_by", uuid.UUID{}),
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

// Edges of the Tender.
func (Tender) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("documents", TenderDocument.Type),
		edge.To("committees", TenderCommittee.Type),
		edge.To("evaluations", TenderEvaluation.Type),
		edge.To("meetings", TenderMeeting.Type),
		edge.To("sections", TenderSection.Type),
		edge.To("submissions", TenderSubmission.Type),
		edge.To("activities", Activity.Type),
		edge.From("project", Project.Type).
			Ref("tenders").
			Field("project_id").
			Unique(),
	}
}

// Indexes of the Tender.
func (Tender) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "tender_number").
			Unique(),
		index.Fields("tenant_id", "status"),
		index.Fields("tenant_id", "deadline"),
	}
}
