#!/usr/bin/env bats

load helpers

setup() {
  setup_gh_files
  export INSTALL_DIR="$BATS_TEST_TMPDIR/tools"
  export GITHUB_PATH="$BATS_TEST_TMPDIR/gh_path"
  : > "$GITHUB_PATH"
  printf 'fake-ocr-binary' > "$BATS_TEST_TMPDIR/asset"
  export CURL_STUB_BODY="$BATS_TEST_TMPDIR/asset"
  export CURL_STUB_URL_LOG="$BATS_TEST_TMPDIR/urls"
  ASSET_SHA="$(compute_sha256 "$BATS_TEST_TMPDIR/asset")"
}

@test "install-ocr verifies a pinned version against the recorded checksum" {
  echo "1.8.4 $ASSET_SHA" > "$BATS_TEST_TMPDIR/checksums"
  export CHECKSUM_FILE="$BATS_TEST_TMPDIR/checksums"
  stub_curl
  OCR_VERSION=1.8.4 run "$SCRIPTS_DIR/install-ocr.sh"
  [ "$status" -eq 0 ]
  [[ "$output" == *"Checksum verified"* ]]
  [ -x "$INSTALL_DIR/ocr" ]
  grep -q "releases/download/v1.8.4/opencodereview-linux-amd64" "$CURL_STUB_URL_LOG"
  grep -q "$INSTALL_DIR" "$GITHUB_PATH"
}

@test "install-ocr fails on a checksum mismatch" {
  echo "1.8.4 0000000000000000000000000000000000000000000000000000000000000000" > "$BATS_TEST_TMPDIR/checksums"
  export CHECKSUM_FILE="$BATS_TEST_TMPDIR/checksums"
  stub_curl
  OCR_VERSION=1.8.4 run "$SCRIPTS_DIR/install-ocr.sh"
  [ "$status" -eq 1 ]
  [[ "$output" == *"checksum mismatch"* ]]
}

@test "install-ocr warns but proceeds for a pinned version with no recorded checksum" {
  echo "1.0.0 $ASSET_SHA" > "$BATS_TEST_TMPDIR/checksums"
  export CHECKSUM_FILE="$BATS_TEST_TMPDIR/checksums"
  stub_curl
  OCR_VERSION=1.8.4 run "$SCRIPTS_DIR/install-ocr.sh"
  [ "$status" -eq 0 ]
  [[ "$output" == *"no checksum recorded"* ]]
  [ -x "$INSTALL_DIR/ocr" ]
}

@test "install-ocr resolves latest via the releases API without verification" {
  # The stub serves the same body for both the API call and the download;
  # a JSON body containing tag_name works for both purposes.
  printf '{"tag_name":"v9.9.9"}' > "$BATS_TEST_TMPDIR/asset"
  stub_curl
  OCR_VERSION=latest run "$SCRIPTS_DIR/install-ocr.sh"
  [ "$status" -eq 0 ]
  grep -q "releases/latest" "$CURL_STUB_URL_LOG"
  grep -q "releases/download/v9.9.9/opencodereview-linux-amd64" "$CURL_STUB_URL_LOG"
  [ -x "$INSTALL_DIR/ocr" ]
}

@test "install-ocr fails when the download fails" {
  export CURL_STUB_EXIT=22
  stub_curl
  OCR_VERSION=1.8.4 run "$SCRIPTS_DIR/install-ocr.sh"
  [ "$status" -eq 1 ]
}
