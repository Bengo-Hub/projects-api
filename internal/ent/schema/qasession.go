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

// QASession is one acceptance run: who runs it, what it accepts (a service tag, a project or a
// free title), a frozen copy of the template's criteria and the pass rule. metadata holds the
// tenant slug and name for branding, and the outcome computed at close.
type QASession struct {
	ent.Schema
}

func (QASession) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "qa_sessions"}}
}

func (QASession) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		field.UUID("tenant_id", uuid.UUID{}).Immutable(),
		field.UUID("template_id", uuid.UUID{}).Optional().Nillable().Immutable(),
		field.Int("template_version").Default(1).Immutable(),
		field.String("title").NotEmpty().MaxLen(200),
		field.String("subject_kind").Default(qamodel.SessionSubjectGeneral),
		// subject_ref is the service tag or the project id; empty for a general session.
		field.String("subject_ref").Optional().MaxLen(100),
		field.JSON("criteria_snapshot", []qamodel.SnapshotCriterion{}).Optional(),
		field.JSON("pass_rule", qamodel.PassRule{}).Optional(),
		field.String("status").Default(qamodel.SessionDraft),
		// share_code is the random part of the /qa/{code} link. The link alone grants nothing:
		// an invited email must still be proven.
		field.String("share_code").NotEmpty().Unique().Immutable(),
		field.UUID("owner_id", uuid.UUID{}),
		field.Time("opened_at").Optional().Nillable(),
		field.Time("closed_at").Optional().Nillable(),
		// document_hash is the SHA-256 of the report PDF rendered at close.
		field.String("document_hash").Optional().MaxLen(64),
		field.JSON("metadata", map[string]any{}).Optional(),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (QASession) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "created_at"),
		index.Fields("tenant_id", "status"),
	}
}
