package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
)

// ExecutionLease holds the schema definition for the ExecutionLease entity.
type ExecutionLease struct {
	ent.Schema
}

func (ExecutionLease) Fields() []ent.Field {
	return []ent.Field{
		field.String("runner_id").NotEmpty(),
		field.Time("started_at").Default(time.Now),
		field.Time("expires_at"),
		field.Time("created_at").Default(time.Now).Immutable(),
	}
}

func (ExecutionLease) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("task", Task.Type).Ref("execution_lease").Unique().Required(),
	}
}
