#!/usr/bin/env bash
# Guard for label-triggered runs. Two checks, fail-closed:
#   1. Only the configured review label triggers anything; other labels
#      are a silent no-op.
#   2. The labeler must have write access. Users with triage permission
#      can apply labels, so the label alone is never trusted; the
#      permission API is the authority. Its legacy "permission" field
#      folds maintain into write and triage into read — exactly the
#      split we want — and the case below still accepts a literal
#      "maintain" as belt and suspenders should the API ever return
#      raw role names.
#
# Inputs (env): EVENT_LABEL, REVIEW_LABEL, SENDER, REPO, GITHUB_TOKEN
# Outputs ($GITHUB_OUTPUT):
#   label-match=true|false  the applied label is the review label
#                           (case-insensitive — GitHub label names are
#                           case-insensitively unique)
#   proceed=true|false      run the review
set -euo pipefail

EVENT_LABEL="${EVENT_LABEL:-}"
REVIEW_LABEL="${REVIEW_LABEL:?REVIEW_LABEL is required}"
SENDER="${SENDER:-}"
REPO="${REPO:?REPO is required}"
GITHUB_TOKEN="${GITHUB_TOKEN:?GITHUB_TOKEN is required}"
API_URL="${GITHUB_API_URL:-https://api.github.com}"
OUT="${GITHUB_OUTPUT:-/dev/null}"
SUMMARY="${GITHUB_STEP_SUMMARY:-/dev/null}"

# tr, not ${var,,}: the latter needs bash 4+ and this script also runs
# under macOS's bash 3.2 in the local test suite.
event_lc="$(printf '%s' "$EVENT_LABEL" | tr '[:upper:]' '[:lower:]')"
review_lc="$(printf '%s' "$REVIEW_LABEL" | tr '[:upper:]' '[:lower:]')"

if [ "$event_lc" != "$review_lc" ]; then
  {
    echo "label-match=false"
    echo "proceed=false"
  } >> "$OUT"
  exit 0
fi
echo "label-match=true" >> "$OUT"

# Any API failure leaves permission empty and lands in the denial branch.
permission="$(curl -fsSL --connect-timeout 10 --max-time 30 \
  -H "Authorization: Bearer ${GITHUB_TOKEN}" \
  -H "Accept: application/vnd.github+json" \
  "${API_URL}/repos/${REPO}/collaborators/${SENDER}/permission" \
  | jq -r '.permission // empty')" || permission=""

case "$permission" in
  admin|write|maintain)
    echo "proceed=true" >> "$OUT"
    echo "Label '${REVIEW_LABEL}' applied by ${SENDER} (permission: ${permission}); running review."
    ;;
  *)
    {
      echo "### Themis review skipped"
      echo ""
      echo "The \`${REVIEW_LABEL}\` label was applied by \`${SENDER}\`, whose repository permission (\`${permission:-unknown}\`) is below write. Ask a user with write access to apply the label."
    } >> "$SUMMARY"
    echo "proceed=false" >> "$OUT"
    echo "Label applied by ${SENDER} with insufficient permission (${permission:-unknown}); skipping review."
    ;;
esac
