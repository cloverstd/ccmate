package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// SessionEvent holds the schema definition for the SessionEvent entity.
type SessionEvent struct {
	ent.Schema
}

func (SessionEvent) Fields() []ent.Field {
	return []ent.Field{
		field.String("event_type").NotEmpty(),
		field.Text("payload_json").Default("{}"),
		field.Int("sequence").Default(0),
		field.Time("created_at").Default(time.Now).Immutable(),
	}
}

func (SessionEvent) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("session", Session.Type).Ref("events").Unique().Required(),
	}
}

func (SessionEvent) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("sequence"),
	}
}
