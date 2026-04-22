package notify

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/cloverstd/ccmate/internal/ent"
	enttask "github.com/cloverstd/ccmate/internal/ent/task"
	"github.com/cloverstd/ccmate/internal/model"
	"github.com/cloverstd/ccmate/internal/settings"
)

// Use settings keys directly.
const (
	keyEnabledStatuses = "notify_enabled_statuses"
	keyBaseURL         = "notify_base_url"
)

// Manager dispatches notifications to registered providers based on settings.
type Manager struct {
	providers   []Notifier
	settingsMgr *settings.Manager
	client      *ent.Client
}

func NewManager(settingsMgr *settings.Manager, client *ent.Client) *Manager {
	return &Manager{
		settingsMgr: settingsMgr,
		client:      client,
	}
}

// RegisterProvider adds a notification provider.
func (m *Manager) RegisterProvider(p Notifier) {
	m.providers = append(m.providers, p)
}

// OnStatusChange should be called after a successful task status transition.
// It checks settings and dispatches to all enabled providers asynchronously.
func (m *Manager) OnStatusChange(ctx context.Context, taskID int, oldStatus, newStatus string) {
	if !m.shouldNotify(ctx, newStatus) {
		return
	}

	// Use a fresh context — the caller's ctx may be cancelled (e.g. task runner finished).
	bgCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)

	event, err := m.buildEvent(bgCtx, taskID, oldStatus, newStatus)
	if err != nil {
		cancel()
		slog.Error("notify: failed to build event", "task_id", taskID, "error", err)
		return
	}

	for _, p := range m.providers {
		go func(p Notifier) {
			defer cancel()
			sendWithRetry(bgCtx, p, event, taskID, 3)
		}(p)
	}
}

// SendTest sends a test notification to all enabled providers.
func (m *Manager) SendTest(ctx context.Context) error {
	event := NotifyEvent{
		TaskID:      0,
		ProjectName: "test-project",
		IssueNumber: 1,
		IssueTitle:  "Test notification",
		OldStatus:   "running",
		NewStatus:   "succeeded",
		BaseURL:     m.settingsMgr.GetWithDefault(ctx, keyBaseURL, ""),
	}

	var lastErr error
	for _, p := range m.providers {
		if err := p.Send(ctx, event); err != nil {
			lastErr = err
			slog.Error("notify: test send failed", "provider", p.Name(), "error", err)
		}
	}
	return lastErr
}

func sendWithRetry(ctx context.Context, p Notifier, event NotifyEvent, taskID int, maxAttempts int) {
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		err := p.Send(ctx, event)
		if err == nil {
			return
		}
		slog.Warn("notify: send attempt failed", "provider", p.Name(), "task_id", taskID, "attempt", attempt, "error", err)
		if attempt < maxAttempts {
			select {
			case <-ctx.Done():
				slog.Error("notify: context cancelled during retry", "provider", p.Name(), "task_id", taskID)
				return
			case <-time.After(time.Duration(attempt) * 2 * time.Second):
			}
		}
	}
	slog.Error("notify: all attempts failed", "provider", p.Name(), "task_id", taskID, "attempts", maxAttempts)
}

func (m *Manager) shouldNotify(ctx context.Context, newStatus string) bool {
	raw, _ := m.settingsMgr.Get(ctx, keyEnabledStatuses)
	if raw == "" {
		return false
	}
	var statuses []string
	if err := json.Unmarshal([]byte(raw), &statuses); err != nil {
		return false
	}
	for _, s := range statuses {
		if s == newStatus {
			return true
		}
	}
	return false
}

func (m *Manager) buildEvent(ctx context.Context, taskID int, oldStatus, newStatus string) (NotifyEvent, error) {
	t, err := m.client.Task.Query().
		Where(enttask.ID(taskID)).
		WithProject().
		Only(ctx)
	if err != nil {
		return NotifyEvent{}, fmt.Errorf("querying task: %w", err)
	}

	event := NotifyEvent{
		TaskID:      taskID,
		IssueNumber: t.IssueNumber,
		OldStatus:   oldStatus,
		NewStatus:   newStatus,
		TaskType:    t.Type.String(),
		CreatedAt:   t.CreatedAt,
		UpdatedAt:   t.UpdatedAt,
		BranchName:  model.TaskBranchName(t.IssueNumber, taskID),
		BaseURL:     m.settingsMgr.GetWithDefault(ctx, keyBaseURL, ""),
	}

	if t.PrNumber != nil {
		event.PRNumber = *t.PrNumber
	}

	if t.Edges.Project != nil {
		event.ProjectName = t.Edges.Project.Name
		event.RepoURL = t.Edges.Project.RepoURL
	}

	// Try to get issue title from git provider is expensive; use a simple label for now.
	// The issue title could be cached on the task in a future iteration.

	return event, nil
}
