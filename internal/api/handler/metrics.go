package handler

import (
	"context"
	"fmt"
	"net/http"

	"github.com/cloverstd/ccmate/internal/ent"
	"github.com/cloverstd/ccmate/internal/ent/commandaudit"
	enttask "github.com/cloverstd/ccmate/internal/ent/task"
	"github.com/cloverstd/ccmate/internal/ent/webhookreceipt"
)

// MetricsHandler exposes Prometheus-compatible metrics.
func MetricsHandler(client *ent.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")

		// Task counts by status
		statuses := []enttask.Status{
			enttask.StatusQueued, enttask.StatusRunning, enttask.StatusPaused,
			enttask.StatusWaitingUser, enttask.StatusSucceeded,
			enttask.StatusFailed, enttask.StatusCancelled,
		}
		for _, s := range statuses {
			count, _ := client.Task.Query().Where(enttask.StatusEQ(s)).Count(ctx)
			fmt.Fprintf(w, "ccmate_tasks_total{status=%q} %d\n", s, count)
		}

		// Task counts by type
		for _, t := range []enttask.Type{enttask.TypeIssueImplementation, enttask.TypeReviewFix, enttask.TypeManualFollowup} {
			count, _ := client.Task.Query().Where(enttask.TypeEQ(t)).Count(ctx)
			fmt.Fprintf(w, "ccmate_tasks_by_type{type=%q} %d\n", t, count)
		}

		// Queue depth (pending + queued)
		queueDepth, _ := client.Task.Query().Where(
			enttask.StatusIn(enttask.StatusQueued, enttask.StatusPending),
		).Count(ctx)
		fmt.Fprintf(w, "ccmate_queue_depth %d\n", queueDepth)

		// Active running tasks
		runningCount, _ := client.Task.Query().Where(enttask.StatusEQ(enttask.StatusRunning)).Count(ctx)
		fmt.Fprintf(w, "ccmate_running_tasks %d\n", runningCount)

		// Project count
		projectCount, _ := client.Project.Query().Count(ctx)
		fmt.Fprintf(w, "ccmate_projects_total %d\n", projectCount)

		// Webhook counts
		webhookTotal, _ := client.WebhookReceipt.Query().Count(ctx)
		fmt.Fprintf(w, "ccmate_webhooks_received_total %d\n", webhookTotal)

		webhookAccepted, _ := client.WebhookReceipt.Query().Where(webhookreceipt.Accepted(true)).Count(ctx)
		fmt.Fprintf(w, "ccmate_webhooks_accepted_total %d\n", webhookAccepted)

		webhookRejected := webhookTotal - webhookAccepted
		fmt.Fprintf(w, "ccmate_webhooks_rejected_total %d\n", webhookRejected)

		// Command audit counts
		cmdAllowed, _ := client.CommandAudit.Query().Where(commandaudit.DecisionEQ(commandaudit.DecisionAllowed)).Count(ctx)
		cmdDenied, _ := client.CommandAudit.Query().Where(commandaudit.DecisionEQ(commandaudit.DecisionDenied)).Count(ctx)
		fmt.Fprintf(w, "ccmate_commands_allowed_total %d\n", cmdAllowed)
		fmt.Fprintf(w, "ccmate_commands_denied_total %d\n", cmdDenied)

		// Attachment count and total size
		attachCount, _ := client.Attachment.Query().Count(ctx)
		fmt.Fprintf(w, "ccmate_attachments_total %d\n", attachCount)

		// Agent profile count
		agentCount, _ := client.AgentProfile.Query().Count(ctx)
		fmt.Fprintf(w, "ccmate_agent_profiles_total %d\n", agentCount)
	}
}

// AlertThresholds defines the thresholds for generating alerts.
type AlertThresholds struct {
	MaxQueueDepth      int
	MaxFailureRate     float64 // percentage of failed tasks
	MaxRunningDuration int     // minutes
}

// DefaultAlertThresholds returns the default alerting thresholds.
func DefaultAlertThresholds() AlertThresholds {
	return AlertThresholds{
		MaxQueueDepth:      10,
		MaxFailureRate:     0.3,
		MaxRunningDuration: 60,
	}
}

// CheckAlerts evaluates current state against alert thresholds and returns warnings.
func CheckAlerts(ctx context.Context, client *ent.Client, thresholds AlertThresholds) []string {
	var alerts []string

	// Queue depth alert
	queueDepth, _ := client.Task.Query().Where(
		enttask.StatusIn(enttask.StatusQueued, enttask.StatusPending),
	).Count(ctx)
	if queueDepth > thresholds.MaxQueueDepth {
		alerts = append(alerts, fmt.Sprintf("ALERT: queue depth %d exceeds threshold %d", queueDepth, thresholds.MaxQueueDepth))
	}

	// Failure rate alert
	total, _ := client.Task.Query().Count(ctx)
	failed, _ := client.Task.Query().Where(enttask.StatusEQ(enttask.StatusFailed)).Count(ctx)
	if total > 10 {
		failRate := float64(failed) / float64(total)
		if failRate > thresholds.MaxFailureRate {
			alerts = append(alerts, fmt.Sprintf("ALERT: failure rate %.1f%% exceeds threshold %.1f%%", failRate*100, thresholds.MaxFailureRate*100))
		}
	}

	// Webhook signature failure surge
	rejected, _ := client.WebhookReceipt.Query().Where(webhookreceipt.Accepted(false)).Count(ctx)
	if rejected > 50 {
		alerts = append(alerts, fmt.Sprintf("ALERT: %d rejected webhooks, possible signature attack", rejected))
	}

	return alerts
}
