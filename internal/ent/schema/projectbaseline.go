package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"

	"github.com/bengobox/projects-service/internal/planmodel"
)

// ProjectBaseline is one saved snapshot of a project's plan (sprint 16): task dates and estimates
// and milestone dates, kept as one JSON document per save so the Gantt chart can draw it under
// the live bars and the slips report can compare against it.
type ProjectBaseline struct {
	ent.Schema
}

func (ProjectBaseline) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "project_baselines"}}
}

func (ProjectBaseline) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		field.UUID("tenant_id", uuid.UUID{}),
		field.UUID("project_id", uuid.UUID{}),
		field.String("name").NotEmpty(),
		field.String("note").Optional(),
		field.JSON("snapshot", planmodel.Snapshot{}),
		field.Int("task_count").Default(0),
		field.UUID("created_by", uuid.UUID{}).Optional(),
		field.Time("created_at").Default(time.Now).Immutable(),
	}
}

// Indexes: a project's baselines newest first (the latest is the Gantt default).
func (ProjectBaseline) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "project_id", "created_at"),
	}
}
