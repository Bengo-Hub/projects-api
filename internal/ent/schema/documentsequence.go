package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// DocumentSequence is the per-tenant counter behind human-readable numbers (tender numbers today).
// internal/services/sequence advances it with one INSERT ... ON CONFLICT DO UPDATE ... RETURNING
// inside the caller's transaction, so the row lock serialises concurrent allocations.
type DocumentSequence struct {
	ent.Schema
}

// Fields of the DocumentSequence.
func (DocumentSequence) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		field.UUID("tenant_id", uuid.UUID{}),
		field.String("kind").NotEmpty(),
		field.Int64("last_value").Default(0).Comment("Last number handed out"),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

// Indexes of the DocumentSequence.
func (DocumentSequence) Indexes() []ent.Index {
	return []ent.Index{index.Fields("tenant_id", "kind").Unique()}
}
