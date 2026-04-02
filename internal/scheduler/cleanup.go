package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/cloverstd/ccmate/internal/ent"
	"github.com/cloverstd/ccmate/internal/ent/commandaudit"
	enttask "github.com/cloverstd/ccmate/internal/ent/task"
	"github.com/cloverstd/ccmate/internal/ent/webhookreceipt"
	"github.com/cloverstd/ccmate/internal/settings"
)

func RunCleanup(ctx context.Context, client *ent.Client, mgr *settings.Manager) {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	slog.Info("cleanup scheduler started")

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cleanupWorkspaces(ctx, client, mgr)
			cleanupWebhookReceipts(ctx, client, mgr)
			cleanupAuditLogs(ctx, client, mgr)
		}
	}
}

func cleanupWorkspaces(ctx context.Context, client *ent.Client, mgr *settings.Manager) {
	successDays, _ := strconv.Atoi(mgr.GetWithDefault(ctx, "retention_success_days", "7"))
	failureDays, _ := strconv.Atoi(mgr.GetWithDefault(ctx, "retention_failure_days", "30"))
	wsDir := filepath.Join(mgr.GetWithDefault(ctx, settings.KeyStorageBasePath, "data"), "workspaces")

	successCutoff := time.Now().AddDate(0, 0, -successDays)
	failureCutoff := time.Now().AddDate(0, 0, -failureDays)

	tasks, _ := client.Task.Query().
		Where(enttask.StatusEQ(enttask.StatusSucceeded), enttask.UpdatedAtLT(successCutoff)).
		WithProject().All(ctx)

	for _, t := range tasks {
		if t.Edges.Project != nil {
			path := filepath.Join(wsDir, fmt.Sprintf("%d", t.Edges.Project.ID), fmt.Sprintf("%d", t.ID))
			os.RemoveAll(path)
		}
	}

	failedTasks, _ := client.Task.Query().
		Where(enttask.StatusIn(enttask.StatusFailed, enttask.StatusCancelled), enttask.UpdatedAtLT(failureCutoff)).
		WithProject().All(ctx)

	for _, t := range failedTasks {
		if t.Edges.Project != nil {
			path := filepath.Join(wsDir, fmt.Sprintf("%d", t.Edges.Project.ID), fmt.Sprintf("%d", t.ID))
			os.RemoveAll(path)
		}
	}

	if cleaned := len(tasks) + len(failedTasks); cleaned > 0 {
		slog.Info("cleanup: removed workspaces", "count", cleaned)
	}
}

func cleanupWebhookReceipts(ctx context.Context, client *ent.Client, mgr *settings.Manager) {
	auditDays, _ := strconv.Atoi(mgr.GetWithDefault(ctx, "retention_audit_days", "180"))
	cutoff := time.Now().AddDate(0, 0, -auditDays)
	deleted, _ := client.WebhookReceipt.Delete().Where(webhookreceipt.ReceivedAtLT(cutoff)).Exec(ctx)
	if deleted > 0 {
		slog.Info("cleanup: removed webhook receipts", "count", deleted)
	}
}

func cleanupAuditLogs(ctx context.Context, client *ent.Client, mgr *settings.Manager) {
	auditDays, _ := strconv.Atoi(mgr.GetWithDefault(ctx, "retention_audit_days", "180"))
	cutoff := time.Now().AddDate(0, 0, -auditDays)
	deleted, _ := client.CommandAudit.Delete().Where(commandaudit.CreatedAtLT(cutoff)).Exec(ctx)
	if deleted > 0 {
		slog.Info("cleanup: removed audit logs", "count", deleted)
	}
}
