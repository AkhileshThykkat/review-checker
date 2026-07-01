// Package gh talks to the GitHub API: fetching PR files, mapping diff
// positions, and posting/superseding review comments.
package gh

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/google/go-github/v66/github"
)

// Marker identifies comments posted by review-checker so re-runs can
// supersede them. Invisible in rendered Markdown.
const Marker = "<!-- review-checker -->"

// PRFile is one changed file in the PR.
type PRFile struct {
	Path  string
	Patch string
}

// Comment is a resolved, postable line comment.
type Comment struct {
	Path     string
	Position int
	Body     string
}

// Client wraps the GitHub API for a single PR.
type Client struct {
	api    *github.Client
	owner  string
	repo   string
	number int
}

// NewClient builds a client for owner/repo PR number, authenticated with
// token. Honors GITHUB_API_URL so the action works on GitHub Enterprise
// Server (Actions sets it to e.g. https://ghe.example.com/api/v3).
func NewClient(token, owner, repo string, number int) (*Client, error) {
	api := github.NewClient(nil).WithAuthToken(token)
	if u := os.Getenv("GITHUB_API_URL"); u != "" && u != "https://api.github.com" {
		var err error
		api, err = api.WithEnterpriseURLs(u, u)
		if err != nil {
			return nil, fmt.Errorf("GITHUB_API_URL %q: %w", u, err)
		}
	}
	return &Client{
		api:    api,
		owner:  owner,
		repo:   repo,
		number: number,
	}, nil
}

// FetchFile returns a file's content from the repo at ref via the contents
// API — lets the action run without a checkout step.
func (c *Client) FetchFile(ctx context.Context, path, ref string) ([]byte, error) {
	content, _, _, err := c.api.Repositories.GetContents(ctx, c.owner, c.repo, path,
		&github.RepositoryContentGetOptions{Ref: ref})
	if err != nil {
		return nil, fmt.Errorf("fetch %s@%s from GitHub: %w", path, ref, err)
	}
	text, err := content.GetContent()
	if err != nil {
		return nil, fmt.Errorf("decode %s: %w", path, err)
	}
	return []byte(text), nil
}

// ChangedFiles returns every file in the PR diff that has patch text.
// Binary files and huge files GitHub omits patches for are skipped.
func (c *Client) ChangedFiles(ctx context.Context) ([]PRFile, error) {
	var files []PRFile
	opt := &github.ListOptions{PerPage: 100}
	for {
		page, resp, err := c.api.PullRequests.ListFiles(ctx, c.owner, c.repo, c.number, opt)
		if err != nil {
			return nil, fmt.Errorf("list PR files: %w", err)
		}
		for _, f := range page {
			if f.GetPatch() == "" {
				continue
			}
			files = append(files, PRFile{Path: f.GetFilename(), Patch: f.GetPatch()})
		}
		if resp.NextPage == 0 {
			break
		}
		opt.Page = resp.NextPage
	}
	return files, nil
}

// DeleteStaleComments removes line comments and summary comments from
// previous runs, identified by Marker. Called before posting fresh ones
// (dismiss/supersede, v1).
func (c *Client) DeleteStaleComments(ctx context.Context) error {
	// Line comments (pull request review comments).
	var staleLine []int64
	lineOpt := &github.PullRequestListCommentsOptions{
		ListOptions: github.ListOptions{PerPage: 100},
	}
	for {
		comments, resp, err := c.api.PullRequests.ListComments(ctx, c.owner, c.repo, c.number, lineOpt)
		if err != nil {
			return fmt.Errorf("list review comments: %w", err)
		}
		for _, cm := range comments {
			if strings.Contains(cm.GetBody(), Marker) {
				staleLine = append(staleLine, cm.GetID())
			}
		}
		if resp.NextPage == 0 {
			break
		}
		lineOpt.Page = resp.NextPage
	}

	// Summary comments (issue comments on the PR conversation).
	var staleIssue []int64
	issueOpt := &github.IssueListCommentsOptions{
		ListOptions: github.ListOptions{PerPage: 100},
	}
	for {
		comments, resp, err := c.api.Issues.ListComments(ctx, c.owner, c.repo, c.number, issueOpt)
		if err != nil {
			return fmt.Errorf("list issue comments: %w", err)
		}
		for _, cm := range comments {
			if strings.Contains(cm.GetBody(), Marker) {
				staleIssue = append(staleIssue, cm.GetID())
			}
		}
		if resp.NextPage == 0 {
			break
		}
		issueOpt.Page = resp.NextPage
	}

	// Best effort: a stale comment we cannot delete should not block
	// posting the fresh review.
	for _, id := range staleLine {
		if _, err := c.api.PullRequests.DeleteComment(ctx, c.owner, c.repo, id); err != nil {
			log.Printf("warn: delete stale line comment %d: %v", id, err)
		}
	}
	for _, id := range staleIssue {
		if _, err := c.api.Issues.DeleteComment(ctx, c.owner, c.repo, id); err != nil {
			log.Printf("warn: delete stale summary comment %d: %v", id, err)
		}
	}
	if n := len(staleLine) + len(staleIssue); n > 0 {
		log.Printf("superseded %d stale comment(s) from previous run", n)
	}
	return nil
}

// PostReview submits one review containing all line comments, as a
// non-blocking COMMENT event. The review body stays empty: submitted
// reviews can never be deleted via the API, so anything durable there
// would pile up on every re-push — the summary goes in a deletable
// issue comment instead (PostSummary).
func (c *Client) PostReview(ctx context.Context, comments []Comment) error {
	draft := make([]*github.DraftReviewComment, 0, len(comments))
	for _, cm := range comments {
		draft = append(draft, &github.DraftReviewComment{
			Path:     github.String(cm.Path),
			Position: github.Int(cm.Position),
			Body:     github.String(cm.Body),
		})
	}
	review := &github.PullRequestReviewRequest{
		Event:    github.String("COMMENT"),
		Comments: draft,
	}
	if _, _, err := c.api.PullRequests.CreateReview(ctx, c.owner, c.repo, c.number, review); err != nil {
		return fmt.Errorf("create review: %w", err)
	}
	return nil
}

// PostSummary adds the run summary as an issue comment on the PR
// conversation — deletable on the next run, unlike a review body.
func (c *Client) PostSummary(ctx context.Context, body string) error {
	comment := &github.IssueComment{Body: github.String(body)}
	if _, _, err := c.api.Issues.CreateComment(ctx, c.owner, c.repo, c.number, comment); err != nil {
		return fmt.Errorf("create summary comment: %w", err)
	}
	return nil
}
