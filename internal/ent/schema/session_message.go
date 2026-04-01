package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// SessionMessage holds the schema definition for the SessionMessage entity.
type SessionMessage struct {
	ent.Schema
}

func (SessionMessage) Fields() []ent.Field {
	return []ent.Field{
		field.String("role").NotEmpty(),
		field.String("content_type").Default("text"),
		field.Text("content"),
		field.Int("sequence").Default(0),
		field.Time("created_at").Default(time.Now).Immutable(),
	}
}

func (SessionMessage) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("session", Session.Type).Ref("messages").Unique().Required(),
		edge.To("attachments", Attachment.Type),
	}
}

func (SessionMessage) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("sequence"),
	}
}
