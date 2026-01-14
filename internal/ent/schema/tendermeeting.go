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
type TenderMeeting struct {
	ent.Schema
}

// Fields of the TenderMeeting.
func (TenderMeeting) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New).
			Immutable(),
		field.UUID("tenant_id", uuid.UUID{}),
		field.UUID("tender_id", uuid.UUID{}),
		field.UUID("committee_id", uuid.UUID{}).
			Optional(),
		field.String("title").
			NotEmpty(),
		field.String("description").
			Optional(),
		field.String("meeting_type").
			Default("evaluation").
			Comment("evaluation, kickoff, review, decision, clarification"),
		field.Time("scheduled_at"),
		field.Int("duration_minutes").
			Default(60),
		field.String("location").
			Optional().
			Comment("Physical location or 'virtual'"),
		field.String("platform").
			Optional().
			Comment("google_meet, teams, zoom, zoho_meet"),
		field.String("meeting_url").
			Optional(),
		field.String("meeting_id").
			Optional().
			Comment("External meeting ID from platform"),
		field.String("status").
			Default("scheduled").
			Comment("scheduled, in_progress, completed, cancelled"),
		field.JSON("attendees", []uuid.UUID{}).
			Optional().
			Comment("List of invited user IDs"),
		field.String("agenda").
			Optional(),
		field.String("minutes").
			Optional().
			Comment("Meeting minutes/notes"),
		field.JSON("decisions", []map[string]any{}).
			Optional().
			Comment("Decisions made during meeting"),
		field.JSON("action_items", []map[string]any{}).
			Optional().
			Comment("Action items from meeting"),
		field.String("recording_url").
			Optional(),
		field.UUID("organized_by", uuid.UUID{}),
		field.Time("started_at").
			Optional(),
		field.Time("ended_at").
			Optional(),
		field.Time("created_at").
			Default(time.Now).
			Immutable(),
		field.Time("updated_at").
			Default(time.Now).
			UpdateDefault(time.Now),
		field.JSON("metadata", map[string]any{}).
			Optional(),
	}
}

// Edges of the TenderMeeting.
func (TenderMeeting) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("tender", Tender.Type).
			Ref("meetings").
			Field("tender_id").
			Unique().
			Required(),
		edge.From("committee", TenderCommittee.Type).
			Ref("meetings").
			Field("committee_id").
			Unique(),
	}
}

// Indexes of the TenderMeeting.
func (TenderMeeting) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "tender_id"),
		index.Fields("tender_id", "scheduled_at"),
		index.Fields("tender_id", "status"),
	}
}
