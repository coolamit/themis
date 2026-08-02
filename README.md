# Themis

AI-powered pull request review as a GitHub Action, named for the Greek goddess of fair judgement. Themis wraps Alibaba's [Open Code Review (OCR)](https://github.com/alibaba/open-code-review) CLI and publishes its findings as inline PR review comments — with GitHub suggestion blocks, content-fingerprint deduplication, comment budgeting, and an optional severity-based merge gate.

## Themis and the upstream OCR action

Upstream OCR ships its own official composite action (the `action.yml` at the root of `alibaba/open-code-review`). Themis is not a competitor — it exists for personal use and is published in case it's useful to others. If you want the setup maintained by the OCR authors themselves, use theirs. Use Themis if the differences below matter to you:

| | Upstream action | Themis |
|---|---|---|
| OCR version | `latest` by default | `latest` by default; **pinnable via `ocr-version`**, checksum-verified whenever the version's hash is recorded |
| Runtime | Node.js + npm install every run | **Two static binaries**, no runtime |
| Dedupe | Line-range overlap (positional) | **Content fingerprint** — survives line drift and LLM rewording |
| Merge gating | No | **`fail-on-severity`** input |
| Comment budgeting | Batch-size only | **`max-comments`** with critical-severity exemption + overflow summary |

## Quickstart

1. Add an LLM API key to your repository secrets (e.g. `LLM_API_KEY`).
2. Create `.github/workflows/themis.yml`:

```yaml
name: Code review

on:
  pull_request:
    types: [opened, synchronize]
  pull_request_target:
    types: [labeled]

permissions:
  contents: read
  pull-requests: write

jobs:
  review:
    runs-on: ubuntu-latest
    steps:
      - uses: coolamit/themis@latest
        with:
          llm-url: https://openrouter.ai/api/v1
          llm-api-key: ${{ secrets.LLM_API_KEY }}
          llm-model: anthropic/claude-sonnet-5
          # fail-on-severity: high
```

That's it — no checkout step needed (Themis performs its own), no Node.js, no npm. Same-repo PRs get reviewed automatically; apply the `themis-review` label to review any PR (including forks) on demand. The complete example lives at [`examples/themis.yml`](examples/themis.yml).

## Provider recipes

OCR speaks the OpenAI chat-completions protocol by default (it appends `/chat/completions` to `llm-url` when the path is missing) and the Anthropic protocol via `protocol: anthropic`.

### OpenRouter

```yaml
with:
  llm-url: https://openrouter.ai/api/v1
  llm-api-key: ${{ secrets.OPENROUTER_API_KEY }}
  llm-model: anthropic/claude-sonnet-5
```

### Anthropic (direct)

```yaml
with:
  llm-url: https://api.anthropic.com
  llm-api-key: ${{ secrets.ANTHROPIC_API_KEY }}
  llm-model: claude-sonnet-5
  protocol: anthropic
```

### Amazon Bedrock

Bedrock exposes an OpenAI-compatible endpoint that works with a [Bedrock API key](https://docs.aws.amazon.com/bedrock/latest/userguide/api-keys.html) — no SigV4 signing needed:

```yaml
with:
  llm-url: https://bedrock-runtime.us-east-1.amazonaws.com/openai/v1
  llm-api-key: ${{ secrets.BEDROCK_API_KEY }}
  llm-model: us.anthropic.claude-sonnet-4-5-20250929-v1:0
```

Bedrock model IDs are region- and profile-specific. List the IDs valid for **your** key and region before guessing:

```bash
curl -s -H "Authorization: Bearer $BEDROCK_API_KEY" \
  https://bedrock-runtime.us-east-1.amazonaws.com/openai/v1/models | jq -r '.data[].id'
```

For models the OpenAI-compatible endpoint doesn't serve, put a [LiteLLM](https://docs.litellm.ai/) gateway in front of Bedrock and point `llm-url` at the gateway instead.

## Trigger modes

Themis has no `mode` input — your workflow's `on:` block selects the behavior. The 2×2 space:

| | Auto | Manual (label only) |
|---|---|---|
| **Same-repo PRs only** | Mode 1 | Mode 4 |
| **Including fork PRs** | Mode 2 | Mode 3 |

**Mode 1 (recommended, what the quickstart ships)** — auto for same-repo PRs, label override for any PR. Fork label-override requires `pull_request_target` because plain `pull_request` events never receive secrets on forks:

```yaml
on:
  pull_request:
    types: [opened, synchronize]
  pull_request_target:
    types: [labeled]
```

**Mode 2 — auto including forks.** Be aware of the billing exposure: any fork PR triggers a paid review the moment it's opened or pushed to. Your repo, your call.

```yaml
on:
  pull_request_target:
    types: [opened, synchronize]
```

**Mode 3 — manual only, including forks.** Nothing runs until someone with write access applies the label.

```yaml
on:
  pull_request_target:
    types: [labeled]
```

**Mode 4 — manual only, same-repo PRs only.** Forks are structurally excluded by GitHub (no secrets on `pull_request` fork events).

```yaml
on:
  pull_request:
    types: [labeled]
```

> **Mode 4 known limitation:** if someone labels a *fork* PR in this mode, the workflow token is read-only, so Themis cannot remove the label — it posts a "review skipped" notice to the job summary and the label sits there. Harmless; it's GitHub's restriction, not Themis's.

**Label flow:** only the configured `review-label` triggers anything (other labels are a silent no-op), and the labeler must have write, maintain, or admin permission — users with triage permission can apply labels, so the label alone is never trusted. One label application = one review; the label is removed when the run finishes, so re-apply it to re-review. Stick to ASCII label names: the case-insensitive match folds ASCII only, so a `review-label` differing from the repo's label in non-ASCII case alone will silently never trigger.

## Inputs

### The ones you'll actually use

| Input | Required | Default | Description |
|---|---|---|---|
| `llm-url` | yes | — | Provider base URL. OCR appends `/chat/completions` if the path is missing. |
| `llm-api-key` | yes | — | LLM API key, from repository secrets. |
| `llm-model` | yes | — | Model ID exactly as the provider names it. |
| `github-token` | no | `${{ github.token }}` | Token for posting comments; needs `pull-requests: write`. |
| `language` | no | `English` | Language OCR writes review comments in. |
| `fail-on-severity` | no | `''` (off) | `critical`, `high`, `medium`, or `low` — severity gate threshold. Empty = report-only. |
| `max-comments` | no | `25` | Inline comment budget per run; the rest go to one overflow summary. |
| `max-critical-comments` | no | `50` | Critical findings may exceed `max-comments`, up to this cap. |
| `enable-thinking` | no | unset | `'true'`/`'false'` sends `{"thinking":{"type":...}}` via `llm.extra_body`; unset sends nothing. See [Thinking](#thinking--reasoning-effort). |
| `review-label` | no | `themis-review` | Label name for the manual trigger mode. |

### Advanced (you probably don't need these)

| Input | Default | Description |
|---|---|---|
| `llm-extra-body` | `''` | Raw JSON merged into every LLM request body. **Wins over `enable-thinking`.** |
| `protocol` | `''` | `anthropic`, `openai`, or `openai-responses`; unset uses the OpenAI protocol. |
| `auth-header` | `''` | Anthropic auth header name (`x-api-key` vs `authorization`). |
| `extra-headers` | `''` | Custom HTTP headers for LLM requests, as `K=V,K=V`. |
| `llm-timeout` | `''` | Per-request LLM timeout in seconds. |
| `review-concurrency` | `''` | OCR review concurrency. |
| `rule` | `''` | Repo-relative path to a custom OCR rules JSON file. |
| `exclude` | `''` | File patterns to exclude from review. |
| `background` | `''` | Extra context text for the reviewer. |
| `max-tokens-budget` | `''` | Token budget cap for the run; `budget_exceeded` surfaces in OCR's output when hit. |
| `ocr-version` | `latest` | OCR release to install. Pin a version with a recorded checksum (e.g. `1.8.5`) for a verified install — recommended for production. The 3 newest OCR releases are supported; older ones install with an unsupported warning. |

## Thinking / reasoning effort

`enable-thinking: 'true'` or `'false'` sends `{"thinking": {"type": "enabled"}}` (or `"disabled"`) with every LLM request via OCR's `llm.extra_body`. Leaving it unset sends nothing — the only universally safe default.

> **Warning:** this `thinking` dialect is understood by the GLM/DashScope model family and most gateways, but strict OpenAI or Anthropic-direct APIs may reject it with a 400. On those providers, leave `enable-thinking` unset and use `llm-extra-body` with the provider's own dialect instead.

Copy-paste `llm-extra-body` values per provider (raw JSON; it wins over `enable-thinking`):

```yaml
# Anthropic direct — adaptive thinking (current Claude models)
llm-extra-body: '{"thinking": {"type": "adaptive"}}'

# OpenAI reasoning models
llm-extra-body: '{"reasoning_effort": "high"}'

# OpenRouter (normalizes across vendors)
llm-extra-body: '{"reasoning": {"effort": "high"}}'

# GLM / DashScope family
llm-extra-body: '{"thinking": {"type": "enabled"}}'
```

## Severity gate

Set `fail-on-severity` and any **new** finding at or above the threshold fails the job with exit code 2 (distinct from operational failures, which exit 1). The red check does the reporting; branch protection does the blocking. Findings with no or unknown severity never trip the gate, and findings that were already posted on a previous push don't re-trip it.

> **Caveat:** severity is LLM-assigned — the gate is only as calibrated as the model; poor models give poor results. Default off is deliberate: build trust before granting veto power.

Findings alone never fail the run otherwise — a red check on every PR that gets comments trains people to ignore CI.

## Comment budgeting & overflow

Findings are posted as inline review comments, prefixed by severity: 🔴 critical · 🟠 high · 🟡 medium · 🔵 low · ⚪ unspecified. When OCR proposes a fix, the comment carries a GitHub `suggestion` block you can apply with one click — but only when the flagged code still matches the PR head, so a stale suggestion can never apply a wrong patch.

Per run, at most `max-comments` (default 25) findings go inline, filled in severity order. Critical findings are exempt from that budget: they always go inline, up to `max-critical-comments` (default 50). Everything that doesn't fit — plus any comment GitHub rejects because its line isn't in the diff — lands in **one overflow summary comment** with links into the diff. Nothing is ever dropped. If a run produces no new findings, Themis posts nothing at all.

## Deduplication — no spam on every push

Themis reviews the full diff on every push, but it won't repost findings it has already made. Each comment carries an invisible fingerprint of *what the finding is about* — the file, the flagged code (whitespace-normalized), and the category — rather than line numbers or the LLM's wording. So a finding stays recognized when later pushes shift its line number, when the LLM rewords the same complaint, or when indentation churns; it re-posts only when the flagged code itself actually changed. Comments whose code was fixed are collapsed as "outdated" by GitHub's built-in behavior.

## Security

- The `pull_request_target` modes never execute fork code: Themis checks out the **trusted base branch**, and the PR head is fetched as git objects only — never materialized into the working tree. OCR reads the diff from git objects.
- Label-triggered runs are fail-closed: the labeler must have write/maintain/admin permission (triage can apply labels, so the label alone is never trusted), and any permission-API failure counts as a denial.
- Residual risk: prompt injection via diff content. A malicious diff can try to steer the reviewer; the blast radius is misleading review comments, not code execution or secret exfiltration.
- Recommend pinning Themis by commit SHA (see below).

## Versioning & referencing Themis

- **`@latest`** — a moving tag updated on every release; set-and-forget. Caveat: it follows breaking releases too.
- **`@v0.1.0`** (exact tag) — for stability; the action code and its `themis-publish` binary are both fixed, and the binary is checksum-verified against that release. Dependabot's `github-actions` ecosystem can propose reviewed updates for pinned refs.
- A **commit SHA** pins the action code itself with the strongest guarantee, with one caveat: only version tags map to a specific binary release, so a SHA-pinned Themis still downloads the **latest** `themis-publish` binary, unverified. For a fully pinned setup, prefer an exact version tag (or the SHA a version tag points at, which resolves the same way).
- **`@master`** — discouraged. It tracks unreleased commits whose `themis-publish` binaries may not exist yet, so runs can fail at the install step.

## Supply chain

- **Zero external Go dependencies** — `themis-publish` is built from the standard library alone; `go.sum` is empty and stays that way.
- Releases ship the static linux/amd64 binary plus a SHA-256 checksums file, built by CI from a commit verified to be on `master`.
- **OCR support policy:** the 3 newest OCR releases are officially supported; the window is resolved live from OCR's releases API at install time. Older versions still install, with a workflow warning — they may work, but no support is promised.
- Any install (pinned or `latest` after resolution) whose version has a hash recorded in this repo (`scripts/ocr-checksums.txt`) is checksum-verified, and a mismatch fails the install. Versions without a recorded hash install unverified with a workflow warning. Pin a recorded version (e.g. `1.8.5`) for production.
- When Themis is referenced by a version tag, the `themis-publish` binary is checksum-verified against that release's `checksums.txt`; branch and SHA refs fetch the latest release binary unverified (see Versioning above).

## Exit codes

| Code | Meaning |
|---|---|
| 0 | Review published (with or without findings), or clean intentional skip (e.g. fork PR without secrets). |
| 1 | Operational failure: bad configuration, LLM connectivity failure, OCR failed, or publish API failure. |
| 2 | Severity gate tripped — everything else succeeded. |

## Roadmap (v2)

- Deterministic stale-comment resolution: resolve threads whose flagged code no longer exists in the head (file content as the truth source, never LLM re-reporting).
- Sticky summary comment (update-in-place).
- Auto PR-context: pass the PR title/description to the reviewer via `--background`.
- Incremental review on `synchronize` (diff from the previous head instead of the merge base).
- linux/arm64 binaries.
- Dogfooding workflow (deferred — cost undecided; possible middle path: label-gated with a cheap model).
- Category/severity routing to the summary, upstream-style.
- Upstreaming the fingerprint dedupe to OCR's JS helper as a contribution.

## License

[Apache-2.0](LICENSE)
