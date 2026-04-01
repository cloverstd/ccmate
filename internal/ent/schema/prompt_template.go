package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
)

// PromptTemplate holds the schema definition for the PromptTemplate entity.
type PromptTemplate struct {
	ent.Schema
}

func (PromptTemplate) Fields() []ent.Field {
	return []ent.Field{
		field.String("name").NotEmpty(),
		field.Text("system_prompt").Default(""),
		field.Text("task_prompt").Default(""),
		field.Bool("is_builtin").Default(false),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (PromptTemplate) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("label_rules", ProjectLabelRule.Type),
	}
}
