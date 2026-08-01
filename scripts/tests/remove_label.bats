#!/usr/bin/env bats

load helpers

setup() {
  setup_gh_files
  export REVIEW_LABEL="themis-review"
  export REPO="o/r"
  export PR_NUMBER=7
  export GITHUB_TOKEN="test-token"
}

@test "remove-label succeeds when the API accepts the delete" {
  export CURL_STUB_URL_LOG="$BATS_TEST_TMPDIR/urls"
  stub_curl
  run "$SCRIPTS_DIR/remove-label.sh"
  [ "$status" -eq 0 ]
  [[ "$output" == *"Removed label"* ]]
  grep -q '/repos/o/r/issues/7/labels/themis-review' "$CURL_STUB_URL_LOG"
}

@test "remove-label tolerates a read-only token" {
  export CURL_STUB_EXIT=22
  stub_curl
  run "$SCRIPTS_DIR/remove-label.sh"
  [ "$status" -eq 0 ]
  [[ "$output" == *"continuing"* ]]
}

@test "remove-label URL-encodes the label name" {
  export CURL_STUB_URL_LOG="$BATS_TEST_TMPDIR/urls"
  stub_curl
  REVIEW_LABEL="needs review!" run "$SCRIPTS_DIR/remove-label.sh"
  [ "$status" -eq 0 ]
  grep -q 'labels/needs%20review%21' "$CURL_STUB_URL_LOG"
}
