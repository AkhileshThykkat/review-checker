package main

import (
	"strings"
	"testing"

	"github.com/akhileshthykkat/review-checker/internal/config"
	"github.com/akhileshthykkat/review-checker/internal/llm"
)

func TestFilterSuppressed(t *testing.T) {
	findings := []llm.Finding{
		{File: "app/views.py", Line: 1, Severity: llm.SeverityWarn, Comment: "Use select_related here."},
		{File: "tests/test_x.py", Line: 2, Severity: llm.SeverityNit, Comment: "Magic number."},
		{File: "app/models.py", Line: 3, Severity: llm.SeverityBlock, Comment: "FloatField for money."},
	}
	rules := []config.SuppressRule{
		{Text: "select_related"},
		{Path: "tests/**"},
	}

	got := filterSuppressed(findings, rules)
	if len(got) != 1 || got[0].File != "app/models.py" {
		t.Errorf("filterSuppressed = %+v, want only app/models.py finding", got)
	}
}

func TestFilterSuppressedBothMustMatch(t *testing.T) {
	findings := []llm.Finding{
		{File: "app/views.py", Line: 1, Severity: llm.SeverityWarn, Comment: "raw SQL"},
	}
	rules := []config.SuppressRule{{Path: "tests/**", Text: "raw sql"}}
	if got := filterSuppressed(findings, rules); len(got) != 1 {
		t.Errorf("rule with path+text should require both to match, got %+v", got)
	}
}

func TestBuildSummary(t *testing.T) {
	counts := map[string]int{
		llm.SeverityBlock: 1,
		llm.SeverityWarn:  2,
	}
	usage := llm.Usage{PromptTokens: 1200, CompletionTokens: 300, TotalTokens: 1500}

	got := buildSummary("deepseek-chat", counts, usage)

	for _, want := range []string{
		"found 3 issue(s)",
		"🔴 1 block",
		"🟡 2 warn",
		"`deepseek-chat`",
		"Tokens: 1500 (1200 prompt + 300 completion)",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("summary missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "nit") {
		t.Errorf("summary should skip zero-count severities:\n%s", got)
	}
}

func TestBuildSummaryWithoutUsage(t *testing.T) {
	got := buildSummary("gpt-4o-mini", map[string]int{llm.SeverityNit: 1}, llm.Usage{})
	if strings.Contains(got, "Tokens:") {
		t.Errorf("summary should omit tokens when provider reports no usage:\n%s", got)
	}
	if !strings.Contains(got, "🔵 1 nit") {
		t.Errorf("summary missing nit count:\n%s", got)
	}
}

func TestResolveCommentsCounts(t *testing.T) {
	positions := map[string]map[int]int{
		"a.py": {10: 1, 20: 2},
	}
	findings := []llm.Finding{
		{File: "a.py", Line: 10, Severity: llm.SeverityBlock, Comment: "bad"},
		{File: "a.py", Line: 20, Severity: llm.SeverityWarn, Comment: "risky"},
		{File: "a.py", Line: 99, Severity: llm.SeverityWarn, Comment: "outside diff"},
	}

	comments, counts := resolveComments(findings, positions)
	if len(comments) != 2 {
		t.Fatalf("comments = %d, want 2", len(comments))
	}
	if counts[llm.SeverityBlock] != 1 || counts[llm.SeverityWarn] != 1 {
		t.Errorf("counts = %+v, want 1 block + 1 warn (dropped finding not counted)", counts)
	}
}
