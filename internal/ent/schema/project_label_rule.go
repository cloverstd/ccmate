package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
)

// ProjectLabelRule holds the schema definition for the ProjectLabelRule entity.
type ProjectLabelRule struct {
	ent.Schema
}

func (ProjectLabelRule) Fields() []ent.Field {
	return []ent.Field{
		field.String("issue_label").NotEmpty(),
		field.Enum("trigger_mode").Values("auto", "manual").Default("auto"),
	}
}

func (ProjectLabelRule) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("project", Project.Type).Ref("label_rules").Unique().Required(),
		edge.From("prompt_template", PromptTemplate.Type).Ref("label_rules").Unique(),
	}
}
