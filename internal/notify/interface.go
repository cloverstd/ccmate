package notify

import "context"

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
}

// TaskURL returns the ccmate UI link for this task.
func (e NotifyEvent) TaskURL() string {
	if e.BaseURL == "" {
		return ""
	}
	return e.BaseURL + "/tasks/" + itoa(e.TaskID)
}

// IssueURL returns the GitHub issue link.
func (e NotifyEvent) IssueURL() string {
	if e.RepoURL == "" || e.IssueNumber == 0 {
		return ""
	}
	return e.RepoURL + "/issues/" + itoa(e.IssueNumber)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	s := ""
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	return s
}

// Notifier is the interface that notification providers must implement.
type Notifier interface {
	// Name returns the provider identifier (e.g. "telegram").
	Name() string
	// Send delivers a notification. Implementations should not block for long.
	Send(ctx context.Context, event NotifyEvent) error
}
