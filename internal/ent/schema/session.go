package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
)

// Session holds the schema definition for the Session entity.
type Session struct {
	ent.Schema
}

func (Session) Fields() []ent.Field {
	return []ent.Field{
		field.String("provider_session_key").Default(""),
		field.Enum("status").
			Values("created", "streaming", "paused", "closed", "errored").
			Default("created"),
		field.Time("started_at").Optional().Nillable(),
		field.Time("ended_at").Optional().Nillable(),
		field.Time("created_at").Default(time.Now).Immutable(),
	}
}

func (Session) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("task", Task.Type).Ref("sessions").Unique().Required(),
		edge.To("messages", SessionMessage.Type),
		edge.To("events", SessionEvent.Type),
	}
}
