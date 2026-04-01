package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

// AgentProfile holds the schema definition for the AgentProfile entity.
type AgentProfile struct {
	ent.Schema
}

func (AgentProfile) Fields() []ent.Field {
	return []ent.Field{
		field.String("provider").NotEmpty(),
		field.String("model").NotEmpty(),
		field.Bool("supports_image").Default(false),
		field.Bool("supports_resume").Default(false),
		field.Text("config_json").Default("{}"),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
