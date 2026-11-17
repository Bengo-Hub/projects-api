package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// ProjectRisk is one entry of a project's risk register (sprint 16). Score is likelihood times
// impact, stored so the register sorts and filters on it.
type ProjectRisk struct {
	ent.Schema
}

func (ProjectRisk) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "project_risks"}}
}

func (ProjectRisk) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		field.UUID("tenant_id", uuid.UUID{}),
		field.UUID("project_id", uuid.UUID{}),
		field.String("title").NotEmpty(),
		field.String("description").Optional(),
		field.String("category").Optional(),
		field.Int("likelihood").Comment("1-5"),
		field.Int("impact").Comment("1-5"),
		field.Int("score").Comment("likelihood x impact, 1-25"),
		field.UUID("owner_id", uuid.UUID{}).Optional().Nillable(),
		field.String("mitigation").Optional(),
		field.String("status").Default("open"),
		field.Time("due_date").Optional().Nillable(),
		field.UUID("created_by", uuid.UUID{}).Optional(),
		field.JSON("metadata", map[string]any{}).Optional(),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

// Indexes: the register by status and by score, and the heat map grouping.
func (ProjectRisk) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "project_id", "status"),
		index.Fields("tenant_id", "project_id", "score"),
	}
}
