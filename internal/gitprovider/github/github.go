package github

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"

	"github.com/bradleyfalzon/ghinstallation/v2"
	"github.com/cloverstd/ccmate/internal/gitprovider"
	"github.com/cloverstd/ccmate/internal/model"
	gh "github.com/google/go-github/v72/github"
)

// Factory creates GitHub providers.
type Factory struct{}

func (f *Factory) Name() string { return "github" }

func (f *Factory) Create(cfg gitprovider.ProviderConfig) (gitprovider.GitProvider, error) {
	var client *gh.Client
	var token string

	if cfg.AppID > 0 && cfg.PrivateKeyPath != "" {
		// GitHub App installation token (preferred for production)
		keyBytes, err := os.ReadFile(cfg.PrivateKeyPath)
		if err != nil {
			return nil, fmt.Errorf("reading private key: %w", err)
		}

		itr, err := ghinstallation.New(http.DefaultTransport, cfg.AppID, cfg.InstallationID, keyBytes)
		if err != nil {
			return nil, fmt.Errorf("creating github app transport: %w", err)
		}

		client = gh.NewClient(&http.Client{Transport: itr})
	} else if cfg.PersonalToken != "" {
		// Personal access token (for local development)
		client = gh.NewClient(nil).WithAuthToken(cfg.PersonalToken)
		token = cfg.PersonalToken
	} else {
		client = gh.NewClient(nil)
	}

	return &Provider{
		client:        client,
		webhookSecret: cfg.WebhookSecret,
		token:         token,
	}, nil
}

// Provider implements gitprovider.GitProvider for GitHub.
type Provider struct {
	client        *gh.Client
	webhookSecret string
	token         string
}

func (p *Provider) VerifyWebhook(req *http.Request) (*model.NormalizedEvent, error) {
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, fmt.Errorf("reading body: %w", err)
	}
	defer req.Body.Close()

	if p.webhookSecret != "" {
		sig := req.Header.Get("X-Hub-Signature-256")
		if !verifySignature(body, sig, p.webhookSecret) {
			return nil, fmt.Errorf("invalid webhook signature")
		}
	}

	eventType := req.Header.Get("X-GitHub-Event")
	deliveryID := req.Header.Get("X-GitHub-Delivery")

	event := &model.NormalizedEvent{DeliveryID: deliveryID}

	switch eventType {
	case "issues":
		return p.parseIssueEvent(body, event)
	case "issue_comment":
		return p.parseIssueCommentEvent(body, event)
	case "pull_request_review":
		return p.parsePRReviewEvent(body, event)
	case "pull_request":
		return p.parsePREvent(body, event)
	default:
		return nil, fmt.Errorf("unsupported event type: %s", eventType)
	}
}

func (p *Provider) parseIssueEvent(body []byte, event *model.NormalizedEvent) (*model.NormalizedEvent, error) {
	var payload struct {
		Action string `json:"action"`
		Issue  struct {
			Number int `json:"number"`
		} `json:"issue"`
		Label struct {
			Name string `json:"name"`
		} `json:"label"`
		Repository struct {
			Owner struct {
				Login string `json:"login"`
			} `json:"owner"`
			Name string `json:"name"`
		} `json:"repository"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("parsing issue event: %w", err)
	}
	if payload.Action != "labeled" {
		return nil, fmt.Errorf("ignoring issue action: %s", payload.Action)
	}
	event.Type = model.EventIssueLabeled
	event.IssueNumber = payload.Issue.Number
	event.Label = payload.Label.Name
	event.Repo = model.RepoRef{Owner: payload.Repository.Owner.Login, Name: payload.Repository.Name}
	return event, nil
}

func (p *Provider) parseIssueCommentEvent(body []byte, event *model.NormalizedEvent) (*model.NormalizedEvent, error) {
	var payload struct {
		Action string `json:"action"`
		Issue  struct {
			Number          int `json:"number"`
			PullRequestURLs *struct {
				URL string `json:"url"`
			} `json:"pull_request"`
		} `json:"issue"`
		Comment struct {
			Body string `json:"body"`
			User struct {
				Login string `json:"login"`
			} `json:"user"`
		} `json:"comment"`
		Repository struct {
			Owner struct {
				Login string `json:"login"`
			} `json:"owner"`
			Name string `json:"name"`
		} `json:"repository"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("parsing comment event: %w", err)
	}
	if payload.Action != "created" {
		return nil, fmt.Errorf("ignoring comment action: %s", payload.Action)
	}
	if payload.Issue.PullRequestURLs != nil {
		event.Type = model.EventPRCommentCreated
		event.PRNumber = payload.Issue.Number
	} else {
		event.Type = model.EventIssueCommentCreated
		event.IssueNumber = payload.Issue.Number
	}
	event.CommentBody = payload.Comment.Body
	event.CommentUser = payload.Comment.User.Login
	event.Repo = model.RepoRef{Owner: payload.Repository.Owner.Login, Name: payload.Repository.Name}
	return event, nil
}

func (p *Provider) parsePRReviewEvent(body []byte, event *model.NormalizedEvent) (*model.NormalizedEvent, error) {
	var payload struct {
		Action      string `json:"action"`
		PullRequest struct {
			Number int `json:"number"`
		} `json:"pull_request"`
		Review struct {
			State string `json:"state"`
			Body  string `json:"body"`
			User  struct {
				Login string `json:"login"`
			} `json:"user"`
		} `json:"review"`
		Repository struct {
			Owner struct {
				Login string `json:"login"`
			} `json:"owner"`
			Name string `json:"name"`
		} `json:"repository"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("parsing PR review event: %w", err)
	}
	if payload.Action != "submitted" {
		return nil, fmt.Errorf("ignoring PR review action: %s", payload.Action)
	}
	event.Type = model.EventPRReviewSubmitted
	event.PRNumber = payload.PullRequest.Number
	event.ReviewState = payload.Review.State
	event.CommentBody = payload.Review.Body
	event.CommentUser = payload.Review.User.Login
	event.Repo = model.RepoRef{Owner: payload.Repository.Owner.Login, Name: payload.Repository.Name}
	return event, nil
}

func (p *Provider) parsePREvent(body []byte, event *model.NormalizedEvent) (*model.NormalizedEvent, error) {
	var payload struct {
		Action      string `json:"action"`
		PullRequest struct {
			Number int `json:"number"`
		} `json:"pull_request"`
		Repository struct {
			Owner struct {
				Login string `json:"login"`
			} `json:"owner"`
			Name string `json:"name"`
		} `json:"repository"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("parsing PR event: %w", err)
	}
	if payload.Action != "synchronize" {
		return nil, fmt.Errorf("ignoring PR action: %s", payload.Action)
	}
	event.Type = model.EventPRSynchronize
	event.PRNumber = payload.PullRequest.Number
	event.Repo = model.RepoRef{Owner: payload.Repository.Owner.Login, Name: payload.Repository.Name}
	return event, nil
}

func (p *Provider) GetIssue(ctx context.Context, repo model.RepoRef, issueNumber int) (*model.Issue, error) {
	issue, _, err := p.client.Issues.Get(ctx, repo.Owner, repo.Name, issueNumber)
	if err != nil {
		return nil, fmt.Errorf("getting issue: %w", err)
	}
	labels := make([]string, len(issue.Labels))
	for i, l := range issue.Labels {
		labels[i] = l.GetName()
	}
	return &model.Issue{
		Number: issue.GetNumber(), Title: issue.GetTitle(), Body: issue.GetBody(),
		Labels: labels, State: issue.GetState(), User: issue.GetUser().GetLogin(),
		CreatedAt: issue.GetCreatedAt().Time, UpdatedAt: issue.GetUpdatedAt().Time,
	}, nil
}

func (p *Provider) ListIssueComments(ctx context.Context, repo model.RepoRef, issueNumber int) ([]model.Comment, error) {
	comments, _, err := p.client.Issues.ListComments(ctx, repo.Owner, repo.Name, issueNumber, nil)
	if err != nil {
		return nil, fmt.Errorf("listing comments: %w", err)
	}
	result := make([]model.Comment, len(comments))
	for i, c := range comments {
		result[i] = model.Comment{ID: c.GetID(), Body: c.GetBody(), User: c.GetUser().GetLogin(), CreatedAt: c.GetCreatedAt().Time}
	}
	return result, nil
}

func (p *Provider) ListRepoBranches(ctx context.Context, repo model.RepoRef) ([]model.RepoBranch, error) {
	branches, _, err := p.client.Repositories.ListBranches(ctx, repo.Owner, repo.Name, &gh.BranchListOptions{ListOptions: gh.ListOptions{PerPage: 100}})
	if err != nil {
		return nil, fmt.Errorf("listing branches: %w", err)
	}

	result := make([]model.RepoBranch, 0, len(branches))
	for _, branch := range branches {
		hash := branch.GetCommit().GetSHA()
		message := ""
		if hash != "" {
			commit, _, err := p.client.Repositories.GetCommit(ctx, repo.Owner, repo.Name, hash, nil)
			if err == nil && commit.Commit != nil && commit.Commit.Message != nil {
				message = strings.Split(commit.GetCommit().GetMessage(), "\n")[0]
			}
		}
		if len(hash) > 12 {
			hash = hash[:12]
		}
		result = append(result, model.RepoBranch{
			Name:    branch.GetName(),
			Hash:    hash,
			Message: message,
		})
	}
	return result, nil
}

func (p *Provider) ListRepoTags(ctx context.Context, repo model.RepoRef) ([]model.RepoTag, error) {
	tags, _, err := p.client.Repositories.ListTags(ctx, repo.Owner, repo.Name, &gh.ListOptions{PerPage: 100})
	if err != nil {
		return nil, fmt.Errorf("listing tags: %w", err)
	}
	result := make([]model.RepoTag, 0, len(tags))
	for _, tag := range tags {
		hash := tag.GetCommit().GetSHA()
		if len(hash) > 12 {
			hash = hash[:12]
		}
		result = append(result, model.RepoTag{
			Name: tag.GetName(),
			Hash: hash,
		})
	}
	return result, nil
}

func (p *Provider) CreateIssueComment(ctx context.Context, repo model.RepoRef, issueNumber int, body string) error {
	_, _, err := p.client.Issues.CreateComment(ctx, repo.Owner, repo.Name, issueNumber, &gh.IssueComment{Body: &body})
	return err
}

func (p *Provider) CreateBranch(ctx context.Context, repo model.RepoRef, base string, newBranch string) error {
	ref, _, err := p.client.Git.GetRef(ctx, repo.Owner, repo.Name, "refs/heads/"+base)
	if err != nil {
		return fmt.Errorf("getting base ref: %w", err)
	}
	newRef := "refs/heads/" + newBranch
	_, _, err = p.client.Git.CreateRef(ctx, repo.Owner, repo.Name, &gh.Reference{Ref: &newRef, Object: ref.Object})
	if err != nil && !strings.Contains(err.Error(), "Reference already exists") {
		return fmt.Errorf("creating branch: %w", err)
	}
	return nil
}

func (p *Provider) PushBranch(ctx context.Context, repo model.RepoRef, localPath string, branch string) error {
	cmd := exec.CommandContext(ctx, "git", "push", "origin", branch)
	cmd.Dir = localPath
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("pushing branch: %w: %s", err, string(output))
	}
	return nil
}

func (p *Provider) CreatePullRequest(ctx context.Context, repo model.RepoRef, req model.CreatePRRequest) (*model.PullRequest, error) {
	pr, _, err := p.client.PullRequests.Create(ctx, repo.Owner, repo.Name, &gh.NewPullRequest{
		Title: &req.Title, Body: &req.Body, Head: &req.Head, Base: &req.Base,
	})
	if err != nil {
		return nil, fmt.Errorf("creating PR: %w", err)
	}
	return &model.PullRequest{
		Number: pr.GetNumber(), Title: pr.GetTitle(), Body: pr.GetBody(),
		State: pr.GetState(), User: pr.GetUser().GetLogin(), HTMLURL: pr.GetHTMLURL(),
		Head: pr.GetHead().GetRef(), Base: pr.GetBase().GetRef(),
		CreatedAt: pr.GetCreatedAt().Time, UpdatedAt: pr.GetUpdatedAt().Time,
	}, nil
}

func (p *Provider) GetPullRequest(ctx context.Context, repo model.RepoRef, prNumber int) (*model.PullRequest, error) {
	pr, _, err := p.client.PullRequests.Get(ctx, repo.Owner, repo.Name, prNumber)
	if err != nil {
		return nil, fmt.Errorf("getting PR: %w", err)
	}
	result := &model.PullRequest{
		Number: pr.GetNumber(), Title: pr.GetTitle(), Body: pr.GetBody(),
		State: pr.GetState(), User: pr.GetUser().GetLogin(), HTMLURL: pr.GetHTMLURL(),
		Head: pr.GetHead().GetRef(), Base: pr.GetBase().GetRef(),
		Mergeable: pr.Mergeable,
		CreatedAt: pr.GetCreatedAt().Time, UpdatedAt: pr.GetUpdatedAt().Time,
	}

	// Fetch combined check status for the head SHA
	if headSHA := pr.GetHead().GetSHA(); headSHA != "" {
		if combined, _, err := p.client.Repositories.GetCombinedStatus(ctx, repo.Owner, repo.Name, headSHA, nil); err == nil {
			result.CheckStatus = combined.GetState()
		}
		if checks, _, err := p.client.Checks.ListCheckRunsForRef(ctx, repo.Owner, repo.Name, headSHA, nil); err == nil {
			for _, cr := range checks.CheckRuns {
				result.CheckDetails = append(result.CheckDetails, model.CheckRun{
					Name: cr.GetName(), Status: cr.GetStatus(), Conclusion: cr.GetConclusion(),
				})
			}
		}
	}

	return result, nil
}

func (p *Provider) FindPullRequestByHead(ctx context.Context, repo model.RepoRef, head string) (*model.PullRequest, error) {
	prs, _, err := p.client.PullRequests.List(ctx, repo.Owner, repo.Name, &gh.PullRequestListOptions{
		Head: repo.Owner + ":" + head, State: "all", ListOptions: gh.ListOptions{PerPage: 1},
	})
	if err != nil {
		return nil, fmt.Errorf("finding PR by head: %w", err)
	}
	if len(prs) == 0 {
		return nil, nil
	}
	pr := prs[0]
	return &model.PullRequest{
		Number: pr.GetNumber(), Title: pr.GetTitle(), Body: pr.GetBody(),
		State: pr.GetState(), User: pr.GetUser().GetLogin(), HTMLURL: pr.GetHTMLURL(),
		Head: pr.GetHead().GetRef(), Base: pr.GetBase().GetRef(),
		CreatedAt: pr.GetCreatedAt().Time, UpdatedAt: pr.GetUpdatedAt().Time,
	}, nil
}

func (p *Provider) ListPullRequestReviews(ctx context.Context, repo model.RepoRef, prNumber int) ([]model.Review, error) {
	reviews, _, err := p.client.PullRequests.ListReviews(ctx, repo.Owner, repo.Name, prNumber, nil)
	if err != nil {
		return nil, fmt.Errorf("listing reviews: %w", err)
	}
	result := make([]model.Review, len(reviews))
	for i, r := range reviews {
		result[i] = model.Review{ID: r.GetID(), State: r.GetState(), Body: r.GetBody(), User: r.GetUser().GetLogin()}
	}
	return result, nil
}

func (p *Provider) GetPullRequestDiff(ctx context.Context, repo model.RepoRef, prNumber int) (string, error) {
	diff, _, err := p.client.PullRequests.GetRaw(ctx, repo.Owner, repo.Name, prNumber, gh.RawOptions{Type: gh.Diff})
	if err != nil {
		return "", fmt.Errorf("getting PR diff: %w", err)
	}
	return diff, nil
}

func (p *Provider) IsAuthorizedCommenter(ctx context.Context, repo model.RepoRef, user string) (bool, error) {
	isCollab, _, err := p.client.Repositories.IsCollaborator(ctx, repo.Owner, repo.Name, user)
	if err != nil {
		return false, fmt.Errorf("checking collaborator: %w", err)
	}
	return isCollab, nil
}

func (p *Provider) CloneRepo(ctx context.Context, repo model.RepoRef, destPath string, branch string) error {
	cloneURL := fmt.Sprintf("https://github.com/%s/%s.git", repo.Owner, repo.Name)
	if p.token != "" {
		cloneURL = fmt.Sprintf("https://x-access-token:%s@github.com/%s/%s.git", p.token, repo.Owner, repo.Name)
	}
	cmd := exec.CommandContext(ctx, "git", "clone", "--depth=1", "--branch", branch, cloneURL, destPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("cloning repo: %w: %s", err, string(output))
	}
	return nil
}

func (p *Provider) ListRepoIssues(ctx context.Context, repo model.RepoRef) ([]model.Issue, error) {
	issues, _, err := p.client.Issues.ListByRepo(ctx, repo.Owner, repo.Name, &gh.IssueListByRepoOptions{
		State: "open", ListOptions: gh.ListOptions{PerPage: 30},
	})
	if err != nil {
		return nil, fmt.Errorf("listing issues: %w", err)
	}
	var result []model.Issue
	for _, issue := range issues {
		if issue.PullRequestLinks != nil {
			continue // skip PRs (GitHub API returns PRs as issues too)
		}
		labels := make([]string, len(issue.Labels))
		for i, l := range issue.Labels {
			labels[i] = l.GetName()
		}
		result = append(result, model.Issue{
			Number: issue.GetNumber(), Title: issue.GetTitle(), Body: issue.GetBody(),
			Labels: labels, State: issue.GetState(), User: issue.GetUser().GetLogin(),
			CreatedAt: issue.GetCreatedAt().Time, UpdatedAt: issue.GetUpdatedAt().Time,
		})
	}
	return result, nil
}

func (p *Provider) ListRepoPRs(ctx context.Context, repo model.RepoRef) ([]model.PullRequest, error) {
	prs, _, err := p.client.PullRequests.List(ctx, repo.Owner, repo.Name, &gh.PullRequestListOptions{
		State: "open", ListOptions: gh.ListOptions{PerPage: 30},
	})
	if err != nil {
		return nil, fmt.Errorf("listing PRs: %w", err)
	}
	result := make([]model.PullRequest, len(prs))
	for i, pr := range prs {
		result[i] = model.PullRequest{
			Number: pr.GetNumber(), Title: pr.GetTitle(), Body: pr.GetBody(),
			State: pr.GetState(), HTMLURL: pr.GetHTMLURL(),
			Head: pr.GetHead().GetRef(), Base: pr.GetBase().GetRef(),
			CreatedAt: pr.GetCreatedAt().Time, UpdatedAt: pr.GetUpdatedAt().Time,
		}
	}
	return result, nil
}

func (p *Provider) CreateIssue(ctx context.Context, repo model.RepoRef, title string, body string, labels []string) (*model.Issue, error) {
	issue, _, err := p.client.Issues.Create(ctx, repo.Owner, repo.Name, &gh.IssueRequest{
		Title:  &title,
		Body:   &body,
		Labels: &labels,
	})
	if err != nil {
		return nil, fmt.Errorf("creating issue: %w", err)
	}
	issueLabels := make([]string, len(issue.Labels))
	for i, l := range issue.Labels {
		issueLabels[i] = l.GetName()
	}
	return &model.Issue{
		Number: issue.GetNumber(), Title: issue.GetTitle(), Body: issue.GetBody(),
		Labels: issueLabels, State: issue.GetState(), User: issue.GetUser().GetLogin(),
		CreatedAt: issue.GetCreatedAt().Time, UpdatedAt: issue.GetUpdatedAt().Time,
	}, nil
}

func (p *Provider) ListAccessibleRepos(ctx context.Context) ([]model.RepoInfo, error) {
	var allRepos []model.RepoInfo
	opts := &gh.RepositoryListOptions{
		Sort:        "updated",
		ListOptions: gh.ListOptions{PerPage: 100},
	}

	repos, _, err := p.client.Repositories.List(ctx, "", opts)
	if err != nil {
		return nil, fmt.Errorf("listing repos: %w", err)
	}

	for _, repo := range repos {
		allRepos = append(allRepos, model.RepoInfo{
			FullName:      repo.GetFullName(),
			HTMLURL:       repo.GetHTMLURL(),
			DefaultBranch: repo.GetDefaultBranch(),
			Description:   repo.GetDescription(),
			Private:       repo.GetPrivate(),
		})
	}
	return allRepos, nil
}

func (p *Provider) CloseIssue(ctx context.Context, repo model.RepoRef, issueNumber int) error {
	state := "closed"
	_, _, err := p.client.Issues.Edit(ctx, repo.Owner, repo.Name, issueNumber, &gh.IssueRequest{
		State: &state,
	})
	if err != nil {
		return fmt.Errorf("closing issue: %w", err)
	}
	return nil
}

func (p *Provider) MergePullRequest(ctx context.Context, repo model.RepoRef, prNumber int) error {
	_, _, err := p.client.PullRequests.Merge(ctx, repo.Owner, repo.Name, prNumber, "", &gh.PullRequestOptions{
		MergeMethod: "squash",
	})
	if err != nil {
		return fmt.Errorf("merging PR: %w", err)
	}
	return nil
}

func verifySignature(body []byte, signature string, secret string) bool {
	if !strings.HasPrefix(signature, "sha256=") {
		return false
	}
	sig, err := hex.DecodeString(strings.TrimPrefix(signature, "sha256="))
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hmac.Equal(sig, mac.Sum(nil))
}
