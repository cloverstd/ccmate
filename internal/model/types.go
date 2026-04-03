package model

import (
	"net/http"
	"time"
)

// TaskType represents the type of a task.
type TaskType string

const (
	TaskTypeIssueImplementation TaskType = "issue_implementation"
	TaskTypeReviewFix           TaskType = "review_fix"
	TaskTypeManualFollowup      TaskType = "manual_followup"
)

// TaskStatus represents the status of a task.
type TaskStatus string

const (
	TaskStatusPending     TaskStatus = "pending"
	TaskStatusQueued      TaskStatus = "queued"
	TaskStatusRunning     TaskStatus = "running"
	TaskStatusPaused      TaskStatus = "paused"
	TaskStatusWaitingUser TaskStatus = "waiting_user"
	TaskStatusSucceeded   TaskStatus = "succeeded"
	TaskStatusFailed      TaskStatus = "failed"
	TaskStatusCancelled   TaskStatus = "cancelled"
)

// IsActive returns true if the task is in an active (non-terminal) state.
func (s TaskStatus) IsActive() bool {
	switch s {
	case TaskStatusQueued, TaskStatusRunning, TaskStatusPaused, TaskStatusWaitingUser:
		return true
	}
	return false
}

// SessionStatus represents the status of an agent session.
type SessionStatus string

const (
	SessionStatusCreated   SessionStatus = "created"
	SessionStatusStreaming SessionStatus = "streaming"
	SessionStatusPaused    SessionStatus = "paused"
	SessionStatusClosed    SessionStatus = "closed"
	SessionStatusErrored   SessionStatus = "errored"
)

// TriggerSource indicates how a task was triggered.
type TriggerSource string

const (
	TriggerSourceWebhook TriggerSource = "webhook"
	TriggerSourceWeb     TriggerSource = "web"
	TriggerSourceCommand TriggerSource = "command"
)

// NormalizedEventType is the standardized event type across git providers.
type NormalizedEventType string

const (
	EventIssueLabeled        NormalizedEventType = "issue.labeled"
	EventIssueCommentCreated NormalizedEventType = "issue.comment.created"
	EventPRReviewSubmitted   NormalizedEventType = "pull_request.review_submitted"
	EventPRCommentCreated    NormalizedEventType = "pull_request.comment.created"
	EventPRSynchronize       NormalizedEventType = "pull_request.synchronize"
)

// NormalizedEvent is the platform-agnostic representation of a git event.
type NormalizedEvent struct {
	Type        NormalizedEventType
	DeliveryID  string
	Repo        RepoRef
	IssueNumber int
	PRNumber    int
	Label       string
	CommentBody string
	CommentUser string
	ReviewState string
	RawRequest  *http.Request `json:"-"`
}

// RepoRef uniquely identifies a repository.
type RepoRef struct {
	Owner string
	Name  string
}

func (r RepoRef) FullName() string {
	return r.Owner + "/" + r.Name
}

// RepoInfo represents a repository from a git provider.
type RepoInfo struct {
	FullName      string `json:"full_name"`
	HTMLURL       string `json:"html_url"`
	DefaultBranch string `json:"default_branch"`
	Description   string `json:"description"`
	Private       bool   `json:"private"`
}

type RepoBranch struct {
	Name    string `json:"name"`
	Hash    string `json:"hash"`
	Message string `json:"message"`
}

type RepoTag struct {
	Name string `json:"name"`
	Hash string `json:"hash"`
}

// Issue represents an issue from a git provider.
type Issue struct {
	Number    int       `json:"number"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	Labels    []string  `json:"labels"`
	State     string    `json:"state"`
	User      string    `json:"user"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Comment represents a comment on an issue or PR.
type Comment struct {
	ID        int64     `json:"id"`
	Body      string    `json:"body"`
	User      string    `json:"user"`
	CreatedAt time.Time `json:"created_at"`
}

// PullRequest represents a pull request.
type PullRequest struct {
	Number    int       `json:"number"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	State     string    `json:"state"`
	User      string    `json:"user"`
	HTMLURL   string    `json:"html_url"`
	Head      string    `json:"head"`
	Base      string    `json:"base"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Review represents a PR review.
type Review struct {
	ID    int64  `json:"id"`
	State string `json:"state"`
	Body  string `json:"body"`
	User  string `json:"user"`
}

// CreatePRRequest contains parameters for creating a pull request.
type CreatePRRequest struct {
	Title string
	Body  string
	Head  string
	Base  string
}

// AgentEvent is a unified event from an agent adapter.
type AgentEventType string

const (
	AgentEventMessageDelta     AgentEventType = "message.delta"
	AgentEventMessageCompleted AgentEventType = "message.completed"
	AgentEventToolCall         AgentEventType = "tool.call"
	AgentEventToolResult       AgentEventType = "tool.result"
	AgentEventRunStatus        AgentEventType = "run.status"
	AgentEventArtifactCreated  AgentEventType = "artifact.created"
	AgentEventError            AgentEventType = "error"
)

// AgentEvent represents a single event emitted by an agent.
type AgentEvent struct {
	Type    AgentEventType         `json:"type"`
	Payload map[string]interface{} `json:"payload"`
}

// AgentCapabilities describes what an agent adapter supports.
type AgentCapabilities struct {
	SupportsImage     bool `json:"supports_image"`
	SupportsResume    bool `json:"supports_resume"`
	SupportsStreaming bool `json:"supports_streaming"`
}

// CommandDecision indicates the result of command authorization.
type CommandDecision string

const (
	CommandDecisionAllowed CommandDecision = "allowed"
	CommandDecisionDenied  CommandDecision = "denied"
)
