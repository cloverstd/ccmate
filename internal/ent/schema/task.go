package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// Task holds the schema definition for the Task entity.
type Task struct {
	ent.Schema
}

func (Task) Fields() []ent.Field {
	return []ent.Field{
		field.Int("issue_number"),
		field.Int("pr_number").Optional().Nillable(),
		field.Int("agent_profile_id").Optional().Nillable(),
		field.Enum("type").Values("issue_implementation", "review_fix", "manual_followup"),
		field.Enum("status").
			Values("pending", "queued", "running", "paused", "waiting_user", "succeeded", "failed", "cancelled").
			Default("queued"),
		field.Int("priority").Default(0),
		field.String("trigger_source").Default("webhook"),
		field.Int("current_session_id").Optional().Nillable(),
		field.String("telegram_chat_id").Optional().Nillable().Sensitive(),
		field.Int64("telegram_message_id").Optional().Nillable(),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (Task) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("project", Project.Type).Ref("tasks").Unique().Required(),
		edge.To("sessions", Session.Type),
		edge.To("prompt_snapshot", PromptTemplateSnapshot.Type).Unique(),
		edge.To("attachments", Attachment.Type),
		edge.To("command_audits", CommandAudit.Type),
		edge.To("execution_lease", ExecutionLease.Type).Unique(),
	}
}

func (Task) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("status"),
		index.Fields("issue_number"),
		index.Fields("telegram_chat_id", "telegram_message_id"),
	}
}
