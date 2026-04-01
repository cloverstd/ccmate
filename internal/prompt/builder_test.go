package prompt

import (
	"strings"
	"testing"

	"github.com/cloverstd/ccmate/internal/model"
)

func TestBuildSystemPrompt(t *testing.T) {
	b := NewBuilder()
	prompt := b.BuildSystemPrompt()

	// Must contain platform safety rules
	if !strings.Contains(prompt, "Platform Rules") {
		t.Error("system prompt missing platform rules")
	}
	if !strings.Contains(prompt, "ONLY work within the current task working directory") {
		t.Error("system prompt missing workspace boundary rule")
	}
	if !strings.Contains(prompt, "must NOT read, print, upload") {
		t.Error("system prompt missing secret protection rule")
	}
}

func TestBuildSystemPromptWithProject(t *testing.T) {
	b := NewBuilder().WithProjectPrompts("Use Go 1.21+", "Follow TDD")
	prompt := b.BuildSystemPrompt()

	if !strings.Contains(prompt, "Use Go 1.21+") {
		t.Error("system prompt missing project system prompt")
	}
}

func TestBuildTaskPrompt(t *testing.T) {
	b := NewBuilder()

	issue := &model.Issue{
		Title:  "Add user authentication",
		Body:   "We need to add login functionality",
		Labels: []string{"feature", "auth"},
	}

	comments := []model.Comment{
		{User: "alice", Body: "Please use OAuth2"},
		{User: "bot", Body: "/ccmate run"},
	}

	prompt := b.BuildTaskPrompt(issue, comments, "issue_implementation")

	// Must contain UNTRUSTED_CONTEXT wrapping
	if !strings.Contains(prompt, "<UNTRUSTED_CONTEXT") {
		t.Error("task prompt missing UNTRUSTED_CONTEXT wrapper")
	}
	if !strings.Contains(prompt, "</UNTRUSTED_CONTEXT>") {
		t.Error("task prompt missing UNTRUSTED_CONTEXT closing tag")
	}

	// Must contain issue data
	if !strings.Contains(prompt, "Add user authentication") {
		t.Error("task prompt missing issue title")
	}

	// Must contain non-command comments
	if !strings.Contains(prompt, "Please use OAuth2") {
		t.Error("task prompt missing regular comment")
	}

	// Must NOT include ccmate command comments
	if strings.Contains(prompt, "/ccmate run") {
		t.Error("task prompt should not include command comments")
	}
}

func TestBuildReviewFixPrompt(t *testing.T) {
	b := NewBuilder()

	issue := &model.Issue{Title: "Fix bug", Body: "Something is broken"}
	reviews := []model.Review{
		{User: "reviewer", State: "changes_requested", Body: "Please fix the null check"},
	}

	prompt := b.BuildReviewFixPrompt(issue, reviews, "diff content here")

	if !strings.Contains(prompt, "Address PR review feedback") {
		t.Error("review fix prompt missing task description")
	}
	if !strings.Contains(prompt, "Please fix the null check") {
		t.Error("review fix prompt missing review content")
	}
	if !strings.Contains(prompt, "diff content here") {
		t.Error("review fix prompt missing diff")
	}
}
