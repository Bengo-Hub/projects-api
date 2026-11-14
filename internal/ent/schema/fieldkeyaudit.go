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

// FieldKeyAudit records who did what to the field encryption keys: a backup download, a rotation
// started, a key retired. It names keys by id and fingerprint only, never by material.
type FieldKeyAudit struct {
	ent.Schema
}

func (FieldKeyAudit) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "field_key_audits"}}
}

func (FieldKeyAudit) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		// action: backup, rotate, retire.
		field.String("action").MaxLen(32).Immutable(),
		field.UUID("actor_id", uuid.UUID{}).Optional().Nillable().Immutable(),
		field.String("actor_email").Optional().MaxLen(254).Immutable(),
		field.String("key_id").Optional().MaxLen(16).Immutable(),
		field.String("fingerprint").Optional().MaxLen(16).Immutable(),
		field.String("root_fingerprint").Optional().MaxLen(16).Immutable(),
		field.String("detail").Optional().MaxLen(500).Immutable(),
		field.Time("created_at").Default(time.Now).Immutable(),
	}
}

func (FieldKeyAudit) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("action", "created_at"),
	}
}
