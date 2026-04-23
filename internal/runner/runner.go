package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/cloverstd/ccmate/internal/agentprovider"
	"github.com/cloverstd/ccmate/internal/ent"
	"github.com/cloverstd/ccmate/internal/ent/project"
	"github.com/cloverstd/ccmate/internal/ent/projectlabelrule"
	"github.com/cloverstd/ccmate/internal/ent/prompttemplate"
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
	client        *ent.Client
	settingsMgr   *settings.Manager
	broker        *sse.Broker
	gitProvider   gitprovider.GitProvider
	agentRegistry *agentprovider.Registry

	// OnHandleReady is called when the agent session handle is ready, allowing
	// the scheduler to forward user input to the agent.
	OnHandleReady func(agentprovider.AgentAdapter, *agentprovider.SessionHandle)
	// OnStatusChange is called after the runner directly changes task status.
	OnStatusChange func(ctx context.Context, taskID int, oldStatus, newStatus string)
}

func New(
	client *ent.Client, settingsMgr *settings.Manager, broker *sse.Broker,
	gitProvider gitprovider.GitProvider, agentRegistry *agentprovider.Registry,
) *Runner {
	return &Runner{client: client, settingsMgr: settingsMgr, broker: broker, gitProvider: gitProvider, agentRegistry: agentRegistry}
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
	agentAdapter, modelName, modelVersion, err := r.resolveAgent(ctx, t)
	if err != nil {
		return r.failTask(ctx, taskID, err)
	}

	// Review tasks have a specialized flow: they don't commit, don't push, don't
	// wait for user input — they fetch the PR, ask the agent for a structured
	// verdict, post a GitHub review, and optionally enqueue a review_fix task.
	if !isResume && t.Type == enttask.TypeReview {
		return r.runReviewTask(ctx, t, proj, sess, logStep, agentAdapter, modelName, modelVersion, ws, repo, topic, &sequence)
	}

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
		if err := ws.PrepareClean(); err != nil {
			return r.failTask(ctx, taskID, fmt.Errorf("preparing workspace: %w", err))
		}

		logStep("clone", fmt.Sprintf("Cloning %s (branch: %s)", proj.RepoURL, proj.DefaultBranch))
		if err := r.cloneWithCredentials(ctx, repo, ws.RepoPath, proj.DefaultBranch); err != nil {
			return r.failTask(ctx, taskID, fmt.Errorf("cloning repo: %w", err))
		}

		authorName := r.settingsMgr.GetWithDefault(ctx, settings.KeyGitCommitAuthorName, "ccmate-bot")
		authorEmail := r.settingsMgr.GetWithDefault(ctx, settings.KeyGitCommitAuthorEmail, "ccmate-bot@users.noreply.github.com")
		if err := ws.SetGitIdentity(ctx, authorName, authorEmail); err != nil {
			return r.failTask(ctx, taskID, fmt.Errorf("setting git identity: %w", err))
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
		builder.WithTemplateVars(prompt.TemplateVars{
			IssueNumber:  issue.Number,
			IssueTitle:   issue.Title,
			IssueBody:    issue.Body,
			IssueLabels:  issue.Labels,
			IssueUser:    issue.User,
			IssueLink:    fmt.Sprintf("https://github.com/%s/issues/%d", repo.FullName(), issue.Number),
			RepoOwner:    repo.Owner,
			RepoName:     repo.Name,
			RepoFullName: repo.FullName(),
			TaskType:     t.Type.String(),
			BranchName:   branchName,
		})
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
	handle, err := agentAdapter.StartSession(ctx, agentprovider.StartSessionRequest{
		WorkDir: ws.RepoPath, SystemPrompt: systemPrompt, TaskPrompt: taskPrompt,
		ResumeSessionID: resumeSessionID,
	})
	if err != nil {
		return r.failTask(ctx, taskID, fmt.Errorf("starting agent: %w", err))
	}
	defer agentAdapter.Close(ctx, handle)

	// Register handle for user input forwarding
	if r.OnHandleReady != nil {
		r.OnHandleReady(agentAdapter, handle)
	}

	// Log debug info from the agent adapter (command line, args, env, etc.)
	if handle.DebugInfo != nil {
		for k, v := range handle.DebugInfo {
			logStep("debug_"+k, v)
		}
	}
	if debug {
		caps := agentAdapter.Capabilities()
		logStep("debug_agent_caps", fmt.Sprintf("image=%v, resume=%v, streaming=%v",
			caps.SupportsImage, caps.SupportsResume, caps.SupportsStreaming))
	}

	logStep("agent_streaming", "Starting event stream from agent")
	eventCh, err := agentAdapter.StreamEvents(ctx, handle)
	if err != nil {
		return r.failTask(ctx, taskID, fmt.Errorf("streaming events: %w", err))
	}

	totalLogBytes := int64(0)
	maxLogMB, _ := strconv.Atoi(r.settingsMgr.GetWithDefault(ctx, settings.KeyMaxLogSizeMB, "50"))
	maxLogBytes := int64(maxLogMB) * 1024 * 1024
	var agentErrors []string
	var turnCompleted bool

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

		// Attach sequence to SSE payload so the frontend can dedupe against polled history.
		ssePayload := make(map[string]interface{}, len(sanitizedPayload)+1)
		for k, v := range sanitizedPayload {
			ssePayload[k] = v
		}
		ssePayload["_sequence"] = sequence
		r.broker.Publish(topic, sse.Event{Type: string(event.Type), Data: ssePayload})

		// Track errors from agent
		if event.Type == model.AgentEventError {
			errMsg, _ := event.Payload["message"].(string)
			if errMsg != "" {
				agentErrors = append(agentErrors, errMsg)
				logStep("agent_error", errMsg)
			}
		}

		// Turn is done when the adapter explicitly signals it, or the channel closes.
		// The adapter layer owns provider-specific completion detection; runner stays provider-neutral.
		if event.Type == model.AgentEventTurnCompleted {
			turnCompleted = true
			break
		}
	}

	// stderr banners (e.g. codex "Reading additional input from stdin...") surface
	// as AgentEventError but are informational. Only treat them as fatal if the
	// turn never completed.
	if len(agentErrors) > 0 && !turnCompleted {
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

	// Detect PR created by agent if we don't have one yet
	if t.PrNumber == nil {
		r.detectAgentPR(ctx, taskID, repo, ws.RepoPath, logStep)
	}

	// Re-load the task so we see the PR number that may have just been set.
	if refreshed, rerr := r.client.Task.Query().Where(enttask.ID(taskID)).Only(ctx); rerr == nil {
		r.enqueueReviewIfEnabled(ctx, refreshed, proj, logStep)
	}

	_, err = r.client.Task.UpdateOneID(taskID).SetStatus(enttask.StatusWaitingUser).Save(ctx)
	if err != nil {
		return fmt.Errorf("marking task waiting_user: %w", err)
	}

	if r.OnStatusChange != nil {
		r.OnStatusChange(ctx, taskID, string(model.TaskStatusRunning), string(model.TaskStatusWaitingUser))
	}
	r.broker.Publish(topic, sse.Event{Type: "run.status", Data: map[string]interface{}{"task_id": taskID, "status": "awaiting_user_confirmation"}})
	slog.Info("task finished and awaiting user confirmation", "task_id", taskID)
	return nil
}

// detectAgentPR checks if the agent created a PR during its session and saves the PR number.
// It reads the current branch from the workspace and searches GitHub for a matching PR.
func (r *Runner) detectAgentPR(ctx context.Context, taskID int, repo model.RepoRef, repoPath string, logStep func(string, string)) {
	if r.gitProvider == nil {
		return
	}

	// Try current branch in workspace
	currentBranch, err := CurrentBranch(ctx, repoPath)
	if err != nil || currentBranch == "" {
		return
	}

	pr, err := r.gitProvider.FindPullRequestByHead(ctx, repo, currentBranch)
	if err != nil || pr == nil {
		return
	}

	logStep("pr_detected", fmt.Sprintf("Detected agent-created PR #%d: %s", pr.Number, pr.HTMLURL))
	_, _ = r.client.Task.UpdateOneID(taskID).SetPrNumber(pr.Number).Save(ctx)
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

// applyTemplate applies a single template's system_prompt and task_prompt to the builder.
func applyTemplate(builder *prompt.Builder, tmpl *ent.PromptTemplate) {
	if tmpl.SystemPrompt != "" {
		builder.WithSystemPrompt(tmpl.SystemPrompt)
	}
	if tmpl.TaskPrompt != "" {
		builder.WithTaskPrompt(tmpl.TaskPrompt)
	}
}

// loadProjectPromptTemplate loads the PromptTemplate based on the project's scope setting.
func (r *Runner) loadProjectPromptTemplate(ctx context.Context, proj *ent.Project, builder *prompt.Builder) {
	scope := proj.PromptTemplateScope

	switch scope {
	case project.PromptTemplateScopeGlobalOnly:
		if tmpl := r.loadGlobalDefaultTemplate(ctx); tmpl != nil {
			applyTemplate(builder, tmpl)
		}

	case project.PromptTemplateScopeMerged:
		globalTmpl := r.loadGlobalDefaultTemplate(ctx)
		projectTmpl := r.loadProjectDefaultTemplate(ctx, proj)
		var sysParts, taskParts []string
		if globalTmpl != nil {
			if globalTmpl.SystemPrompt != "" {
				sysParts = append(sysParts, globalTmpl.SystemPrompt)
			}
			if globalTmpl.TaskPrompt != "" {
				taskParts = append(taskParts, globalTmpl.TaskPrompt)
			}
		}
		if projectTmpl != nil {
			if projectTmpl.SystemPrompt != "" {
				sysParts = append(sysParts, projectTmpl.SystemPrompt)
			}
			if projectTmpl.TaskPrompt != "" {
				taskParts = append(taskParts, projectTmpl.TaskPrompt)
			}
		}
		if len(sysParts) > 0 {
			builder.WithSystemPrompt(strings.Join(sysParts, "\n\n"))
		}
		if len(taskParts) > 0 {
			builder.WithTaskPrompt(strings.Join(taskParts, "\n\n"))
		}

	default: // project_only (default, backward compatible)
		if tmpl := r.loadProjectDefaultTemplate(ctx, proj); tmpl != nil {
			applyTemplate(builder, tmpl)
		}
	}
}

// loadGlobalDefaultTemplate loads the global default prompt template from settings.
func (r *Runner) loadGlobalDefaultTemplate(ctx context.Context) *ent.PromptTemplate {
	idStr := r.settingsMgr.GetWithDefault(ctx, settings.KeyDefaultPromptTemplateID, "")
	if idStr == "" {
		return nil
	}
	id, err := strconv.Atoi(idStr)
	if err != nil {
		return nil
	}
	tmpl, err := r.client.PromptTemplate.Query().
		Where(prompttemplate.ID(id)).
		Only(ctx)
	if err != nil {
		return nil
	}
	return tmpl
}

// loadReviewPromptTemplate loads the project's review-specific prompt template.
// Only the system_prompt is applied; the task prompt is always produced by
// BuildReviewPrompt to preserve the JSON output contract.
func (r *Runner) loadReviewPromptTemplate(ctx context.Context, proj *ent.Project, builder *prompt.Builder) {
	if proj.ReviewPromptTemplateID == nil {
		return
	}
	tmpl, err := r.client.PromptTemplate.Query().
		Where(
			prompttemplate.ID(*proj.ReviewPromptTemplateID),
			prompttemplate.Or(
				prompttemplate.ProjectIDIsNil(),
				prompttemplate.ProjectID(proj.ID),
			),
		).
		Only(ctx)
	if err != nil || tmpl == nil {
		slog.Warn("review prompt template not loadable",
			"project_id", proj.ID, "template_id", *proj.ReviewPromptTemplateID, "err", err)
		return
	}
	if tmpl.SystemPrompt != "" {
		builder.WithSystemPrompt(tmpl.SystemPrompt)
	}
}

// loadProjectDefaultTemplate loads the project's default prompt template, falling back to label rules.
func (r *Runner) loadProjectDefaultTemplate(ctx context.Context, proj *ent.Project) *ent.PromptTemplate {
	if proj.DefaultPromptTemplateID != nil {
		tmpl, err := r.client.PromptTemplate.Query().
			Where(prompttemplate.ID(*proj.DefaultPromptTemplateID)).
			Only(ctx)
		if err == nil && tmpl != nil {
			return tmpl
		}
	}

	rules, err := r.client.ProjectLabelRule.Query().
		Where(projectlabelrule.HasProjectWith(project.ID(proj.ID))).
		WithPromptTemplate().
		All(ctx)
	if err != nil || len(rules) == 0 {
		return nil
	}

	for _, rule := range rules {
		if tmpl := rule.Edges.PromptTemplate; tmpl != nil {
			return tmpl
		}
	}
	return nil
}

func (r *Runner) resolveAgent(ctx context.Context, t *ent.Task) (agentprovider.AgentAdapter, string, string, error) {
	if r.agentRegistry == nil {
		return nil, "", "", fmt.Errorf("agent registry not configured")
	}

	providers := r.loadConfiguredProviders(ctx)
	if t.AgentProfileID != nil {
		profile, err := r.client.AgentProfile.Get(ctx, *t.AgentProfileID)
		if err != nil {
			return nil, "", "", fmt.Errorf("loading task agent profile: %w", err)
		}
		providerName, binary := normalizeProvider(profile.Provider, providers)
		adapter, err := r.agentRegistry.Create(providerName, agentprovider.AgentConfig{
			Binary: binary,
			Model:  profile.Model,
			Extra: map[string]string{
				"debug": r.settingsMgr.GetWithDefault(ctx, settings.KeyDebugMode, "false"),
			},
		})
		if err != nil {
			return nil, "", "", fmt.Errorf("creating agent adapter: %w", err)
		}
		return adapter, profile.Model, providerName, nil
	}

	if len(providers) > 0 {
		providerName, binary := normalizeProvider(providers[0]["name"], providers)
		adapter, err := r.agentRegistry.Create(providerName, agentprovider.AgentConfig{
			Binary: binary,
			Model:  providers[0]["model"],
			Extra: map[string]string{
				"debug": r.settingsMgr.GetWithDefault(ctx, settings.KeyDebugMode, "false"),
			},
		})
		if err == nil {
			return adapter, providerName, binary, nil
		}
	}

	adapter, err := r.agentRegistry.Create("mock", agentprovider.AgentConfig{
		Extra: map[string]string{"debug": r.settingsMgr.GetWithDefault(ctx, settings.KeyDebugMode, "false")},
	})
	if err != nil {
		return nil, "", "", err
	}
	return adapter, "mock", "", nil
}

func (r *Runner) loadConfiguredProviders(ctx context.Context) []map[string]string {
	provJSON := r.settingsMgr.GetWithDefault(ctx, settings.KeyAgentProviders, "")
	if provJSON == "" {
		return nil
	}
	var providers []map[string]string
	if err := json.Unmarshal([]byte(provJSON), &providers); err != nil {
		return nil
	}
	return providers
}

func normalizeProvider(provider string, configured []map[string]string) (string, string) {
	p := strings.TrimSpace(strings.ToLower(provider))
	switch p {
	case "cc":
		p = "codex"
	case "claude":
		p = "claude-code"
	}

	for _, item := range configured {
		name := strings.ToLower(strings.TrimSpace(item["name"]))
		binary := item["binary"]
		if name == p {
			return item["name"], binary
		}
		if strings.ToLower(strings.TrimSpace(binary)) == p {
			return item["name"], binary
		}
	}
	return p, ""
}

// cloneWithCredentials clones using a temporary git credential helper.
func (r *Runner) cloneWithCredentials(ctx context.Context, repo model.RepoRef, destPath string, branch string) error {
	return r.gitProvider.CloneRepo(ctx, repo, destPath, branch)
}

// cleanupCredentials removes any temporary credential files and strips inline
// credentials from the remote URL so tokens are not left on disk.
func (r *Runner) cleanupCredentials(repoPath string) {
	credFile := filepath.Join(repoPath, ".git", "credentials")
	os.Remove(credFile)

	// Read current remote URL and strip embedded credentials
	getCmd := exec.Command("git", "remote", "get-url", "origin")
	getCmd.Dir = repoPath
	out, err := getCmd.Output()
	if err != nil {
		return
	}
	remoteURL := strings.TrimSpace(string(out))
	// Strip credentials from https://user:token@host/... → https://host/...
	if u, err := url.Parse(remoteURL); err == nil && u.User != nil {
		u.User = nil
		setCmd := exec.Command("git", "remote", "set-url", "origin", u.String())
		setCmd.Dir = repoPath
		_ = setCmd.Run()
	}
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

	if r.OnStatusChange != nil {
		r.OnStatusChange(ctx, taskID, string(model.TaskStatusRunning), string(model.TaskStatusFailed))
	}
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

// reviewVerdict is the structured output we demand from the review agent.
type reviewVerdict struct {
	Decision string                `json:"decision"`
	Summary  string                `json:"summary"`
	Comments []model.ReviewComment `json:"comments"`
}

var jsonFencedRE = regexp.MustCompile("(?s)```(?:json)?\\s*(\\{.*?\\})\\s*```")

// extractReviewVerdict pulls the last fenced JSON block out of the agent's
// final message. Falls back to the last balanced {...} substring.
func extractReviewVerdict(text string) (*reviewVerdict, error) {
	matches := jsonFencedRE.FindAllStringSubmatch(text, -1)
	var candidate string
	if len(matches) > 0 {
		candidate = matches[len(matches)-1][1]
	} else {
		// Fallback: last {...} block by naive scan
		start := strings.LastIndex(text, "{")
		end := strings.LastIndex(text, "}")
		if start >= 0 && end > start {
			candidate = text[start : end+1]
		}
	}
	if candidate == "" {
		return nil, fmt.Errorf("no JSON block found")
	}
	var v reviewVerdict
	if err := json.Unmarshal([]byte(candidate), &v); err != nil {
		return nil, err
	}
	return &v, nil
}

func (r *Runner) runReviewTask(
	ctx context.Context,
	t *ent.Task,
	proj *ent.Project,
	sess *ent.Session,
	logStep func(step, detail string),
	agentAdapter agentprovider.AgentAdapter,
	modelName, modelVersion string,
	ws *Workspace,
	repo model.RepoRef,
	topic string,
	sequence *int,
) error {
	taskID := t.ID

	if t.PrNumber == nil {
		return r.failTask(ctx, taskID, fmt.Errorf("review task missing pr_number"))
	}
	prNum := *t.PrNumber

	logStep("init", fmt.Sprintf("Review task #%d for PR #%d (iteration %d)", taskID, prNum, t.ReviewIteration))

	// Mark the PR with 👀 so reviewers can see ccmate is actively reviewing.
	var eyesReactionID int64
	if rid, rerr := r.gitProvider.AddIssueReaction(ctx, repo, prNum, "eyes"); rerr == nil {
		eyesReactionID = rid
		logStep("reaction_added", fmt.Sprintf("added 👀 reaction (id=%d)", rid))
	} else {
		logStep("reaction_failed", rerr.Error())
	}
	defer func() {
		if eyesReactionID == 0 {
			return
		}
		// Use a fresh context in case the parent was cancelled.
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if derr := r.gitProvider.RemoveIssueReaction(cleanupCtx, repo, prNum, eyesReactionID); derr != nil {
			logStep("reaction_cleanup_failed", derr.Error())
		} else {
			logStep("reaction_removed", "removed 👀 reaction")
		}
	}()

	pr, err := r.gitProvider.GetPullRequest(ctx, repo, prNum)
	if err != nil {
		return r.failTask(ctx, taskID, fmt.Errorf("loading PR: %w", err))
	}
	diff, err := r.gitProvider.GetPullRequestDiff(ctx, repo, prNum)
	if err != nil {
		return r.failTask(ctx, taskID, fmt.Errorf("loading PR diff: %w", err))
	}
	priorReviews, _ := r.gitProvider.ListPullRequestReviews(ctx, repo, prNum)

	var issueForCtx *model.Issue
	if iss, ierr := r.gitProvider.GetIssue(ctx, repo, t.IssueNumber); ierr == nil {
		issueForCtx = iss
	}

	// Clone the PR head branch so the agent can read repo files for context.
	logStep("workspace", fmt.Sprintf("Preparing workspace at %s", ws.RepoPath))
	if err := ws.PrepareClean(); err != nil {
		return r.failTask(ctx, taskID, fmt.Errorf("preparing workspace: %w", err))
	}
	headBranch := pr.Head
	if headBranch == "" {
		headBranch = proj.DefaultBranch
	}
	logStep("clone", fmt.Sprintf("Cloning %s (branch: %s)", proj.RepoURL, headBranch))
	if err := r.cloneWithCredentials(ctx, repo, ws.RepoPath, headBranch); err != nil {
		return r.failTask(ctx, taskID, fmt.Errorf("cloning repo: %w", err))
	}

	builder := prompt.NewBuilder()
	r.loadReviewPromptTemplate(ctx, proj, builder)
	systemPrompt := builder.BuildSystemPrompt()
	taskPrompt := builder.BuildReviewPrompt(issueForCtx, pr, diff, priorReviews)

	_, _ = r.client.PromptTemplateSnapshot.Create().
		SetTask(t).SetSystemPrompt(systemPrompt).SetTaskPrompt(taskPrompt).
		SetModelName(modelName).SetModelVersion(modelVersion).Save(ctx)

	logStep("agent_start", fmt.Sprintf("Starting review agent (model=%s)", modelName))
	handle, err := agentAdapter.StartSession(ctx, agentprovider.StartSessionRequest{
		WorkDir: ws.RepoPath, SystemPrompt: systemPrompt, TaskPrompt: taskPrompt,
	})
	if err != nil {
		return r.failTask(ctx, taskID, fmt.Errorf("starting agent: %w", err))
	}
	defer agentAdapter.Close(ctx, handle)

	if r.OnHandleReady != nil {
		r.OnHandleReady(agentAdapter, handle)
	}

	eventCh, err := agentAdapter.StreamEvents(ctx, handle)
	if err != nil {
		return r.failTask(ctx, taskID, fmt.Errorf("streaming events: %w", err))
	}

	var lastMessage strings.Builder
	var deltaBuf strings.Builder
	var agentErrors []string
	var turnCompleted bool
	for event := range eventCh {
		*sequence++
		sanitized := sanitize.SanitizeMap(event.Payload)
		payloadJSON, _ := json.Marshal(sanitized)
		_, _ = r.client.SessionEvent.Create().
			SetSession(sess).SetEventType(string(event.Type)).
			SetPayloadJSON(string(payloadJSON)).SetSequence(*sequence).Save(ctx)

		ssePayload := make(map[string]interface{}, len(sanitized)+1)
		for k, v := range sanitized {
			ssePayload[k] = v
		}
		ssePayload["_sequence"] = *sequence
		r.broker.Publish(topic, sse.Event{Type: string(event.Type), Data: ssePayload})

		if event.Type == model.AgentEventMessageDelta {
			if c, ok := sanitized["content"].(string); ok {
				deltaBuf.WriteString(c)
			}
		}
		if event.Type == model.AgentEventMessageCompleted {
			if c, ok := sanitized["content"].(string); ok && c != "" {
				lastMessage.Reset()
				lastMessage.WriteString(c)
			} else if deltaBuf.Len() > 0 {
				lastMessage.Reset()
				lastMessage.WriteString(deltaBuf.String())
			}
			deltaBuf.Reset()
		}
		if event.Type == model.AgentEventError {
			if msg, ok := sanitized["message"].(string); ok && msg != "" {
				agentErrors = append(agentErrors, msg)
				logStep("agent_error", msg)
			}
		}
		if event.Type == model.AgentEventTurnCompleted {
			turnCompleted = true
			break
		}
	}
	if lastMessage.Len() == 0 && deltaBuf.Len() > 0 {
		lastMessage.WriteString(deltaBuf.String())
	}

	r.cleanupCredentials(ws.RepoPath)

	// stderr banners (e.g. codex "Reading additional input from stdin...") surface
	// as AgentEventError but are informational. Only treat them as fatal if the
	// turn never completed — a completed turn with structured output is a success
	// regardless of side-channel chatter.
	if len(agentErrors) > 0 && !turnCompleted {
		return r.failTask(ctx, taskID, fmt.Errorf("agent error: %s", strings.Join(agentErrors, "; ")))
	}

	now := time.Now()
	_, _ = r.client.Session.UpdateOne(sess).SetStatus(session.StatusClosed).SetEndedAt(now).Save(ctx)

	verdict, perr := extractReviewVerdict(lastMessage.String())
	if perr != nil {
		logStep("review_parse_failed", perr.Error())
		verdict = &reviewVerdict{Decision: "comment", Summary: "Review agent produced no structured verdict; no actionable findings recorded."}
	}
	logStep("review_decision", fmt.Sprintf("decision=%s comments=%d", verdict.Decision, len(verdict.Comments)))

	event := "COMMENT"
	switch strings.ToLower(verdict.Decision) {
	case "approve":
		event = "APPROVE"
	case "request_changes":
		event = "REQUEST_CHANGES"
	}

	// GitHub rejects REQUEST_CHANGES / APPROVE with an empty body; ensure one.
	body := verdict.Summary
	if body == "" {
		body = "ccmate auto-review"
	}

	// If line-level posting fails (e.g. a comment points outside the diff),
	// fall back to a review body with findings inlined so review_fix can still
	// read them from prior reviews.
	fallbackBody := body
	if len(verdict.Comments) > 0 {
		var sb strings.Builder
		sb.WriteString(body)
		sb.WriteString("\n\nLine-level findings:\n")
		for _, c := range verdict.Comments {
			sb.WriteString(fmt.Sprintf("- `%s:%d`: %s\n", c.Path, c.Line, c.Body))
		}
		fallbackBody = sb.String()
	}

	if _, rerr := r.gitProvider.CreatePullRequestReview(ctx, repo, prNum, model.CreateReviewRequest{
		Body: body, Event: event, Comments: verdict.Comments,
	}); rerr != nil {
		logStep("review_post_failed", rerr.Error())
		if _, retryErr := r.gitProvider.CreatePullRequestReview(ctx, repo, prNum, model.CreateReviewRequest{
			Body: fallbackBody, Event: event,
		}); retryErr != nil {
			_ = r.gitProvider.CreateIssueComment(ctx, repo, prNum, fmt.Sprintf("ccmate review (fallback): %s\n\n%s", verdict.Decision, fallbackBody))
		}
	} else {
		logStep("review_posted", fmt.Sprintf("Posted %s review on PR #%d", event, prNum))
	}

	// If the agent wants changes, enqueue a review_fix task to address them.
	if event == "REQUEST_CHANGES" {
		r.enqueueReviewFix(ctx, t, proj, verdict, logStep)
	}

	if _, err := r.client.Task.UpdateOneID(taskID).SetStatus(enttask.StatusSucceeded).Save(ctx); err != nil {
		return fmt.Errorf("marking review task succeeded: %w", err)
	}
	if r.OnStatusChange != nil {
		r.OnStatusChange(ctx, taskID, string(model.TaskStatusRunning), string(model.TaskStatusSucceeded))
	}
	r.broker.Publish(topic, sse.Event{Type: "run.status", Data: map[string]interface{}{"task_id": taskID, "status": "succeeded"}})
	return nil
}

// enqueueReviewFix creates a review_fix task that inherits the project's default
// coding agent (not the review agent) so the fix is done by the implementer.
func (r *Runner) enqueueReviewFix(ctx context.Context, reviewTask *ent.Task, proj *ent.Project, verdict *reviewVerdict, logStep func(string, string)) {
	if reviewTask.PrNumber == nil {
		return
	}
	prNum := *reviewTask.PrNumber

	// Dedup: don't stack multiple active fix tasks for the same PR.
	activeExists, _ := r.client.Task.Query().
		Where(
			enttask.HasProjectWith(project.ID(proj.ID)),
			enttask.IssueNumber(reviewTask.IssueNumber),
			enttask.TypeEQ(enttask.TypeReviewFix),
			enttask.StatusIn(enttask.StatusQueued, enttask.StatusRunning, enttask.StatusPaused, enttask.StatusWaitingUser),
		).Exist(ctx)
	if activeExists {
		logStep("review_fix_skipped", "active review_fix task already exists")
		return
	}

	builder := r.client.Task.Create().
		SetProject(proj).SetIssueNumber(reviewTask.IssueNumber).
		SetNillablePrNumber(&prNum).
		SetType(enttask.TypeReviewFix).SetStatus(enttask.StatusQueued).
		SetReviewIteration(reviewTask.ReviewIteration).
		SetTriggerSource(string(model.TriggerSourceWebhook))
	if proj.DefaultAgentProfileID != nil {
		builder = builder.SetAgentProfileID(*proj.DefaultAgentProfileID)
	}
	if _, err := builder.Save(ctx); err != nil {
		logStep("review_fix_error", err.Error())
		return
	}
	logStep("review_fix_enqueued", fmt.Sprintf("queued review_fix for PR #%d (iteration %d)", prNum, reviewTask.ReviewIteration))
}

// enqueueReviewIfEnabled is called after a PR has fresh code (created or pushed)
// to start a new auto-review iteration. No-op if the project has no review agent
// configured or the iteration cap has been reached.
func (r *Runner) enqueueReviewIfEnabled(ctx context.Context, t *ent.Task, proj *ent.Project, logStep func(string, string)) {
	if proj.ReviewAgentProfileID == nil {
		return
	}
	if t.PrNumber == nil {
		return
	}
	prNum := *t.PrNumber

	maxIter := 3
	if raw := r.settingsMgr.GetWithDefault(ctx, settings.KeyMaxReviewIterations, "3"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			maxIter = n
		}
	}

	// Highest review_iteration observed across review/review_fix tasks on this issue.
	prior, err := r.client.Task.Query().
		Where(
			enttask.HasProjectWith(project.ID(proj.ID)),
			enttask.IssueNumber(t.IssueNumber),
			enttask.TypeIn(enttask.TypeReview, enttask.TypeReviewFix),
		).
		Order(ent.Desc(enttask.FieldReviewIteration)).
		Limit(1).
		All(ctx)
	if err != nil {
		logStep("review_iteration_lookup_error", err.Error())
		return
	}

	nextIter := 1
	if len(prior) > 0 {
		nextIter = prior[0].ReviewIteration + 1
	}
	if nextIter > maxIter {
		logStep("review_cap_reached", fmt.Sprintf("max review iterations (%d) reached for PR #%d", maxIter, prNum))
		_ = r.gitProvider.CreateIssueComment(ctx, repoFromProject(proj), prNum,
			fmt.Sprintf("ccmate: reached max review iterations (%d); stopping auto-review loop.", maxIter))
		return
	}

	// Dedup: skip if an active review task already exists for this PR.
	activeExists, err := r.client.Task.Query().
		Where(
			enttask.HasProjectWith(project.ID(proj.ID)),
			enttask.IssueNumber(t.IssueNumber),
			enttask.TypeEQ(enttask.TypeReview),
			enttask.StatusIn(enttask.StatusQueued, enttask.StatusRunning, enttask.StatusPaused),
		).Exist(ctx)
	if err != nil {
		logStep("review_dedup_error", err.Error())
		return
	}
	if activeExists {
		return
	}

	builder := r.client.Task.Create().
		SetProject(proj).SetIssueNumber(t.IssueNumber).
		SetNillablePrNumber(&prNum).
		SetType(enttask.TypeReview).SetStatus(enttask.StatusQueued).
		SetReviewIteration(nextIter).
		SetAgentProfileID(*proj.ReviewAgentProfileID).
		SetTriggerSource(string(model.TriggerSourceWebhook))
	if _, err := builder.Save(ctx); err != nil {
		logStep("review_enqueue_error", err.Error())
		return
	}
	logStep("review_enqueued", fmt.Sprintf("queued review task for PR #%d (iteration %d)", prNum, nextIter))
}

func repoFromProject(proj *ent.Project) model.RepoRef {
	return parseRepoURL(proj.RepoURL)
}
