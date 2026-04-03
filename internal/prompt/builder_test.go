package prompt

import (
	"strings"
	"testing"

	"github.com/cloverstd/ccmate/internal/model"
)

func TestBuildSystemPrompt(t *testing.T) {
	b := NewBuilder()
	prompt := b.BuildSystemPrompt()

	if prompt != "" {
		t.Error("system prompt should be empty when no system prompt is set")
	}
}

func TestBuildSystemPromptWithProject(t *testing.T) {
	b := NewBuilder().WithSystemPrompt("Use Go 1.21+")
	prompt := b.BuildSystemPrompt()

	if prompt != "Use Go 1.21+" {
		t.Errorf("system prompt should be exactly the configured value, got: %s", prompt)
	}
}

func TestBuildTaskPromptWithTemplate(t *testing.T) {
	b := NewBuilder().
		WithTaskPrompt("Focus on {{.RepoFullName}} issue #{{.IssueNumber}}").
		WithTemplateVars(TemplateVars{
			IssueNumber:  42,
			RepoOwner:    "acme",
			RepoName:     "app",
			RepoFullName: "acme/app",
		})

	issue := &model.Issue{
		Number: 42,
		Title:  "Test issue",
		Body:   "Body",
	}

	prompt := b.BuildTaskPrompt(issue, nil, "issue_implementation")

	if !strings.Contains(prompt, "Focus on acme/app issue #42") {
		t.Error("task prompt template variables not rendered")
	}
}

func TestBuildTaskPromptEmpty(t *testing.T) {
	b := NewBuilder()

	issue := &model.Issue{Number: 1, Title: "Test", Body: "Body"}
	prompt := b.BuildTaskPrompt(issue, nil, "issue_implementation")

	if prompt != "" {
		t.Errorf("task prompt should be empty without template, got: %s", prompt)
	}
}

func TestBuildTaskPromptOnlyTemplate(t *testing.T) {
	b := NewBuilder().
		WithTaskPrompt("Fix issue #{{.IssueNumber}}").
		WithTemplateVars(TemplateVars{IssueNumber: 5})

	issue := &model.Issue{Number: 5, Title: "Bug", Body: "Details"}
	comments := []model.Comment{{User: "alice", Body: "info"}}
	prompt := b.BuildTaskPrompt(issue, comments, "issue_implementation")

	if prompt != "Fix issue #5" {
		t.Errorf("expected only rendered template, got: %s", prompt)
	}
	// Should NOT contain issue/comment data
	if strings.Contains(prompt, "Bug") || strings.Contains(prompt, "info") {
		t.Error("task prompt should not contain issue/comment data")
	}
}

func TestBuildReviewFixPromptOnlyTemplate(t *testing.T) {
	b := NewBuilder().
		WithTaskPrompt("Review task").
		WithTemplateVars(TemplateVars{})

	issue := &model.Issue{Number: 1, Title: "Fix bug", Body: "Broken"}
	reviews := []model.Review{{User: "r", State: "changes_requested", Body: "fix null"}}
	prompt := b.BuildReviewFixPrompt(issue, reviews, "diff here")

	if prompt != "Review task" {
		t.Errorf("expected only rendered template, got: %s", prompt)
	}
}
