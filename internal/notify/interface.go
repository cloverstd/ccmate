package notify

import (
	"context"
	"strconv"
	"time"
)

// NotifyEvent carries task status change information for notification providers.
type NotifyEvent struct {
	TaskID      int
	ProjectName string
	RepoURL     string // e.g. "https://github.com/owner/repo"
	IssueNumber int
	IssueTitle  string
	OldStatus   string
	NewStatus   string
	Error       string // populated only on failure
	BaseURL     string // ccmate base URL for task links
	PRNumber    int    // 0 when no PR yet
	BranchName  string
	AgentName   string
	TaskType    string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// PRURL returns the GitHub PR link.
func (e NotifyEvent) PRURL() string {
	if e.RepoURL == "" || e.PRNumber == 0 {
		return ""
	}
	return e.RepoURL + "/pull/" + strconv.Itoa(e.PRNumber)
}

// TaskURL returns the ccmate UI link for this task.
func (e NotifyEvent) TaskURL() string {
	if e.BaseURL == "" {
		return ""
	}
	return e.BaseURL + "/tasks/" + strconv.Itoa(e.TaskID)
}

// IssueURL returns the GitHub issue link.
func (e NotifyEvent) IssueURL() string {
	if e.RepoURL == "" || e.IssueNumber == 0 {
		return ""
	}
	return e.RepoURL + "/issues/" + strconv.Itoa(e.IssueNumber)
}

// Notifier is the interface that notification providers must implement.
type Notifier interface {
	// Name returns the provider identifier (e.g. "telegram").
	Name() string
	// Send delivers a notification. Implementations should not block for long.
	Send(ctx context.Context, event NotifyEvent) error
}
