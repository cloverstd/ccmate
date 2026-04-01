package scheduler

import (
	"context"
	"fmt"

	"github.com/cloverstd/ccmate/internal/ent"
	enttask "github.com/cloverstd/ccmate/internal/ent/task"
	"github.com/cloverstd/ccmate/internal/model"
)

// validTransitions defines the allowed state transitions for tasks.
var validTransitions = map[model.TaskStatus][]model.TaskStatus{
	model.TaskStatusPending:     {model.TaskStatusQueued, model.TaskStatusCancelled},
	model.TaskStatusQueued:      {model.TaskStatusRunning, model.TaskStatusCancelled},
	model.TaskStatusRunning:     {model.TaskStatusPaused, model.TaskStatusWaitingUser, model.TaskStatusSucceeded, model.TaskStatusFailed, model.TaskStatusCancelled},
	model.TaskStatusPaused:      {model.TaskStatusQueued, model.TaskStatusCancelled},
	model.TaskStatusWaitingUser: {model.TaskStatusRunning, model.TaskStatusCancelled},
	model.TaskStatusFailed:      {model.TaskStatusQueued, model.TaskStatusCancelled},
}

// IsValidTransition checks if a state transition is allowed.
func IsValidTransition(from, to model.TaskStatus) bool {
	targets, ok := validTransitions[from]
	if !ok {
		return false
	}
	for _, t := range targets {
		if t == to {
			return true
		}
	}
	return false
}

// TransitionTask atomically transitions a task from one state to another.
func TransitionTask(ctx context.Context, client *ent.Client, taskID int, from, to model.TaskStatus) error {
	if !IsValidTransition(from, to) {
		return fmt.Errorf("invalid state transition: %s -> %s", from, to)
	}

	tx, err := client.Tx(ctx)
	if err != nil {
		return fmt.Errorf("starting transaction: %w", err)
	}

	// Lock and verify current state
	t, err := tx.Task.Query().
		Where(enttask.ID(taskID), enttask.StatusEQ(enttask.Status(from))).
		Only(ctx)
	if err != nil {
		_ = tx.Rollback()
		if ent.IsNotFound(err) {
			return fmt.Errorf("task %d not in expected state %s", taskID, from)
		}
		return fmt.Errorf("querying task: %w", err)
	}

	_, err = tx.Task.UpdateOne(t).
		SetStatus(enttask.Status(to)).
		Save(ctx)
	if err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("updating task status: %w", err)
	}

	return tx.Commit()
}
