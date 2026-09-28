package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// TaskDependency holds the schema definition for the TaskDependency entity.
type TaskDependency struct {
	ent.Schema
}

// Fields of the TaskDependency.
func (TaskDependency) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New).
			Immutable(),
		field.UUID("task_id", uuid.UUID{}),
		field.UUID("depends_on_task_id", uuid.UUID{}),
		field.String("dependency_type").
			Default("blocks"),
		field.Time("created_at").
			Default(time.Now).
			Immutable(),
	}
}

// Indexes of the TaskDependency: the cycle check walks the graph from both ends.
func (TaskDependency) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("task_id", "depends_on_task_id"),
		index.Fields("depends_on_task_id"),
	}
}

// Edges of the TaskDependency.
func (TaskDependency) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("task", Task.Type).
			Ref("dependencies").
			Field("task_id").
			Unique().
			Required(),
	}
}

