package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// TaskAssignee is one person assigned to a task. It is the only record of who a task is assigned
// to: the primary assignee is the row with is_primary set (at most one per task, kept by the tasks
// service), the others share the work. user_id is the auth-api user id.
type TaskAssignee struct {
	ent.Schema
}

// Fields of the TaskAssignee.
func (TaskAssignee) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		field.UUID("tenant_id", uuid.UUID{}),
		field.UUID("task_id", uuid.UUID{}),
		field.UUID("user_id", uuid.UUID{}).Comment("auth-api user id"),
		field.Bool("is_primary").Default(false),
		field.Time("created_at").Default(time.Now).Immutable(),
	}
}

// Indexes of the TaskAssignee: one row per task and person, and "my tasks" (My Work, the
// assignee filter, the *_own permissions) read by tenant and person.
func (TaskAssignee) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("task_id", "user_id").Unique(),
		index.Fields("tenant_id", "user_id"),
	}
}

// Edges of the TaskAssignee. The task side is edge.From with the task_id field, so ent adds no
// second foreign key column.
func (TaskAssignee) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("task", Task.Type).
			Ref("assignees").
			Field("task_id").
			Unique().
			Required(),
	}
}
