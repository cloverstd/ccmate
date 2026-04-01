package scheduler

import (
	"context"
	"log/slog"
	"math"
	"time"

	enttask "github.com/cloverstd/ccmate/internal/ent/task"
	"github.com/cloverstd/ccmate/internal/model"
)

const maxAutoRetries = 3

// CheckAutoRetries scans failed tasks that are eligible for automatic retry.
func (s *Scheduler) CheckAutoRetries(ctx context.Context) {
	tasks, err := s.client.Task.Query().
		Where(
			enttask.StatusEQ(enttask.StatusFailed),
			enttask.PriorityLT(maxAutoRetries), // reuse priority field as retry count
		).
		All(ctx)
	if err != nil {
		return
	}

	for _, t := range tasks {
		retryCount := t.Priority
		backoff := time.Duration(math.Pow(2, float64(retryCount))) * 30 * time.Second

		if time.Since(t.UpdatedAt) < backoff {
			continue // not yet time for retry
		}

		slog.Info("auto-retrying task",
			"task_id", t.ID,
			"retry", retryCount+1,
			"backoff", backoff.String(),
		)

		err := TransitionTask(ctx, s.client, t.ID, model.TaskStatusFailed, model.TaskStatusQueued)
		if err != nil {
			slog.Error("failed to auto-retry task", "task_id", t.ID, "error", err)
			continue
		}

		// Increment retry count
		_, _ = s.client.Task.UpdateOneID(t.ID).
			SetPriority(retryCount + 1).
			Save(ctx)
	}
}
