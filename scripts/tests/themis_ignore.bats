#!/usr/bin/env bats

load helpers

# Builds a real repo whose master is the review base; each test commits
# its own PR head (with or without a .themisignore) on a branch. The
# script needs only local git objects — no origin, no network.
setup() {
  setup_gh_files
  export GIT_AUTHOR_NAME=t GIT_AUTHOR_EMAIL=t@t GIT_COMMITTER_NAME=t GIT_COMMITTER_EMAIL=t@t

  WORK="$BATS_TEST_TMPDIR/work"
  git init -q -b master "$WORK"
  cd "$WORK"
  echo base > index.php
  mkdir -p lib docs
  echo base > lib/func.php
  echo base > docs/readme.md
  git add . && git commit -qm "base"
  export MERGE_BASE
  MERGE_BASE="$(git rev-parse HEAD)"
  git checkout -qb pr
}

# Commits everything currently staged/modified plus any new files and
# exports HEAD_SHA for the script.
commit_head() {
  git add -A && git commit -qm "pr head"
  export HEAD_SHA
  HEAD_SHA="$(git rev-parse HEAD)"
}

@test "themis-ignore does nothing when .themisignore is absent" {
  echo change > index.php
  commit_head
  run "$SCRIPTS_DIR/themis-ignore.sh"
  [ "$status" -eq 0 ]
  [[ "$output" == *"no .themisignore"* ]]
  grep -q '^exclude=$' "$GITHUB_OUTPUT"
  grep -q '^skip=false$' "$GITHUB_OUTPUT"
}

@test "themis-ignore does nothing when .themisignore has only comments and blanks" {
  echo change > index.php
  printf '# nothing real\n\n   \n# more comments\n' > .themisignore
  commit_head
  run "$SCRIPTS_DIR/themis-ignore.sh"
  [ "$status" -eq 0 ]
  [[ "$output" == *"no entries"* ]]
  grep -q '^skip=false$' "$GITHUB_OUTPUT"
}

@test "themis-ignore excludes a wildcard match at any depth" {
  mkdir -p dist
  echo min > dist/app.min.js
  echo change > index.php
  echo '*.min.js' > .themisignore
  commit_head
  run "$SCRIPTS_DIR/themis-ignore.sh"
  [ "$status" -eq 0 ]
  grep -q '^exclude=dist/app.min.js$' "$GITHUB_OUTPUT"
  grep -q '^skip=false$' "$GITHUB_OUTPUT"
}

@test "themis-ignore excludes a directory pattern" {
  mkdir -p vendor/pkg
  echo v > vendor/pkg/a.php
  echo change > index.php
  echo 'vendor/' > .themisignore
  commit_head
  run "$SCRIPTS_DIR/themis-ignore.sh"
  [ "$status" -eq 0 ]
  grep -q '^exclude=vendor/pkg/a.php$' "$GITHUB_OUTPUT"
}

@test "themis-ignore anchors a leading-slash pattern to the repo root" {
  mkdir -p build src/build
  echo b > build/x.js
  echo b > src/build/y.js
  echo change > index.php
  echo '/build/' > .themisignore
  commit_head
  run "$SCRIPTS_DIR/themis-ignore.sh"
  [ "$status" -eq 0 ]
  grep -q '^exclude=build/x.js$' "$GITHUB_OUTPUT"
  ! grep -q 'src/build' "$GITHUB_OUTPUT"
}

@test "themis-ignore honors negation" {
  mkdir -p logs
  echo l > logs/a.log
  echo l > logs/keep.log
  echo change > index.php
  # logs/* not logs/ — gitignore cannot re-include a file whose parent
  # directory is excluded, exactly like real .gitignore.
  printf 'logs/*\n!logs/keep.log\n' > .themisignore
  commit_head
  run "$SCRIPTS_DIR/themis-ignore.sh"
  [ "$status" -eq 0 ]
  grep -q '^exclude=logs/a.log$' "$GITHUB_OUTPUT"
  ! grep -q 'keep.log' "$GITHUB_OUTPUT"
}

@test "themis-ignore rejects a bare star entry" {
  echo change > index.php
  echo '*' > .themisignore
  commit_head
  run "$SCRIPTS_DIR/themis-ignore.sh"
  [ "$status" -eq 1 ]
  [[ "$output" == *"whole repository"* ]]
}

@test "themis-ignore rejects entries that cover every file individually" {
  git rm -q docs/readme.md
  echo change > index.php
  printf '/lib/\n/index.php\n/docs/\n' > .themisignore
  commit_head
  run "$SCRIPTS_DIR/themis-ignore.sh"
  [ "$status" -eq 1 ]
  [[ "$output" == *"excludes every file"* ]]
}

@test "themis-ignore rejects pattern-based total coverage" {
  git rm -q docs/readme.md
  echo change > index.php
  printf '*.php\n*.md\n' > .themisignore
  commit_head
  run "$SCRIPTS_DIR/themis-ignore.sh"
  [ "$status" -eq 1 ]
  [[ "$output" == *"excludes every file"* ]]
}

@test "themis-ignore rejects the everything-but-itself dodge" {
  git rm -q docs/readme.md
  echo change > index.php
  printf '*.php\n*.md\n!.themisignore\n' > .themisignore
  commit_head
  run "$SCRIPTS_DIR/themis-ignore.sh"
  [ "$status" -eq 1 ]
  [[ "$output" == *"excludes every file"* ]]
}

@test "themis-ignore skips the review when every changed file is ignored" {
  # .themisignore pre-exists at the merge base; the PR touches only
  # ignored paths. (A PR that itself adds .themisignore has that file
  # in its changed set, so it never all-excludes — by design.)
  echo 'vendor/' > .themisignore
  git add -A && git commit -qm "add ignore file"
  MERGE_BASE="$(git rev-parse HEAD)"
  mkdir -p vendor
  echo v > vendor/only.php
  commit_head
  run "$SCRIPTS_DIR/themis-ignore.sh"
  [ "$status" -eq 0 ]
  grep -q '^skip=true$' "$GITHUB_OUTPUT"
  grep -q 'themisignore' "$GITHUB_STEP_SUMMARY"
}

@test "themis-ignore lowercases and escapes the emitted paths" {
  mkdir -p 'weird' UPPER
  echo w > 'weird/a[1]*.php'
  echo u > UPPER/File.PHP
  echo change > index.php
  printf 'weird/\nUPPER/\n' > .themisignore
  commit_head
  run "$SCRIPTS_DIR/themis-ignore.sh"
  [ "$status" -eq 0 ]
  grep -F 'upper/file.php' "$GITHUB_OUTPUT" > /dev/null
  grep -F 'weird/a\[1\]\*.php' "$GITHUB_OUTPUT" > /dev/null
}

@test "themis-ignore warns about and reviews a comma-containing path" {
  mkdir -p vendor
  echo v > 'vendor/a,b.php'
  echo v > vendor/plain.php
  echo change > index.php
  echo 'vendor/' > .themisignore
  commit_head
  run "$SCRIPTS_DIR/themis-ignore.sh"
  [ "$status" -eq 0 ]
  [[ "$output" == *"::warning::"*"comma"* ]]
  grep -q '^exclude=vendor/plain.php$' "$GITHUB_OUTPUT"
}
