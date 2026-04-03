package webhook

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/cloverstd/ccmate/internal/ent"
	"github.com/cloverstd/ccmate/internal/ent/commandaudit"
	"github.com/cloverstd/ccmate/internal/ent/project"
	enttask "github.com/cloverstd/ccmate/internal/ent/task"
	"github.com/cloverstd/ccmate/internal/gitprovider"
	"github.com/cloverstd/ccmate/internal/model"
	"github.com/cloverstd/ccmate/internal/settings"
)

// Command represents a parsed /ccmate command.
type Command struct {
	Name string
	Args []string
}

// ParseCommand extracts a ccmate command from a comment body.
func ParseCommand(body string) (*Command, error) {
	body = strings.TrimSpace(body)
	if !strings.HasPrefix(body, "/ccmate") {
		return nil, fmt.Errorf("not a ccmate command")
	}

	parts := strings.Fields(body)
	if len(parts) < 2 {
		return nil, fmt.Errorf("incomplete command")
	}

	cmd := &Command{Name: parts[1]}
	if len(parts) > 2 {
		cmd.Args = parts[2:]
	}

	switch cmd.Name {
	case "run", "pause", "resume", "retry", "status", "fix-review":
		return cmd, nil
	default:
		return nil, fmt.Errorf("unknown command: %s", cmd.Name)
	}
}

// ParseAndExecuteCommand parses, authorizes, executes a command, and writes back results.
func ParseAndExecuteCommand(ctx context.Context, client *ent.Client, event *model.NormalizedEvent, gitProv gitprovider.GitProvider, settingsMgr *settings.Manager) error {
	cmd, err := ParseCommand(event.CommentBody)
	if err != nil {
		slog.Info("ignoring invalid command", "body", event.CommentBody, "error", err)
		return nil
	}

	repoURL := fmt.Sprintf("https://github.com/%s", event.Repo.FullName())
	proj, err := client.Project.Query().Where(project.RepoURL(repoURL)).Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("finding project: %w", err)
	}

	// Check authorization
	decision := commandaudit.DecisionAllowed
	reason := ""
	if gitProv != nil {
		authorized, err := gitProv.IsAuthorizedCommenter(ctx, event.Repo, event.CommentUser)
		if err != nil || !authorized {
			decision = commandaudit.DecisionDenied
			reason = "user not authorized"
		}
	}

	issueNumber := event.IssueNumber
	if issueNumber == 0 {
		issueNumber = event.PRNumber
	}

	task, _ := client.Task.Query().
		Where(
			enttask.HasProjectWith(project.ID(proj.ID)),
			enttask.IssueNumber(issueNumber),
			enttask.StatusIn(
				enttask.StatusQueued, enttask.StatusRunning, enttask.StatusPaused,
				enttask.StatusWaitingUser, enttask.StatusFailed,
			),
		).Only(ctx)

	// Record audit
	auditBuilder := client.CommandAudit.Create().
		SetSource("comment").SetActor(event.CommentUser).
		SetCommand(fmt.Sprintf("/ccmate %s", cmd.Name)).
		SetDecision(decision).SetReason(reason)
	if task != nil {
		auditBuilder = auditBuilder.SetTaskID(task.ID)
	}
	_, _ = auditBuilder.Save(ctx)

	if decision == commandaudit.DecisionDenied {
		slog.Info("command denied", "command", cmd.Name, "user", event.CommentUser)
		writeBack(ctx, gitProv, event.Repo, issueNumber,
			fmt.Sprintf("Command `/ccmate %s` denied for @%s: %s", cmd.Name, event.CommentUser, reason))
		return nil
	}

	// Execute command
	var resultMsg string
	switch cmd.Name {
	case "run":
		if task != nil {
			resultMsg = "A task is already active for this issue."
		} else {
			builder := client.Task.Create().
				SetProject(proj).SetIssueNumber(issueNumber).
				SetType(enttask.TypeIssueImplementation).
				SetStatus(enttask.StatusQueued).
				SetTriggerSource(string(model.TriggerSourceCommand))
			if agentProfileID := resolveProjectAgentProfileID(ctx, client, settingsMgr, proj); agentProfileID != nil {
				builder = builder.SetAgentProfileID(*agentProfileID)
			}
			_, err := builder.Save(ctx)
			if err != nil {
				resultMsg = fmt.Sprintf("Failed to create task: %v", err)
			} else {
				resultMsg = "Task created and queued."
			}
		}

	case "pause":
		if task == nil || task.Status != enttask.StatusRunning {
			resultMsg = "No running task to pause."
		} else {
			_, err := client.Task.UpdateOneID(task.ID).SetStatus(enttask.StatusPaused).Save(ctx)
			if err != nil {
				resultMsg = fmt.Sprintf("Failed to pause: %v", err)
			} else {
				resultMsg = "Task paused."
			}
		}

	case "resume":
		if task == nil || task.Status != enttask.StatusPaused {
			resultMsg = "No paused task to resume."
		} else {
			_, err := client.Task.UpdateOneID(task.ID).SetStatus(enttask.StatusQueued).Save(ctx)
			if err != nil {
				resultMsg = fmt.Sprintf("Failed to resume: %v", err)
			} else {
				resultMsg = "Task resumed and re-queued."
			}
		}

	case "retry":
		if task == nil || task.Status != enttask.StatusFailed {
			resultMsg = "No failed task to retry."
		} else {
			_, err := client.Task.UpdateOneID(task.ID).SetStatus(enttask.StatusQueued).Save(ctx)
			if err != nil {
				resultMsg = fmt.Sprintf("Failed to retry: %v", err)
			} else {
				resultMsg = "Task retried and re-queued."
			}
		}

	case "status":
		if task == nil {
			resultMsg = "No active task for this issue."
		} else {
			resultMsg = fmt.Sprintf("Task #%d: status=%s, type=%s", task.ID, task.Status, task.Type)
		}

	case "fix-review":
		prNum := event.PRNumber
		if prNum == 0 {
			resultMsg = "This command is only valid on PR comments."
		} else {
			builder := client.Task.Create().
				SetProject(proj).SetIssueNumber(issueNumber).
				SetNillablePrNumber(&prNum).
				SetType(enttask.TypeReviewFix).
				SetStatus(enttask.StatusQueued).
				SetTriggerSource(string(model.TriggerSourceCommand))
			if agentProfileID := resolveProjectAgentProfileID(ctx, client, settingsMgr, proj); agentProfileID != nil {
				builder = builder.SetAgentProfileID(*agentProfileID)
			}
			_, err := builder.Save(ctx)
			if err != nil {
				resultMsg = fmt.Sprintf("Failed to create review fix task: %v", err)
			} else {
				resultMsg = "Review fix task created and queued."
			}
		}
	}

	slog.Info("command executed", "command", cmd.Name, "user", event.CommentUser, "result", resultMsg)
	writeBack(ctx, gitProv, event.Repo, issueNumber, fmt.Sprintf("`/ccmate %s`: %s", cmd.Name, resultMsg))
	return nil
}

func resolveProjectAgentProfileID(ctx context.Context, client *ent.Client, settingsMgr *settings.Manager, proj *ent.Project) *int {
	if proj.DefaultAgentProfileID != nil {
		return proj.DefaultAgentProfileID
	}
	if settingsMgr == nil {
		return nil
	}
	if fallback := settingsMgr.GetOptionalInt(ctx, settings.KeyDefaultAgentProfileID); fallback != nil {
		if _, err := client.AgentProfile.Get(ctx, *fallback); err == nil {
			return fallback
		}
	}
	return nil
}

func writeBack(ctx context.Context, gitProv gitprovider.GitProvider, repo model.RepoRef, issueNumber int, msg string) {
	if gitProv == nil || issueNumber == 0 {
		return
	}
	_ = gitProv.CreateIssueComment(ctx, repo, issueNumber, msg)
}
