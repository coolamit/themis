#!/usr/bin/env bash
# Guard for label-triggered runs. Three checks:
#   1. Only the configured review label triggers anything; other labels
#      are a silent no-op. (fail-closed)
#   2. The labeler must have write access. Users with triage permission
#      can apply labels, so the label alone is never trusted; the
#      permission API is the authority. Its legacy "permission" field
#      folds maintain into write and triage into read — exactly the
#      split we want — and the case below still accepts a literal
#      "maintain" as belt and suspenders should the API ever return
#      raw role names. (fail-closed)
#   3. No review of this head may already be running: a second review of
#      unchanged code is pointless, so the run skips and the always-run
#      cleanup removes the label. Unlike 1 and 2 this guards cost, not
#      security, so it FAILS OPEN — any API failure (including the 403
#      when the workflow lacks `actions: read`) skips the check, not the
#      review; the worst case is a duplicate review that dedupe silences.
#
# Inputs (env): EVENT_LABEL, REVIEW_LABEL, SENDER, REPO, GITHUB_TOKEN;
#   RUN_ID + HEAD_SHA enable check 3 (skipped when either is empty).
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
RUN_ID="${RUN_ID:-}"
HEAD_SHA="${HEAD_SHA:-}"
API_URL="${GITHUB_API_URL:-https://api.github.com}"
OUT="${GITHUB_OUTPUT:-/dev/null}"
SUMMARY="${GITHUB_STEP_SUMMARY:-/dev/null}"

# tr, not ${var,,}: the latter needs bash 4+ and this script also runs
# under macOS's bash 3.2 in the local test suite. Known limitation: the
# fold is ASCII-only (no portable alternative exists across bash 3.2 and
# BSD/mawk userlands), so labels differing only in non-ASCII case (e.g.
# Über-review vs über-review) never match — the guard then no-ops, which
# is the safe direction. Use ASCII label names.
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
    echo "Label '${REVIEW_LABEL}' applied by ${SENDER} (permission: ${permission})."
    ;;
  *)
    {
      echo "### Themis review skipped"
      echo ""
      echo "The \`${REVIEW_LABEL}\` label was applied by \`${SENDER}\`, whose repository permission (\`${permission:-unknown}\`) is below write. Ask a user with write access to apply the label."
    } >> "$SUMMARY"
    echo "proceed=false" >> "$OUT"
    echo "Label applied by ${SENDER} with insufficient permission (${permission:-unknown}); skipping review."
    exit 0
    ;;
esac

# Check 3: is another run of this workflow already reviewing this head?
# Queued counts as busy too — it will review the same code momentarily.
busy=""
if [ -n "$RUN_ID" ] && [ -n "$HEAD_SHA" ]; then
  workflow_id="$(curl -fsSL --connect-timeout 10 --max-time 30 \
    -H "Authorization: Bearer ${GITHUB_TOKEN}" \
    -H "Accept: application/vnd.github+json" \
    "${API_URL}/repos/${REPO}/actions/runs/${RUN_ID}" \
    | jq -r '.workflow_id // empty')" || workflow_id=""
  if [ -z "$workflow_id" ]; then
    echo "note: could not resolve the current workflow via ${API_URL} (missing 'actions: read' permission?); skipping the in-progress check"
  else
    for run_status in queued in_progress; do
      busy="$(curl -fsSL --connect-timeout 10 --max-time 30 \
        -H "Authorization: Bearer ${GITHUB_TOKEN}" \
        -H "Accept: application/vnd.github+json" \
        "${API_URL}/repos/${REPO}/actions/workflows/${workflow_id}/runs?status=${run_status}&per_page=100" \
        | jq -r --arg run "$RUN_ID" --arg sha "$HEAD_SHA" \
            '.workflow_runs[]? | select((.id | tostring) != $run and .head_sha == $sha) | .id' \
        | head -n 1)" || busy=""
      [ -n "$busy" ] && break
    done
  fi
fi

if [ -n "$busy" ]; then
  {
    echo "### Themis review skipped"
    echo ""
    echo "The \`${REVIEW_LABEL}\` label was applied while a review of this PR is already in progress (run ${busy}); the label was removed without starting a second review. Re-apply it after the current review finishes if you still want another one."
  } >> "$SUMMARY"
  echo "proceed=false" >> "$OUT"
  echo "A review of this head is already in progress (run ${busy}); skipping and removing the label."
  exit 0
fi

echo "proceed=true" >> "$OUT"
echo "Running review."
