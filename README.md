# Themis

GitHub Action for AI powered Code Reviews via [Open Code Review (OCR)](https://github.com/alibaba/open-code-review).

OpenAI / Anthropic / OpenRouter / Bedrock compatible - use whichever inference provider you want (as long as its supported by OCR). Bring your own model & API Key.

This project is named after the Greek goddess of fair judgement. **Themis** publishes OCR's findings as inline PR review comments - with GitHub suggestion blocks, content-fingerprint deduplication, comment budgeting, `.themisignore` review exclusions and an optional severity based merge gate.

- [Quickstart](#quickstart)
- [Different from the default OCR action](#different-from-the-default-ocr-action)
- [Provider Recipes](#provider-recipes)
- [Trigger Modes](#trigger-modes)
- [Inputs](#inputs)
- [Thinking / Reasoning effort](#thinking--reasoning-effort)
- [Severity Gate](#severity-gate)
- [Comment budgeting & overflow](#comment-budgeting--overflow)
- [Ignoring files using `.themisignore`](#ignoring-files-using-themisignore)
- [Deduplication: No spam on every push](#deduplication-no-spam-on-every-push)
- [Security](#security)
- [Versioning & referencing Themis](#versioning--referencing-themis)
- [Supply Chain](#supply-chain)
- [Exit codes](#exit-codes)
- [Roadmap](#roadmap)
- [License](#license)

## Quickstart

1. Add an LLM API key to your repository secrets (e.g. `THEMIS_LLM_API_KEY`).
2. Create `.github/workflows/themis.yml`:

```yaml
name: Code Review

# Pushes supersede the stale in-flight auto review; label runs are never
# canceled — Themis skips them itself when a review is already running.
concurrency:
  group: themis-${{ github.event_name }}-${{ github.event_name == 'pull_request' && github.event.pull_request.number || github.run_id }}
  cancel-in-progress: true

on:
  pull_request:
    types: [opened, synchronize]
  pull_request_target:
    types: [labeled]

permissions:
  contents: read
  pull-requests: write
  actions: read # lets Themis skip a label run when a review is already in progress

jobs:
  review:
    runs-on: ubuntu-latest
    timeout-minutes: 30
    steps:
      - uses: coolamit/themis@latest
        with:
          llm-url: https://openrouter.ai/api/v1
          llm-api-key: ${{ secrets.THEMIS_LLM_API_KEY }}
          llm-model: anthropic/claude-sonnet-5
          # fail-on-severity: high
```

That's it, no checkout step needed (**Themis** performs its own), no Node.js, no `npm`. Same repo PRs get reviewed automatically; apply the `themis-review` label to review any PR (including forks) on demand. The complete example lives at [`examples/themis.yml`](examples/themis.yml).

**Themis** needs a linux/amd64 runner (both binaries are built for it; `ubuntu-latest` qualifies). linux/arm64 is on the [roadmap](#roadmap).

## Different from the default OCR action

OCR ships its own official composite action (the `action.yml` at the root of `alibaba/open-code-review`). **Themis** is not a competitor - it exists because I did not quite agree with how it implemented things and I've made it available as OSS in case it's useful to others. If you want the setup maintained by the OCR authors themselves, use theirs. Use **Themis** if the differences below matter to you:

| | Upstream action | Themis |
|---|---|---|
| Runtime | Node.js + `npm install` every run; posting logic in a JS helper | **Two static binaries** created in Golang, no runtime. OCR itself is created in Golang. |
| OCR install | npm package, pinnable via a version spec | Release binary download, pinnable via `ocr-version`; the **3 newest releases** are the supported window, resolved live from OCR's releases API |
| Dedupe on re-push | Positional line-range overlap (IoU), **off by default** | **Content fingerprint + positional fallback, always on**. The fingerprint survives line drift, LLM rewording and indentation churn but can't recognize a finding the LLM re-categorized or re-snippeted; those are caught positionally and **demoted to the summary — relocated, never suppressed** |
| Dedupe integrity | Silent suppression on position match — a genuinely new finding on the same lines is dropped | Fingerprint markers are honored **only from Themis's own posting identity**, so a PR author can't plant markers to suppress findings or bypass the gate; a positional match only ever changes *where* a finding is posted, so nothing is hidden and the gate always sees every new finding |
| Merge gating | No | **`fail-on-severity`** → exit 2, distinct from operational failure |
| Comment budgeting | None — batching only (≤50 per request), plus optional severity/category routing to the summary | **`max-comments`** cap; critical findings filled first, up to `max-critical-comments` |
| Budget overflow | n/a (no budget) | Over-budget and GitHub-rejected findings fold into a **chunked overflow summary**, fingerprinted for dedupe — nothing is ever dropped |
| Suggestions | Posted whenever OCR emits one | Suggestion block emitted **only when the flagged code still matches the PR head** — a stale suggestion can't apply a wrong patch |
| Manual trigger | Left to your workflow (`base_ref`/`head_sha` inputs); no permission guard | **`review-label`** trigger with a permission check on the labeler (write+), fail-closed, automatic label cleanup |
| Credential preflight | None — a missing key surfaces as a review error | Fork/Dependabot PRs without secrets **skip green** with a notice; real misconfiguration hard-fails; **`ocr llm test`** gates before any paid review |
| Excluding files | Not exposed by the action | **`.themisignore`** with true gitignore semantics and anti-footgun guardrails, plus an `exclude` glob input |
| Quiet PRs | Posts/updates a summary comment every run, including "looks good" | **Posts nothing when there's nothing new**; docs-only and all-ignored PRs end green with a job-summary notice |
| Supply chain | npm install at run time | Static binaries; `themis-publish` **checksum-verified** on `v*` tags; releases cut only from commits on `master` |

## Provider Recipes

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

Or, if you have the AWS CLI configured (it authenticates with your AWS credentials, not the Bedrock API key):

```bash
# cross-region inference profile IDs (the us./eu./apac. prefixed ones, like the example above)
aws bedrock list-inference-profiles --region us-east-1 \
  --query 'inferenceProfileSummaries[].inferenceProfileId' --output text | tr '\t' '\n'

# plain foundation model IDs
aws bedrock list-foundation-models --region us-east-1 \
  --query 'modelSummaries[].modelId' --output text | tr '\t' '\n'
```

For models the OpenAI-compatible endpoint doesn't serve, put a [LiteLLM](https://docs.litellm.ai/) gateway in front of Bedrock and point `llm-url` at the gateway instead.

## Trigger Modes

**Themis** has no `mode` input — your workflow's `on:` block selects the behavior. The 2x2 space:

| | Auto | Manual (label only) |
|---|---|---|
| **Same-repo PRs only** | Mode 1 | Mode 4 |
| **Including fork PRs** | Mode 2 | Mode 3 |

Mode 1 actually straddles both rows: same-repo PRs are reviewed automatically **and** fork PRs can be reviewed via the label. Pick Mode 2 only if you want forks reviewed *without* anyone applying a label.

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

> **Mode 4 known limitation:** if someone labels a *fork* PR in this mode, the workflow token is read-only, so **Themis** cannot remove the label — it posts a "review skipped" notice to the job summary and the label sits there. Harmless; it's GitHub's restriction, not **Themis**'s.

**Label flow:** only the configured `review-label` triggers anything (other labels are a silent no-op), and the labeler must have write, maintain, or admin permission — users with triage permission can apply labels, so the label alone is never trusted. One label application = one review; the label is removed when the run finishes, so re-apply it to re-review. Applying the label while a review of the PR is **already in progress** is a no-op: the label is removed with a job-summary notice and no second review starts — nothing changed, so there's nothing new to review (this check needs `actions: read` in the workflow's permissions; without it, it is skipped and the duplicate review simply runs). Stick to ASCII label names: the case-insensitive match folds ASCII only, so a `review-label` differing from the repo's label in non-ASCII case alone will silently never trigger.

**Dependabot:** under a plain `pull_request` event, Dependabot PRs receive no repository secrets (GitHub's rule, same as forks), so **Themis** skips them green with a job-summary notice instead of failing. Use the label trigger to review one on demand.

## Inputs

### The ones you'll actually use

| Input | Required | Default | Description |
|---|---|---|---|
| `llm-url` | yes | — | Provider base URL. OCR appends `/chat/completions` if the path is missing. |
| `llm-api-key` | yes | — | LLM API key, store in repository secrets. |
| `llm-model` | yes | — | Model ID exactly as the provider names it. |
| `github-token` | no | `${{ github.token }}` | Token for posting comments; needs `pull-requests: write`. |
| `language` | no | `English` | Language OCR writes review comments in. |
| `fail-on-severity` | no | `''` (off) | `critical`, `high`, `medium`, or `low` — severity gate threshold. Empty = report-only. |
| `max-comments` | no | `25` | Inline comment budget per run; the rest go to one overflow summary. |
| `max-critical-comments` | no | `50` | Critical findings may exceed `max-comments`, up to this cap. |
| `enable-thinking` | no | `''` (unset) | `'true'`/`'false'` sends `{"thinking":{"type":...}}` via `llm.extra_body`; unset sends nothing. See [Thinking](#thinking--reasoning-effort). |
| `review-label` | no | `themis-review` | Label name for the manual trigger mode. |

### Advanced (you probably don't need these)

| Input | Default | Description |
|---|---|---|
| `llm-extra-body` | `''` | Raw JSON merged into every LLM request body. **Wins over `enable-thinking`.** |
| `protocol` | `''` | `anthropic`, `openai`, or `openai-responses`; unset uses the OpenAI protocol. |
| `auth-header` | `''` | Anthropic auth header name (`x-api-key` vs `authorization`). |
| `extra-headers` | `''` | Custom HTTP headers for LLM requests, as `K=V,K=V`. |
| `llm-timeout` | `''` | Per-request LLM timeout in seconds. |
| `review-concurrency` | `''` | Number of files OCR reviews in parallel. Unset uses OCR's built-in default of 8. Lower it if you hit LLM rate limits. |
| `rule` | `''` | Repo-relative path to a custom OCR rules JSON file. |
| `exclude` | `''` | File patterns to exclude from review — comma-separated globs in OCR's doublestar dialect. For gitignore semantics use [`.themisignore`](#ignoring-files-using-themisignore); both compose. |
| `background` | `''` | Extra context text for the reviewer. |
| `max-tokens-budget` | `''` | Token budget cap for the run; `budget_exceeded` surfaces in OCR's output when hit. |
| `ocr-version` | `latest` | OCR release to install, `latest` or a pinned version (e.g. `1.8.5`). The 3 newest OCR releases are supported; older ones install with an unsupported warning. |

## Thinking / Reasoning effort

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

## Severity Gate

Set `fail-on-severity` and any **new** finding at or above the threshold fails the job with exit code 2 (distinct from operational failures, which exit 1). The red check does the reporting; branch protection does the blocking. Findings with no or unknown severity never trip the gate, and findings that were already posted on a previous push don't re-trip it.

The gate evaluates every new finding, including ones that only appear in the overflow summary — going over the comment budget can't smuggle a critical past it. Reviews are always submitted as `COMMENT`, never `REQUEST_CHANGES`, so protect the branch by requiring the **Themis** check run, not by review state.

> **Caveat:** severity is LLM-assigned — the gate is only as calibrated as the model; poor models give poor results. Default off is deliberate: build trust before granting veto power.

Findings alone never fail the run otherwise; a red check on every PR that gets comments trains people to ignore CI.

## Comment budgeting & overflow

Findings are posted as inline review comments, headed by severity and category (e.g. 🔴 **critical · bug**):
- 🔴 critical
- 🟠 high
- 🟡 medium
- 🔵 low
- ⚪ unspecified

When OCR proposes a fix, the comment carries a GitHub `suggestion` block you can apply with one click but only when the flagged code still matches the PR head (compared line by line; only trailing whitespace is forgiven, indentation counts), so a stale suggestion can never apply a wrong patch. When it doesn't match, the comment is still posted — just without the suggestion.

Per run, at most `max-comments` (default 25) findings go inline. Critical findings are filled first — always inline, up to `max-critical-comments` (default 50), even when that alone exceeds `max-comments`; non-critical findings then take whatever budget remains, in severity order. Everything that doesn't fit, plus any comment GitHub rejects because its line isn't in the diff, lands in an **overflow summary** with links into the diff — split into `(part n/N)` comments when it outgrows GitHub's comment-size cap. Nothing is ever dropped. If a run produces no new findings, **Themis** posts nothing at all.

## Ignoring files using `.themisignore`

Add a `.themisignore` file at the repository root to exclude files and directories from review. It supports full `.gitignore` pattern syntax, the matching is done by git itself, so wildcards (`*.min.js` matches at any depth), directory patterns (`vendor/`), root anchoring (`/build/`), `**`, `!` negation, and `#` comments all behave exactly as they do in a `.gitignore`:

```gitignore
# generated and vendored code
vendor/
dist/
*.min.js
# but do review our own bundler config
!dist/bundler.config.js
```

### `.themisignore` details

- **The PR head's version governs.** Changes to `.themisignore` apply immediately, including in the PR that introduces or edits it. The file is read from git objects only, the PR head is never checked out.
- **Two things fail the run loudly (exit 1):** a bare catch-all entry (`*`, `**`, `/*`, `/**`) and any combination of entries that covers every file in the repository. Excluding everything is not a configuration **Themis** accepts.
- **Matching is case-sensitive**, regardless of the runner's filesystem. And paths containing a comma can't be excluded — OCR's exclude list is comma-separated — so such a file gets a workflow warning and is reviewed anyway.
- **A PR touching only ignored paths skips cleanly.** When every changed file is ignored, the run ends green with a job-summary notice instead of invoking OCR — no LLM cost, no red ✗.
- **Security note for `fail-on-severity` users:** because the head governs, a PR author can add `.themisignore` entries for specific files they also modify, taking those files out of review and out of the gate. Watch `.themisignore` changes in PRs the same way you'd watch workflow changes.
- The `exclude` input still works and composes with `.themisignore` (both apply). Note they speak different dialects: `exclude` passes patterns straight to OCR's glob matcher, while `.themisignore` gets true gitignore semantics.

## Deduplication: No spam on every push

**Themis** reviews the full PR range (merge-base to head) on every push deliberately, not just the latest changeset. A later push can make earlier code buggy without touching it again (a changed function contract, a removed guard), so the whole PR is re-examined in the context of its newest state; a delta only review would never look back.

The token cost of re-reviewing is capped by `max-tokens-budget`, and the noise cost is contained by two layers of dedupe.

**Layer 1 — content fingerprint (suppresses).** Each comment carries an invisible fingerprint of *what the finding is about* — the file, the flagged code (whitespace normalized) and the category — rather than line numbers or the LLM's wording. A finding whose fingerprint is already on the PR is not posted again: it stays recognized when later commits shift its line number, when the LLM rewords the same complaint or when indentation churns, and re-posts only when the flagged code itself actually changed. Suppression here is safe by construction: a suppressed finding is provably already visible on the PR. Comments whose code was fixed are collapsed as "outdated" by GitHub's built-in behavior. Overflow-summary entries carry fingerprints too, so they dedupe across runs exactly like inline comments.

**Layer 2 — positional fallback (demotes, never suppresses).** The fingerprint's blind spot is the LLM itself: on a re-review of unchanged code, OCR can re-categorize a finding or capture a differently-sized snippet of the same flagged code, minting a fresh fingerprint for a complaint it already made. When a new-fp finding's line range overlaps a comment **Themis** already has on the same file, the finding is *demoted*: posted in the summary comment, labeled as a possible repeat, instead of inline on the diff. Nothing is ever dropped — a demoted finding is still published, still fingerprinted for future dedupe, and still counts toward `fail-on-severity`. The worst a wrong positional match can do is move a genuinely new finding into the summary; it can never hide one or weaken the gate. (This is the deliberate difference from positional-only dedupe schemes, where a position match silently discards the finding.)

Edge cases worth knowing:

- **Fingerprints and positions are only trusted from Themis's own posting identity** (`github-actions[bot]`, or whatever login your `github-token` resolves to) — and positions additionally require the comment to carry a Themis fingerprint marker, since other tooling also posts as `github-actions[bot]`. Fingerprints are computable from public PR content, so honoring anyone's would let a PR author pre-post markers to suppress findings — and with them, the severity gate. Consequence for custom tokens: if you supply a PAT or App token whose identity can't be resolved via `GET /user`, previously posted comments stop being recognized and findings repost on every push.
- **Findings without a code snippet** fall back to a line-range identity, so those (rare) findings can repost when later commits shift their lines.
- **Residual duplicates are possible.** A finding about *missing* code (no timeout, no concurrency group) has no natural anchor, and OCR may attach it to a different line on each run. When the anchors don't overlap, neither layer can safely connect the two — a proximity window wide enough to catch re-anchoring has been observed to mis-match genuinely distinct findings a few lines apart — so such a finding can occasionally repost. Outdated comments release their positions on purpose: once the code at a comment's lines changes, a new finding there is treated as new.

## Security

- The `pull_request_target` modes never execute fork code: **Themis** checks out the **trusted base branch**, and the PR head is fetched as git objects only — never materialized into the working tree. OCR reads the diff from git objects.
- Label triggered runs are fail closed: the labeler must have write/maintain/admin permission (triage can apply labels, so the label alone is never trusted) and any permission API failure counts as a denial.
- Residual risk: prompt injection via diff content. A malicious diff can try to steer the reviewer; the blast radius is misleading review comments, not code execution or secret exfiltration.
- Recommend pinning **Themis** to an exact version tag (e.g. `@v0.1.0`) — that's the ref form whose binary is checksum verified (see [Versioning](#versioning--referencing-themis)).

## Versioning & referencing Themis

- **`@latest`** — a moving tag updated on every release; set-and-forget. Caveats: it follows breaking releases too, and like every non-`v*` ref it fetches the newest `themis-publish` binary unverified — only exact version tags get a checksum-verified binary.
- **`@v0.1.0`** (exact tag) — for stability; the action code and its `themis-publish` binary are both fixed and the binary is checksum verified against that release. Dependabot's `github-actions` ecosystem can propose reviewed updates for pinned refs.
- A **commit SHA** pins the action code itself with the strongest guarantee, with one caveat: only version tags map to a specific binary release, so a SHA pinned **Themis** still downloads the **latest** `themis-publish` binary, unverified. For a fully pinned setup, prefer an exact version tag (or the SHA a version tag points at, which resolves the same way).
- **`@master`** — discouraged. It tracks unreleased commits whose `themis-publish` binaries may not exist yet, so runs can fail at the install step. `themis-publish` binaries are compiled only when a release is created.

## Supply Chain

- **Zero external Go dependencies:** `themis-publish` is built from the standard library alone — there isn't even a `go.sum`, and there never will be.
- Releases ship the static linux/amd64 binary plus a SHA-256 checksums file, built by CI from a commit verified to be on `master` branch.
- **OCR support policy:** the 3 newest OCR releases are officially supported; the window is resolved live from OCR's releases API at install time. Older versions still install, with a workflow warning — they may work or they may not.
- The OCR binary itself installs **without checksum verification** — deliberately: **Themis** doesn't maintain hashes for another project's releases, and a hash recorded after the fact protects nothing.
- When **Themis** is referenced by an exact `v*` version tag, the `themis-publish` binary is checksum verified against that release's `checksums.txt`; every other ref (`@latest`, branches, SHAs) fetches the latest release binary unverified (see Versioning above).

## Exit codes

These are the job's outcomes (`themis-publish` itself uses the same three codes):

| Code | Meaning |
|---|---|
| 0 | Review published (with or without findings), or clean skip: fork or Dependabot PR without secrets, every changed file ignored by `.themisignore`, nothing reviewable changed (e.g. a docs-only PR — OCR reviews code and config files, not `.md`/`.txt`), or a label event that shouldn't trigger (wrong label, a labeler below write permission, or a review of the PR already in progress). Skips end green with a job-summary notice. |
| 1 | Operational failure: bad configuration, unresolvable PR refs, a `.themisignore` guardrail violation, LLM connectivity failure (`ocr llm test`), OCR failed or returned an unrecognized status (usually schema drift in a new OCR release — pin `ocr-version`), or publish API failure. |
| 2 | Severity gate tripped — everything else succeeded. |

## Roadmap

- Deterministic stale comment resolution: resolve threads whose flagged code no longer exists in the head (file content as the truth source, never LLM re-reporting).
- Sticky summary comment (update in place).
- Auto PR context: pass the PR title/description to the reviewer via `--background`.
- Incremental review on `synchronize` (diff from the previous head instead of the merge base). This is on the roadmap but has practical implications on quality/relevance — see [Deduplication](#deduplication-no-spam-on-every-push) for why the full range is reviewed today.
- linux/arm64 binaries - to allow for non default Ubuntu runners.
- Category/severity routing to the summary, upstream style.

## License

[Apache-2.0](LICENSE)
