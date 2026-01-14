package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// TenderSubmission holds the schema definition for the TenderSubmission entity.
type TenderSubmission struct {
	ent.Schema
}

// Fields of the TenderSubmission.
func (TenderSubmission) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New).
			Immutable(),
		field.UUID("tenant_id", uuid.UUID{}),
		field.UUID("tender_id", uuid.UUID{}),
		field.String("submission_type").
			Default("email").
			Comment("email, physical, portal"),
		field.String("status").
			Default("draft").
			Comment("draft, ready, submitted, confirmed, rejected"),
		field.Time("submitted_at").
			Optional(),
		field.UUID("submitted_by", uuid.UUID{}).
			Optional(),
		field.String("recipient_email").
			Optional(),
		field.String("recipient_address").
			Optional(),
		field.String("portal_url").
			Optional(),
		field.String("portal_confirmation_number").
			Optional(),
		field.String("courier_service").
			Optional().
			Comment("DHL, FedEx, UPS, etc."),
		field.String("tracking_number").
			Optional(),
		field.Time("estimated_delivery").
			Optional(),
		field.Time("delivered_at").
			Optional(),
		field.String("delivery_proof_url").
			Optional().
			Comment("URL to delivery confirmation document"),
		field.String("email_message_id").
			Optional().
			Comment("Email message ID for tracking"),
		field.Bool("email_opened").
			Optional(),
		field.Time("email_opened_at").
			Optional(),
		field.JSON("documents", []map[string]any{}).
			Optional().
			Comment("List of documents included in submission"),
		field.Int("total_pages").
			Optional(),
		field.Int("copy_count").
			Optional().
			Comment("Number of physical copies submitted"),
		field.String("notes").
			Optional(),
		field.String("rejection_reason").
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

// Edges of the TenderSubmission.
func (TenderSubmission) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("tender", Tender.Type).
			Ref("submissions").
			Field("tender_id").
			Unique().
			Required(),
	}
}

// Indexes of the TenderSubmission.
func (TenderSubmission) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "tender_id"),
		index.Fields("tender_id", "status"),
		index.Fields("tender_id", "submission_type"),
	}
}
