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
  grep -q '^proceed=false$' "$GITHUB_OUTPUT"
  [ ! -s "$GITHUB_STEP_SUMMARY" ]
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
