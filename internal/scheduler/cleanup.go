package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/cloverstd/ccmate/internal/config"
	"github.com/cloverstd/ccmate/internal/ent"
	"github.com/cloverstd/ccmate/internal/ent/commandaudit"
	enttask "github.com/cloverstd/ccmate/internal/ent/task"
	"github.com/cloverstd/ccmate/internal/ent/webhookreceipt"
)

// RunCleanup starts the periodic data retention cleanup.
func RunCleanup(ctx context.Context, client *ent.Client, cfg *config.Config) {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	slog.Info("cleanup scheduler started")

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cleanupWorkspaces(ctx, client, cfg)
			cleanupWebhookReceipts(ctx, client, cfg)
			cleanupAuditLogs(ctx, client, cfg)
		}
	}
}

func cleanupWorkspaces(ctx context.Context, client *ent.Client, cfg *config.Config) {
	successCutoff := time.Now().AddDate(0, 0, -cfg.Limits.RetentionSuccessDays)
	failureCutoff := time.Now().AddDate(0, 0, -cfg.Limits.RetentionFailureDays)

	tasks, err := client.Task.Query().
		Where(enttask.StatusEQ(enttask.StatusSucceeded), enttask.UpdatedAtLT(successCutoff)).
		WithProject().All(ctx)
	if err != nil {
		slog.Error("cleanup: failed to query succeeded tasks", "error", err)
		return
	}

	for _, t := range tasks {
		if t.Edges.Project != nil {
			wsPath := filepath.Join(cfg.Storage.WorkspacesDir,
				fmt.Sprintf("%d", t.Edges.Project.ID),
				fmt.Sprintf("%d", t.ID))
			if err := os.RemoveAll(wsPath); err != nil && !os.IsNotExist(err) {
				slog.Warn("cleanup: failed to remove workspace", "path", wsPath, "error", err)
			}
		}
	}

	failedTasks, err := client.Task.Query().
		Where(
			enttask.StatusIn(enttask.StatusFailed, enttask.StatusCancelled),
			enttask.UpdatedAtLT(failureCutoff),
		).WithProject().All(ctx)
	if err != nil {
		return
	}

	for _, t := range failedTasks {
		if t.Edges.Project != nil {
			wsPath := filepath.Join(cfg.Storage.WorkspacesDir,
				fmt.Sprintf("%d", t.Edges.Project.ID),
				fmt.Sprintf("%d", t.ID))
			_ = os.RemoveAll(wsPath)
		}
	}

	cleaned := len(tasks) + len(failedTasks)
	if cleaned > 0 {
		slog.Info("cleanup: removed workspaces", "count", cleaned)
	}
}

func cleanupWebhookReceipts(ctx context.Context, client *ent.Client, cfg *config.Config) {
	cutoff := time.Now().AddDate(0, 0, -cfg.Limits.RetentionAuditDays)
	deleted, err := client.WebhookReceipt.Delete().
		Where(webhookreceipt.ReceivedAtLT(cutoff)).Exec(ctx)
	if err != nil {
		slog.Error("cleanup: failed to delete old webhook receipts", "error", err)
		return
	}
	if deleted > 0 {
		slog.Info("cleanup: removed webhook receipts", "count", deleted)
	}
}

func cleanupAuditLogs(ctx context.Context, client *ent.Client, cfg *config.Config) {
	cutoff := time.Now().AddDate(0, 0, -cfg.Limits.RetentionAuditDays)
	deleted, err := client.CommandAudit.Delete().
		Where(commandaudit.CreatedAtLT(cutoff)).Exec(ctx)
	if err != nil {
		slog.Error("cleanup: failed to delete old audit logs", "error", err)
		return
	}
	if deleted > 0 {
		slog.Info("cleanup: removed audit logs", "count", deleted)
	}
}
