#!/usr/bin/env bats

load helpers

setup() {
  setup_gh_files
  export REVIEW_LABEL="themis-review"
  export SENDER="someone"
  export REPO="o/r"
  export GITHUB_TOKEN="test-token"
}

@test "label guard ignores an unrelated label" {
  EVENT_LABEL="documentation" run "$SCRIPTS_DIR/label-guard.sh"
  [ "$status" -eq 0 ]
  grep -q '^label-match=false$' "$GITHUB_OUTPUT"
  grep -q '^proceed=false$' "$GITHUB_OUTPUT"
  [ ! -s "$GITHUB_STEP_SUMMARY" ]
}

@test "label guard matches the review label case-insensitively" {
  echo '{"permission":"write"}' > "$BATS_TEST_TMPDIR/resp"
  export CURL_STUB_BODY="$BATS_TEST_TMPDIR/resp"
  stub_curl
  EVENT_LABEL="Themis-Review" run "$SCRIPTS_DIR/label-guard.sh"
  [ "$status" -eq 0 ]
  grep -q '^label-match=true$' "$GITHUB_OUTPUT"
  grep -q '^proceed=true$' "$GITHUB_OUTPUT"
}

@test "label guard proceeds for a labeler with write permission" {
  echo '{"permission":"write"}' > "$BATS_TEST_TMPDIR/resp"
  export CURL_STUB_BODY="$BATS_TEST_TMPDIR/resp"
  stub_curl
  EVENT_LABEL="themis-review" run "$SCRIPTS_DIR/label-guard.sh"
  [ "$status" -eq 0 ]
  grep -q '^proceed=true$' "$GITHUB_OUTPUT"
}

@test "label guard proceeds for an admin labeler" {
  echo '{"permission":"admin"}' > "$BATS_TEST_TMPDIR/resp"
  export CURL_STUB_BODY="$BATS_TEST_TMPDIR/resp"
  stub_curl
  EVENT_LABEL="themis-review" run "$SCRIPTS_DIR/label-guard.sh"
  [ "$status" -eq 0 ]
  grep -q '^proceed=true$' "$GITHUB_OUTPUT"
}

@test "label guard proceeds for a labeler with maintain permission" {
  echo '{"permission":"maintain"}' > "$BATS_TEST_TMPDIR/resp"
  export CURL_STUB_BODY="$BATS_TEST_TMPDIR/resp"
  stub_curl
  EVENT_LABEL="themis-review" run "$SCRIPTS_DIR/label-guard.sh"
  [ "$status" -eq 0 ]
  grep -q '^proceed=true$' "$GITHUB_OUTPUT"
}

@test "label guard denies a labeler with read permission" {
  echo '{"permission":"read"}' > "$BATS_TEST_TMPDIR/resp"
  export CURL_STUB_BODY="$BATS_TEST_TMPDIR/resp"
  stub_curl
  EVENT_LABEL="themis-review" run "$SCRIPTS_DIR/label-guard.sh"
  [ "$status" -eq 0 ]
  grep -q '^proceed=false$' "$GITHUB_OUTPUT"
  grep -q 'below write' "$GITHUB_STEP_SUMMARY"
}

@test "label guard fails closed when the permission API errors" {
  export CURL_STUB_EXIT=22
  stub_curl
  EVENT_LABEL="themis-review" run "$SCRIPTS_DIR/label-guard.sh"
  [ "$status" -eq 0 ]
  grep -q '^proceed=false$' "$GITHUB_OUTPUT"
}

# In-progress check (fail-open). Shared fixture: an authorized labeler,
# the current run is 111, the PR head is "headsha".
setup_busy_check() {
  echo '{"permission":"write"}' > "$BATS_TEST_TMPDIR/perm"
  export CURL_STUB_BODY_PERMISSION="$BATS_TEST_TMPDIR/perm"
  echo '{"workflow_id":77}' > "$BATS_TEST_TMPDIR/wfrun"
  export CURL_STUB_BODY_RUN="$BATS_TEST_TMPDIR/wfrun"
  echo '{"workflow_runs":[]}' > "$BATS_TEST_TMPDIR/empty_runs"
  export CURL_STUB_BODY_QUEUED="$BATS_TEST_TMPDIR/empty_runs"
  export CURL_STUB_BODY_INPROGRESS="$BATS_TEST_TMPDIR/empty_runs"
  export RUN_ID=111
  export HEAD_SHA="headsha"
}

@test "label guard skips and keeps label-match when a review of this head is in progress" {
  setup_busy_check
  echo '{"workflow_runs":[{"id":999,"head_sha":"headsha"}]}' > "$BATS_TEST_TMPDIR/busy"
  export CURL_STUB_BODY_INPROGRESS="$BATS_TEST_TMPDIR/busy"
  stub_curl
  EVENT_LABEL="themis-review" run "$SCRIPTS_DIR/label-guard.sh"
  [ "$status" -eq 0 ]
  grep -q '^label-match=true$' "$GITHUB_OUTPUT"
  grep -q '^proceed=false$' "$GITHUB_OUTPUT"
  grep -q 'already in progress' "$GITHUB_STEP_SUMMARY"
}

@test "label guard treats a queued run of this head as busy" {
  setup_busy_check
  echo '{"workflow_runs":[{"id":999,"head_sha":"headsha"}]}' > "$BATS_TEST_TMPDIR/busy"
  export CURL_STUB_BODY_QUEUED="$BATS_TEST_TMPDIR/busy"
  stub_curl
  EVENT_LABEL="themis-review" run "$SCRIPTS_DIR/label-guard.sh"
  [ "$status" -eq 0 ]
  grep -q '^proceed=false$' "$GITHUB_OUTPUT"
}

@test "label guard ignores its own run in the in-progress check" {
  setup_busy_check
  echo '{"workflow_runs":[{"id":111,"head_sha":"headsha"}]}' > "$BATS_TEST_TMPDIR/self"
  export CURL_STUB_BODY_INPROGRESS="$BATS_TEST_TMPDIR/self"
  stub_curl
  EVENT_LABEL="themis-review" run "$SCRIPTS_DIR/label-guard.sh"
  [ "$status" -eq 0 ]
  grep -q '^proceed=true$' "$GITHUB_OUTPUT"
}

@test "label guard ignores in-progress runs of a different head" {
  setup_busy_check
  echo '{"workflow_runs":[{"id":999,"head_sha":"otherhead"}]}' > "$BATS_TEST_TMPDIR/other"
  export CURL_STUB_BODY_INPROGRESS="$BATS_TEST_TMPDIR/other"
  stub_curl
  EVENT_LABEL="themis-review" run "$SCRIPTS_DIR/label-guard.sh"
  [ "$status" -eq 0 ]
  grep -q '^proceed=true$' "$GITHUB_OUTPUT"
}

@test "label guard proceeds when the in-progress check API fails (fail-open)" {
  setup_busy_check
  export CURL_STUB_EXIT_ACTIONS=22
  stub_curl
  EVENT_LABEL="themis-review" run "$SCRIPTS_DIR/label-guard.sh"
  [ "$status" -eq 0 ]
  grep -q '^proceed=true$' "$GITHUB_OUTPUT"
  [[ "$output" == *"skipping the in-progress check"* ]]
}
