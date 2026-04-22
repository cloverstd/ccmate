package gitprovider

import (
	"context"
	"fmt"
	"net/http"

	"github.com/cloverstd/ccmate/internal/model"
)

// GitProvider abstracts git platform operations.
type GitProvider interface {
	VerifyWebhook(req *http.Request) (*model.NormalizedEvent, error)
	GetIssue(ctx context.Context, repo model.RepoRef, issueNumber int) (*model.Issue, error)
	ListIssueComments(ctx context.Context, repo model.RepoRef, issueNumber int) ([]model.Comment, error)
	CreateIssueComment(ctx context.Context, repo model.RepoRef, issueNumber int, body string) error
	CreateBranch(ctx context.Context, repo model.RepoRef, base string, newBranch string) error
	PushBranch(ctx context.Context, repo model.RepoRef, localPath string, branch string) error
	CreatePullRequest(ctx context.Context, repo model.RepoRef, req model.CreatePRRequest) (*model.PullRequest, error)
	GetPullRequest(ctx context.Context, repo model.RepoRef, prNumber int) (*model.PullRequest, error)
	FindPullRequestByHead(ctx context.Context, repo model.RepoRef, head string) (*model.PullRequest, error)
	ListPullRequestReviews(ctx context.Context, repo model.RepoRef, prNumber int) ([]model.Review, error)
	CreatePullRequestReview(ctx context.Context, repo model.RepoRef, prNumber int, req model.CreateReviewRequest) (*model.Review, error)
	GetPullRequestDiff(ctx context.Context, repo model.RepoRef, prNumber int) (string, error)
	IsAuthorizedCommenter(ctx context.Context, repo model.RepoRef, user string) (bool, error)
	CloneRepo(ctx context.Context, repo model.RepoRef, destPath string, branch string) error
	ListRepoIssues(ctx context.Context, repo model.RepoRef) ([]model.Issue, error)
	ListRepoPRs(ctx context.Context, repo model.RepoRef) ([]model.PullRequest, error)
	CreateIssue(ctx context.Context, repo model.RepoRef, title string, body string, labels []string) (*model.Issue, error)
	ListAccessibleRepos(ctx context.Context) ([]model.RepoInfo, error)
	ListRepoBranches(ctx context.Context, repo model.RepoRef) ([]model.RepoBranch, error)
	ListRepoTags(ctx context.Context, repo model.RepoRef) ([]model.RepoTag, error)
	CloseIssue(ctx context.Context, repo model.RepoRef, issueNumber int) error
	MergePullRequest(ctx context.Context, repo model.RepoRef, prNumber int) error
}

// GitProviderFactory creates GitProvider instances.
type GitProviderFactory interface {
	Name() string
	Create(cfg ProviderConfig) (GitProvider, error)
}

// ProviderConfig holds configuration for a git provider.
type ProviderConfig struct {
	AppID          int64
	InstallationID int64
	PrivateKeyPath string
	WebhookSecret  string
	PersonalToken  string
}

// Registry holds registered git provider factories.
type Registry struct {
	factories map[string]GitProviderFactory
}

func NewRegistry() *Registry {
	return &Registry{factories: make(map[string]GitProviderFactory)}
}

func (r *Registry) Register(factory GitProviderFactory) {
	r.factories[factory.Name()] = factory
}

func (r *Registry) Create(name string, cfg ProviderConfig) (GitProvider, error) {
	factory, ok := r.factories[name]
	if !ok {
		return nil, fmt.Errorf("unknown git provider: %s", name)
	}
	return factory.Create(cfg)
}
