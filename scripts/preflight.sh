#!/usr/bin/env bash
# Credential preflight. Distinguishes the two ways credentials can be
# missing: a fork PR under a plain pull_request trigger (secrets are
# structurally unavailable — clean skip, exit 0) versus a misconfigured
# workflow (hard fail, exit 1). Never lets empty values reach OCR.
#
# Inputs (env): LLM_URL, LLM_API_KEY, LLM_MODEL, EVENT_NAME, IS_FORK
# Outputs ($GITHUB_OUTPUT): skip=true|false
set -euo pipefail

LLM_URL="${LLM_URL:-}"
LLM_API_KEY="${LLM_API_KEY:-}"
LLM_MODEL="${LLM_MODEL:-}"
EVENT_NAME="${EVENT_NAME:-}"
IS_FORK="${IS_FORK:-false}"
OUT="${GITHUB_OUTPUT:-/dev/null}"
SUMMARY="${GITHUB_STEP_SUMMARY:-/dev/null}"

if [ -n "$LLM_URL" ] && [ -n "$LLM_API_KEY" ] && [ -n "$LLM_MODEL" ]; then
  echo "skip=false" >> "$OUT"
  exit 0
fi

if [ "$EVENT_NAME" = "pull_request" ] && [ "$IS_FORK" = "true" ]; then
  {
    echo "### Themis review skipped"
    echo ""
    echo "Credentials are unavailable for fork PRs in this configuration: plain \`pull_request\` events never receive secrets on forks. This is expected, not an error. To review fork PRs, see the trigger-mode matrix in the Themis README."
  } >> "$SUMMARY"
  echo "skip=true" >> "$OUT"
  echo "Review skipped: credentials unavailable for fork PRs in this configuration."
  exit 0
fi

echo "::error::Themis is misconfigured: llm-url, llm-api-key, and llm-model are all required. Check the workflow 'with:' block and the repository secrets it references."
exit 1
