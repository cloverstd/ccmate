package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
)

// CommandAudit holds the schema definition for the CommandAudit entity.
type CommandAudit struct {
	ent.Schema
}

func (CommandAudit) Fields() []ent.Field {
	return []ent.Field{
		field.String("source").NotEmpty(),
		field.String("actor").NotEmpty(),
		field.String("command").NotEmpty(),
		field.Enum("decision").Values("allowed", "denied"),
		field.String("reason").Default(""),
		field.Time("created_at").Default(time.Now).Immutable(),
	}
}

func (CommandAudit) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("task", Task.Type).Ref("command_audits").Unique(),
	}
}
