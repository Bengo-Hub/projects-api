package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// TimeLog holds the schema definition for the TimeLog entity.
type TimeLog struct{ ent.Schema }

func (TimeLog) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		field.UUID("project_id", uuid.UUID{}),
		field.UUID("task_id", uuid.UUID{}).Optional(),
		field.UUID("tenant_id", uuid.UUID{}),
		field.UUID("user_id", uuid.UUID{}),
		field.Float("hours"),
		field.Text("description").Optional(),
		field.Time("logged_date"),
		field.Bool("is_billable").Default(false),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (TimeLog) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("project", Project.Type).Ref("time_logs").Field("project_id").Unique().Required(),
	}
}

func (TimeLog) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "project_id"),
		index.Fields("tenant_id", "user_id"),
	}
}
