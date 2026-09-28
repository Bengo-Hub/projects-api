package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// TenderMeeting holds the schema definition for the TenderMeeting entity.
type TenderMeeting struct{ ent.Schema }

func (TenderMeeting) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		field.UUID("tender_id", uuid.UUID{}),
		field.UUID("tenant_id", uuid.UUID{}),
		field.String("title").NotEmpty(),
		field.Time("scheduled_at"),
		field.String("platform").Default("physical"),
		field.String("meeting_url").Optional(),
		field.Text("notes").Optional(),
		field.UUID("created_by", uuid.UUID{}),
		field.Time("created_at").Default(time.Now).Immutable(),
	}
}

// Indexes: meetings are always read per tender.
func (TenderMeeting) Indexes() []ent.Index {
	return []ent.Index{index.Fields("tender_id")}
}

func (TenderMeeting) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("tender", Tender.Type).Ref("meetings").Field("tender_id").Unique().Required(),
	}
}
