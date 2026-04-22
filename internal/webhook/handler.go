package webhook

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/cloverstd/ccmate/internal/ent"
	"github.com/cloverstd/ccmate/internal/ent/project"
	"github.com/cloverstd/ccmate/internal/ent/projectlabelrule"
	enttask "github.com/cloverstd/ccmate/internal/ent/task"
	"github.com/cloverstd/ccmate/internal/ent/webhookreceipt"
	"github.com/cloverstd/ccmate/internal/gitprovider"
	"github.com/cloverstd/ccmate/internal/model"
	"github.com/cloverstd/ccmate/internal/settings"
)

// Processor handles normalized webhook events.
type Processor struct {
	client  *ent.Client
	gitProv gitprovider.GitProvider
	settingsMgr *settings.Manager
}

func NewProcessor(client *ent.Client, gitProv gitprovider.GitProvider, settingsMgr *settings.Manager) *Processor {
	return &Processor{client: client, gitProv: gitProv, settingsMgr: settingsMgr}
}

// ProcessEvent handles a normalized event from any git provider.
func (p *Processor) ProcessEvent(ctx context.Context, event *model.NormalizedEvent) error {
	exists, err := p.client.WebhookReceipt.Query().
		Where(webhookreceipt.DeliveryID(event.DeliveryID), webhookreceipt.Provider("github")).
		Exist(ctx)
	if err != nil {
		return fmt.Errorf("checking dedup: %w", err)
	}
	if exists {
		slog.Info("duplicate webhook delivery, ignoring", "delivery_id", event.DeliveryID)
		return nil
	}

	_, err = p.client.WebhookReceipt.Create().
		SetProvider("github").SetDeliveryID(event.DeliveryID).
		SetEventType(string(event.Type)).SetAccepted(true).
		Save(ctx)
	if err != nil {
		return fmt.Errorf("recording receipt: %w", err)
	}

	switch event.Type {
	case model.EventIssueLabeled:
		return p.handleIssueLabeled(ctx, event)
	case model.EventIssueCommentCreated, model.EventPRCommentCreated:
		return p.handleComment(ctx, event)
	case model.EventPRReviewSubmitted:
		return p.handlePRReview(ctx, event)
	case model.EventPRSynchronize:
		return p.handlePRSynchronize(ctx, event)
	default:
		slog.Info("unhandled event type", "type", event.Type)
		return nil
	}
}

func (p *Processor) handleIssueLabeled(ctx context.Context, event *model.NormalizedEvent) error {
	repoURL := fmt.Sprintf("https://github.com/%s", event.Repo.FullName())
	proj, err := p.client.Project.Query().
		Where(project.RepoURL(repoURL)).WithLabelRules().Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("finding project: %w", err)
	}

	// Check project-specific label rules first
	triggerMode := ""
	rule, err := p.client.ProjectLabelRule.Query().
		Where(
			projectlabelrule.HasProjectWith(project.ID(proj.ID)),
			projectlabelrule.IssueLabel(event.Label),
		).Only(ctx)
	if err == nil {
		triggerMode = string(rule.TriggerMode)
	} else if ent.IsNotFound(err) {
		// Fall back to global label rules from settings
		globalRules := p.settingsMgr.GetLabelRules(ctx)
		for _, gr := range globalRules {
			if gr.Label == event.Label {
				triggerMode = gr.TriggerMode
				break
			}
		}
	} else {
		return fmt.Errorf("finding label rule: %w", err)
	}

	if triggerMode == "" {
		return nil // no matching rule
	}

	if triggerMode == "manual" && !proj.AutoMode {
		return nil
	}

	activeExists, _ := p.client.Task.Query().
		Where(
			enttask.HasProjectWith(project.ID(proj.ID)),
			enttask.IssueNumber(event.IssueNumber),
			enttask.StatusIn(enttask.StatusQueued, enttask.StatusRunning, enttask.StatusPaused, enttask.StatusWaitingUser),
		).Exist(ctx)
	if activeExists {
		return nil
	}

	builder := p.client.Task.Create().
		SetProject(proj).SetIssueNumber(event.IssueNumber).
		SetType(enttask.TypeIssueImplementation).SetStatus(enttask.StatusQueued).
		SetTriggerSource(string(model.TriggerSourceWebhook))
	if agentProfileID := p.resolveAutoAgentProfileID(ctx, proj); agentProfileID != nil {
		builder = builder.SetAgentProfileID(*agentProfileID)
	}
	_, err = builder.Save(ctx)
	if err != nil {
		return fmt.Errorf("creating task: %w", err)
	}

	slog.Info("task created from labeled issue", "project", proj.Name, "issue", event.IssueNumber, "label", event.Label)
	return nil
}

func (p *Processor) handleComment(ctx context.Context, event *model.NormalizedEvent) error {
	if !strings.HasPrefix(strings.TrimSpace(event.CommentBody), "/ccmate") {
		return nil
	}
	return ParseAndExecuteCommand(ctx, p.client, event, p.gitProv, p.settingsMgr)
}

func (p *Processor) handlePRReview(ctx context.Context, event *model.NormalizedEvent) error {
	if event.ReviewState != "changes_requested" {
		return nil
	}

	repoURL := fmt.Sprintf("https://github.com/%s", event.Repo.FullName())
	proj, err := p.client.Project.Query().Where(project.RepoURL(repoURL)).Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("finding project: %w", err)
	}

	existingTask, err := p.client.Task.Query().
		Where(enttask.HasProjectWith(project.ID(proj.ID)), enttask.PrNumberNotNil()).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("finding task: %w", err)
	}

	activeExists, _ := p.client.Task.Query().
		Where(
			enttask.HasProjectWith(project.ID(proj.ID)),
			enttask.IssueNumber(existingTask.IssueNumber),
			enttask.TypeEQ(enttask.TypeReviewFix),
			enttask.StatusIn(enttask.StatusQueued, enttask.StatusRunning, enttask.StatusPaused, enttask.StatusWaitingUser),
		).Exist(ctx)
	if activeExists {
		return nil
	}

	prNum := event.PRNumber
	builder := p.client.Task.Create().
		SetProject(proj).SetIssueNumber(existingTask.IssueNumber).
		SetNillablePrNumber(&prNum).
		SetType(enttask.TypeReviewFix).SetStatus(enttask.StatusQueued).
		SetTriggerSource(string(model.TriggerSourceWebhook))
	if agentProfileID := p.resolveAutoAgentProfileID(ctx, proj); agentProfileID != nil {
		builder = builder.SetAgentProfileID(*agentProfileID)
	}
	_, err = builder.Save(ctx)
	if err != nil {
		return fmt.Errorf("creating review fix task: %w", err)
	}

	slog.Info("review fix task created", "pr", event.PRNumber)
	return nil
}

// handlePRSynchronize auto-enqueues a review task when a ccmate-managed PR
// receives new commits. Only fires if the project has a review agent configured
// and the iteration cap has not been reached. Acts as a fallback to the
// runner's own enqueue (which covers pushes we initiate).
func (p *Processor) handlePRSynchronize(ctx context.Context, event *model.NormalizedEvent) error {
	repoURL := fmt.Sprintf("https://github.com/%s", event.Repo.FullName())
	proj, err := p.client.Project.Query().Where(project.RepoURL(repoURL)).Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("finding project: %w", err)
	}
	if proj.ReviewAgentProfileID == nil {
		return nil
	}

	// Only react to PRs ccmate manages (there's at least one task linked to this PR).
	linkedTask, err := p.client.Task.Query().
		Where(
			enttask.HasProjectWith(project.ID(proj.ID)),
			enttask.PrNumber(event.PRNumber),
		).
		Order(ent.Desc(enttask.FieldID)).
		First(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("finding linked task: %w", err)
	}

	// Dedup: skip if an active review task already exists for this PR.
	activeExists, err := p.client.Task.Query().
		Where(
			enttask.HasProjectWith(project.ID(proj.ID)),
			enttask.IssueNumber(linkedTask.IssueNumber),
			enttask.TypeEQ(enttask.TypeReview),
			enttask.StatusIn(enttask.StatusQueued, enttask.StatusRunning, enttask.StatusPaused),
		).Exist(ctx)
	if err != nil {
		return fmt.Errorf("checking active review task: %w", err)
	}
	if activeExists {
		return nil
	}

	// Determine next iteration; enforce cap.
	maxIter := 3
	if p.settingsMgr != nil {
		if raw := p.settingsMgr.GetWithDefault(ctx, settings.KeyMaxReviewIterations, "3"); raw != "" {
			if n, perr := strconv.Atoi(raw); perr == nil && n > 0 {
				maxIter = n
			}
		}
	}
	prior, err := p.client.Task.Query().
		Where(
			enttask.HasProjectWith(project.ID(proj.ID)),
			enttask.IssueNumber(linkedTask.IssueNumber),
			enttask.TypeIn(enttask.TypeReview, enttask.TypeReviewFix),
		).
		Order(ent.Desc(enttask.FieldReviewIteration)).
		Limit(1).
		All(ctx)
	if err != nil {
		return fmt.Errorf("finding prior review iterations: %w", err)
	}
	nextIter := 1
	if len(prior) > 0 {
		nextIter = prior[0].ReviewIteration + 1
	}
	if nextIter > maxIter {
		slog.Info("review iteration cap reached", "pr", event.PRNumber, "max", maxIter)
		return nil
	}

	prNum := event.PRNumber
	_, err = p.client.Task.Create().
		SetProject(proj).SetIssueNumber(linkedTask.IssueNumber).
		SetNillablePrNumber(&prNum).
		SetType(enttask.TypeReview).SetStatus(enttask.StatusQueued).
		SetReviewIteration(nextIter).
		SetAgentProfileID(*proj.ReviewAgentProfileID).
		SetTriggerSource(string(model.TriggerSourceWebhook)).
		Save(ctx)
	if err != nil {
		return fmt.Errorf("creating review task: %w", err)
	}
	slog.Info("review task created from PR synchronize", "pr", prNum, "iteration", nextIter)
	return nil
}

func (p *Processor) resolveAutoAgentProfileID(ctx context.Context, proj *ent.Project) *int {
	if proj.DefaultAgentProfileID != nil {
		return proj.DefaultAgentProfileID
	}
	if p.settingsMgr == nil {
		return nil
	}
	if fallback := p.settingsMgr.GetOptionalInt(ctx, settings.KeyDefaultAgentProfileID); fallback != nil {
		if _, err := p.client.AgentProfile.Get(ctx, *fallback); err == nil {
			return fallback
		}
	}
	return nil
}
