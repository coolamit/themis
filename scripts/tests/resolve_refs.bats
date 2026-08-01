#!/usr/bin/env bats

load helpers

# Builds a real origin repo with a base branch, a PR head behind
# refs/pull/7/head, and a local clone to run the script in.
setup() {
  setup_gh_files
  export GIT_AUTHOR_NAME=t GIT_AUTHOR_EMAIL=t@t GIT_COMMITTER_NAME=t GIT_COMMITTER_EMAIL=t@t

  ORIGIN="$BATS_TEST_TMPDIR/origin.git"
  WORK="$BATS_TEST_TMPDIR/work"
  SEED="$BATS_TEST_TMPDIR/seed"

  git init -q -b master "$SEED"
  (cd "$SEED" && echo base > file.txt && git add . && git commit -qm "base commit")
  git clone -q --bare "$SEED" "$ORIGIN"

  (cd "$SEED" && git checkout -qb pr-branch && echo change > file.txt && git commit -qam "pr commit")
  HEAD_SHA_VALUE="$(cd "$SEED" && git rev-parse HEAD)"
  (cd "$SEED" && git push -q "$ORIGIN" pr-branch:refs/pull/7/head)

  git clone -q "$ORIGIN" "$WORK"
  cd "$WORK"
}

@test "resolve-refs computes the merge base and emits outputs" {
  BASE_REF=master HEAD_SHA="$HEAD_SHA_VALUE" PR_NUMBER=7 \
    run "$SCRIPTS_DIR/resolve-refs.sh"
  [ "$status" -eq 0 ]
  merge_base="$(git rev-parse "origin/master")"
  grep -q "^merge-base=${merge_base}$" "$GITHUB_OUTPUT"
  grep -q "^head-sha=${HEAD_SHA_VALUE}$" "$GITHUB_OUTPUT"
}

@test "resolve-refs fails on a head commit that does not exist" {
  BASE_REF=master HEAD_SHA=0000000000000000000000000000000000000000 PR_NUMBER=7 \
    run "$SCRIPTS_DIR/resolve-refs.sh"
  [ "$status" -ne 0 ]
  [[ "$output" == *"not available"* ]]
}

@test "resolve-refs fails on a missing base ref" {
  BASE_REF=no-such-branch HEAD_SHA="$HEAD_SHA_VALUE" PR_NUMBER=7 \
    run "$SCRIPTS_DIR/resolve-refs.sh"
  [ "$status" -ne 0 ]
}
