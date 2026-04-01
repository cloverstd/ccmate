package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
)

// Attachment holds the schema definition for the Attachment entity.
type Attachment struct {
	ent.Schema
}

func (Attachment) Fields() []ent.Field {
	return []ent.Field{
		field.String("file_name").NotEmpty(),
		field.String("mime_type").Default("application/octet-stream"),
		field.Int64("size").Default(0),
		field.String("storage_path").NotEmpty(),
		field.Time("created_at").Default(time.Now).Immutable(),
	}
}

func (Attachment) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("task", Task.Type).Ref("attachments").Unique(),
		edge.From("message", SessionMessage.Type).Ref("attachments").Unique(),
	}
}
