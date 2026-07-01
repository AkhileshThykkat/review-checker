// Package gh talks to the GitHub API: fetching PR files, mapping diff
// positions, and posting/superseding review comments.
package gh

import (
	"context"
	"fmt"
	"log"
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

// NewClient builds a client for owner/repo PR number, authenticated with token.
func NewClient(token, owner, repo string, number int) *Client {
	return &Client{
		api:    github.NewClient(nil).WithAuthToken(token),
		owner:  owner,
		repo:   repo,
		number: number,
	}
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

// DeleteStaleComments removes review comments from previous runs, identified
// by Marker. Called before posting fresh comments (dismiss/supersede, v1).
func (c *Client) DeleteStaleComments(ctx context.Context) error {
	opt := &github.PullRequestListCommentsOptions{
		ListOptions: github.ListOptions{PerPage: 100},
	}
	var stale []int64
	for {
		comments, resp, err := c.api.PullRequests.ListComments(ctx, c.owner, c.repo, c.number, opt)
		if err != nil {
			return fmt.Errorf("list review comments: %w", err)
		}
		for _, cm := range comments {
			if strings.Contains(cm.GetBody(), Marker) {
				stale = append(stale, cm.GetID())
			}
		}
		if resp.NextPage == 0 {
			break
		}
		opt.Page = resp.NextPage
	}

	for _, id := range stale {
		if _, err := c.api.PullRequests.DeleteComment(ctx, c.owner, c.repo, id); err != nil {
			// Best effort: a stale comment we cannot delete should not
			// block posting the fresh review.
			log.Printf("warn: delete stale comment %d: %v", id, err)
		}
	}
	if len(stale) > 0 {
		log.Printf("superseded %d stale comment(s) from previous run", len(stale))
	}
	return nil
}

// PostReview submits one review containing all line comments, as a
// non-blocking COMMENT event.
func (c *Client) PostReview(ctx context.Context, summary string, comments []Comment) error {
	draft := make([]*github.DraftReviewComment, 0, len(comments))
	for _, cm := range comments {
		draft = append(draft, &github.DraftReviewComment{
			Path:     github.String(cm.Path),
			Position: github.Int(cm.Position),
			Body:     github.String(cm.Body),
		})
	}
	review := &github.PullRequestReviewRequest{
		Body:     github.String(summary),
		Event:    github.String("COMMENT"),
		Comments: draft,
	}
	if _, _, err := c.api.PullRequests.CreateReview(ctx, c.owner, c.repo, c.number, review); err != nil {
		return fmt.Errorf("create review: %w", err)
	}
	return nil
}
