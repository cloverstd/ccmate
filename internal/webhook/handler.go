package webhook

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/cloverstd/ccmate/internal/ent"
	"github.com/cloverstd/ccmate/internal/ent/project"
	"github.com/cloverstd/ccmate/internal/ent/projectlabelrule"
	enttask "github.com/cloverstd/ccmate/internal/ent/task"
	"github.com/cloverstd/ccmate/internal/ent/webhookreceipt"
	"github.com/cloverstd/ccmate/internal/gitprovider"
	"github.com/cloverstd/ccmate/internal/model"
)

// Processor handles normalized webhook events.
type Processor struct {
	client  *ent.Client
	gitProv gitprovider.GitProvider
}

func NewProcessor(client *ent.Client, gitProv gitprovider.GitProvider) *Processor {
	return &Processor{client: client, gitProv: gitProv}
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

	rule, err := p.client.ProjectLabelRule.Query().
		Where(
			projectlabelrule.HasProjectWith(project.ID(proj.ID)),
			projectlabelrule.IssueLabel(event.Label),
		).Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("finding label rule: %w", err)
	}

	if rule.TriggerMode == projectlabelrule.TriggerModeManual && !proj.AutoMode {
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

	_, err = p.client.Task.Create().
		SetProject(proj).SetIssueNumber(event.IssueNumber).
		SetType(enttask.TypeIssueImplementation).SetStatus(enttask.StatusQueued).
		SetTriggerSource(string(model.TriggerSourceWebhook)).
		Save(ctx)
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
	return ParseAndExecuteCommand(ctx, p.client, event, p.gitProv)
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
	_, err = p.client.Task.Create().
		SetProject(proj).SetIssueNumber(existingTask.IssueNumber).
		SetNillablePrNumber(&prNum).
		SetType(enttask.TypeReviewFix).SetStatus(enttask.StatusQueued).
		SetTriggerSource(string(model.TriggerSourceWebhook)).
		Save(ctx)
	if err != nil {
		return fmt.Errorf("creating review fix task: %w", err)
	}

	slog.Info("review fix task created", "pr", event.PRNumber)
	return nil
}
