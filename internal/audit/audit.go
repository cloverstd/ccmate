package audit

import (
	"context"
	"log/slog"

	"github.com/cloverstd/ccmate/internal/ent"
	"github.com/cloverstd/ccmate/internal/ent/commandaudit"
)

// Logger provides structured audit logging.
type Logger struct {
	client *ent.Client
}

func NewLogger(client *ent.Client) *Logger {
	return &Logger{client: client}
}

// LogCommand records a command execution attempt.
func (l *Logger) LogCommand(ctx context.Context, taskID int, source, actor, command, decision, reason string) {
	builder := l.client.CommandAudit.Create().
		SetSource(source).
		SetActor(actor).
		SetCommand(command).
		SetDecision(commandaudit.Decision(decision)).
		SetReason(reason)

	if taskID > 0 {
		builder = builder.SetTaskID(taskID)
	}

	_, err := builder.Save(ctx)
	if err != nil {
		slog.Error("failed to record command audit", "error", err)
	}

	slog.Info("command audit",
		"task_id", taskID,
		"source", source,
		"actor", actor,
		"command", command,
		"decision", decision,
		"reason", reason,
	)
}

// LogStateChange records a task state transition.
func (l *Logger) LogStateChange(taskID int, from, to string, actor string) {
	slog.Info("task state change",
		"task_id", taskID,
		"from", from,
		"to", to,
		"actor", actor,
	)
}

// LogSecurityEvent records a security-relevant event.
func (l *Logger) LogSecurityEvent(event string, details map[string]interface{}) {
	slog.Warn("security event",
		"event", event,
		"details", details,
	)
}

// LogWebhook records a webhook receipt.
func (l *Logger) LogWebhook(provider, deliveryID, eventType string, accepted bool) {
	slog.Info("webhook received",
		"provider", provider,
		"delivery_id", deliveryID,
		"event_type", eventType,
		"accepted", accepted,
	)
}
