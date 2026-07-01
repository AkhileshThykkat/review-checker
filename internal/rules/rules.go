// Package rules bakes the generic review rules into the binary and merges
// them with a consumer repo's custom rules into the review prompt.
package rules

import (
	_ "embed"
	"fmt"
	"strings"
)

// DefaultRules is versioned with Action releases: every consumer repo gets
// rule improvements on upgrade, no per-repo copying.
//
//go:embed default_rules.md
var DefaultRules string

// SystemPrompt fixes the reviewer persona and the output contract.
const SystemPrompt = `You are a strict, senior backend code reviewer for pull requests.

You will receive review rules and a unified diff. Report only real, actionable problems introduced or touched by this diff. Do not praise, do not summarize, do not restate the diff.

Output contract — respond with ONLY a JSON array, no prose, no code fences:
[
  {"file": "<path exactly as given>", "line": <new-file line number>, "severity": "block|warn|nit", "comment": "<specific, actionable problem and fix>"}
]

Rules for output:
- "line" must be a NEW-file line number visible in the diff (an added or context line). Never reference lines outside the shown hunks.
- "severity": "block" = bug/security/data-loss, "warn" = likely problem or risky pattern, "nit" = minor improvement.
- One finding per distinct problem. No duplicates across lines.
- If a file's diff is marked truncated, do not guess about the hidden part.
- If there is nothing worth reporting, output [].`

// FileDiff is one file's patch, possibly truncated to the token budget.
type FileDiff struct {
	Path      string
	Patch     string
	Truncated bool
}

// BuildUserPrompt assembles the generic rules, repo-specific rules, and the
// diff into clearly labeled sections so the model treats the generic set as
// baseline and the repo set as addition.
func BuildUserPrompt(customRules []string, files []FileDiff) string {
	var b strings.Builder

	b.WriteString("# Generic backend rules\n\n")
	b.WriteString(strings.TrimSpace(DefaultRules))
	b.WriteString("\n\n")

	if len(customRules) > 0 {
		b.WriteString("# Repo-specific rules (additions to the baseline)\n\n")
		for _, r := range customRules {
			fmt.Fprintf(&b, "- %s\n", r)
		}
		b.WriteString("\n")
	}

	b.WriteString("# Pull request diff\n")
	for _, f := range files {
		fmt.Fprintf(&b, "\n## File: %s\n", f.Path)
		if f.Truncated {
			b.WriteString("(diff truncated to fit token budget — do not assume anything about the omitted part)\n")
		}
		b.WriteString("```diff\n")
		b.WriteString(f.Patch)
		b.WriteString("\n```\n")
	}

	return b.String()
}

// TruncatePatch enforces the per-file token budget (approximated at 4 chars
// per token), cutting on a line boundary so the diff stays parseable.
func TruncatePatch(patch string, maxTokens int) (string, bool) {
	maxChars := maxTokens * 4
	if len(patch) <= maxChars {
		return patch, false
	}
	cut := patch[:maxChars]
	if idx := strings.LastIndex(cut, "\n"); idx > 0 {
		cut = cut[:idx]
	}
	return cut, true
}
