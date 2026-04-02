package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"strconv"

	"github.com/cloverstd/ccmate/internal/agentprovider"
	"github.com/cloverstd/ccmate/internal/ent"
	enttask "github.com/cloverstd/ccmate/internal/ent/task"
	"github.com/cloverstd/ccmate/internal/gitprovider"
	"github.com/cloverstd/ccmate/internal/model"
	"github.com/cloverstd/ccmate/internal/runner"
	"github.com/cloverstd/ccmate/internal/settings"
	"github.com/cloverstd/ccmate/internal/sse"
)

// Scheduler manages task scheduling, concurrency control, and lifecycle.
type Scheduler struct {
	client       *ent.Client
	settingsMgr  *settings.Manager
	broker       *sse.Broker
	gitProvider  gitprovider.GitProvider
	agentAdapter agentprovider.AgentAdapter

	mu             sync.Mutex
	running        map[int]context.CancelFunc
	runningHandles map[int]*agentprovider.SessionHandle
}

func New(client *ent.Client, settingsMgr *settings.Manager, broker *sse.Broker) *Scheduler {
	return &Scheduler{
		client:         client,
		settingsMgr:    settingsMgr,
		broker:         broker,
		running:        make(map[int]context.CancelFunc),
		runningHandles: make(map[int]*agentprovider.SessionHandle),
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

	// Global max concurrency
	maxConcStr := s.settingsMgr.GetWithDefault(ctx, settings.KeyMaxConcurrency, "2")
	maxConc, _ := strconv.Atoi(maxConcStr)
	if maxConc <= 0 {
		maxConc = 2
	}

	// Check global running count
	globalRunning, _ := s.client.Task.Query().
		Where(enttask.StatusEQ(enttask.StatusRunning)).Count(ctx)

	for _, t := range tasks {
		if globalRunning >= maxConc {
			break
		}

		// Transition to running and start execution
		err = TransitionTask(ctx, s.client, t.ID, model.TaskStatusQueued, model.TaskStatusRunning)
		if err != nil {
			slog.Error("failed to transition task", "task_id", t.ID, "error", err)
			continue
		}

		// Start task execution in background
		s.startTask(ctx, t.ID)
		globalRunning++
	}
}

func (s *Scheduler) startTask(parentCtx context.Context, taskID int) {
	timeoutMin, _ := strconv.Atoi(s.settingsMgr.GetWithDefault(parentCtx, settings.KeyTaskTimeoutMin, "60"))
	if timeoutMin <= 0 {
		timeoutMin = 60
	}
	s.mu.Lock()
	taskCtx, cancel := context.WithTimeout(parentCtx, time.Duration(timeoutMin)*time.Minute)
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

		r := runner.New(s.client, s.settingsMgr, s.broker, s.gitProvider, s.agentAdapter)
		r.OnHandleReady = func(h *agentprovider.SessionHandle) {
			s.mu.Lock()
			s.runningHandles[taskID] = h
			s.mu.Unlock()
		}
		if err := r.RunTask(taskCtx, taskID); err != nil {
			slog.Error("task execution failed", "task_id", taskID, "error", err)
		}
		s.mu.Lock()
		delete(s.runningHandles, taskID)
		s.mu.Unlock()
	}()
}

func (s *Scheduler) checkTimeouts(ctx context.Context) {
	timeoutMin, _ := strconv.Atoi(s.settingsMgr.GetWithDefault(ctx, settings.KeyTaskTimeoutMin, "60"))
	timeout := time.Duration(timeoutMin) * time.Minute
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

// ResumeWithMessage resumes a stopped task by starting a new agent session with --resume
// and sending the user's message.
func (s *Scheduler) ResumeWithMessage(ctx context.Context, taskID int, message string) error {
	t, err := s.client.Task.Query().
		Where(enttask.ID(taskID)).
		WithProject().
		WithSessions().
		Only(ctx)
	if err != nil {
		return fmt.Errorf("loading task: %w", err)
	}

	// Find the last session's provider_session_key (claude session ID)
	var sessionKey string
	if sessions := t.Edges.Sessions; len(sessions) > 0 {
		last := sessions[len(sessions)-1]
		sessionKey = last.ProviderSessionKey
	}

	slog.Info("resuming task with message",
		"task_id", taskID,
		"session_key", sessionKey,
		"message_len", len(message),
	)

	// Transition to queued (allow from any terminal/paused state)
	currentStatus := model.TaskStatus(t.Status.String())
	if currentStatus == model.TaskStatusPaused || currentStatus == model.TaskStatusFailed {
		TransitionTask(ctx, s.client, taskID, currentStatus, model.TaskStatusQueued)
	} else if currentStatus == model.TaskStatusSucceeded {
		// For succeeded tasks, directly set to queued
		_, _ = s.client.Task.UpdateOneID(taskID).SetStatus(enttask.StatusQueued).Save(ctx)
	}

	// Store the resume info so the runner can use it
	// We use a convention: store message + session key in a pending user message
	if t.CurrentSessionID != nil {
		_, _ = s.client.SessionMessage.Create().
			SetSessionID(*t.CurrentSessionID).
			SetRole("user").
			SetContentType("text").
			SetContent(message).
			Save(ctx)
	}

	// The scheduler tick will pick up the queued task and start it
	// The runner will detect the session key and use --resume
	return nil
}

// HandleUserInput forwards user input to the running agent.
func (s *Scheduler) HandleUserInput(ctx context.Context, taskID int, event model.AgentEvent) error {
	// Try to forward message to agent
	s.mu.Lock()
	handle, hasHandle := s.runningHandles[taskID]
	s.mu.Unlock()

	if hasHandle && s.agentAdapter != nil {
		content, _ := event.Payload["content"].(string)
		if content != "" {
			if err := s.agentAdapter.SendInput(ctx, handle, agentprovider.UserInput{Text: content}); err != nil {
				slog.Warn("failed to send input to agent", "task_id", taskID, "error", err)
			}
		}
	}

	return nil
}
