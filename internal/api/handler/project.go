package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/cloverstd/ccmate/internal/ent"
	"github.com/cloverstd/ccmate/internal/ent/project"
	"github.com/cloverstd/ccmate/internal/ent/prompttemplate"
	enttask "github.com/cloverstd/ccmate/internal/ent/task"
	"github.com/cloverstd/ccmate/internal/gitprovider"
	"github.com/cloverstd/ccmate/internal/model"
	"github.com/cloverstd/ccmate/internal/runner"
	"github.com/cloverstd/ccmate/internal/settings"
	"github.com/go-chi/chi/v5"
)

type ProjectHandler struct {
	client      *ent.Client
	gitProvMgr  *gitprovider.Manager
	settingsMgr *settings.Manager
}

func NewProjectHandler(client *ent.Client, gitProvMgr *gitprovider.Manager, settingsMgr *settings.Manager) *ProjectHandler {
	return &ProjectHandler{client: client, gitProvMgr: gitProvMgr, settingsMgr: settingsMgr}
}

// gitProv returns the currently active git provider (may be nil).
func (h *ProjectHandler) gitProv() gitprovider.GitProvider {
	if h.gitProvMgr == nil {
		return nil
	}
	return h.gitProvMgr.Current()
}

type CreateProjectRequest struct {
	Name                    string `json:"name"`
	RepoURL                 string `json:"repo_url"`
	GitProvider             string `json:"git_provider"`
	DefaultBranch           string `json:"default_branch"`
	AutoMode                bool   `json:"auto_mode"`
	DefaultAgentProfileID   *int   `json:"default_agent_profile_id"`
	ReviewAgentProfileID    *int   `json:"review_agent_profile_id"`
	DefaultPromptTemplateID *int   `json:"default_prompt_template_id"`
	PromptTemplateScope     string `json:"prompt_template_scope"`
}

func (h *ProjectHandler) List(w http.ResponseWriter, r *http.Request) {
	projects, err := h.client.Project.Query().Order(ent.Desc(project.FieldCreatedAt)).All(r.Context())
	if err != nil {
		http.Error(w, `{"error":"failed to list projects"}`, http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, projects)
}

func (h *ProjectHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(chi.URLParam(r, "id"))
	p, err := h.client.Project.Query().Where(project.ID(id)).Only(r.Context())
	if err != nil {
		if ent.IsNotFound(err) {
			http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
			return
		}
		http.Error(w, `{"error":"failed to get project"}`, http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (h *ProjectHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req CreateProjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}
	if req.DefaultBranch == "" {
		req.DefaultBranch = "main"
	}
	builder := h.client.Project.Create().
		SetName(req.Name).SetRepoURL(req.RepoURL).SetGitProvider(req.GitProvider).
		SetDefaultBranch(req.DefaultBranch).SetAutoMode(req.AutoMode)
	if req.DefaultAgentProfileID != nil {
		if _, err := h.client.AgentProfile.Get(r.Context(), *req.DefaultAgentProfileID); err != nil {
			http.Error(w, `{"error":"default agent profile not found"}`, http.StatusBadRequest)
			return
		}
		builder = builder.SetDefaultAgentProfileID(*req.DefaultAgentProfileID)
	}
	if req.ReviewAgentProfileID != nil {
		if _, err := h.client.AgentProfile.Get(r.Context(), *req.ReviewAgentProfileID); err != nil {
			http.Error(w, `{"error":"review agent profile not found"}`, http.StatusBadRequest)
			return
		}
		builder = builder.SetReviewAgentProfileID(*req.ReviewAgentProfileID)
	}
	if req.DefaultPromptTemplateID != nil {
		if _, err := h.client.PromptTemplate.Get(r.Context(), *req.DefaultPromptTemplateID); err != nil {
			http.Error(w, `{"error":"default prompt template not found"}`, http.StatusBadRequest)
			return
		}
		builder = builder.SetDefaultPromptTemplateID(*req.DefaultPromptTemplateID)
	}
	if req.PromptTemplateScope != "" {
		builder = builder.SetPromptTemplateScope(project.PromptTemplateScope(req.PromptTemplateScope))
	}
	p, err := builder.Save(r.Context())
	if err != nil {
		http.Error(w, `{"error":"failed to create project"}`, http.StatusInternalServerError)
		return
	}

	// Clone repo to local storage in background
	go func() {
		basePath := h.settingsMgr.GetWithDefault(r.Context(), settings.KeyStorageBasePath, "data")
		repoDir := runner.RepoDir(basePath, p.ID)
		if _, err := os.Stat(repoDir); err == nil {
			return // already exists
		}
		token := h.settingsMgr.GetWithDefault(r.Context(), settings.KeyGitHubPersonalToken, "")
		if err := runner.CloneProject(r.Context(), req.RepoURL, repoDir, token); err != nil {
			slog.Error("failed to clone project repo", "project", p.Name, "error", err)
		} else {
			slog.Info("project repo cloned", "project", p.Name, "path", repoDir)
		}
	}()

	writeJSON(w, http.StatusCreated, p)
}

func (h *ProjectHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(chi.URLParam(r, "id"))
	var req CreateProjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}
	builder := h.client.Project.UpdateOneID(id).
		SetName(req.Name).SetRepoURL(req.RepoURL).SetGitProvider(req.GitProvider).
		SetDefaultBranch(req.DefaultBranch).SetAutoMode(req.AutoMode)
	if req.DefaultAgentProfileID != nil {
		if _, err := h.client.AgentProfile.Get(r.Context(), *req.DefaultAgentProfileID); err != nil {
			http.Error(w, `{"error":"default agent profile not found"}`, http.StatusBadRequest)
			return
		}
		builder = builder.SetDefaultAgentProfileID(*req.DefaultAgentProfileID)
	} else {
		builder = builder.ClearDefaultAgentProfileID()
	}
	if req.ReviewAgentProfileID != nil {
		if _, err := h.client.AgentProfile.Get(r.Context(), *req.ReviewAgentProfileID); err != nil {
			http.Error(w, `{"error":"review agent profile not found"}`, http.StatusBadRequest)
			return
		}
		builder = builder.SetReviewAgentProfileID(*req.ReviewAgentProfileID)
	} else {
		builder = builder.ClearReviewAgentProfileID()
	}
	if req.DefaultPromptTemplateID != nil {
		if _, err := h.client.PromptTemplate.Get(r.Context(), *req.DefaultPromptTemplateID); err != nil {
			http.Error(w, `{"error":"default prompt template not found"}`, http.StatusBadRequest)
			return
		}
		builder = builder.SetDefaultPromptTemplateID(*req.DefaultPromptTemplateID)
	} else {
		builder = builder.ClearDefaultPromptTemplateID()
	}
	if req.PromptTemplateScope != "" {
		builder = builder.SetPromptTemplateScope(project.PromptTemplateScope(req.PromptTemplateScope))
	}
	p, err := builder.Save(r.Context())
	if err != nil {
		if ent.IsNotFound(err) {
			http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
			return
		}
		http.Error(w, `{"error":"failed to update project"}`, http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

// --- Project Tasks ---

func (h *ProjectHandler) ListProjectTasks(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(chi.URLParam(r, "id"))
	tasks, err := h.client.Task.Query().
		Where(enttask.HasProjectWith(project.ID(id))).
		Order(ent.Desc(enttask.FieldCreatedAt)).
		All(r.Context())
	if err != nil {
		http.Error(w, `{"error":"failed to list tasks"}`, http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, tasks)
}

// --- Git Info (local repo) ---

func (h *ProjectHandler) getLocalRepoPath(r *http.Request) (string, int, error) {
	id, _ := strconv.Atoi(chi.URLParam(r, "id"))
	basePath := h.settingsMgr.GetWithDefault(r.Context(), settings.KeyStorageBasePath, "data")
	return runner.RepoDir(basePath, id), id, nil
}

func (h *ProjectHandler) GetRepoInfo(w http.ResponseWriter, r *http.Request) {
	repoPath, projectID, _ := h.getLocalRepoPath(r)
	ctx := r.Context()
	repo := h.getRepoRef(w, r)
	if repo == nil {
		return
	}

	branches, err := h.gitProv().ListRepoBranches(ctx, *repo)
	if err != nil {
		http.Error(w, `{"error":"failed to list remote branches: `+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}
	tags, err := h.gitProv().ListRepoTags(ctx, *repo)
	if err != nil {
		http.Error(w, `{"error":"failed to list remote tags: `+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"project_id": projectID,
		"repo_path":  repoPath,
		"branches":   branches,
		"tags":       tags,
	})
}

func (h *ProjectHandler) GetBranchCommits(w http.ResponseWriter, r *http.Request) {
	repoPath, _, _ := h.getLocalRepoPath(r)
	branch := r.URL.Query().Get("branch")
	if branch == "" {
		branch = "HEAD"
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 20
	}

	commits, err := runner.ListCommits(r.Context(), repoPath, branch, limit)
	if err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, commits)
}

func (h *ProjectHandler) PullRepo(w http.ResponseWriter, r *http.Request) {
	repoPath, _, _ := h.getLocalRepoPath(r)
	if err := runner.FetchProject(r.Context(), repoPath); err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "fetched"})
}

// --- Repo Issues & PRs (via GitHub API) ---

func (h *ProjectHandler) ListRepoIssues(w http.ResponseWriter, r *http.Request) {
	repo := h.getRepoRef(w, r)
	if repo == nil {
		return
	}

	ctx := r.Context()
	// Use gitProvider to list open issues
	issues, err := h.gitProv().ListRepoIssues(ctx, *repo)
	if err != nil {
		http.Error(w, `{"error":"failed to list issues: `+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, issues)
}

func (h *ProjectHandler) ListRepoPRs(w http.ResponseWriter, r *http.Request) {
	repo := h.getRepoRef(w, r)
	if repo == nil {
		return
	}

	prs, err := h.gitProv().ListRepoPRs(r.Context(), *repo)
	if err != nil {
		http.Error(w, `{"error":"failed to list PRs: `+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, prs)
}

func (h *ProjectHandler) getRepoRef(w http.ResponseWriter, r *http.Request) *model.RepoRef {
	if h.gitProv() == nil {
		http.Error(w, `{"error":"git provider not configured"}`, http.StatusInternalServerError)
		return nil
	}
	id, _ := strconv.Atoi(chi.URLParam(r, "id"))
	proj, err := h.client.Project.Get(r.Context(), id)
	if err != nil {
		http.Error(w, `{"error":"project not found"}`, http.StatusNotFound)
		return nil
	}
	repo := parseRepoURL(proj.RepoURL)
	return &repo
}

// --- GitHub Repos ---

func (h *ProjectHandler) ListGitHubRepos(w http.ResponseWriter, r *http.Request) {
	gp := h.gitProv()
	if gp == nil {
		http.Error(w, `{"error":"git provider not configured"}`, http.StatusInternalServerError)
		return
	}
	repos, err := gp.ListAccessibleRepos(r.Context())
	if err != nil {
		http.Error(w, `{"error":"failed to list repos: `+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, repos)
}

// --- Global Label Rules (from config) ---

func (h *ProjectHandler) ListGlobalLabelRules(w http.ResponseWriter, r *http.Request) {
	rules := h.settingsMgr.GetLabelRules(r.Context())
	writeJSON(w, http.StatusOK, rules)
}

// --- Prompt Templates ---

type PromptTemplateRequest struct {
	Name         string `json:"name"`
	SystemPrompt string `json:"system_prompt"`
	TaskPrompt   string `json:"task_prompt"`
	IsBuiltin    bool   `json:"is_builtin"`
	ProjectID    *int   `json:"project_id"`
}

func (h *ProjectHandler) ListPromptTemplates(w http.ResponseWriter, r *http.Request) {
	query := h.client.PromptTemplate.Query()
	scope := r.URL.Query().Get("scope")
	projectIDStr := r.URL.Query().Get("project_id")
	switch scope {
	case "global":
		query = query.Where(prompttemplate.ProjectIDIsNil())
	case "project":
		if pid, err := strconv.Atoi(projectIDStr); err == nil {
			query = query.Where(prompttemplate.ProjectID(pid))
		}
	case "all":
		if pid, err := strconv.Atoi(projectIDStr); err == nil {
			query = query.Where(prompttemplate.Or(
				prompttemplate.ProjectIDIsNil(),
				prompttemplate.ProjectID(pid),
			))
		}
	}
	templates, err := query.All(r.Context())
	if err != nil {
		http.Error(w, `{"error":"failed to list templates"}`, http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, templates)
}

func (h *ProjectHandler) CreatePromptTemplate(w http.ResponseWriter, r *http.Request) {
	var req PromptTemplateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}
	builder := h.client.PromptTemplate.Create().
		SetName(req.Name).SetSystemPrompt(req.SystemPrompt).
		SetTaskPrompt(req.TaskPrompt).SetIsBuiltin(req.IsBuiltin)
	if req.ProjectID != nil {
		builder = builder.SetProjectID(*req.ProjectID)
	}
	t, err := builder.Save(r.Context())
	if err != nil {
		http.Error(w, `{"error":"failed to create template"}`, http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusCreated, t)
}

func (h *ProjectHandler) UpdatePromptTemplate(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(chi.URLParam(r, "id"))
	var req PromptTemplateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}
	t, err := h.client.PromptTemplate.UpdateOneID(id).
		SetName(req.Name).SetSystemPrompt(req.SystemPrompt).SetTaskPrompt(req.TaskPrompt).Save(r.Context())
	if err != nil {
		http.Error(w, `{"error":"failed to update template"}`, http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, t)
}

func (h *ProjectHandler) DeletePromptTemplate(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(chi.URLParam(r, "id"))
	if err := h.client.PromptTemplate.DeleteOneID(id).Exec(r.Context()); err != nil {
		http.Error(w, `{"error":"failed to delete template"}`, http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Models ---

type ModelRequest struct {
	Provider       string `json:"provider"`
	Model          string `json:"model"`
	SupportsImage  bool   `json:"supports_image"`
	SupportsResume bool   `json:"supports_resume"`
	ConfigJSON     string `json:"config_json"`
}

func (h *ProjectHandler) ListModels(w http.ResponseWriter, r *http.Request) {
	models, err := h.client.AgentProfile.Query().All(r.Context())
	if err != nil {
		http.Error(w, `{"error":"failed to list models"}`, http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, models)
}

func (h *ProjectHandler) CreateModel(w http.ResponseWriter, r *http.Request) {
	var req ModelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}
	if req.ConfigJSON == "" {
		req.ConfigJSON = "{}"
	}
	m, err := h.client.AgentProfile.Create().
		SetProvider(req.Provider).SetModel(req.Model).
		SetSupportsImage(req.SupportsImage).SetSupportsResume(req.SupportsResume).
		SetConfigJSON(req.ConfigJSON).Save(r.Context())
	if err != nil {
		http.Error(w, `{"error":"failed to create model"}`, http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusCreated, m)
}

func (h *ProjectHandler) UpdateModel(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(chi.URLParam(r, "id"))
	var req ModelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}
	m, err := h.client.AgentProfile.UpdateOneID(id).
		SetProvider(req.Provider).SetModel(req.Model).
		SetSupportsImage(req.SupportsImage).SetSupportsResume(req.SupportsResume).
		SetConfigJSON(req.ConfigJSON).Save(r.Context())
	if err != nil {
		http.Error(w, `{"error":"failed to update model"}`, http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, m)
}

func (h *ProjectHandler) DeleteModel(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(chi.URLParam(r, "id"))
	if err := h.client.AgentProfile.DeleteOneID(id).Exec(r.Context()); err != nil {
		http.Error(w, `{"error":"failed to delete model"}`, http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- helpers ---

func parseRepoURL(url string) model.RepoRef {
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

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}
