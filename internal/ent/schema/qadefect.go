package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"

	"github.com/bengobox/projects-service/internal/qamodel"
)

// QADefect is the one defect a session holds per failing criterion, and the ticket raised for it
// in ticketing-api. The unique (session_id, criterion_id) index is what keeps two testers failing
// the same criterion from opening two tickets.
type QADefect struct {
	ent.Schema
}

func (QADefect) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "qa_defects"}}
}

func (QADefect) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		field.UUID("tenant_id", uuid.UUID{}).Immutable(),
		field.UUID("session_id", uuid.UUID{}).Immutable(),
		field.UUID("criterion_id", uuid.UUID{}).Immutable(),
		field.UUID("first_result_id", uuid.UUID{}).Immutable(),
		field.Int("severity"),
		field.String("status").Default(qamodel.DefectOpen),
		field.String("ticket_id").Optional().MaxLen(64),
		field.String("ticket_number").Optional().MaxLen(64),
		field.String("last_error").Optional().MaxLen(500),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (QADefect) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("session_id", "criterion_id").Unique(),
	}
}
