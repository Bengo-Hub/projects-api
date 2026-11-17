package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// TenderSubmission records one delivery of a bid (sprint 1, US-1.10 and US-1.11): how it went
// (online portal, email or physical), when, to whom, the confirmation or tracking reference, and
// the documents sent and the proof of receipt (tender documents). A real table because a tender
// can be delivered more than once (an amended bid, a clarification) and each delivery is an
// append-only record listed per tender; it is never rewritten. No foreign key, like the sprint 16
// tables; deleting a tender deletes its submissions in the same transaction.
type TenderSubmission struct{ ent.Schema }

func (TenderSubmission) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		field.UUID("tenant_id", uuid.UUID{}),
		field.UUID("tender_id", uuid.UUID{}),
		field.String("method").Comment("online, email or physical"),
		field.Time("submitted_at"),
		field.UUID("submitted_by", uuid.UUID{}),
		field.String("recipient").Optional().Comment("portal name, email address or delivery address"),
		field.String("reference").Optional().Comment("portal confirmation, email message id or courier tracking number"),
		field.String("courier").Optional(),
		field.Text("notes").Optional(),
		field.UUID("document_id", uuid.UUID{}).Optional().Nillable().Comment("the bid as sent (a tender document)"),
		field.UUID("proof_document_id", uuid.UUID{}).Optional().Nillable().Comment("receipt or proof of delivery (a tender document)"),
		field.JSON("metadata", map[string]any{}).Optional(),
		field.Time("created_at").Default(time.Now).Immutable(),
	}
}

// Indexes: submissions are listed per tender, newest first.
func (TenderSubmission) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "tender_id", "submitted_at"),
	}
}
