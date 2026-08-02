#!/usr/bin/env bash
# Consistency checks for action.yml:
#   - valid YAML (when a parser is available)
#   - every declared input is referenced in the steps
#   - every referenced input is declared
#   - every input has a non-empty description
#   - every input is either required or has an explicit default
set -euo pipefail

ACTION_FILE="${1:-action.yml}"
fail=0

if python3 -c 'import yaml' 2>/dev/null; then
  if ! python3 -c 'import sys, yaml; yaml.safe_load(open(sys.argv[1]))' "$ACTION_FILE"; then
    echo "FAIL: $ACTION_FILE is not valid YAML"
    exit 1
  fi
elif command -v ruby >/dev/null 2>&1; then
  if ! ruby -ryaml -e 'YAML.safe_load(File.read(ARGV[0]))' "$ACTION_FILE" >/dev/null; then
    echo "FAIL: $ACTION_FILE is not valid YAML"
    exit 1
  fi
else
  echo "note: no YAML parser found; skipping syntax validation"
fi

declared="$(awk '/^inputs:/{f=1; next} /^[^ ]/{f=0} f && /^  [a-z0-9-]+:$/{sub(/^  /,""); sub(/:$/,""); print}' "$ACTION_FILE")"
referenced="$(grep -oE 'inputs\.[a-z0-9-]+' "$ACTION_FILE" | sed 's/inputs\.//' | sort -u || true)"

for name in $declared; do
  if ! grep -q "inputs\.${name}[^a-z0-9-]" "$ACTION_FILE" && ! grep -q "inputs\.${name}\$" "$ACTION_FILE"; then
    echo "FAIL: input '${name}' is declared but never referenced in the steps"
    fail=1
  fi
done

for name in $referenced; do
  if ! printf '%s\n' "$declared" | grep -qx "$name"; then
    echo "FAIL: input '${name}' is referenced but not declared"
    fail=1
  fi
done

for name in $declared; do
  block="$(awk -v name="$name" '
    $0 == "  " name ":" {f=1; next}
    f && /^  [a-z0-9-]+:$/ {f=0}
    f && /^[^ ]/ {f=0}
    f {print}
  ' "$ACTION_FILE")"

  # A description passes when it carries a non-empty scalar (quoted-empty,
  # null, and ~ all count as empty) or a block indicator followed by a
  # non-empty line.
  desc_ok="$(printf '%s\n' "$block" | awk '
    /^ *description:/ {
      value = $0
      sub(/^ *description: */, "", value)
      sub(/ *$/, "", value)
      if (value == "" || value ~ /^[>|][+-]?$/) {
        getline nxt
        if (nxt ~ /[^ ]/) print "ok"
        exit
      }
      if (value != "null" && value != "~" && value != "\047\047" && value != "\"\"") print "ok"
      exit
    }
  ')"
  if [ "$desc_ok" != "ok" ]; then
    echo "FAIL: input '${name}' has no (or an empty) description"
    fail=1
  fi

  if ! printf '%s\n' "$block" | grep -qE '^ *(required: true|default:)'; then
    echo "FAIL: input '${name}' is neither required nor has a default"
    fail=1
  fi
done

if [ "$fail" -ne 0 ]; then
  exit 1
fi
echo "action.yml consistency: OK ($(printf '%s\n' "$declared" | wc -l | tr -d ' ') inputs checked)"
