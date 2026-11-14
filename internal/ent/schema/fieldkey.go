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

// FieldKey is one data key of the field encryption ring (internal/shared/fieldcrypt). The key is
// kept sealed under the root FIELD_ENCRYPTION_KEY, never in the clear. Exactly one key is current:
// new values are sealed under it. A rotation makes a new current key and turns the old one
// "previous"; the re-seal run moves every value to the current key, then retires the previous keys,
// which clears their material. The re-seal progress of a rotation lives on its current key.
type FieldKey struct {
	ent.Schema
}

func (FieldKey) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "field_keys"}}
}

func (FieldKey) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").MaxLen(16).NotEmpty().Immutable(),
		// key_enc is the data key sealed under the root; cleared when the key is retired.
		field.String("key_enc").Optional().Sensitive().MaxLen(512),
		field.String("fingerprint").MaxLen(16).Immutable(),
		// root_fingerprint names the root key_enc is sealed under.
		field.String("root_fingerprint").MaxLen(16).Immutable(),
		field.Enum("status").Values("current", "previous", "retired").Default("current"),
		field.UUID("created_by", uuid.UUID{}).Optional().Nillable().Immutable(),
		field.Time("created_at").Default(time.Now).Immutable(),
		// Re-seal progress of the rotation that made this key current.
		field.Time("reseal_heartbeat_at").Optional().Nillable(),
		field.Time("reseal_done_at").Optional().Nillable(),
		field.Int("resealed").Default(0),
		field.Int("reseal_failed").Default(0),
		field.Time("retired_at").Optional().Nillable(),
	}
}

func (FieldKey) Indexes() []ent.Index {
	return []ent.Index{
		// One current key at a time.
		index.Fields("status").Unique().Annotations(entsql.IndexWhere("status = 'current'")).StorageKey("fieldkey_one_current"),
	}
}
