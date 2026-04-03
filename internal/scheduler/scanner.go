package scheduler

import (
	"context"
	"log/slog"
	"time"

	"github.com/cloverstd/ccmate/internal/ent"
	"github.com/cloverstd/ccmate/internal/ent/project"
	enttask "github.com/cloverstd/ccmate/internal/ent/task"
	"github.com/cloverstd/ccmate/internal/gitprovider"
	"github.com/cloverstd/ccmate/internal/model"
	"github.com/cloverstd/ccmate/internal/settings"
)

// RunIssueScanner periodically scans auto-mode projects for issues matching label rules.
func RunIssueScanner(ctx context.Context, client *ent.Client, settingsMgr *settings.Manager, gitProv gitprovider.GitProvider) {
	if gitProv == nil {
		slog.Info("issue scanner disabled: no git provider configured")
		return
	}

	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	slog.Info("issue scanner started (60s interval)")

	for {
		select {
		case <-ctx.Done():
			slog.Info("issue scanner stopped")
			return
		case <-ticker.C:
			scanProjects(ctx, client, settingsMgr, gitProv)
		}
	}
}

func scanProjects(ctx context.Context, client *ent.Client, settingsMgr *settings.Manager, gitProv gitprovider.GitProvider) {
	projects, err := client.Project.Query().
		Where(project.AutoMode(true)).
		WithLabelRules().
		All(ctx)
	if err != nil {
		slog.Error("issue scanner: failed to query projects", "error", err)
		return
	}

	globalRules := settingsMgr.GetLabelRules(ctx)

	for _, proj := range projects {
		scanProject(ctx, client, settingsMgr, gitProv, proj, globalRules)
	}
}

func scanProject(ctx context.Context, client *ent.Client, settingsMgr *settings.Manager, gitProv gitprovider.GitProvider, proj *ent.Project, globalRules []settings.LabelRule) {
	// Collect auto-trigger labels from project rules + global rules
	autoLabels := make(map[string]bool)
	for _, rule := range proj.Edges.LabelRules {
		if rule.TriggerMode.String() == "auto" {
			autoLabels[rule.IssueLabel] = true
		}
	}
	for _, rule := range globalRules {
		if rule.TriggerMode == "auto" {
			autoLabels[rule.Label] = true
		}
	}
	if len(autoLabels) == 0 {
		return
	}

	repo := parseRepoURL(proj.RepoURL)
	if repo.Owner == "" {
		return
	}

	issues, err := gitProv.ListRepoIssues(ctx, repo)
	if err != nil {
		slog.Warn("issue scanner: failed to list issues", "project", proj.Name, "error", err)
		return
	}

	for _, issue := range issues {
		if issue.State != "open" {
			continue
		}
		matched := false
		for _, label := range issue.Labels {
			if autoLabels[label] {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}

		// Check no existing task for this issue (any status)
		taskExists, _ := client.Task.Query().
			Where(
				enttask.HasProjectWith(project.ID(proj.ID)),
				enttask.IssueNumber(issue.Number),
			).Exist(ctx)
		if taskExists {
			continue
		}

		// Create task
		builder := client.Task.Create().
			SetProject(proj).SetIssueNumber(issue.Number).
			SetType(enttask.TypeIssueImplementation).SetStatus(enttask.StatusQueued).
			SetTriggerSource("scanner")
		if agentProfileID := resolveAutoAgentProfileID(ctx, client, settingsMgr, proj); agentProfileID != nil {
			builder = builder.SetAgentProfileID(*agentProfileID)
		}
		if _, err := builder.Save(ctx); err != nil {
			slog.Error("issue scanner: failed to create task", "project", proj.Name, "issue", issue.Number, "error", err)
			continue
		}
		slog.Info("issue scanner: task created", "project", proj.Name, "issue", issue.Number)
	}
}

func resolveAutoAgentProfileID(ctx context.Context, client *ent.Client, settingsMgr *settings.Manager, proj *ent.Project) *int {
	if proj.DefaultAgentProfileID != nil {
		return proj.DefaultAgentProfileID
	}
	if fallback := settingsMgr.GetOptionalInt(ctx, settings.KeyDefaultAgentProfileID); fallback != nil {
		if _, err := client.AgentProfile.Get(ctx, *fallback); err == nil {
			return fallback
		}
	}
	return nil
}

func parseRepoURL(repoURL string) model.RepoRef {
	for _, sep := range []string{"github.com/", "gitlab.com/", "gitee.com/"} {
		idx := 0
		for i := 0; i <= len(repoURL)-len(sep); i++ {
			if repoURL[i:i+len(sep)] == sep {
				idx = i + len(sep)
				break
			}
		}
		if idx > 0 {
			rest := repoURL[idx:]
			if len(rest) > 4 && rest[len(rest)-4:] == ".git" {
				rest = rest[:len(rest)-4]
			}
			for j := 0; j < len(rest); j++ {
				if rest[j] == '/' {
					return model.RepoRef{Owner: rest[:j], Name: rest[j+1:]}
				}
			}
		}
	}
	return model.RepoRef{}
}
