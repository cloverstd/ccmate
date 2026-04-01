package runner

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// Workspace manages the working directory for a task.
type Workspace struct {
	BasePath  string
	ProjectID int
	TaskID    int
	RepoPath  string
}

// NewWorkspace creates a workspace for a task.
func NewWorkspace(basePath string, projectID, taskID int) *Workspace {
	repoPath := filepath.Join(basePath, fmt.Sprintf("%d", projectID), fmt.Sprintf("%d", taskID), "repo")
	return &Workspace{
		BasePath:  basePath,
		ProjectID: projectID,
		TaskID:    taskID,
		RepoPath:  repoPath,
	}
}

// Prepare creates the workspace directory.
func (w *Workspace) Prepare() error {
	return os.MkdirAll(w.RepoPath, 0755)
}

// BranchName returns the standard branch name for this task.
func (w *Workspace) BranchName(issueNumber int) string {
	return fmt.Sprintf("ccmate/issue-%d-task-%d", issueNumber, w.TaskID)
}

// GitInit initializes git in the workspace (for cases where we don't clone).
func (w *Workspace) GitInit(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "git", "init")
	cmd.Dir = w.RepoPath
	return cmd.Run()
}

// GitCheckoutBranch creates and checks out a new branch.
func (w *Workspace) GitCheckoutBranch(ctx context.Context, branchName string) error {
	cmd := exec.CommandContext(ctx, "git", "checkout", "-b", branchName)
	cmd.Dir = w.RepoPath
	output, err := cmd.CombinedOutput()
	if err != nil {
		// Branch might already exist, try switching
		cmd = exec.CommandContext(ctx, "git", "checkout", branchName)
		cmd.Dir = w.RepoPath
		output, err = cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("checkout branch: %w: %s", err, string(output))
		}
	}
	return nil
}

// GitAdd stages all changes.
func (w *Workspace) GitAdd(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "git", "add", "-A")
	cmd.Dir = w.RepoPath
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git add: %w: %s", err, string(output))
	}
	return nil
}

// GitCommit creates a commit with the given message.
func (w *Workspace) GitCommit(ctx context.Context, message string) error {
	cmd := exec.CommandContext(ctx, "git", "commit", "-m", message, "--allow-empty")
	cmd.Dir = w.RepoPath
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git commit: %w: %s", err, string(output))
	}
	return nil
}

// GitDiff returns the diff of staged and unstaged changes.
func (w *Workspace) GitDiff(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "diff", "HEAD")
	cmd.Dir = w.RepoPath
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git diff: %w: %s", err, string(output))
	}
	return string(output), nil
}

// Cleanup removes the workspace directory.
func (w *Workspace) Cleanup() error {
	return os.RemoveAll(filepath.Join(w.BasePath, fmt.Sprintf("%d", w.ProjectID), fmt.Sprintf("%d", w.TaskID)))
}
