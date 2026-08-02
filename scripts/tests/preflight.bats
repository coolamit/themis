#!/usr/bin/env bats

load helpers

setup() {
  setup_gh_files
  unset LLM_URL LLM_API_KEY LLM_MODEL EVENT_NAME IS_FORK ACTOR || true
}

@test "preflight passes with all credentials present" {
  LLM_URL=https://example.com/v1 LLM_API_KEY=secret LLM_MODEL=some-model \
    run "$SCRIPTS_DIR/preflight.sh"
  [ "$status" -eq 0 ]
  grep -q '^skip=false$' "$GITHUB_OUTPUT"
}

@test "preflight cleanly skips a fork PR without credentials" {
  EVENT_NAME=pull_request IS_FORK=true run "$SCRIPTS_DIR/preflight.sh"
  [ "$status" -eq 0 ]
  grep -q '^skip=true$' "$GITHUB_OUTPUT"
  grep -q 'fork PRs' "$GITHUB_STEP_SUMMARY"
}

@test "preflight cleanly skips a dependabot PR without credentials" {
  EVENT_NAME=pull_request IS_FORK=false ACTOR='dependabot[bot]' \
    run "$SCRIPTS_DIR/preflight.sh"
  [ "$status" -eq 0 ]
  grep -q '^skip=true$' "$GITHUB_OUTPUT"
  grep -qi 'dependabot' "$GITHUB_STEP_SUMMARY"
}

@test "preflight hard-fails a misconfigured same-repo run" {
  EVENT_NAME=pull_request IS_FORK=false run "$SCRIPTS_DIR/preflight.sh"
  [ "$status" -eq 1 ]
  [[ "$output" == *misconfigured* ]]
}

@test "preflight hard-fails when only some credentials are set" {
  LLM_URL=https://example.com/v1 EVENT_NAME=pull_request_target IS_FORK=true \
    run "$SCRIPTS_DIR/preflight.sh"
  [ "$status" -eq 1 ]
}
