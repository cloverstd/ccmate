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
	"github.com/cloverstd/ccmate/internal/settings"
	"github.com/cloverstd/ccmate/internal/sse"
)

// Runner manages the execution of a single task.
type Runner struct {
	client       *ent.Client
	settingsMgr  *settings.Manager
	broker       *sse.Broker
	gitProvider  gitprovider.GitProvider
	agentAdapter agentprovider.AgentAdapter

	// OnHandleReady is called when the agent session handle is ready, allowing
	// the scheduler to forward user input to the agent.
	OnHandleReady func(h *agentprovider.SessionHandle)
}

func New(
	client *ent.Client, settingsMgr *settings.Manager, broker *sse.Broker,
	gitProvider gitprovider.GitProvider, agentAdapter agentprovider.AgentAdapter,
) *Runner {
	return &Runner{client: client, settingsMgr: settingsMgr, broker: broker, gitProvider: gitProvider, agentAdapter: agentAdapter}
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

	debug := r.settingsMgr.GetWithDefault(ctx, settings.KeyDebugMode, "false") == "true"
	topic := fmt.Sprintf("task:%d", taskID)

	// Create session early so we can log steps into it
	sess, err := r.client.Session.Create().
		SetTask(t).SetStatus(session.StatusStreaming).SetStartedAt(time.Now()).Save(ctx)
	if err != nil {
		return r.failTask(ctx, taskID, fmt.Errorf("creating session: %w", err))
	}
	_, _ = r.client.Task.UpdateOneID(taskID).SetCurrentSessionID(sess.ID).Save(ctx)

	sequence := 0
	logStep := func(step string, detail string) {
		sequence++
		payload := map[string]interface{}{"step": step, "detail": detail}
		payloadJSON, _ := json.Marshal(payload)
		_, _ = r.client.SessionEvent.Create().
			SetSession(sess).SetEventType("run.step").
			SetPayloadJSON(string(payloadJSON)).SetSequence(sequence).Save(ctx)
		r.broker.Publish(topic, sse.Event{Type: "run.step", Data: payload})
		slog.Info("task step", "task_id", taskID, "step", step, "detail", detail)
	}

	// Check for existing session to resume
	var resumeSessionID string
	existingSessions, _ := r.client.Session.Query().
		Where(session.HasTaskWith(enttask.ID(taskID))).
		All(ctx)
	for _, es := range existingSessions {
		if es.ProviderSessionKey != "" {
			resumeSessionID = es.ProviderSessionKey
		}
	}

	isResume := resumeSessionID != ""

	wsDir := r.settingsMgr.GetWithDefault(ctx, settings.KeyStorageBasePath, "data")
	ws := NewWorkspace(filepath.Join(wsDir, "workspaces"), proj.ID, taskID)
	repo := parseRepoURL(proj.RepoURL)
	branchName := ws.BranchName(t.IssueNumber)

	var systemPrompt, taskPrompt string
	modelName, modelVersion := r.resolveModelInfo(ctx)

	if isResume {
		// === RESUME MODE ===
		logStep("resume", fmt.Sprintf("Resuming task #%d, claude session %s", taskID, resumeSessionID))

		// Workspace should already exist — just make sure
		ws.Prepare()

		// Find the latest user message as the prompt to send
		lastUserMsgs, _ := r.client.SessionMessage.Query().
			Where(
				sessionmessage.HasSessionWith(session.HasTaskWith(enttask.ID(taskID))),
				sessionmessage.Role("user"),
			).
			Order(ent.Desc(sessionmessage.FieldSequence)).
			Limit(1).
			All(ctx)

		if len(lastUserMsgs) > 0 {
			taskPrompt = lastUserMsgs[0].Content
			logStep("resume_message", taskPrompt)
		} else {
			taskPrompt = "Please continue."
		}

		// System prompt stays the same
		builder := prompt.NewBuilder()
		r.loadProjectPromptTemplate(ctx, proj, builder)
		systemPrompt = builder.BuildSystemPrompt()

	} else {
		// === FRESH START MODE ===
		logStep("init", fmt.Sprintf("Starting task #%d for project %s, issue #%d", taskID, proj.Name, t.IssueNumber))

		logStep("workspace", fmt.Sprintf("Preparing workspace at %s", ws.RepoPath))
		if err := ws.Prepare(); err != nil {
			return r.failTask(ctx, taskID, fmt.Errorf("preparing workspace: %w", err))
		}

		logStep("clone", fmt.Sprintf("Cloning %s (branch: %s)", proj.RepoURL, proj.DefaultBranch))
		if err := r.cloneWithCredentials(ctx, repo, ws.RepoPath, proj.DefaultBranch); err != nil {
			return r.failTask(ctx, taskID, fmt.Errorf("cloning repo: %w", err))
		}

		logStep("branch", fmt.Sprintf("Creating branch %s", branchName))
		if err := ws.GitCheckoutBranch(ctx, branchName); err != nil {
			return r.failTask(ctx, taskID, fmt.Errorf("creating branch: %w", err))
		}

		logStep("fetch_issue", fmt.Sprintf("Fetching issue #%d from GitHub", t.IssueNumber))
		issue, err := r.gitProvider.GetIssue(ctx, repo, t.IssueNumber)
		if err != nil {
			return r.failTask(ctx, taskID, fmt.Errorf("getting issue: %w", err))
		}
		logStep("issue_loaded", fmt.Sprintf("Issue: %s", issue.Title))

		comments, _ := r.gitProvider.ListIssueComments(ctx, repo, t.IssueNumber)
		if len(comments) > 0 {
			logStep("comments", fmt.Sprintf("Loaded %d comments", len(comments)))
		}

		builder := prompt.NewBuilder()
		r.loadProjectPromptTemplate(ctx, proj, builder)
		systemPrompt = builder.BuildSystemPrompt()

		if t.Type == enttask.TypeReviewFix && t.PrNumber != nil {
			reviews, _ := r.gitProvider.ListPullRequestReviews(ctx, repo, *t.PrNumber)
			diff, _ := r.gitProvider.GetPullRequestDiff(ctx, repo, *t.PrNumber)
			priorHistory := r.loadPriorSessionHistory(ctx, t)
			if priorHistory != "" {
				systemPrompt += "\n## Prior Session Context\n" + priorHistory
			}
			taskPrompt = builder.BuildReviewFixPrompt(issue, reviews, diff)
			logStep("prompt", fmt.Sprintf("Built review fix prompt (system: %d, task: %d)", len(systemPrompt), len(taskPrompt)))
		} else {
			taskPrompt = builder.BuildTaskPrompt(issue, comments, t.Type.String())
			logStep("prompt", fmt.Sprintf("Built task prompt (system: %d, task: %d)", len(systemPrompt), len(taskPrompt)))
		}
	}

	if debug {
		logStep("debug_system_prompt", systemPrompt)
		logStep("debug_task_prompt", taskPrompt)
	}

	_, _ = r.client.PromptTemplateSnapshot.Create().
		SetTask(t).SetSystemPrompt(systemPrompt).SetTaskPrompt(taskPrompt).
		SetModelName(modelName).SetModelVersion(modelVersion).Save(ctx)

	logStep("agent_start", fmt.Sprintf("Starting agent (resume=%v, model=%s)", isResume, modelName))
	handle, err := r.agentAdapter.StartSession(ctx, agentprovider.StartSessionRequest{
		WorkDir: ws.RepoPath, SystemPrompt: systemPrompt, TaskPrompt: taskPrompt,
		ResumeSessionID: resumeSessionID,
	})
	if err != nil {
		return r.failTask(ctx, taskID, fmt.Errorf("starting agent: %w", err))
	}
	defer r.agentAdapter.Close(ctx, handle)

	// Register handle for user input forwarding
	if r.OnHandleReady != nil {
		r.OnHandleReady(handle)
	}

	// Log debug info from the agent adapter (command line, args, env, etc.)
	if handle.DebugInfo != nil {
		for k, v := range handle.DebugInfo {
			logStep("debug_"+k, v)
		}
	}
	if debug {
		caps := r.agentAdapter.Capabilities()
		logStep("debug_agent_caps", fmt.Sprintf("image=%v, resume=%v, streaming=%v",
			caps.SupportsImage, caps.SupportsResume, caps.SupportsStreaming))
	}

	logStep("agent_streaming", "Starting event stream from agent")
	eventCh, err := r.agentAdapter.StreamEvents(ctx, handle)
	if err != nil {
		return r.failTask(ctx, taskID, fmt.Errorf("streaming events: %w", err))
	}

	totalLogBytes := int64(0)
	maxLogMB, _ := strconv.Atoi(r.settingsMgr.GetWithDefault(ctx, settings.KeyMaxLogSizeMB, "50"))
	maxLogBytes := int64(maxLogMB) * 1024 * 1024
	var agentErrors []string

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

		// Capture claude session ID for resume
		if event.Type == model.AgentEventRunStatus {
			if sid, ok := event.Payload["session_id"].(string); ok && sid != "" {
				_, _ = r.client.Session.UpdateOne(sess).SetProviderSessionKey(sid).Save(ctx)
				logStep("session_id", sid)
			}
		}

		// Save all meaningful events as messages for history replay
		switch event.Type {
		case model.AgentEventMessageDelta:
			content, _ := sanitizedPayload["content"].(string)
			if content != "" {
				_, _ = r.client.SessionMessage.Create().
					SetSession(sess).SetRole("assistant").SetContentType("text").
					SetContent(content).SetSequence(sequence).Save(ctx)
			}
		case model.AgentEventMessageCompleted:
			content, _ := sanitizedPayload["content"].(string)
			if content != "" {
				_, _ = r.client.SessionMessage.Create().
					SetSession(sess).SetRole("assistant").SetContentType("result").
					SetContent(content).SetSequence(sequence).Save(ctx)
			}
		case model.AgentEventToolCall:
			toolJSON, _ := json.Marshal(sanitizedPayload)
			_, _ = r.client.SessionMessage.Create().
				SetSession(sess).SetRole("assistant").SetContentType("tool_call").
				SetContent(string(toolJSON)).SetSequence(sequence).Save(ctx)
		case model.AgentEventToolResult:
			resultJSON, _ := json.Marshal(sanitizedPayload)
			_, _ = r.client.SessionMessage.Create().
				SetSession(sess).SetRole("tool").SetContentType("tool_result").
				SetContent(string(resultJSON)).SetSequence(sequence).Save(ctx)
		}

		r.broker.Publish(topic, sse.Event{Type: string(event.Type), Data: sanitizedPayload})

		// Track errors from agent
		if event.Type == model.AgentEventError {
			errMsg, _ := event.Payload["message"].(string)
			if errMsg != "" {
				agentErrors = append(agentErrors, errMsg)
				logStep("agent_error", errMsg)
			}
		}

		if event.Type == model.AgentEventRunStatus {
			if status, ok := event.Payload["status"].(string); ok && status == "completed" {
				break
			}
		}
	}

	// If agent had errors, fail the task
	if len(agentErrors) > 0 {
		return r.failTask(ctx, taskID, fmt.Errorf("agent error: %s", strings.Join(agentErrors, "; ")))
	}

	logStep("agent_done", "Agent execution completed")

	now := time.Now()
	_, _ = r.client.Session.UpdateOne(sess).SetStatus(session.StatusClosed).SetEndedAt(now).Save(ctx)

	r.cleanupCredentials(ws.RepoPath)

	logStep("check_diff", "Checking for code changes")
	diff, _ := ws.GitDiff(ctx)
	if diff != "" {
		logStep("git_add", "Staging changes")
		if err := ws.GitAdd(ctx); err != nil {
			return r.failTask(ctx, taskID, fmt.Errorf("staging changes: %w", err))
		}

		commitMsg := fmt.Sprintf("ccmate: implement changes for issue #%d", t.IssueNumber)
		logStep("git_commit", fmt.Sprintf("Committing: %s", commitMsg))
		if err := ws.GitCommit(ctx, commitMsg); err != nil {
			return r.failTask(ctx, taskID, fmt.Errorf("committing: %w", err))
		}

		logStep("git_push", fmt.Sprintf("Pushing branch %s", branchName))
		if err := r.gitProvider.PushBranch(ctx, repo, ws.RepoPath, branchName); err != nil {
			return r.failTask(ctx, taskID, fmt.Errorf("pushing: %w", err))
		}

		if t.PrNumber == nil {
			// Fetch issue title for PR
			issueForPR, _ := r.gitProvider.GetIssue(ctx, repo, t.IssueNumber)
			prTitle := fmt.Sprintf("Fix #%d", t.IssueNumber)
			if issueForPR != nil {
				prTitle = fmt.Sprintf("Fix #%d: %s", t.IssueNumber, issueForPR.Title)
			}
			logStep("create_pr", fmt.Sprintf("Creating PR: %s", prTitle))
			pr, err := r.gitProvider.CreatePullRequest(ctx, repo, model.CreatePRRequest{
				Title: prTitle,
				Body:  fmt.Sprintf("Automated implementation for issue #%d\n\nGenerated by ccmate", t.IssueNumber),
				Head:  branchName, Base: proj.DefaultBranch,
			})
			if err != nil {
				return r.failTask(ctx, taskID, fmt.Errorf("creating PR: %w", err))
			}
			_, _ = r.client.Task.UpdateOneID(taskID).SetPrNumber(pr.Number).Save(ctx)
			_ = r.gitProvider.CreateIssueComment(ctx, repo, t.IssueNumber, fmt.Sprintf("PR created: %s", pr.HTMLURL))
			logStep("pr_created", fmt.Sprintf("PR #%d created: %s", pr.Number, pr.HTMLURL))
		}
	} else {
		logStep("no_changes", "No code changes detected")
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
func (r *Runner) resolveModelInfo(ctx context.Context) (string, string) {
	provJSON := r.settingsMgr.GetWithDefault(ctx, settings.KeyAgentProviders, "")
	if provJSON != "" {
		var providers []map[string]string
		if err := json.Unmarshal([]byte(provJSON), &providers); err == nil && len(providers) > 0 {
			return providers[0]["name"], providers[0]["binary"]
		}
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
