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

// QAInvite lets one email address into one session. Testers and observers are kept as the keyed
// hash (to match a signed-in or code-proven email) and a masked hint for the owner's list. Signers
// also keep the address encrypted at rest (email_enc), because close and decision notices go to
// them; it is cleared when an invite stops being a signer.
type QAInvite struct {
	ent.Schema
}

func (QAInvite) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "qa_invites"}}
}

func (QAInvite) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		field.UUID("tenant_id", uuid.UUID{}).Immutable(),
		field.UUID("session_id", uuid.UUID{}).Immutable(),
		field.String("email_hash").NotEmpty().MaxLen(64).Immutable(),
		field.String("email_hint").Optional().MaxLen(200),
		// email_enc is the sealed address (FIELD_ENCRYPTION_KEY), signers only. Sensitive keeps it
		// out of JSON and logs.
		field.String("email_enc").Optional().Sensitive().MaxLen(1000),
		field.String("name").Optional().MaxLen(200),
		field.String("role").Default(qamodel.RoleTester),
		field.String("status").Default(qamodel.InviteInvited),
		field.Time("last_seen_at").Optional().Nillable(),
		field.UUID("invited_by", uuid.UUID{}).Optional().Nillable(),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (QAInvite) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("session_id", "email_hash").Unique(),
	}
}
