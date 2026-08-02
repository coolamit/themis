#!/usr/bin/env bash
# Applies the PR head's .themisignore (gitignore-style patterns) to the
# review: matching changed files are excluded from OCR.
#
# The file is read from the head's git objects only — the head is never
# checked out into the working tree. Matching is done by git itself:
# the file's content becomes the .gitignore of an empty scratch
# repository and path lists are filtered through `git check-ignore`,
# giving exact gitignore semantics (wildcards at depth, dir/ suffixes,
# anchoring, **, ! negation) isolated from the consumer repository's
# real ignore files.
#
# Guardrails, both fail the run: an entry that is a bare "*" variant,
# and entries that in combination cover every tracked file at head
# (.themisignore itself does not count as a survivor).
#
# OCR's --exclude is doublestar glob matching (not gitignore), so the
# resolved excluded paths are passed to it as literal entries:
# lowercased (OCR lowercases the candidate path; the fold here is
# ASCII-only, so a non-ASCII uppercase path falls back to being
# reviewed — the safe direction) with glob metacharacters escaped.
# A path containing a comma cannot ride OCR's comma-separated exclude
# list; it is warned about and reviewed anyway.
#
# Inputs (env): MERGE_BASE, HEAD_SHA
# Outputs ($GITHUB_OUTPUT):
#   exclude=<comma-joined literal paths>
#   skip=true|false   (true only when every changed file is ignored)
set -euo pipefail

MERGE_BASE="${MERGE_BASE:?MERGE_BASE is required}"
HEAD_SHA="${HEAD_SHA:?HEAD_SHA is required}"
OUT="${GITHUB_OUTPUT:-/dev/null}"
SUMMARY="${GITHUB_STEP_SUMMARY:-/dev/null}"

no_exclusions() {
  echo "$1"
  { echo "exclude="; echo "skip=false"; } >> "$OUT"
  exit 0
}

if ! git cat-file -e "${HEAD_SHA}:.themisignore" 2>/dev/null; then
  no_exclusions "no .themisignore at the PR head; reviewing everything"
fi

content="$(git show "${HEAD_SHA}:.themisignore")"

# Count effective entries (comments/blanks dropped, CR tolerated) and
# reject any entry that excludes the whole repository outright.
effective=0
while IFS= read -r line; do
  line="${line%$'\r'}"
  trimmed="$(printf '%s' "$line" | sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//')"
  case "$trimmed" in
    ''|'#'*) continue ;;
    '*'|'**'|'/*'|'/**')
      echo "::error::.themisignore: entry \"${trimmed}\" excludes the whole repository from review; that is not allowed"
      exit 1
      ;;
  esac
  effective=$((effective + 1))
done <<< "$content"

if [ "$effective" -eq 0 ]; then
  no_exclusions ".themisignore has no entries; reviewing everything"
fi

# Scratch repository: git evaluates the patterns. core.ignorecase is
# pinned so matching is identical on the Linux runner and
# case-insensitive local dev filesystems.
scratch="$(mktemp -d)"
trap 'rm -rf "$scratch"' EXIT
git init -q -b main "$scratch"
git -C "$scratch" config core.ignorecase false
printf '%s\n' "$content" > "$scratch/.gitignore"

# Reads NUL-delimited paths on stdin, writes the ignored ones one per
# line. check-ignore exits 1 when nothing matches — not an error here.
match_ignored() {
  git -C "$scratch" check-ignore -z --stdin | tr '\0' '\n' || true
}

# Guardrail: at least one tracked file besides .themisignore itself
# must survive the patterns.
all_files="$(git ls-tree -r --name-only "$HEAD_SHA")"
ignored_all="$(git ls-tree -r --name-only -z "$HEAD_SHA" | match_ignored)"
if [ -z "$ignored_all" ]; then
  survivors="$(printf '%s\n' "$all_files" | grep -vxF '.themisignore' || true)"
else
  survivors="$(printf '%s\n' "$all_files" | grep -vxF -f <(printf '%s\n' "$ignored_all") | grep -vxF '.themisignore' || true)"
fi
if [ -z "$survivors" ]; then
  echo "::error::.themisignore excludes every file in the repository from review; that is not allowed"
  exit 1
fi

changed="$(git diff --name-only --diff-filter=d "$MERGE_BASE" "$HEAD_SHA")"
if [ -z "$changed" ]; then
  no_exclusions "no changed files in the review range"
fi
ignored_changed="$(git diff --name-only --diff-filter=d -z "$MERGE_BASE" "$HEAD_SHA" | match_ignored)"
if [ -z "$ignored_changed" ]; then
  no_exclusions ".themisignore matches no changed files; reviewing everything"
fi

changed_count="$(printf '%s\n' "$changed" | wc -l | tr -d ' ')"
ignored_count="$(printf '%s\n' "$ignored_changed" | wc -l | tr -d ' ')"
if [ "$changed_count" -eq "$ignored_count" ]; then
  echo "review skipped: every changed file in this PR is ignored by .themisignore"
  echo "Themis: review skipped — every changed file in this PR is ignored by \`.themisignore\`." >> "$SUMMARY"
  { echo "exclude="; echo "skip=true"; } >> "$OUT"
  exit 0
fi

exclude=""
while IFS= read -r path; do
  [ -n "$path" ] || continue
  case "$path" in
    *,*)
      echo "::warning::.themisignore: cannot exclude \"${path}\" — the path contains a comma, which OCR's exclude list cannot carry; it will be reviewed"
      continue
      ;;
  esac
  literal="$(printf '%s' "$path" | tr '[:upper:]' '[:lower:]' | sed -e 's/[][*?{}\\]/\\&/g')"
  exclude="${exclude:+${exclude},}${literal}"
done <<< "$ignored_changed"

if [ -z "$exclude" ]; then
  no_exclusions "no excludable changed files remain; reviewing everything"
fi

echo "excluding from review (.themisignore): ${exclude}"
{ echo "exclude=${exclude}"; echo "skip=false"; } >> "$OUT"
