package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
)

// PromptTemplateSnapshot holds the schema definition for the PromptTemplateSnapshot entity.
type PromptTemplateSnapshot struct {
	ent.Schema
}

func (PromptTemplateSnapshot) Fields() []ent.Field {
	return []ent.Field{
		field.Text("system_prompt"),
		field.Text("task_prompt"),
		field.String("model_name"),
		field.String("model_version").Default(""),
		field.Time("created_at").Default(time.Now).Immutable(),
	}
}

func (PromptTemplateSnapshot) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("task", Task.Type).Ref("prompt_snapshot").Unique().Required(),
	}
}
