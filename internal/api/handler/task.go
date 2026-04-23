package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/cloverstd/ccmate/internal/config"
	"github.com/cloverstd/ccmate/internal/ent"
	"github.com/cloverstd/ccmate/internal/ent/project"
	enttask "github.com/cloverstd/ccmate/internal/ent/task"
	"github.com/cloverstd/ccmate/internal/gitprovider"
	"github.com/cloverstd/ccmate/internal/model"
	"github.com/cloverstd/ccmate/internal/runner"
	"github.com/cloverstd/ccmate/internal/scheduler"
	"github.com/cloverstd/ccmate/internal/settings"
	"github.com/cloverstd/ccmate/internal/sse"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type TaskHandler struct {
	client      *ent.Client
	cfg         *config.Config
	broker      *sse.Broker
	sched       *scheduler.Scheduler
	gitProvMgr  *gitprovider.Manager
	settingsMgr *settings.Manager
}

// gitProv returns the currently active git provider (may be nil).
func (h *TaskHandler) gitProv() gitprovider.GitProvider {
	if h.gitProvMgr == nil {
		return nil
	}
	return h.gitProvMgr.Current()
}

type taskGitSummary struct {
	Branch       string             `json:"branch"`
	LatestCommit *runner.CommitInfo `json:"latest_commit,omitempty"`
	Branches     []model.RepoBranch `json:"branches,omitempty"`
}

func NewTaskHandler(client *ent.Client, cfg *config.Config, broker *sse.Broker, sched *scheduler.Scheduler, gitProvMgr *gitprovider.Manager, settingsMgr *settings.Manager) *TaskHandler {
	return &TaskHandler{client: client, cfg: cfg, broker: broker, sched: sched, gitProvMgr: gitProvMgr, settingsMgr: settingsMgr}
}

func (h *TaskHandler) List(w http.ResponseWriter, r *http.Request) {
	query := h.client.Task.Query().
		Order(ent.Desc(enttask.FieldCreatedAt)).
		WithProject()

	if status := r.URL.Query().Get("status"); status != "" {
		query = query.Where(enttask.StatusEQ(enttask.Status(status)))
	}

	if projectID := r.URL.Query().Get("project_id"); projectID != "" {
		if id, err := strconv.Atoi(projectID); err == nil {
			query = query.Where(enttask.HasProjectWith(project.ID(id)))
		}
	}

	tasks, err := query.All(r.Context())
	if err != nil {
		http.Error(w, `{"error":"failed to list tasks"}`, http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, tasks)
}

type CreateTaskRequest struct {
	ProjectID      int    `json:"project_id"`
	IssueNumber    int    `json:"issue_number"`
	TaskType       string `json:"type"`
	AgentProfileID *int   `json:"agent_profile_id"`
}

// Create manually creates a task for an issue (P1-06).
func (h *TaskHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req CreateTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	if req.ProjectID == 0 || req.IssueNumber == 0 {
		http.Error(w, `{"error":"project_id and issue_number are required"}`, http.StatusBadRequest)
		return
	}

	// Check project exists
	proj, err := h.client.Project.Get(r.Context(), req.ProjectID)
	if err != nil {
		http.Error(w, `{"error":"project not found"}`, http.StatusNotFound)
		return
	}

	// Check no active task for this issue
	exists, err := h.client.Task.Query().
		Where(
			enttask.HasProjectWith(project.ID(proj.ID)),
			enttask.IssueNumber(req.IssueNumber),
			enttask.StatusIn(
				enttask.StatusQueued, enttask.StatusRunning,
				enttask.StatusPaused, enttask.StatusWaitingUser,
			),
		).Exist(r.Context())
	if err != nil {
		http.Error(w, `{"error":"query failed"}`, http.StatusInternalServerError)
		return
	}
	if exists {
		http.Error(w, `{"error":"active task already exists for this issue"}`, http.StatusConflict)
		return
	}

	taskType := enttask.TypeIssueImplementation
	if req.TaskType == "manual_followup" {
		taskType = enttask.TypeManualFollowup
	}

	agentProfileID, err := h.resolveAgentProfileID(r.Context(), proj, req.AgentProfileID)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusBadRequest)
		return
	}

	builder := h.client.Task.Create().
		SetProject(proj).
		SetIssueNumber(req.IssueNumber).
		SetType(taskType).
		SetStatus(enttask.StatusQueued).
		SetTriggerSource(string(model.TriggerSourceWeb))
	if agentProfileID != nil {
		builder = builder.SetAgentProfileID(*agentProfileID)
	}
	t, err := builder.Save(r.Context())
	if err != nil {
		http.Error(w, `{"error":"failed to create task"}`, http.StatusInternalServerError)
		return
	}

	h.broker.Publish("tasks", sse.Event{
		Type: "task.created",
		Data: map[string]interface{}{"task_id": t.ID, "project_id": proj.ID, "status": string(t.Status)},
	})

	writeJSON(w, http.StatusCreated, t)
}

type CreateFromPromptRequest struct {
	ProjectID      int      `json:"project_id"`
	Title          string   `json:"title"`
	Body           string   `json:"body"`
	Labels         []string `json:"labels"`
	AgentProfileID *int     `json:"agent_profile_id"`
}

// CreateFromPrompt creates a GitHub issue from the prompt, then creates a task for it.
func (h *TaskHandler) CreateFromPrompt(w http.ResponseWriter, r *http.Request) {
	var req CreateFromPromptRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	if req.ProjectID == 0 || req.Title == "" || req.Body == "" {
		http.Error(w, `{"error":"project_id, title and body are required"}`, http.StatusBadRequest)
		return
	}

	gp := h.gitProv()
	if gp == nil {
		http.Error(w, `{"error":"git provider not configured"}`, http.StatusInternalServerError)
		return
	}

	proj, err := h.client.Project.Get(r.Context(), req.ProjectID)
	if err != nil {
		http.Error(w, `{"error":"project not found"}`, http.StatusNotFound)
		return
	}

	repo := parseRepoURLFromString(proj.RepoURL)

	// Create issue on GitHub
	issue, err := gp.CreateIssue(r.Context(), repo, req.Title, req.Body, req.Labels)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"failed to create issue: %s"}`, err.Error()), http.StatusInternalServerError)
		return
	}

	// Create task for the new issue
	agentProfileID, err := h.resolveAgentProfileID(r.Context(), proj, req.AgentProfileID)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusBadRequest)
		return
	}

	builder := h.client.Task.Create().
		SetProject(proj).
		SetIssueNumber(issue.Number).
		SetType(enttask.TypeIssueImplementation).
		SetStatus(enttask.StatusQueued).
		SetTriggerSource(string(model.TriggerSourceWeb))
	if agentProfileID != nil {
		builder = builder.SetAgentProfileID(*agentProfileID)
	}
	t, err := builder.Save(r.Context())
	if err != nil {
		http.Error(w, `{"error":"failed to create task"}`, http.StatusInternalServerError)
		return
	}

	h.broker.Publish("tasks", sse.Event{
		Type: "task.created",
		Data: map[string]interface{}{"task_id": t.ID, "project_id": proj.ID, "status": string(t.Status)},
	})

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"issue": issue,
		"task":  t,
	})
}

func (h *TaskHandler) resolveAgentProfileID(ctx context.Context, proj *ent.Project, requested *int) (*int, error) {
	if requested != nil {
		if _, err := h.client.AgentProfile.Get(ctx, *requested); err != nil {
			return nil, fmt.Errorf("agent profile not found")
		}
		return requested, nil
	}
	if proj.DefaultAgentProfileID != nil {
		return proj.DefaultAgentProfileID, nil
	}
	if fallback := h.settingsMgr.GetOptionalInt(ctx, settings.KeyDefaultAgentProfileID); fallback != nil {
		if _, err := h.client.AgentProfile.Get(ctx, *fallback); err == nil {
			return fallback, nil
		}
	}
	return nil, nil
}

func parseRepoURLFromString(url string) model.RepoRef {
	for _, sep := range []string{"github.com/", "gitlab.com/", "gitee.com/"} {
		if i := strings.Index(url, sep); i >= 0 {
			rest := url[i+len(sep):]
			rest = strings.TrimSuffix(rest, ".git")
			parts := strings.SplitN(rest, "/", 2)
			if len(parts) == 2 {
				return model.RepoRef{Owner: parts[0], Name: parts[1]}
			}
		}
	}
	return model.RepoRef{}
}

// Get returns the fast core of a task: task DB record + workspace_path + agent_profile.
// Slower data sourced from GitHub (issue, pull_request) and local git (git summary) are
// served by dedicated endpoints so the detail page can render immediately while those
// pieces load in parallel. See GetIssue/GetPullRequest/GetGit below.
func (h *TaskHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, `{"error":"invalid id"}`, http.StatusBadRequest)
		return
	}

	t, err := h.client.Task.Query().
		Where(enttask.ID(id)).
		WithProject().
		WithPromptSnapshot().
		WithSessions(func(q *ent.SessionQuery) {
			q.WithMessages()
			q.WithEvents()
		}).
		Only(r.Context())
	if err != nil {
		if ent.IsNotFound(err) {
			http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
			return
		}
		http.Error(w, `{"error":"failed to get task"}`, http.StatusInternalServerError)
		return
	}

	basePath := h.settingsMgr.GetWithDefault(r.Context(), settings.KeyStorageBasePath, "data")
	var workspacePath string
	if t.Edges.Project != nil {
		workspacePath = filepath.Join(basePath, "workspaces", fmt.Sprintf("%d", t.Edges.Project.ID), fmt.Sprintf("%d", t.ID), "repo")
	}

	var agentProfile *ent.AgentProfile
	if t.AgentProfileID != nil {
		agentProfile, _ = h.client.AgentProfile.Get(r.Context(), *t.AgentProfileID)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"task":           t,
		"workspace_path": workspacePath,
		"agent_profile":  agentProfile,
	})
}

// loadTaskWithProject loads a task with its project edge for the sub-detail endpoints.
func (h *TaskHandler) loadTaskWithProject(w http.ResponseWriter, r *http.Request) (*ent.Task, bool) {
	id, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, `{"error":"invalid id"}`, http.StatusBadRequest)
		return nil, false
	}
	t, err := h.client.Task.Query().Where(enttask.ID(id)).WithProject().Only(r.Context())
	if err != nil {
		if ent.IsNotFound(err) {
			http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
		} else {
			http.Error(w, `{"error":"failed to get task"}`, http.StatusInternalServerError)
		}
		return nil, false
	}
	return t, true
}

// GetIssue returns the GitHub issue linked to the task. Served separately so the detail
// page isn't blocked when the GitHub API is slow or unavailable.
func (h *TaskHandler) GetIssue(w http.ResponseWriter, r *http.Request) {
	t, ok := h.loadTaskWithProject(w, r)
	if !ok {
		return
	}
	// Null is reserved for "no project/provider configured"; provider failures surface
	// as 502 so the frontend can distinguish empty state from transient errors.
	var issue *model.Issue
	if t.Edges.Project != nil {
		repo := parseRepoURLFromString(t.Edges.Project.RepoURL)
		if gp := h.gitProv(); gp != nil && repo.Owner != "" {
			var err error
			issue, err = gp.GetIssue(r.Context(), repo, t.IssueNumber)
			if err != nil {
				slog.Warn("failed to fetch task issue", "task_id", t.ID, "issue_number", t.IssueNumber, "error", err)
				http.Error(w, `{"error":"failed to fetch issue from git provider"}`, http.StatusBadGateway)
				return
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"issue": issue})
}

// GetPullRequest returns the PR associated with the task (by pr_number or by branch name).
func (h *TaskHandler) GetPullRequest(w http.ResponseWriter, r *http.Request) {
	t, ok := h.loadTaskWithProject(w, r)
	if !ok {
		return
	}
	// Distinguish three cases: no PR exists (null), provider not configured (still build
	// a minimal stub from task.pr_number if present), provider failed (502).
	var pr *model.PullRequest
	if t.Edges.Project != nil {
		repo := parseRepoURLFromString(t.Edges.Project.RepoURL)
		gp := h.gitProv()
		if gp != nil && repo.Owner != "" {
			if t.PrNumber != nil && *t.PrNumber > 0 {
				fetched, err := gp.GetPullRequest(r.Context(), repo, *t.PrNumber)
				if err != nil {
					slog.Warn("failed to fetch task pull request", "task_id", t.ID, "pr_number", *t.PrNumber, "error", err)
					http.Error(w, `{"error":"failed to fetch pull request from git provider"}`, http.StatusBadGateway)
					return
				}
				if fetched != nil {
					pr = fetched
				} else {
					pr = &model.PullRequest{
						Number:  *t.PrNumber,
						HTMLURL: fmt.Sprintf("%s/pull/%d", t.Edges.Project.RepoURL, *t.PrNumber),
						State:   "unknown",
					}
				}
			}
			if pr == nil {
				basePath := h.settingsMgr.GetWithDefault(r.Context(), settings.KeyStorageBasePath, "data")
				ws := runner.NewWorkspace(filepath.Join(basePath, "workspaces"), t.Edges.Project.ID, t.ID)
				branchName := ws.BranchName(t.IssueNumber)
				found, err := gp.FindPullRequestByHead(r.Context(), repo, branchName)
				if err != nil {
					slog.Warn("failed to find PR by head", "task_id", t.ID, "branch", branchName, "error", err)
					http.Error(w, `{"error":"failed to locate pull request from git provider"}`, http.StatusBadGateway)
					return
				}
				pr = found
			}
		} else if t.PrNumber != nil && *t.PrNumber > 0 {
			pr = &model.PullRequest{
				Number:  *t.PrNumber,
				HTMLURL: fmt.Sprintf("%s/pull/%d", t.Edges.Project.RepoURL, *t.PrNumber),
				State:   "unknown",
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"pull_request": pr})
}

// GetGit returns the local-git summary (current branch, latest commit, branch list) for
// the task's workspace. Does a fetch under the hood — can take seconds, so isolated here.
func (h *TaskHandler) GetGit(w http.ResponseWriter, r *http.Request) {
	t, ok := h.loadTaskWithProject(w, r)
	if !ok {
		return
	}
	var git *taskGitSummary
	if t.Edges.Project != nil {
		basePath := h.settingsMgr.GetWithDefault(r.Context(), settings.KeyStorageBasePath, "data")
		ws := runner.NewWorkspace(filepath.Join(basePath, "workspaces"), t.Edges.Project.ID, t.ID)
		git = &taskGitSummary{Branch: ws.BranchName(t.IssueNumber)}
		if _, err := os.Stat(ws.RepoPath); err == nil {
			_ = runner.FetchProject(r.Context(), ws.RepoPath)
			if actual, err := runner.CurrentBranch(r.Context(), ws.RepoPath); err == nil && actual != "" {
				git.Branch = actual
			}
			commits, err := runner.ListCommits(r.Context(), ws.RepoPath, "HEAD", 1)
			if err == nil && len(commits) > 0 {
				git.LatestCommit = &commits[0]
			}
			localBranches, err := runner.ListBranches(r.Context(), ws.RepoPath)
			if err == nil {
				gitBranches := make([]model.RepoBranch, len(localBranches))
				for i, b := range localBranches {
					gitBranches[i] = model.RepoBranch{Name: b.Name, Hash: b.Hash, Message: b.Message}
				}
				git.Branches = gitBranches
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"git": git})
}

func (h *TaskHandler) Pause(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(chi.URLParam(r, "id"))
	if err := h.sched.PauseTask(r.Context(), id); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *TaskHandler) Resume(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(chi.URLParam(r, "id"))
	if err := h.sched.ResumeTask(r.Context(), id); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *TaskHandler) Retry(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(chi.URLParam(r, "id"))
	if err := h.sched.RetryTask(r.Context(), id); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *TaskHandler) Cancel(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(chi.URLParam(r, "id"))
	if err := h.sched.CancelTask(r.Context(), id); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type CompleteTaskRequest struct {
	CloseIssue bool `json:"close_issue"`
	MergePR    bool `json:"merge_pr"`
}

// Complete marks a task as done, closes the issue, and merges the PR if requested.
func (h *TaskHandler) Complete(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(chi.URLParam(r, "id"))
	var req CompleteTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err != io.EOF {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}
	if !req.CloseIssue && !req.MergePR {
		http.Error(w, `{"error":"select at least one completion action"}`, http.StatusBadRequest)
		return
	}

	t, err := h.client.Task.Query().
		Where(enttask.ID(id)).WithProject().Only(r.Context())
	if err != nil {
		http.Error(w, `{"error":"task not found"}`, http.StatusNotFound)
		return
	}

	proj := t.Edges.Project
	gp := h.gitProv()
	if proj == nil || gp == nil {
		http.Error(w, `{"error":"project or git provider not available"}`, http.StatusBadRequest)
		return
	}

	repo := parseRepoURLFromString(proj.RepoURL)
	ctx := r.Context()
	var actions []string

	// 1. Merge PR if requested
	if req.MergePR {
		if t.PrNumber == nil || *t.PrNumber <= 0 {
			http.Error(w, `{"error":"task has no PR to merge"}`, http.StatusBadRequest)
			return
		}
		if err := gp.MergePullRequest(ctx, repo, *t.PrNumber); err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"failed to merge PR #%d: %s"}`, *t.PrNumber, err.Error()), http.StatusInternalServerError)
			return
		}
		actions = append(actions, fmt.Sprintf("PR #%d merged", *t.PrNumber))
	}

	// 2. Close issue if requested or implied by merge.
	if req.CloseIssue || req.MergePR {
		if err := gp.CloseIssue(ctx, repo, t.IssueNumber); err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"failed to close issue #%d: %s"}`, t.IssueNumber, err.Error()), http.StatusInternalServerError)
			return
		}
		actions = append(actions, fmt.Sprintf("Issue #%d closed", t.IssueNumber))
	}

	// 3. Mark task as succeeded
	prevStatus := string(t.Status)
	if _, err := h.client.Task.UpdateOneID(id).SetStatus(enttask.StatusSucceeded).Save(ctx); err != nil {
		slog.Error("failed to persist task completion status", "task_id", id, "error", err)
		http.Error(w, `{"error":"failed to persist task completion status"}`, http.StatusInternalServerError)
		return
	}
	scheduler.PublishStatusChange(h.broker, id, prevStatus, string(enttask.StatusSucceeded))

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":  "completed",
		"actions": actions,
	})
}

type SendMessageRequest struct {
	Content     string `json:"content"`
	ContentType string `json:"content_type"`
}

func (h *TaskHandler) SendMessage(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(chi.URLParam(r, "id"))

	var req SendMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	t, err := h.client.Task.Get(r.Context(), id)
	if err != nil {
		http.Error(w, `{"error":"task not found"}`, http.StatusNotFound)
		return
	}

	if t.CurrentSessionID == nil || *t.CurrentSessionID == 0 {
		http.Error(w, `{"error":"no active session"}`, http.StatusBadRequest)
		return
	}

	contentType := req.ContentType
	if contentType == "" {
		contentType = "text"
	}

	msg, err := h.client.SessionMessage.Create().
		SetSessionID(*t.CurrentSessionID).
		SetRole("user").
		SetContentType(contentType).
		SetContent(req.Content).
		Save(r.Context())
	if err != nil {
		http.Error(w, `{"error":"failed to save message"}`, http.StatusInternalServerError)
		return
	}

	h.broker.Publish(fmt.Sprintf("task:%d", id), sse.Event{
		Type: "message.created",
		Data: msg,
	})

	// Forward message to running agent, or resume if not running
	if t.Status == enttask.StatusRunning {
		_ = h.sched.HandleUserInput(r.Context(), id, model.AgentEvent{
			Type:    model.AgentEventMessageDelta,
			Payload: map[string]interface{}{"content": req.Content},
		})
	} else if t.Status == enttask.StatusWaitingUser || t.Status == enttask.StatusPaused || t.Status == enttask.StatusFailed || t.Status == enttask.StatusSucceeded {
		// Resume the task with the new message
		if err := h.sched.ResumeWithMessage(r.Context(), id, req.Content); err != nil {
			slog.Warn("failed to resume task with message", "task_id", id, "error", err)
			http.Error(w, fmt.Sprintf(`{"error":"failed to resume task: %s"}`, err.Error()), http.StatusBadRequest)
			return
		}
	}

	writeJSON(w, http.StatusCreated, msg)
}

var allowedMimeTypes = map[string]bool{
	"image/jpeg": true, "image/png": true, "image/gif": true, "image/webp": true,
}

// UploadAttachment handles file upload for a task (P1-04).
func (h *TaskHandler) UploadAttachment(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(chi.URLParam(r, "id"))

	maxSizeMB, _ := strconv.Atoi(h.settingsMgr.GetWithDefault(r.Context(), settings.KeyMaxAttachmentMB, "10"))
	maxSize := int64(maxSizeMB) * 1024 * 1024
	r.Body = http.MaxBytesReader(w, r.Body, maxSize)

	if err := r.ParseMultipartForm(maxSize); err != nil {
		http.Error(w, `{"error":"file too large"}`, http.StatusRequestEntityTooLarge)
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, `{"error":"no file provided"}`, http.StatusBadRequest)
		return
	}
	defer file.Close()

	// Validate MIME type
	mimeType := header.Header.Get("Content-Type")
	if !allowedMimeTypes[mimeType] {
		http.Error(w, `{"error":"unsupported file type"}`, http.StatusBadRequest)
		return
	}

	// Sanitize filename (prevent directory traversal)
	fileName := filepath.Base(header.Filename)
	fileName = strings.ReplaceAll(fileName, "..", "")

	// Generate storage path
	storageName := fmt.Sprintf("%s_%s", uuid.New().String()[:8], fileName)
	basePath := h.settingsMgr.GetWithDefault(r.Context(), settings.KeyStorageBasePath, "data")
	storageDir := filepath.Join(basePath, "attachments")
	if err := os.MkdirAll(storageDir, 0755); err != nil {
		http.Error(w, `{"error":"storage error"}`, http.StatusInternalServerError)
		return
	}
	storagePath := filepath.Join(storageDir, storageName)

	dst, err := os.Create(storagePath)
	if err != nil {
		http.Error(w, `{"error":"storage error"}`, http.StatusInternalServerError)
		return
	}
	defer dst.Close()

	written, err := io.Copy(dst, file)
	if err != nil {
		os.Remove(storagePath)
		http.Error(w, `{"error":"write error"}`, http.StatusInternalServerError)
		return
	}

	attachment, err := h.client.Attachment.Create().
		SetTaskID(id).
		SetFileName(fileName).
		SetMimeType(mimeType).
		SetSize(written).
		SetStoragePath(storagePath).
		Save(r.Context())
	if err != nil {
		os.Remove(storagePath)
		http.Error(w, `{"error":"failed to save attachment"}`, http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusCreated, attachment)
}

func (h *TaskHandler) EventStream(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	topic := fmt.Sprintf("task:%s", id)
	h.broker.ServeHTTP(w, r, topic)
}

// TasksEventStream streams cross-task lifecycle events (creation, status changes, failures).
// Used by UI views that need to react to any task updating — e.g. sidebar running count.
func (h *TaskHandler) TasksEventStream(w http.ResponseWriter, r *http.Request) {
	h.broker.ServeHTTP(w, r, "tasks")
}
