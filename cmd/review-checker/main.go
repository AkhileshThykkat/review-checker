// review-checker fetches a PR's diff from the GitHub API, asks an
// OpenAI-compatible LLM to review it against generic + repo-specific rules,
// and posts line-level review comments back. v1 is non-blocking: comments
// only, CI never fails on findings.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/bmatcuk/doublestar/v4"

	"github.com/akhileshthykkat/review-checker/internal/config"
	"github.com/akhileshthykkat/review-checker/internal/gh"
	"github.com/akhileshthykkat/review-checker/internal/llm"
	"github.com/akhileshthykkat/review-checker/internal/rules"
)

func main() {
	log.SetFlags(0)
	log.SetPrefix("review-checker: ")

	configPath := flag.String("config", ".review-checker.yaml", "path to config file")
	flag.Parse()

	if err := run(context.Background(), *configPath); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context, configPath string) error {
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		return fmt.Errorf("GITHUB_TOKEN is empty — pass it through the workflow env")
	}

	pr, err := githubContext()
	if err != nil {
		return err
	}
	client, err := gh.NewClient(token, pr.owner, pr.repo, pr.number)
	if err != nil {
		return err
	}

	cfg, err := loadConfig(ctx, client, configPath, pr.headSHA)
	if err != nil {
		return err
	}

	apiKey := os.Getenv(cfg.LLM.APIKeyEnv)
	if apiKey == "" {
		return fmt.Errorf("env var %s (llm.api_key_env) is empty — pass the secret through the workflow env", cfg.LLM.APIKeyEnv)
	}
	log.Printf("reviewing %s/%s#%d with %s", pr.owner, pr.repo, pr.number, cfg.LLM.Model)

	files, err := client.ChangedFiles(ctx)
	if err != nil {
		return err
	}
	files = filterIgnored(files, cfg.IgnoreGlobs())
	if len(files) == 0 {
		log.Print("no reviewable files in diff, nothing to do")
		return nil
	}
	log.Printf("%d file(s) to review", len(files))

	// Build prompt under per-file and total token budgets. Once the total
	// budget can't fit a useful slice of the next file, the rest are
	// omitted and disclosed to the model.
	const minUsefulTokens = 256
	diffs := make([]rules.FileDiff, 0, len(files))
	positions := make(map[string]map[int]int, len(files))
	var omitted []string
	remaining := cfg.MaxTotalTokens
	for _, f := range files {
		if remaining < minUsefulTokens {
			omitted = append(omitted, f.Path)
			continue
		}
		budget := min(cfg.MaxFileTokens, remaining)
		patch, truncated := rules.TruncatePatch(f.Patch, budget)
		if truncated {
			log.Printf("truncated %s to ~%d tokens", f.Path, budget)
		}
		remaining -= len(patch) / 4
		diffs = append(diffs, rules.FileDiff{Path: f.Path, Patch: patch, Truncated: truncated})
		// Positions are mapped from the truncated patch: the model only saw
		// that much, and a position past the cut would be invalid anyway.
		positions[f.Path] = gh.BuildPositionMap(patch)
	}
	if len(omitted) > 0 {
		log.Printf("omitted %d file(s) over the ~%d total token budget: %s",
			len(omitted), cfg.MaxTotalTokens, strings.Join(omitted, ", "))
	}

	llmClient := llm.NewOpenAICompat(cfg.LLM.BaseURL, apiKey, cfg.LLM.Model, cfg.LLM.Temperature)
	response, usage, err := llmClient.Complete(ctx, rules.SystemPrompt, rules.BuildUserPrompt(cfg.CustomRules, diffs, omitted))
	if err != nil {
		return err
	}
	if usage.TotalTokens > 0 {
		log.Printf("token usage: %d prompt + %d completion = %d total",
			usage.PromptTokens, usage.CompletionTokens, usage.TotalTokens)
	}

	findings, err := llm.ParseFindings(response)
	if err != nil {
		return err
	}
	log.Printf("model reported %d finding(s)", len(findings))

	comments, counts := resolveComments(findings, positions)
	if dropped := len(findings) - len(comments); dropped > 0 {
		log.Printf("dropped %d finding(s) pointing outside the diff", dropped)
	}

	if err := client.DeleteStaleComments(ctx); err != nil {
		return err
	}

	if len(comments) == 0 {
		log.Print("no postable findings, skipping review")
		return nil
	}

	if err := client.PostReview(ctx, comments); err != nil {
		return err
	}
	if err := client.PostSummary(ctx, buildSummary(cfg.LLM.Model, counts, usage)); err != nil {
		return err
	}
	log.Printf("posted review with %d comment(s)", len(comments))
	return nil
}

type prContext struct {
	owner   string
	repo    string
	number  int
	headSHA string
}

// githubContext resolves the PR coordinates from Actions env:
// GITHUB_REPOSITORY ("owner/repo") and the pull_request event payload.
func githubContext() (*prContext, error) {
	full := os.Getenv("GITHUB_REPOSITORY")
	owner, repo, ok := strings.Cut(full, "/")
	if !ok {
		return nil, fmt.Errorf("GITHUB_REPOSITORY malformed or unset: %q", full)
	}

	eventPath := os.Getenv("GITHUB_EVENT_PATH")
	if eventPath == "" {
		return nil, fmt.Errorf("GITHUB_EVENT_PATH unset — must run inside GitHub Actions on a pull_request event")
	}
	raw, err := os.ReadFile(eventPath)
	if err != nil {
		return nil, fmt.Errorf("read event payload: %w", err)
	}
	var event struct {
		PullRequest struct {
			Number int `json:"number"`
			Head   struct {
				SHA string `json:"sha"`
			} `json:"head"`
		} `json:"pull_request"`
	}
	if err := json.Unmarshal(raw, &event); err != nil {
		return nil, fmt.Errorf("parse event payload: %w", err)
	}
	if event.PullRequest.Number == 0 {
		return nil, fmt.Errorf("no pull_request in event payload — trigger the workflow on pull_request events")
	}
	return &prContext{
		owner:   owner,
		repo:    repo,
		number:  event.PullRequest.Number,
		headSHA: event.PullRequest.Head.SHA,
	}, nil
}

// loadConfig reads the config from the local checkout when present, else
// fetches it from the repo at the PR head via the GitHub API — so consumer
// workflows don't need a checkout step.
func loadConfig(ctx context.Context, client *gh.Client, path, ref string) (*config.Config, error) {
	if _, statErr := os.Stat(path); statErr == nil {
		return config.Load(path)
	}
	log.Printf("%s not in workspace, fetching from repo via API", path)
	raw, err := client.FetchFile(ctx, path, ref)
	if err != nil {
		return nil, fmt.Errorf("config %s not found locally and not fetchable from repo: %w", path, err)
	}
	return config.Parse(raw, path)
}

func filterIgnored(files []gh.PRFile, globs []string) []gh.PRFile {
	kept := files[:0]
	for _, f := range files {
		if matchesAny(f.Path, globs) {
			log.Printf("skipping %s (ignored)", f.Path)
			continue
		}
		kept = append(kept, f)
	}
	return kept
}

func matchesAny(path string, globs []string) bool {
	for _, g := range globs {
		if ok, err := doublestar.Match(g, path); err == nil && ok {
			return true
		}
	}
	return false
}

// resolveComments translates model findings (file + new-file line) into
// GitHub diff positions, dropping any finding the diff cannot anchor and
// deduplicating repeats the model may emit. counts tallies the kept
// findings per severity for the summary comment.
func resolveComments(findings []llm.Finding, positions map[string]map[int]int) (comments []gh.Comment, counts map[string]int) {
	badge := map[string]string{
		llm.SeverityBlock: "🔴 **block**",
		llm.SeverityWarn:  "🟡 **warn**",
		llm.SeverityNit:   "🔵 **nit**",
	}

	seen := make(map[string]bool, len(findings))
	counts = make(map[string]int)
	for _, f := range findings {
		// Paths are already normalized by ParseFindings, so they match the
		// exact paths GitHub reported.
		fileMap, ok := positions[f.File]
		if !ok {
			log.Printf("dropping finding for unknown file %s", f.File)
			continue
		}
		pos, ok := fileMap[f.Line]
		if !ok {
			log.Printf("dropping finding at %s:%d (line not in diff)", f.File, f.Line)
			continue
		}
		key := fmt.Sprintf("%s:%d:%s", f.File, f.Line, f.Comment)
		if seen[key] {
			log.Printf("dropping duplicate finding at %s:%d", f.File, f.Line)
			continue
		}
		seen[key] = true
		counts[f.Severity]++
		comments = append(comments, gh.Comment{
			Path:     f.File,
			Position: pos,
			Body:     fmt.Sprintf("%s\n%s: %s", gh.Marker, badge[f.Severity], f.Comment),
		})
	}
	return comments, counts
}

// buildSummary renders the PR conversation comment: severity breakdown,
// model, and provider-reported token usage (omitted when the provider
// doesn't return a usage block).
func buildSummary(model string, counts map[string]int, usage llm.Usage) string {
	total := counts[llm.SeverityBlock] + counts[llm.SeverityWarn] + counts[llm.SeverityNit]

	var parts []string
	for _, s := range []struct{ severity, emoji string }{
		{llm.SeverityBlock, "🔴"},
		{llm.SeverityWarn, "🟡"},
		{llm.SeverityNit, "🔵"},
	} {
		if n := counts[s.severity]; n > 0 {
			parts = append(parts, fmt.Sprintf("%s %d %s", s.emoji, n, s.severity))
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%s\n**review-checker** found %d issue(s)", gh.Marker, total)
	if len(parts) > 0 {
		fmt.Fprintf(&b, ": %s", strings.Join(parts, " · "))
	}
	fmt.Fprintf(&b, "\n\nModel: `%s`", model)
	if usage.TotalTokens > 0 {
		fmt.Fprintf(&b, " · Tokens: %d (%d prompt + %d completion)",
			usage.TotalTokens, usage.PromptTokens, usage.CompletionTokens)
	}
	b.WriteString("\n\nSeverity is informational in v1 — this check never blocks CI.")
	return b.String()
}
