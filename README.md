# review-checker

AI-powered pull request review as a reusable GitHub Action. On every PR it fetches the diff from the GitHub API, sends it to an LLM of your choice (any OpenAI-compatible endpoint — DeepSeek, OpenRouter, OpenAI, Groq, Together, Ollama, ...), and posts line-level review comments back on the PR.

**v1 is non-blocking**: comments only, CI never fails on findings. Every finding is still severity-tagged (`block` / `warn` / `nit`) so a blocking mode can be added later without re-teaching the model.

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
3. Builds one prompt from two rule tiers: **generic backend rules** baked into the binary (versioned with releases, improve on upgrade) and your repo's **`custom_rules`**.
4. Large diffs are truncated to a per-file token budget, with the truncation flagged in the prompt so the model doesn't guess about hidden context.
5. The model returns findings as JSON (`file`, `line`, `severity`, `comment`). Each line number is translated to a GitHub diff position by parsing the patch hunks; findings pointing outside the diff are dropped.
6. Comments from previous runs are superseded: on a new push the old comments are deleted and fresh ones posted.

## Configuration reference

| Key | Required | Default | Description |
|---|---|---|---|
| `llm.base_url` | yes | — | OpenAI-compatible endpoint, including version prefix (e.g. `https://api.deepseek.com/v1`) |
| `llm.api_key_env` | yes | — | Name of the env var holding the API key |
| `llm.model` | yes | — | Model name passed to the endpoint |
| `mode` | no | `comment_only` | Only `comment_only` in v1 |
| `ignore` | no | `[]` | Glob patterns (doublestar `**` supported) added to built-in defaults |
| `custom_rules` | no | `[]` | Repo-specific rules appended to the generic rules |
| `max_file_tokens` | no | `8000` | Approximate per-file token budget before truncation |

### Action inputs

| Input | Default | Description |
|---|---|---|
| `config` | `.review-checker.yaml` | Path to the config file |
| `version` | pinned per action tag | review-checker binary release to download |

## Writing custom rules

`custom_rules` entries are injected into the review prompt verbatim, as additions to the built-in generic rules (see [internal/rules/default_rules.md](internal/rules/default_rules.md) for the baseline — don't repeat what's already there). Each rule is one instruction the model applies to every file in the diff.

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
- If a rule is universal (applies to every backend repo), it belongs in the generic baseline — open a PR against `default_rules.md` here instead.
- Rules don't disable the baseline; per-rule opt-out of generic rules isn't supported in v1.
- After editing rules, open a test PR with a known violation to confirm the model catches it.

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

Releases: push a tag `vX.Y.Z` — the release workflow builds static binaries for linux/darwin × amd64/arm64 and publishes them; `action.yml` downloads the pinned binary at run time (~1–2 s, no Docker pull).

## License

MIT
