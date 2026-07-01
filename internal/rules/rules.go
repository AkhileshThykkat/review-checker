// Package rules bakes the built-in review rule packs into the binary,
// selects the packs relevant to a diff, and merges them with a consumer
// repo's custom rules into the review prompt.
package rules

import (
	"embed"
	"fmt"
	"path"
	"strings"
)

// Rule packs are versioned with Action releases: every consumer repo gets
// rule improvements on upgrade, no per-repo copying.
//
//go:embed packs/*.md
var packsFS embed.FS

// Pack names. Core is always active; the rest are selected per diff.
const (
	PackCore          = "core"
	PackPythonBackend = "python-backend"
	PackTypeScript    = "typescript"
	PackReact         = "react"
)

// packOrder fixes prompt ordering: generic rules first, then language,
// then framework.
var packOrder = []string{PackCore, PackPythonBackend, PackTypeScript, PackReact}

var packContent = map[string]string{}

func init() {
	for _, name := range packOrder {
		raw, err := packsFS.ReadFile("packs/" + name + ".md")
		if err != nil {
			panic(fmt.Sprintf("rule pack %s not embedded: %v", name, err))
		}
		packContent[name] = string(raw)
	}
}

// PackNames returns every available pack name in prompt order.
func PackNames() []string {
	return append([]string{}, packOrder...)
}

// ValidPack reports whether name is a built-in rule pack.
func ValidPack(name string) bool {
	_, ok := packContent[name]
	return ok
}

// DetectPacks selects the rule packs relevant to the changed file paths,
// by extension. Core is always included.
func DetectPacks(paths []string) []string {
	need := map[string]bool{}
	for _, p := range paths {
		switch strings.ToLower(path.Ext(p)) {
		case ".py":
			need[PackPythonBackend] = true
		case ".js", ".mjs", ".cjs", ".ts", ".mts", ".cts":
			need[PackTypeScript] = true
		case ".jsx", ".tsx":
			need[PackTypeScript] = true
			need[PackReact] = true
		}
	}
	var out []string
	for _, name := range packOrder {
		if name == PackCore || need[name] {
			out = append(out, name)
		}
	}
	return out
}

// NormalizePacks dedupes an explicit pack list, forces core in, and puts
// the packs in prompt order.
func NormalizePacks(names []string) []string {
	want := map[string]bool{PackCore: true}
	for _, n := range names {
		want[n] = true
	}
	var out []string
	for _, name := range packOrder {
		if want[name] {
			out = append(out, name)
		}
	}
	return out
}

// SystemPrompt fixes the reviewer persona and the output contract.
const SystemPrompt = `You are a strict, senior code reviewer for pull requests.

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

// BuildUserPrompt assembles the active rule packs, repo-specific rules, and
// the diff into clearly labeled sections so the model treats the built-in
// packs as baseline and the repo set as addition. Files dropped for the
// total token budget are listed so the model doesn't assume it saw the
// whole PR.
func BuildUserPrompt(packs []string, customRules []string, files []FileDiff, omitted []string) string {
	var b strings.Builder

	for _, p := range packs {
		b.WriteString(strings.TrimSpace(packContent[p]))
		b.WriteString("\n\n")
	}

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

	if len(omitted) > 0 {
		b.WriteString("\n# Files changed in this PR but omitted for token budget (do not assume anything about them)\n")
		for _, path := range omitted {
			fmt.Fprintf(&b, "- %s\n", path)
		}
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
