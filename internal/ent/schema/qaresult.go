package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// QAResult is one tester's verdict on one criterion of a session. Rows are never edited: a new
// verdict clears is_current on the previous one, so history is kept and exactly one current row
// exists per criterion and tester.
type QAResult struct {
	ent.Schema
}

func (QAResult) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "qa_results"}}
}

func (QAResult) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		field.UUID("tenant_id", uuid.UUID{}).Immutable(),
		field.UUID("session_id", uuid.UUID{}).Immutable(),
		field.UUID("criterion_id", uuid.UUID{}).Immutable(),
		// tester_key is the keyed hash of the tester's email; tester_hint is its masked form.
		field.String("tester_key").NotEmpty().MaxLen(64).Immutable(),
		field.String("tester_hint").Optional().MaxLen(200).Immutable(),
		field.UUID("tester_user_id", uuid.UUID{}).Optional().Nillable().Immutable(),
		field.UUID("invite_id", uuid.UUID{}).Optional().Nillable().Immutable(),
		field.String("result").Immutable(),
		field.Int("severity").Optional().Nillable().Immutable(),
		field.String("device").Optional().MaxLen(200).Immutable(),
		field.Text("notes").Optional().Immutable(),
		// evidence_keys are private object keys of uploaded evidence (filled once uploads land).
		field.Strings("evidence_keys").Optional().Immutable(),
		field.UUID("defect_id", uuid.UUID{}).Optional().Nillable(),
		field.Bool("is_current").Default(true),
		field.Time("created_at").Default(time.Now).Immutable(),
	}
}

func (QAResult) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("session_id", "criterion_id", "tester_key").
			Unique().
			Annotations(entsql.IndexWhere("is_current")),
		index.Fields("session_id", "created_at"),
	}
}
