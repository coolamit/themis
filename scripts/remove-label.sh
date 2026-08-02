#!/usr/bin/env bash
# Removes the review label from the PR — the always-run cleanup step of
# label-triggered runs, so re-applying the label means re-reviewing.
# Always exits 0: a read-only token (fork PR under a plain pull_request
# event), an already-absent label, or a failing helper must not fail
# the job.
#
# Inputs (env): REVIEW_LABEL, REPO, PR_NUMBER, GITHUB_TOKEN
set -euo pipefail

REVIEW_LABEL="${REVIEW_LABEL:?REVIEW_LABEL is required}"
REPO="${REPO:?REPO is required}"
PR_NUMBER="${PR_NUMBER:?PR_NUMBER is required}"
GITHUB_TOKEN="${GITHUB_TOKEN:-}"
API_URL="${GITHUB_API_URL:-https://api.github.com}"

if [ -z "$GITHUB_TOKEN" ]; then
  echo "No token available; cannot remove label '${REVIEW_LABEL}' from PR #${PR_NUMBER}; continuing"
  exit 0
fi

if ! encoded="$(jq -rn --arg v "$REVIEW_LABEL" '$v|@uri')"; then
  echo "Could not URL-encode label '${REVIEW_LABEL}'; continuing"
  exit 0
fi
if curl -fsSL -X DELETE \
  -H "Authorization: Bearer ${GITHUB_TOKEN}" \
  -H "Accept: application/vnd.github+json" \
  "${API_URL}/repos/${REPO}/issues/${PR_NUMBER}/labels/${encoded}" >/dev/null 2>&1; then
  echo "Removed label '${REVIEW_LABEL}' from PR #${PR_NUMBER}"
else
  echo "Could not remove label '${REVIEW_LABEL}' from PR #${PR_NUMBER} (label absent or read-only token); continuing"
fi
exit 0
