package runner

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// BranchInfo represents a local git branch with its latest commit.
type BranchInfo struct {
	Name    string `json:"name"`
	Hash    string `json:"hash"`
	Message string `json:"message"`
	Current bool   `json:"current"`
}

// CommitInfo represents a single commit.
type CommitInfo struct {
	Hash    string `json:"hash"`
	Message string `json:"message"`
	Author  string `json:"author"`
	Date    string `json:"date"`
}

// TagInfo represents a git tag.
type TagInfo struct {
	Name string `json:"name"`
	Hash string `json:"hash"`
}

// RepoDir returns the local clone path for a project.
func RepoDir(basePath string, projectID int) string {
	return filepath.Join(basePath, "repos", fmt.Sprintf("%d", projectID))
}

// CloneProject clones a repo to the local storage path.
func CloneProject(ctx context.Context, repoURL, destPath, token string) error {
	cloneURL := repoURL
	if token != "" && strings.Contains(repoURL, "github.com") {
		cloneURL = strings.Replace(repoURL, "https://", fmt.Sprintf("https://x-access-token:%s@", token), 1)
	}

	cmd := exec.CommandContext(ctx, "git", "clone", cloneURL, destPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("clone failed: %w: %s", err, string(output))
	}
	return nil
}

// FetchProject fetches latest from remote.
func FetchProject(ctx context.Context, repoPath string) error {
	cmd := exec.CommandContext(ctx, "git", "fetch", "--all", "--prune")
	cmd.Dir = repoPath
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("fetch failed: %w: %s", err, string(output))
	}
	return nil
}

// ListBranches returns all local and remote branches with latest commit info.
func ListBranches(ctx context.Context, repoPath string) ([]BranchInfo, error) {
	// Get current branch
	currentCmd := exec.CommandContext(ctx, "git", "branch", "--show-current")
	currentCmd.Dir = repoPath
	currentOut, _ := currentCmd.Output()
	currentBranch := strings.TrimSpace(string(currentOut))

	// List all branches (local + remote)
	cmd := exec.CommandContext(ctx, "git", "branch", "-a", "--format=%(refname:short)\t%(objectname:short)\t%(subject)")
	cmd.Dir = repoPath
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("list branches: %w: %s", err, string(output))
	}

	var branches []BranchInfo
	seen := map[string]bool{}
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) < 3 {
			continue
		}
		name := parts[0]
		// Skip HEAD pointer
		if strings.Contains(name, "HEAD") {
			continue
		}
		// Normalize remote branch names
		name = strings.TrimPrefix(name, "origin/")
		if seen[name] {
			continue
		}
		seen[name] = true

		branches = append(branches, BranchInfo{
			Name:    name,
			Hash:    parts[1],
			Message: parts[2],
			Current: name == currentBranch,
		})
	}
	return branches, nil
}

// ListCommits returns recent commits for a branch.
func ListCommits(ctx context.Context, repoPath, branch string, limit int) ([]CommitInfo, error) {
	if limit <= 0 {
		limit = 20
	}
	cmd := exec.CommandContext(ctx, "git", "log", branch,
		fmt.Sprintf("-n%d", limit),
		"--format=%H\t%s\t%an\t%ci")
	cmd.Dir = repoPath
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("list commits: %w: %s", err, string(output))
	}

	var commits []CommitInfo
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 4)
		if len(parts) < 4 {
			continue
		}
		commits = append(commits, CommitInfo{
			Hash:    parts[0][:12],
			Message: parts[1],
			Author:  parts[2],
			Date:    parts[3],
		})
	}
	return commits, nil
}

// ListTags returns all tags.
func ListTags(ctx context.Context, repoPath string) ([]TagInfo, error) {
	cmd := exec.CommandContext(ctx, "git", "tag", "-l", "--format=%(refname:short)\t%(objectname:short)")
	cmd.Dir = repoPath
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("list tags: %w: %s", err, string(output))
	}

	var tags []TagInfo
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) < 2 {
			continue
		}
		tags = append(tags, TagInfo{Name: parts[0], Hash: parts[1]})
	}
	return tags, nil
}
