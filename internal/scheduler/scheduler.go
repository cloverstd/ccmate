package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/cloverstd/ccmate/internal/agentprovider"
	"github.com/cloverstd/ccmate/internal/config"
	"github.com/cloverstd/ccmate/internal/ent"
	"github.com/cloverstd/ccmate/internal/ent/project"
	enttask "github.com/cloverstd/ccmate/internal/ent/task"
	"github.com/cloverstd/ccmate/internal/gitprovider"
	"github.com/cloverstd/ccmate/internal/model"
	"github.com/cloverstd/ccmate/internal/runner"
	"github.com/cloverstd/ccmate/internal/sse"
)

// Scheduler manages task scheduling, concurrency control, and lifecycle.
type Scheduler struct {
	client       *ent.Client
	cfg          *config.Config
	broker       *sse.Broker
	gitProvider  gitprovider.GitProvider
	agentAdapter agentprovider.AgentAdapter

	mu      sync.Mutex
	running map[int]context.CancelFunc // taskID -> cancel function
}

func New(client *ent.Client, cfg *config.Config, broker *sse.Broker) *Scheduler {
	return &Scheduler{
		client:  client,
		cfg:     cfg,
		broker:  broker,
		running: make(map[int]context.CancelFunc),
	}
}

// SetProviders configures the git and agent providers for task execution.
func (s *Scheduler) SetProviders(gp gitprovider.GitProvider, aa agentprovider.AgentAdapter) {
	s.gitProvider = gp
	s.agentAdapter = aa
}

// Run starts the scheduler loop.
func (s *Scheduler) Run(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	slog.Info("scheduler started")

	for {
		select {
		case <-ctx.Done():
			slog.Info("scheduler stopped")
			return
		case <-ticker.C:
			s.tick(ctx)
		}
	}
}

func (s *Scheduler) tick(ctx context.Context) {
	// Check for timed-out tasks
	s.checkTimeouts(ctx)

	// Check for auto-retries with exponential backoff
	s.CheckAutoRetries(ctx)

	// Find queued tasks and try to schedule them
	tasks, err := s.client.Task.Query().
		Where(enttask.StatusEQ(enttask.StatusQueued)).
		WithProject().
		Order(ent.Asc(enttask.FieldCreatedAt)).
		All(ctx)
	if err != nil {
		slog.Error("failed to query queued tasks", "error", err)
		return
	}

	for _, t := range tasks {
		proj := t.Edges.Project
		if proj == nil {
			continue
		}

		// Check project concurrency
		runningCount, err := s.client.Task.Query().
			Where(
				enttask.HasProjectWith(project.ID(proj.ID)),
				enttask.StatusEQ(enttask.StatusRunning),
			).
			Count(ctx)
		if err != nil {
			slog.Error("failed to count running tasks", "error", err)
			continue
		}

		if runningCount >= proj.MaxConcurrency {
			continue
		}

		// Transition to running and start execution
		err = TransitionTask(ctx, s.client, t.ID, model.TaskStatusQueued, model.TaskStatusRunning)
		if err != nil {
			slog.Error("failed to transition task", "task_id", t.ID, "error", err)
			continue
		}

		// Start task execution in background
		s.startTask(ctx, t.ID)
	}
}

func (s *Scheduler) startTask(parentCtx context.Context, taskID int) {
	s.mu.Lock()
	taskCtx, cancel := context.WithTimeout(parentCtx,
		time.Duration(s.cfg.Limits.TaskTimeoutMinutes)*time.Minute)
	s.running[taskID] = cancel
	s.mu.Unlock()

	go func() {
		defer func() {
			s.mu.Lock()
			delete(s.running, taskID)
			s.mu.Unlock()
			cancel()
		}()

		if s.gitProvider == nil || s.agentAdapter == nil {
			slog.Error("providers not configured, cannot run task", "task_id", taskID)
			_, _ = s.client.Task.UpdateOneID(taskID).
				SetStatus(enttask.StatusFailed).
				Save(parentCtx)
			return
		}

		r := runner.New(s.client, s.cfg, s.broker, s.gitProvider, s.agentAdapter)
		if err := r.RunTask(taskCtx, taskID); err != nil {
			slog.Error("task execution failed", "task_id", taskID, "error", err)
		}
	}()
}

func (s *Scheduler) checkTimeouts(ctx context.Context) {
	timeout := time.Duration(s.cfg.Limits.TaskTimeoutMinutes) * time.Minute
	cutoff := time.Now().Add(-timeout)

	tasks, err := s.client.Task.Query().
		Where(
			enttask.StatusEQ(enttask.StatusRunning),
			enttask.UpdatedAtLT(cutoff),
		).
		All(ctx)
	if err != nil {
		return
	}

	for _, t := range tasks {
		slog.Warn("task timed out", "task_id", t.ID)

		s.mu.Lock()
		if cancel, ok := s.running[t.ID]; ok {
			cancel() // Cancel context triggers process termination
			delete(s.running, t.ID)
		}
		s.mu.Unlock()

		_, _ = s.client.Task.UpdateOneID(t.ID).
			SetStatus(enttask.StatusFailed).
			SetPriority(99). // Mark as non-retryable (timeout)
			Save(ctx)

		s.broker.Publish(fmt.Sprintf("task:%d", t.ID), sse.Event{
			Type: "task.failed",
			Data: map[string]interface{}{"task_id": t.ID, "error": "task timed out", "retryable": false},
		})
	}
}

// PauseTask transitions a running task to paused state, saving session snapshot.
func (s *Scheduler) PauseTask(ctx context.Context, taskID int) error {
	// Save session snapshot before pausing
	s.saveSessionSnapshot(ctx, taskID)

	err := TransitionTask(ctx, s.client, taskID, model.TaskStatusRunning, model.TaskStatusPaused)
	if err != nil {
		return err
	}

	// Cancel the running task
	s.mu.Lock()
	if cancel, ok := s.running[taskID]; ok {
		cancel()
		delete(s.running, taskID)
	}
	s.mu.Unlock()

	return nil
}

// saveSessionSnapshot captures the current session state for later recovery.
func (s *Scheduler) saveSessionSnapshot(ctx context.Context, taskID int) {
	t, err := s.client.Task.Get(ctx, taskID)
	if err != nil || t.CurrentSessionID == nil {
		return
	}

	sess, err := s.client.Session.Get(ctx, *t.CurrentSessionID)
	if err != nil {
		return
	}

	// Mark session as paused with current timestamp
	now := time.Now()
	_, _ = s.client.Session.UpdateOne(sess).
		SetStatus("paused").
		SetEndedAt(now).
		Save(ctx)

	slog.Info("session snapshot saved on pause", "task_id", taskID, "session_id", sess.ID)
}

// ResumeTask transitions a paused task back to queued.
func (s *Scheduler) ResumeTask(ctx context.Context, taskID int) error {
	return TransitionTask(ctx, s.client, taskID, model.TaskStatusPaused, model.TaskStatusQueued)
}

// RetryTask transitions a failed task back to queued.
func (s *Scheduler) RetryTask(ctx context.Context, taskID int) error {
	return TransitionTask(ctx, s.client, taskID, model.TaskStatusFailed, model.TaskStatusQueued)
}

// CancelTask cancels a task.
func (s *Scheduler) CancelTask(ctx context.Context, taskID int) error {
	t, err := s.client.Task.Get(ctx, taskID)
	if err != nil {
		return fmt.Errorf("getting task: %w", err)
	}

	if !model.TaskStatus(t.Status.String()).IsActive() {
		return fmt.Errorf("task is not in an active state")
	}

	s.mu.Lock()
	if cancel, ok := s.running[taskID]; ok {
		cancel()
		delete(s.running, taskID)
	}
	s.mu.Unlock()

	_, err = s.client.Task.UpdateOneID(taskID).
		SetStatus(enttask.StatusCancelled).
		Save(ctx)
	return err
}

// HandleUserInput processes user input for a waiting task.
func (s *Scheduler) HandleUserInput(ctx context.Context, taskID int, event model.AgentEvent) error {
	return TransitionTask(ctx, s.client, taskID, model.TaskStatusWaitingUser, model.TaskStatusRunning)
}
