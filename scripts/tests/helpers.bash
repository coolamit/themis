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
# URL-specific overrides (each falls back to the generic vars above),
# for scripts that hit several endpoints in one run:
#   CURL_STUB_BODY_PERMISSION    …/collaborators/…/permission
#   CURL_STUB_BODY_RUN           …/actions/runs/<id>
#   CURL_STUB_BODY_QUEUED        …runs?status=queued…
#   CURL_STUB_BODY_INPROGRESS    …runs?status=in_progress…
#   CURL_STUB_EXIT_ACTIONS       exit code for the three actions endpoints
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
body="${CURL_STUB_BODY:-}"
exit_code="${CURL_STUB_EXIT:-0}"
case "$url" in
  *status=queued*)      body="${CURL_STUB_BODY_QUEUED:-$body}";     exit_code="${CURL_STUB_EXIT_ACTIONS:-$exit_code}" ;;
  *status=in_progress*) body="${CURL_STUB_BODY_INPROGRESS:-$body}"; exit_code="${CURL_STUB_EXIT_ACTIONS:-$exit_code}" ;;
  */actions/runs/*)     body="${CURL_STUB_BODY_RUN:-$body}";        exit_code="${CURL_STUB_EXIT_ACTIONS:-$exit_code}" ;;
  */permission)         body="${CURL_STUB_BODY_PERMISSION:-$body}" ;;
esac
if [ "$exit_code" != "0" ]; then exit "$exit_code"; fi
if [ -n "$body" ]; then
  if [ -n "$out" ]; then cat "$body" > "$out"; else cat "$body"; fi
fi
exit 0
EOF
  chmod +x "$BATS_TEST_TMPDIR/bin/curl"
  export PATH="$BATS_TEST_TMPDIR/bin:$PATH"
}
