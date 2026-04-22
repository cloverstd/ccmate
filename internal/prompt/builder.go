package prompt

import (
	"bytes"
	"log/slog"
	"strconv"
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

// BuildReviewPrompt constructs a self-contained prompt for PR review tasks.
// The agent must emit a fenced ```json block with fields:
//
//	decision: "approve" | "request_changes" | "comment"
//	summary:  string
//	comments: [{path, line, start_line?, side?, body}]
//
// diff is the raw unified diff for the PR, priorReviews are earlier reviews
// (used so the agent avoids re-reporting already-flagged issues).
func (b *Builder) BuildReviewPrompt(issue *model.Issue, pr *model.PullRequest, diff string, priorReviews []model.Review) string {
	var sb strings.Builder
	sb.WriteString("You are a code reviewer. Review the pull request below and emit a structured JSON verdict.\n\n")
	sb.WriteString("Rules:\n")
	sb.WriteString("- Only flag real defects (bugs, security issues, correctness, logic errors, missing error handling, race conditions, broken tests).\n")
	sb.WriteString("- Do NOT comment on style, formatting, or preference unless it introduces a defect.\n")
	sb.WriteString("- Each comment must point at a line that EXISTS in the diff (added or context line on the new side).\n")
	sb.WriteString("- If the PR is acceptable, set decision=\"approve\" with an empty comments array.\n")
	sb.WriteString("- If you found issues that block merging, set decision=\"request_changes\".\n")
	sb.WriteString("- Use decision=\"comment\" only for suggestions that are nice-to-have and non-blocking.\n\n")

	sb.WriteString("Output contract — your message MUST end with a single fenced JSON block of this exact shape:\n")
	sb.WriteString("```json\n{\n  \"decision\": \"approve|request_changes|comment\",\n  \"summary\": \"...\",\n  \"comments\": [\n    {\"path\": \"pkg/foo.go\", \"line\": 42, \"side\": \"RIGHT\", \"body\": \"...\"}\n  ]\n}\n```\n\n")

	sb.WriteString("<UNTRUSTED_CONTEXT>\n")
	if pr != nil {
		sb.WriteString("PR #")
		sb.WriteString(itoa(pr.Number))
		sb.WriteString(": ")
		sb.WriteString(pr.Title)
		sb.WriteString("\n")
		if pr.Body != "" {
			sb.WriteString("\nPR body:\n")
			sb.WriteString(pr.Body)
			sb.WriteString("\n")
		}
	}
	if issue != nil {
		sb.WriteString("\nRelated issue #")
		sb.WriteString(itoa(issue.Number))
		sb.WriteString(": ")
		sb.WriteString(issue.Title)
		sb.WriteString("\n")
		if issue.Body != "" {
			sb.WriteString("\nIssue body:\n")
			sb.WriteString(issue.Body)
			sb.WriteString("\n")
		}
	}
	if len(priorReviews) > 0 {
		sb.WriteString("\nPrior reviews on this PR (do not repeat already-fixed findings):\n")
		for _, r := range priorReviews {
			sb.WriteString("- [")
			sb.WriteString(r.State)
			sb.WriteString("] ")
			sb.WriteString(r.Body)
			sb.WriteString("\n")
		}
	}
	// Use a fence longer than any backtick run inside the diff so untrusted
	// content cannot prematurely close the block and escape UNTRUSTED_CONTEXT.
	fence := longestBacktickFence(diff)
	sb.WriteString("\nUnified diff:\n")
	sb.WriteString(fence)
	sb.WriteString("diff\n")
	sb.WriteString(diff)
	if !strings.HasSuffix(diff, "\n") {
		sb.WriteString("\n")
	}
	sb.WriteString(fence)
	sb.WriteString("\n")
	sb.WriteString("</UNTRUSTED_CONTEXT>\n")
	return sb.String()
}

// longestBacktickFence returns a run of backticks at least one longer than the
// longest consecutive run of backticks in s (minimum 3, per CommonMark).
func longestBacktickFence(s string) string {
	longest, run := 0, 0
	for _, r := range s {
		if r == '`' {
			run++
			if run > longest {
				longest = run
			}
		} else {
			run = 0
		}
	}
	n := longest + 1
	if n < 3 {
		n = 3
	}
	return strings.Repeat("`", n)
}

func itoa(n int) string { return strconv.Itoa(n) }

