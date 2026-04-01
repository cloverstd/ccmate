package handler

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/cloverstd/ccmate/internal/ent"
	"github.com/cloverstd/ccmate/internal/ent/project"
	"github.com/cloverstd/ccmate/internal/ent/projectlabelrule"
	"github.com/go-chi/chi/v5"
)

type ProjectHandler struct {
	client *ent.Client
}

func NewProjectHandler(client *ent.Client) *ProjectHandler {
	return &ProjectHandler{client: client}
}

type CreateProjectRequest struct {
	Name           string `json:"name"`
	RepoURL        string `json:"repo_url"`
	GitProvider    string `json:"git_provider"`
	DefaultBranch  string `json:"default_branch"`
	AutoMode       bool   `json:"auto_mode"`
	MaxConcurrency int    `json:"max_concurrency"`
}

func (h *ProjectHandler) List(w http.ResponseWriter, r *http.Request) {
	projects, err := h.client.Project.Query().
		Order(ent.Desc(project.FieldCreatedAt)).
		All(r.Context())
	if err != nil {
		http.Error(w, `{"error":"failed to list projects"}`, http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, projects)
}

func (h *ProjectHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(chi.URLParam(r, "id"))
	p, err := h.client.Project.Query().Where(project.ID(id)).WithLabelRules().Only(r.Context())
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
	if req.MaxConcurrency <= 0 {
		req.MaxConcurrency = 2
	}
	if req.DefaultBranch == "" {
		req.DefaultBranch = "main"
	}

	p, err := h.client.Project.Create().
		SetName(req.Name).SetRepoURL(req.RepoURL).SetGitProvider(req.GitProvider).
		SetDefaultBranch(req.DefaultBranch).SetAutoMode(req.AutoMode).
		SetMaxConcurrency(req.MaxConcurrency).
		Save(r.Context())
	if err != nil {
		http.Error(w, `{"error":"failed to create project"}`, http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusCreated, p)
}

func (h *ProjectHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(chi.URLParam(r, "id"))
	var req CreateProjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}
	p, err := h.client.Project.UpdateOneID(id).
		SetName(req.Name).SetRepoURL(req.RepoURL).SetGitProvider(req.GitProvider).
		SetDefaultBranch(req.DefaultBranch).SetAutoMode(req.AutoMode).
		SetMaxConcurrency(req.MaxConcurrency).
		Save(r.Context())
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

// --- Label Rules ---

type CreateLabelRuleRequest struct {
	IssueLabel       string `json:"issue_label"`
	TriggerMode      string `json:"trigger_mode"`
	PromptTemplateID *int   `json:"prompt_template_id"`
}

func (h *ProjectHandler) ListLabelRules(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(chi.URLParam(r, "id"))
	rules, err := h.client.ProjectLabelRule.Query().
		Where(projectlabelrule.HasProjectWith(project.ID(id))).
		All(r.Context())
	if err != nil {
		http.Error(w, `{"error":"failed to list rules"}`, http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, rules)
}

func (h *ProjectHandler) CreateLabelRule(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(chi.URLParam(r, "id"))
	var req CreateLabelRuleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}
	mode := projectlabelrule.TriggerModeAuto
	if req.TriggerMode == "manual" {
		mode = projectlabelrule.TriggerModeManual
	}
	builder := h.client.ProjectLabelRule.Create().
		SetIssueLabel(req.IssueLabel).
		SetTriggerMode(mode).
		SetProjectID(id)
	if req.PromptTemplateID != nil {
		builder = builder.SetPromptTemplateID(*req.PromptTemplateID)
	}
	rule, err := builder.Save(r.Context())
	if err != nil {
		http.Error(w, `{"error":"failed to create rule"}`, http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusCreated, rule)
}

func (h *ProjectHandler) DeleteLabelRule(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(chi.URLParam(r, "id"))
	if err := h.client.ProjectLabelRule.DeleteOneID(id).Exec(r.Context()); err != nil {
		http.Error(w, `{"error":"failed to delete rule"}`, http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Prompt Templates ---

type PromptTemplateRequest struct {
	Name         string `json:"name"`
	SystemPrompt string `json:"system_prompt"`
	TaskPrompt   string `json:"task_prompt"`
	IsBuiltin    bool   `json:"is_builtin"`
}

func (h *ProjectHandler) ListPromptTemplates(w http.ResponseWriter, r *http.Request) {
	templates, err := h.client.PromptTemplate.Query().All(r.Context())
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
	t, err := h.client.PromptTemplate.Create().
		SetName(req.Name).SetSystemPrompt(req.SystemPrompt).
		SetTaskPrompt(req.TaskPrompt).SetIsBuiltin(req.IsBuiltin).
		Save(r.Context())
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
		SetName(req.Name).SetSystemPrompt(req.SystemPrompt).
		SetTaskPrompt(req.TaskPrompt).
		Save(r.Context())
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
		SetConfigJSON(req.ConfigJSON).
		Save(r.Context())
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
		SetConfigJSON(req.ConfigJSON).
		Save(r.Context())
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

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}
