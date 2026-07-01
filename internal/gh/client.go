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

// BotComment is a line comment posted by a previous run, identified by
// Marker. Used to supersede (full mode) or dedupe against (incremental
// mode) earlier findings.
type BotComment struct {
	ID   int64
	Path string
	Body string
}

// ListBotLineComments returns the line comments posted by previous runs.
func (c *Client) ListBotLineComments(ctx context.Context) ([]BotComment, error) {
	var out []BotComment
	opt := &github.PullRequestListCommentsOptions{
		ListOptions: github.ListOptions{PerPage: 100},
	}
	for {
		comments, resp, err := c.api.PullRequests.ListComments(ctx, c.owner, c.repo, c.number, opt)
		if err != nil {
			return nil, fmt.Errorf("list review comments: %w", err)
		}
		for _, cm := range comments {
			if strings.Contains(cm.GetBody(), Marker) {
				out = append(out, BotComment{ID: cm.GetID(), Path: cm.GetPath(), Body: cm.GetBody()})
			}
		}
		if resp.NextPage == 0 {
			break
		}
		opt.Page = resp.NextPage
	}
	return out, nil
}

// DeleteLineComments removes previous-run line comments (full-review mode).
// Best effort: a stale comment we cannot delete should not block posting
// the fresh review.
func (c *Client) DeleteLineComments(ctx context.Context, comments []BotComment) {
	for _, cm := range comments {
		if _, err := c.api.PullRequests.DeleteComment(ctx, c.owner, c.repo, cm.ID); err != nil {
			log.Printf("warn: delete stale line comment %d: %v", cm.ID, err)
		}
	}
	if len(comments) > 0 {
		log.Printf("superseded %d stale comment(s) from previous run", len(comments))
	}
}

// FindSummaryComment returns the id and body of the summary comment from a
// previous run, or 0 when there is none. Duplicates — possible when a past
// UpsertSummary fell back from edit to create — are deleted best-effort so
// the PR self-heals to a single summary.
func (c *Client) FindSummaryComment(ctx context.Context) (int64, string, error) {
	var found []*github.IssueComment
	opt := &github.IssueListCommentsOptions{
		ListOptions: github.ListOptions{PerPage: 100},
	}
	for {
		comments, resp, err := c.api.Issues.ListComments(ctx, c.owner, c.repo, c.number, opt)
		if err != nil {
			return 0, "", fmt.Errorf("list issue comments: %w", err)
		}
		for _, cm := range comments {
			if strings.Contains(cm.GetBody(), Marker) {
				found = append(found, cm)
			}
		}
		if resp.NextPage == 0 {
			break
		}
		opt.Page = resp.NextPage
	}
	if len(found) == 0 {
		return 0, "", nil
	}
	for _, cm := range found[1:] {
		if _, err := c.api.Issues.DeleteComment(ctx, c.owner, c.repo, cm.GetID()); err != nil {
			log.Printf("warn: delete duplicate summary comment %d: %v", cm.GetID(), err)
		}
	}
	return found[0].GetID(), found[0].GetBody(), nil
}

// UpsertSummary edits the previous summary comment in place — keeps its
// position in the conversation and avoids a delete/create notification per
// push — or creates it on the first run.
func (c *Client) UpsertSummary(ctx context.Context, id int64, body string) error {
	if id != 0 {
		comment := &github.IssueComment{Body: github.String(body)}
		if _, _, err := c.api.Issues.EditComment(ctx, c.owner, c.repo, id, comment); err == nil {
			return nil
		} else {
			log.Printf("warn: edit summary comment %d failed, creating a new one: %v", id, err)
		}
	}
	return c.PostSummary(ctx, body)
}

// ChangedSince returns the files changed between base and head. ok is false
// when the range cannot anchor an incremental review — base unknown (force
// push), history diverged (rebase), or the API call failed — and the caller
// should fall back to a full review.
func (c *Client) ChangedSince(ctx context.Context, base, head string) (files []string, ok bool) {
	opt := &github.ListOptions{PerPage: 100}
	for {
		cmp, resp, err := c.api.Repositories.CompareCommits(ctx, c.owner, c.repo, base, head, opt)
		if err != nil {
			log.Printf("compare %.7s...%.7s failed, falling back to full review: %v", base, head, err)
			return nil, false
		}
		if s := cmp.GetStatus(); s != "ahead" && s != "identical" {
			log.Printf("history rewritten since last review (compare status %q), falling back to full review", s)
			return nil, false
		}
		for _, f := range cmp.Files {
			files = append(files, f.GetFilename())
		}
		if resp.NextPage == 0 {
			break
		}
		opt.Page = resp.NextPage
	}
	return files, true
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
