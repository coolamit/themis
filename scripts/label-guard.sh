#!/usr/bin/env bash
# Guard for label-triggered runs. Two checks, fail-closed:
#   1. Only the configured review label triggers anything; other labels
#      are a silent no-op.
#   2. The labeler must have write access. Users with triage permission
#      can apply labels, so the label alone is never trusted; the
#      permission API is the authority. (Its legacy "permission" field
#      reports maintain as write and triage as read, which is exactly
#      the split we want.)
#
# Inputs (env): EVENT_LABEL, REVIEW_LABEL, SENDER, REPO, GITHUB_TOKEN
# Outputs ($GITHUB_OUTPUT): proceed=true|false
set -euo pipefail

EVENT_LABEL="${EVENT_LABEL:-}"
REVIEW_LABEL="${REVIEW_LABEL:?REVIEW_LABEL is required}"
SENDER="${SENDER:-}"
REPO="${REPO:?REPO is required}"
GITHUB_TOKEN="${GITHUB_TOKEN:?GITHUB_TOKEN is required}"
API_URL="${GITHUB_API_URL:-https://api.github.com}"
OUT="${GITHUB_OUTPUT:-/dev/null}"
SUMMARY="${GITHUB_STEP_SUMMARY:-/dev/null}"

if [ "$EVENT_LABEL" != "$REVIEW_LABEL" ]; then
  echo "proceed=false" >> "$OUT"
  exit 0
fi

# Any API failure leaves permission empty and lands in the denial branch.
permission="$(curl -fsSL \
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
