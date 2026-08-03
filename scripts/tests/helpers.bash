# Shared helpers for the script test suite.

SCRIPTS_DIR="$(cd "$(dirname "$BATS_TEST_FILENAME")/.." && pwd)"

# Point GitHub-Actions-isms at files we can inspect.
setup_gh_files() {
  export GITHUB_OUTPUT="$BATS_TEST_TMPDIR/gh_output"
  export GITHUB_STEP_SUMMARY="$BATS_TEST_TMPDIR/gh_summary"
  : > "$GITHUB_OUTPUT"
  : > "$GITHUB_STEP_SUMMARY"
}

# Install a fake curl at the head of PATH. The stub's behavior is driven
# by env vars:
#   CURL_STUB_BODY      file whose content is printed (or written to -o)
#   CURL_STUB_EXIT      exit code (default 0)
#   CURL_STUB_URL_LOG   file where each requested URL is appended
stub_curl() {
  mkdir -p "$BATS_TEST_TMPDIR/bin"
  cat > "$BATS_TEST_TMPDIR/bin/curl" <<'EOF'
#!/usr/bin/env bash
out=""
url=""
grab_out=false
for a in "$@"; do
  if $grab_out; then out="$a"; grab_out=false; continue; fi
  case "$a" in
    -o) grab_out=true ;;
    http*) url="$a" ;;
  esac
done
if [ -n "${CURL_STUB_URL_LOG:-}" ]; then echo "$url" >> "$CURL_STUB_URL_LOG"; fi
exit_code="${CURL_STUB_EXIT:-0}"
if [ "$exit_code" != "0" ]; then exit "$exit_code"; fi
if [ -n "${CURL_STUB_BODY:-}" ]; then
  if [ -n "$out" ]; then cat "${CURL_STUB_BODY}" > "$out"; else cat "${CURL_STUB_BODY}"; fi
fi
exit 0
EOF
  chmod +x "$BATS_TEST_TMPDIR/bin/curl"
  export PATH="$BATS_TEST_TMPDIR/bin:$PATH"
}
