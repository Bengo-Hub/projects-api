package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

// Attachment holds the schema definition for the Attachment entity.
type Attachment struct {
	ent.Schema
}

// Fields of the Attachment.
func (Attachment) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New).
			Immutable(),
		field.UUID("tenant_id", uuid.UUID{}),
		field.UUID("project_id", uuid.UUID{}).
			Optional(),
		field.UUID("task_id", uuid.UUID{}).
			Optional(),
		field.String("file_url").
			NotEmpty(),
		field.String("file_name").
			NotEmpty(),
		field.Int64("file_size"),
		field.String("mime_type").
			Optional(),
		field.UUID("uploaded_by", uuid.UUID{}),
		field.Time("uploaded_at").
			Default(time.Now),
		field.JSON("metadata", map[string]any{}).
			Optional(),
	}
}

// Edges of the Attachment.
func (Attachment) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("project", Project.Type).
			Ref("attachments").
			Field("project_id").
			Unique(),
		edge.From("task", Task.Type).
			Ref("attachments").
			Field("task_id").
			Unique(),
	}
}

