# review-checker

AI-powered pull request review as a reusable GitHub Action. On every PR it fetches the diff from the GitHub API, sends it to an LLM of your choice (any OpenAI-compatible endpoint — DeepSeek, OpenRouter, OpenAI, Groq, Together, Ollama, ...), and posts line-level review comments back on the PR.

**v1 is non-blocking**: comments only, CI never fails on findings. Every finding is still severity-tagged (`block` / `warn` / `nit`) so a blocking mode could be added in a future version without re-teaching the model.

## What to expect (read before adopting)

- **This is an LLM reviewer — it will be wrong sometimes.** Expect occasional false positives (flagging fine code) and false negatives (missing real bugs). Treat comments as a first-pass filter, not a verdict. It does not replace human review.
- **Your diff is sent to the LLM provider you configure.** Changed code (patches, file paths) goes to that provider's API under *your* key and *their* data-usage terms. If your code is sensitive, pick a provider/endpoint that meets your policies (e.g. a self-hosted OpenAI-compatible server such as Ollama or vLLM works too).
- **Each PR run costs LLM API tokens** — roughly the diff size plus the rules on input, findings on output. Small PRs are cheap; budget caps (`max_file_tokens`, `max_total_tokens`) bound the worst case.
- **PR content influences the model.** Text inside a diff (comments, strings, docs) becomes part of the prompt, so a PR author can potentially steer or suppress review comments (prompt injection). Findings are advisory and non-blocking, which limits the blast radius — but it's another reason not to treat the output as authoritative.
- **Built-in rules ship as stack-specific packs** — generic correctness/security (always on), Python backends (Django, FastAPI, Flask), JavaScript/TypeScript, and React (including design-system consistency: tokens over hardcoded values, variants over forked components). Packs are auto-selected from the files in the diff. Other stacks still work — the model reviews any diff — but `custom_rules` carries more weight there.

## Quick start

**1. Add `.review-checker.yaml` to your repo root** (see [.review-checker.example.yaml](.review-checker.example.yaml)):

```yaml
llm:
  base_url: https://api.deepseek.com/v1
  api_key_env: DEEPSEEK_API_KEY   # env var name, not the key itself
  model: deepseek-chat

mode: comment_only

ignore:
  - "docs/**"

custom_rules:
  - "Flag any raw SQL string formatting (SQL injection risk)."
  - "Require select_related/prefetch_related on FK/M2M access in list views."
```

**2. Add the workflow** `.github/workflows/review.yml`:

```yaml
name: AI Review
on:
  pull_request:
    types: [opened, synchronize, reopened]

permissions:
  contents: read
  pull-requests: write

jobs:
  review:
    runs-on: ubuntu-latest
    steps:
      - uses: akhileshthykkat/review-checker@v1
        with:
          config: .review-checker.yaml
        env:
          DEEPSEEK_API_KEY: ${{ secrets.DEEPSEEK_API_KEY }}
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
```

**3. Add your LLM API key** as a repo secret (name must match `api_key_env`).

No checkout step is needed — the diff comes from the GitHub API, not the local clone.

## How it works

1. Fetches the PR's changed files and patches via the GitHub API (`pulls/{n}/files`) — no shallow-clone/fetch-depth issues.
2. Filters ignored paths: built-in defaults (migrations, lockfiles, `vendor/`, `node_modules/`, minified/generated files) plus your `ignore:` extensions.
3. Builds one prompt from two rule tiers: **built-in rule packs** baked into the binary (versioned with releases, improve on upgrade) and your repo's **`custom_rules`**. Packs are auto-selected by the file types in the diff — `core` always, `python-backend` for `.py`, `typescript` for `.js`/`.ts`, `react` (with design-system rules) for `.jsx`/`.tsx` — so a pure-Python PR never spends prompt tokens on React rules. Pin the set explicitly with `rule_packs:`.
4. Large diffs are truncated to a per-file token budget, with the truncation flagged in the prompt so the model doesn't guess about hidden context.
5. The model returns findings as JSON (`file`, `line`, `severity`, `comment`). Each line number is translated to a GitHub diff position by parsing the patch hunks; findings pointing outside the diff are dropped.
6. Re-runs are **incremental** by default: the reviewed head SHA is recorded (hidden) in the summary comment, and the next push reviews only files changed since — earlier line comments stay in place (GitHub marks them outdated as code moves), repeat findings are deduplicated by comment text, and the summary comment is edited in place. Force pushes and rebases automatically fall back to a full review. With `review_mode: full`, every push re-reviews the whole diff and supersedes all previous comments. (The summary is an issue comment, not a review body, precisely so it can be edited — submitted reviews can never be deleted.)

## Limitations

- **PRs from forks are not supported.** On `pull_request` events from a fork, GitHub gives the workflow a read-only `GITHUB_TOKEN` and no secrets — the action can neither call your LLM nor post comments. Works for branches within the same repo (the normal setup for private/team repos).
- Findings the model reports on lines outside the diff are dropped (GitHub can't anchor them); the run log lists every dropped finding.
- GitHub Enterprise Server: the action honors the `GITHUB_API_URL` env var that Actions sets on GHES runners, but this path is not regularly tested — please open an issue if it misbehaves.
- Only files with textual patches are reviewed; binary files and files GitHub returns no patch for are skipped. Very large PRs are reviewed partially under `max_total_tokens` (omitted files are listed in the run log).
- Incremental mode caveats: re-running the workflow on the same commit is a no-op (set `review_mode: full` to force a re-review); files omitted over the token budget are not revisited until they change again; duplicate detection keys on the finding text, so a reworded repeat can be posted twice.

## Configuration reference

| Key | Required | Default | Description |
|---|---|---|---|
| `llm.base_url` | yes | — | OpenAI-compatible endpoint, including version prefix (e.g. `https://api.deepseek.com/v1`) |
| `llm.api_key_env` | yes | — | Name of the env var holding the API key |
| `llm.model` | yes | — | Model name passed to the endpoint |
| `llm.temperature` | no | omitted | Sampling temperature; when unset the parameter is not sent (needed for models that reject it, e.g. OpenAI o-series) |
| `mode` | no | `comment_only` | Only `comment_only` in v1 |
| `review_mode` | no | `incremental` | What a re-run reviews: `incremental` = only files changed since the last reviewed commit, keeping earlier comments; `full` = the whole diff on every push, superseding previous comments |
| `ignore` | no | `[]` | Glob patterns (doublestar `**` supported) added to built-in defaults |
| `suppress` | no | `[]` | Rules that hide findings after review (false-positive lever): each entry has `path` (glob) and/or `text` (case-insensitive substring of the finding comment); when both are set, both must match |
| `rule_packs` | no | auto-detect | Pin the built-in rule packs (`core`, `python-backend`, `typescript`, `react`); empty auto-detects from the diff's file extensions, `core` is always included |
| `custom_rules` | no | `[]` | Repo-specific rules appended to the generic rules |
| `max_file_tokens` | no | `8000` | Approximate per-file token budget before a file's diff is truncated |
| `max_total_tokens` | no | `60000` | Approximate budget for the whole diff section; files past it are omitted from the review (listed as omitted in the prompt and logs) |

Unknown keys in the config are rejected — a typo like `custom_rule:` fails the run instead of silently dropping your rules.

### Action inputs

| Input | Default | Description |
|---|---|---|
| `config` | `.review-checker.yaml` | Path to the config file |
| `version` | pinned per action tag | review-checker binary release to download |

## Writing custom rules

`custom_rules` entries are injected into the review prompt verbatim, as additions to the built-in rule packs (see [internal/rules/packs/](internal/rules/packs/) for the baselines — don't repeat what's already there). Each rule is one instruction the model applies to every file in the diff.

**Write rules that are:**

- **Actionable** — describe what to flag and, if possible, what to do instead.
  - ✅ `"Flag raw SQL built with f-strings; require parameterized queries."`
  - ❌ `"Code should be secure."`
- **Detectable in a diff** — the model only sees changed hunks, not the whole repo. Rules that need whole-project knowledge ("ensure this endpoint is documented in the wiki") can't be checked.
  - ✅ `"Flag new Celery tasks missing an explicit `max_retries`."`
  - ❌ `"Ensure all callers of this function are updated."`
- **Specific about scope** — name the pattern, framework, or file kind so the model doesn't over-apply it.
  - ✅ `"In DRF serializers, flag `fields = '__all__'` — list fields explicitly."`
  - ❌ `"Don't expose too much data."`
- **One concern per rule** — split compound rules; the model follows short imperatives better than paragraphs.

**Severity is chosen by the model** (`block` / `warn` / `nit`) — you can steer it inside a rule: `"Flag hardcoded credentials (severity: block)."`

**Examples for a Django backend:**

```yaml
custom_rules:
  - "Flag any raw SQL string formatting (SQL injection risk, severity: block)."
  - "Require select_related/prefetch_related on FK/M2M access in list views."
  - "Flag new endpoints missing a permission_classes declaration."
  - "Flag time.sleep in request handlers; use Celery countdown/eta instead."
  - "Model fields holding money must be DecimalField, never FloatField."
```

**Tips:**

- Start with 3–7 rules covering your repo's recurring review comments; grow the list from real PR feedback.
- If a rule is universal (applies to every repo on that stack), it belongs in the built-in packs — open a PR against the relevant file in `internal/rules/packs/` instead.
- Rules don't disable the baseline; per-rule opt-out of built-in rules isn't supported in v1.
- After editing rules, open a test PR with a known violation to confirm the model catches it.

## Suppressing false positives

The model will sometimes flag fine code. `suppress` hides such findings *after* the review — unlike `ignore`, the file is still reviewed; matching findings are just not posted:

```yaml
suppress:
  - path: "tests/**"            # hide all findings in matching files
  - text: "select_related"     # hide findings whose comment contains this (case-insensitive)
  - path: "app/legacy/**"      # both set: both must match
    text: "SQL"
```

Suppressed findings are listed in the run log, so you can audit what was hidden.

## Provider examples

```yaml
# OpenRouter
llm:
  base_url: https://openrouter.ai/api/v1
  api_key_env: OPENROUTER_API_KEY
  model: deepseek/deepseek-chat

# OpenAI
llm:
  base_url: https://api.openai.com/v1
  api_key_env: OPENAI_API_KEY
  model: gpt-4o-mini

# Groq
llm:
  base_url: https://api.groq.com/openai/v1
  api_key_env: GROQ_API_KEY
  model: llama-3.3-70b-versatile
```

## Development

```bash
go test ./...
go build ./cmd/review-checker
```

Releases: push a tag `vX.Y.Z` — the release workflow builds static binaries for linux/darwin × amd64/arm64 and publishes them; `action.yml` downloads the pinned binary at run time (a small download, no Docker image to pull or build).

## License

MIT
