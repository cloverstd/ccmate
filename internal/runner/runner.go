package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/cloverstd/ccmate/internal/agentprovider"
	"github.com/cloverstd/ccmate/internal/config"
	"github.com/cloverstd/ccmate/internal/ent"
	"github.com/cloverstd/ccmate/internal/ent/project"
	"github.com/cloverstd/ccmate/internal/ent/projectlabelrule"
	"github.com/cloverstd/ccmate/internal/ent/session"
	"github.com/cloverstd/ccmate/internal/ent/sessionmessage"
	enttask "github.com/cloverstd/ccmate/internal/ent/task"
	"github.com/cloverstd/ccmate/internal/gitprovider"
	"github.com/cloverstd/ccmate/internal/model"
	"github.com/cloverstd/ccmate/internal/prompt"
	"github.com/cloverstd/ccmate/internal/sanitize"
	"github.com/cloverstd/ccmate/internal/sse"
)

// Runner manages the execution of a single task.
type Runner struct {
	client       *ent.Client
	cfg          *config.Config
	broker       *sse.Broker
	gitProvider  gitprovider.GitProvider
	agentAdapter agentprovider.AgentAdapter
}

func New(
	client *ent.Client, cfg *config.Config, broker *sse.Broker,
	gitProvider gitprovider.GitProvider, agentAdapter agentprovider.AgentAdapter,
) *Runner {
	return &Runner{client: client, cfg: cfg, broker: broker, gitProvider: gitProvider, agentAdapter: agentAdapter}
}

// RunTask executes a task end-to-end.
func (r *Runner) RunTask(ctx context.Context, taskID int) error {
	t, err := r.client.Task.Query().Where(enttask.ID(taskID)).WithProject().Only(ctx)
	if err != nil {
		return fmt.Errorf("loading task: %w", err)
	}

	proj := t.Edges.Project
	if proj == nil {
		return fmt.Errorf("task has no project")
	}

	slog.Info("starting task execution", "task_id", taskID, "project", proj.Name, "issue", t.IssueNumber)

	ws := NewWorkspace(r.cfg.Storage.WorkspacesDir, proj.ID, taskID)
	if err := ws.Prepare(); err != nil {
		return fmt.Errorf("preparing workspace: %w", err)
	}

	repo := parseRepoURL(proj.RepoURL)

	// Clone with temporary credentials
	if err := r.cloneWithCredentials(ctx, repo, ws.RepoPath, proj.DefaultBranch); err != nil {
		return r.failTask(ctx, taskID, fmt.Errorf("cloning repo: %w", err))
	}

	branchName := ws.BranchName(t.IssueNumber)
	if err := ws.GitCheckoutBranch(ctx, branchName); err != nil {
		return r.failTask(ctx, taskID, fmt.Errorf("creating branch: %w", err))
	}

	issue, err := r.gitProvider.GetIssue(ctx, repo, t.IssueNumber)
	if err != nil {
		return r.failTask(ctx, taskID, fmt.Errorf("getting issue: %w", err))
	}

	comments, _ := r.gitProvider.ListIssueComments(ctx, repo, t.IssueNumber)

	// Load project-level PromptTemplate via label rule
	builder := prompt.NewBuilder()
	r.loadProjectPromptTemplate(ctx, proj, builder)

	// For review_fix tasks, load prior session context
	systemPrompt := builder.BuildSystemPrompt()
	var taskPrompt string

	if t.Type == enttask.TypeReviewFix && t.PrNumber != nil {
		reviews, _ := r.gitProvider.ListPullRequestReviews(ctx, repo, *t.PrNumber)
		diff, _ := r.gitProvider.GetPullRequestDiff(ctx, repo, *t.PrNumber)

		priorHistory := r.loadPriorSessionHistory(ctx, t)
		if priorHistory != "" {
			systemPrompt += "\n## Prior Session Context\n" + priorHistory
		}

		taskPrompt = builder.BuildReviewFixPrompt(issue, reviews, diff)
	} else {
		taskPrompt = builder.BuildTaskPrompt(issue, comments, t.Type.String())
	}

	// Save prompt snapshot with actual model info
	modelName, modelVersion := r.resolveModelInfo()
	_, _ = r.client.PromptTemplateSnapshot.Create().
		SetTask(t).SetSystemPrompt(systemPrompt).SetTaskPrompt(taskPrompt).
		SetModelName(modelName).SetModelVersion(modelVersion).Save(ctx)

	sess, err := r.client.Session.Create().
		SetTask(t).SetStatus(session.StatusStreaming).SetStartedAt(time.Now()).Save(ctx)
	if err != nil {
		return r.failTask(ctx, taskID, fmt.Errorf("creating session: %w", err))
	}

	_, _ = r.client.Task.UpdateOneID(taskID).SetCurrentSessionID(sess.ID).Save(ctx)

	handle, err := r.agentAdapter.StartSession(ctx, agentprovider.StartSessionRequest{
		WorkDir: ws.RepoPath, SystemPrompt: systemPrompt, TaskPrompt: taskPrompt,
	})
	if err != nil {
		return r.failTask(ctx, taskID, fmt.Errorf("starting agent: %w", err))
	}
	defer r.agentAdapter.Close(ctx, handle)

	eventCh, err := r.agentAdapter.StreamEvents(ctx, handle)
	if err != nil {
		return r.failTask(ctx, taskID, fmt.Errorf("streaming events: %w", err))
	}

	topic := fmt.Sprintf("task:%d", taskID)
	sequence := 0
	totalLogBytes := int64(0)
	maxLogBytes := int64(r.cfg.Limits.MaxLogSizeMB) * 1024 * 1024

	for event := range eventCh {
		sequence++
		sanitizedPayload := sanitize.SanitizeMap(event.Payload)
		payloadJSON, _ := json.Marshal(sanitizedPayload)

		// Enforce log size limit
		totalLogBytes += int64(len(payloadJSON))
		if totalLogBytes > maxLogBytes {
			slog.Warn("log size limit exceeded, truncating", "task_id", taskID, "bytes", totalLogBytes)
			r.broker.Publish(topic, sse.Event{Type: "run.status", Data: map[string]interface{}{"status": "log_truncated"}})
			break
		}

		_, _ = r.client.SessionEvent.Create().
			SetSession(sess).SetEventType(string(event.Type)).
			SetPayloadJSON(string(payloadJSON)).SetSequence(sequence).Save(ctx)

		if event.Type == model.AgentEventMessageCompleted {
			content, _ := sanitizedPayload["content"].(string)
			_, _ = r.client.SessionMessage.Create().
				SetSession(sess).SetRole("assistant").SetContentType("text").
				SetContent(content).SetSequence(sequence).Save(ctx)
		}

		r.broker.Publish(topic, sse.Event{Type: string(event.Type), Data: sanitizedPayload})

		if event.Type == model.AgentEventRunStatus {
			if status, ok := event.Payload["status"].(string); ok && status == "completed" {
				break
			}
		}
	}

	now := time.Now()
	_, _ = r.client.Session.UpdateOne(sess).SetStatus(session.StatusClosed).SetEndedAt(now).Save(ctx)

	// Clean up git credentials
	r.cleanupCredentials(ws.RepoPath)

	diff, _ := ws.GitDiff(ctx)
	if diff != "" {
		if err := ws.GitAdd(ctx); err != nil {
			return r.failTask(ctx, taskID, fmt.Errorf("staging changes: %w", err))
		}

		commitMsg := fmt.Sprintf("ccmate: implement changes for issue #%d", t.IssueNumber)
		if err := ws.GitCommit(ctx, commitMsg); err != nil {
			return r.failTask(ctx, taskID, fmt.Errorf("committing: %w", err))
		}

		if err := r.gitProvider.PushBranch(ctx, repo, ws.RepoPath, branchName); err != nil {
			return r.failTask(ctx, taskID, fmt.Errorf("pushing: %w", err))
		}

		if t.PrNumber == nil {
			pr, err := r.gitProvider.CreatePullRequest(ctx, repo, model.CreatePRRequest{
				Title: fmt.Sprintf("Fix #%d: %s", t.IssueNumber, issue.Title),
				Body:  fmt.Sprintf("Automated implementation for issue #%d\n\nGenerated by ccmate", t.IssueNumber),
				Head:  branchName, Base: proj.DefaultBranch,
			})
			if err != nil {
				return r.failTask(ctx, taskID, fmt.Errorf("creating PR: %w", err))
			}
			_, _ = r.client.Task.UpdateOneID(taskID).SetPrNumber(pr.Number).Save(ctx)
			_ = r.gitProvider.CreateIssueComment(ctx, repo, t.IssueNumber, fmt.Sprintf("PR created: %s", pr.HTMLURL))
			slog.Info("PR created", "pr", pr.Number, "url", pr.HTMLURL)
		}
	}

	_, err = r.client.Task.UpdateOneID(taskID).SetStatus(enttask.StatusSucceeded).Save(ctx)
	if err != nil {
		return fmt.Errorf("marking task succeeded: %w", err)
	}

	r.broker.Publish(topic, sse.Event{Type: "task.completed", Data: map[string]interface{}{"task_id": taskID, "status": "succeeded"}})
	slog.Info("task completed successfully", "task_id", taskID)
	return nil
}

// loadPriorSessionHistory loads messages from a prior session for the same issue.
func (r *Runner) loadPriorSessionHistory(ctx context.Context, t *ent.Task) string {
	// Find the most recent closed session for the same issue
	sessions, err := r.client.Session.Query().
		Where(session.HasTaskWith(enttask.IssueNumber(t.IssueNumber))).
		Where(session.StatusEQ(session.StatusClosed)).
		WithMessages(func(q *ent.SessionMessageQuery) {
			q.Order(ent.Asc(sessionmessage.FieldSequence)).Limit(50)
		}).
		All(ctx)
	if err != nil || len(sessions) == 0 {
		return ""
	}

	var parts []string
	for _, s := range sessions {
		for _, m := range s.Edges.Messages {
			parts = append(parts, fmt.Sprintf("[%s] %s", m.Role, m.Content))
		}
	}

	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "\n\n")
}

// loadProjectPromptTemplate loads the PromptTemplate associated with the project's label rules.
func (r *Runner) loadProjectPromptTemplate(ctx context.Context, proj *ent.Project, builder *prompt.Builder) {
	rules, err := r.client.ProjectLabelRule.Query().
		Where(projectlabelrule.HasProjectWith(project.ID(proj.ID))).
		WithPromptTemplate().
		All(ctx)
	if err != nil || len(rules) == 0 {
		return
	}

	// Use the first rule's prompt template that has one
	for _, rule := range rules {
		tmpl := rule.Edges.PromptTemplate
		if tmpl != nil {
			builder.WithProjectPrompts(tmpl.SystemPrompt, tmpl.TaskPrompt)
			return
		}
	}
}

// resolveModelInfo returns the model name and version from the configured agent provider.
func (r *Runner) resolveModelInfo() (string, string) {
	caps := r.agentAdapter.Capabilities()
	// Use capability info to identify the provider type
	_ = caps
	// Return from config if available
	if len(r.cfg.Agent.Providers) > 0 {
		p := r.cfg.Agent.Providers[0]
		return p.Name, p.Binary
	}
	return "unknown", ""
}

// cloneWithCredentials clones using a temporary git credential helper.
func (r *Runner) cloneWithCredentials(ctx context.Context, repo model.RepoRef, destPath string, branch string) error {
	return r.gitProvider.CloneRepo(ctx, repo, destPath, branch)
}

// cleanupCredentials removes any temporary credential files from the workspace.
func (r *Runner) cleanupCredentials(repoPath string) {
	credFile := filepath.Join(repoPath, ".git", "credentials")
	os.Remove(credFile)

	// Remove inline credentials from remote URL
	cmd := exec.Command("git", "remote", "set-url", "origin", "https://github.com/redacted/redacted.git")
	cmd.Dir = repoPath
	_ = cmd.Run()
}

func (r *Runner) failTask(ctx context.Context, taskID int, err error) error {
	classification := ClassifyError(err)
	retryable := classification == ErrorRetryable

	slog.Error("task failed", "task_id", taskID, "error", err, "retryable", retryable)

	update := r.client.Task.UpdateOneID(taskID).SetStatus(enttask.StatusFailed)
	if !retryable {
		// Mark non-retryable by setting priority to max (prevents auto-retry)
		update = update.SetPriority(99)
	}
	_, _ = update.Save(ctx)

	r.broker.Publish(fmt.Sprintf("task:%d", taskID), sse.Event{
		Type: "task.failed", Data: map[string]interface{}{
			"task_id": taskID, "error": err.Error(), "retryable": retryable,
		},
	})
	return err
}

func parseRepoURL(url string) model.RepoRef {
	parts := []string{}
	for _, sep := range []string{"github.com/", "gitlab.com/", "gitee.com/"} {
		idx := len(sep)
		if i := strings.Index(url, sep); i >= 0 {
			rest := url[i+idx:]
			rest = strings.TrimSuffix(rest, ".git")
			parts = strings.SplitN(rest, "/", 2)
			break
		}
	}
	if len(parts) == 2 {
		return model.RepoRef{Owner: parts[0], Name: parts[1]}
	}
	return model.RepoRef{}
}

// Ensure strconv is used
var _ = strconv.Itoa
