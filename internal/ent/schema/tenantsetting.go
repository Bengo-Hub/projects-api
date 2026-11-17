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

// TenantSetting holds a tenant's projects settings as one JSON document (sprint 16 starts it with
// the workload capacity: weekly hours for everyone and per person).
type TenantSetting struct {
	ent.Schema
}

func (TenantSetting) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "tenant_settings"}}
}

func (TenantSetting) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		field.UUID("tenant_id", uuid.UUID{}),
		field.JSON("settings", map[string]any{}).Optional(),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (TenantSetting) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id").Unique(),
	}
}
