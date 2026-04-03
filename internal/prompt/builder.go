package prompt

import (
	"bytes"
	"log/slog"
	"strings"
	"text/template"

	"github.com/cloverstd/ccmate/internal/model"
)

// TemplateVars holds the variables available in task_prompt templates.
type TemplateVars struct {
	// Issue fields
	IssueNumber int
	IssueTitle  string
	IssueBody   string
	IssueLabels []string
	IssueUser   string
	IssueLink   string
	// Repo fields
	RepoOwner string
	RepoName  string
	RepoFullName string
	// Task fields
	TaskType   string
	BranchName string
}

// Builder constructs prompts with proper layering and UNTRUSTED_CONTEXT wrapping.
type Builder struct {
	projectSystemPrompt string
	taskPromptTemplate  string
	templateVars        TemplateVars
}

func NewBuilder() *Builder {
	return &Builder{}
}

// WithSystemPrompt sets the project-level system prompt.
func (b *Builder) WithSystemPrompt(systemPrompt string) *Builder {
	b.projectSystemPrompt = systemPrompt
	return b
}

// WithTaskPrompt sets the project-level task prompt template (Go template syntax).
func (b *Builder) WithTaskPrompt(taskPrompt string) *Builder {
	b.taskPromptTemplate = taskPrompt
	return b
}

// WithTemplateVars sets the variables available for task_prompt template rendering.
func (b *Builder) WithTemplateVars(vars TemplateVars) *Builder {
	b.templateVars = vars
	return b
}

// BuildSystemPrompt returns the configured system prompt.
func (b *Builder) BuildSystemPrompt() string {
	return b.projectSystemPrompt
}

// templateFuncs provides helper functions available in task_prompt templates.
var templateFuncs = template.FuncMap{
	"has": func(slice []string, item string) bool {
		for _, s := range slice {
			if s == item {
				return true
			}
		}
		return false
	},
	"join": strings.Join,
}

// renderTaskPromptTemplate renders the task_prompt template with variables.
func (b *Builder) renderTaskPromptTemplate() string {
	if b.taskPromptTemplate == "" {
		return ""
	}
	tmpl, err := template.New("task_prompt").Funcs(templateFuncs).Parse(b.taskPromptTemplate)
	if err != nil {
		slog.Warn("failed to parse task_prompt template, using raw text", "error", err)
		return b.taskPromptTemplate
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, b.templateVars); err != nil {
		slog.Warn("failed to execute task_prompt template, using raw text", "error", err)
		return b.taskPromptTemplate
	}
	return buf.String()
}

// BuildTaskPrompt returns the rendered task prompt template only.
func (b *Builder) BuildTaskPrompt(issue *model.Issue, comments []model.Comment, taskType string) string {
	return b.renderTaskPromptTemplate()
}

// BuildReviewFixPrompt returns the rendered task prompt template only.
func (b *Builder) BuildReviewFixPrompt(issue *model.Issue, reviews []model.Review, diff string) string {
	return b.renderTaskPromptTemplate()
}

