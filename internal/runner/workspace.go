package runner

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/cloverstd/ccmate/internal/model"
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

// PrepareClean wipes any existing repo contents (leftover from a prior failed
// or cancelled run) and recreates an empty directory. Use this before `git
// clone`, which refuses to write into a non-empty destination.
func (w *Workspace) PrepareClean() error {
	if err := os.RemoveAll(w.RepoPath); err != nil {
		return fmt.Errorf("cleaning repo path: %w", err)
	}
	return os.MkdirAll(w.RepoPath, 0755)
}

// BranchName returns the standard branch name for this task.
func (w *Workspace) BranchName(issueNumber int) string {
	return model.TaskBranchName(issueNumber, w.TaskID)
}

// GitInit initializes git in the workspace (for cases where we don't clone).
func (w *Workspace) GitInit(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "git", "init")
	cmd.Dir = w.RepoPath
	return cmd.Run()
}

// SetGitIdentity writes user.name/user.email to the repo-local git config so
// commits (including those made by the agent subprocess) use the configured
// identity instead of the host machine's global gitconfig.
func (w *Workspace) SetGitIdentity(ctx context.Context, name, email string) error {
	if name == "" || email == "" {
		return nil
	}
	for _, kv := range [][2]string{{"user.name", name}, {"user.email", email}} {
		cmd := exec.CommandContext(ctx, "git", "config", "--local", kv[0], kv[1])
		cmd.Dir = w.RepoPath
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("git config %s: %w: %s", kv[0], err, string(output))
		}
	}
	return nil
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
