package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// TenderSubmission records how and when a tender was handed in (US-1.10, US-1.11): by email
// (recipient, subject, body; sent by notifications-api from the project.tender.submitted event),
// physically (courier, tracking number, address, proof of delivery) or online (portal reference).
type TenderSubmission struct{ ent.Schema }

func (TenderSubmission) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		field.UUID("tenant_id", uuid.UUID{}),
		field.UUID("tender_id", uuid.UUID{}),
		field.String("method").Comment("email, physical or online"),
		field.Time("submitted_at").Default(time.Now),
		field.UUID("submitted_by", uuid.UUID{}),
		field.String("recipient_email").Optional(),
		field.String("email_subject").Optional(),
		field.Text("email_body").Optional(),
		field.String("document_url").Optional(),
		field.String("courier").Optional(),
		field.String("tracking_number").Optional(),
		field.Text("address").Optional(),
		field.String("proof_url").Optional(),
		field.String("confirmation_number").Optional(),
		// queued (email handed to notifications), recorded (physical or online), sent or failed.
		field.String("delivery_status").Default("recorded"),
		field.Text("notes").Optional(),
		field.Time("created_at").Default(time.Now).Immutable(),
	}
}

func (TenderSubmission) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("tender", Tender.Type).Ref("submissions").Field("tender_id").Unique().Required(),
	}
}

func (TenderSubmission) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "tender_id"),
	}
}
