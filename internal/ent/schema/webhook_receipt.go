package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// WebhookReceipt holds the schema definition for the WebhookReceipt entity.
type WebhookReceipt struct {
	ent.Schema
}

func (WebhookReceipt) Fields() []ent.Field {
	return []ent.Field{
		field.String("provider").NotEmpty(),
		field.String("delivery_id").NotEmpty(),
		field.String("event_type").NotEmpty(),
		field.Time("received_at").Default(time.Now),
		field.Bool("accepted").Default(false),
	}
}

func (WebhookReceipt) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("provider", "delivery_id").Unique(),
	}
}
