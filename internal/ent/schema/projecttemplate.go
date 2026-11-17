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

// ProjectTemplate is a reusable project plan (sprint 16): phases, tasks with offsets and
// durations, and roles. A system template has no tenant (tenant_id NULL) and a system_key; it is
// seeded at startup and shown to every tenant read-only.
type ProjectTemplate struct {
	ent.Schema
}

func (ProjectTemplate) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "project_templates"}}
}

func (ProjectTemplate) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		field.UUID("tenant_id", uuid.UUID{}).Optional().Nillable().Comment("NULL for a system template"),
		field.String("system_key").Optional().Nillable().Comment("stable key of a seeded system template"),
		field.String("name").NotEmpty(),
		field.String("description").Optional(),
		field.String("category").Optional(),
		field.JSON("definition", planmodel.TemplateDefinition{}),
		field.Int("task_count").Default(0),
		field.UUID("source_project_id", uuid.UUID{}).Optional().Nillable(),
		field.UUID("created_by", uuid.UUID{}).Optional().Nillable(),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

// Indexes: a tenant's templates by name, and one row per system key.
func (ProjectTemplate) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "name"),
		index.Fields("system_key").Unique(),
	}
}
