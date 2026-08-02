#!/usr/bin/env bash
# Resolves the commit range for the review: fetches the trusted base and
# the PR head — the head as git objects only, never checked out into the
# working tree — and computes their merge-base.
#
# Inputs (env): BASE_REF, HEAD_SHA, PR_NUMBER
# Outputs ($GITHUB_OUTPUT): merge-base=<sha>, head-sha=<sha>
set -euo pipefail

BASE_REF="${BASE_REF:?BASE_REF is required}"
HEAD_SHA="${HEAD_SHA:?HEAD_SHA is required}"
PR_NUMBER="${PR_NUMBER:?PR_NUMBER is required}"
OUT="${GITHUB_OUTPUT:-/dev/null}"

# Bound network fetches where coreutils timeout exists (GitHub runners);
# fall back to unbounded on systems without it (local macOS dev).
fetch_bounded() {
  if command -v timeout >/dev/null 2>&1; then
    timeout 300 git fetch --no-tags origin "$1"
  else
    git fetch --no-tags origin "$1"
  fi
}

if ! fetch_bounded "$BASE_REF"; then
  echo "::error::could not fetch base ref ${BASE_REF} from origin"
  exit 1
fi

# Fork-safe: refs/pull/N/head lives on the base repository for every PR,
# fork or not. Tolerate a failed fetch — the object may already be local.
if ! fetch_bounded "pull/${PR_NUMBER}/head"; then
  echo "note: could not fetch pull/${PR_NUMBER}/head; checking for existing objects"
fi

if ! git cat-file -e "${HEAD_SHA}^{commit}"; then
  echo "::error::head commit ${HEAD_SHA} is not available after fetching pull/${PR_NUMBER}/head"
  exit 1
fi

if ! MERGE_BASE="$(git merge-base "origin/${BASE_REF}" "$HEAD_SHA")" || [ -z "$MERGE_BASE" ]; then
  echo "::error::could not compute merge-base of origin/${BASE_REF} and ${HEAD_SHA}"
  exit 1
fi

{
  echo "merge-base=${MERGE_BASE}"
  echo "head-sha=${HEAD_SHA}"
} >> "$OUT"
echo "Reviewing ${MERGE_BASE}..${HEAD_SHA} (base ${BASE_REF})"
