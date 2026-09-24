#!/usr/bin/env bats

setup() {
  SCRIPT_DIR=$(cd "$(dirname "$BATS_TEST_FILENAME")/.." && pwd)
  # shellcheck source=../review
  source "$SCRIPT_DIR/review"
  TEST_TMP=$(mktemp -d)
  PATH="$TEST_TMP:$PATH"
  export PATH TEST_TMP
}

teardown() { rm -rf "$TEST_TMP"; }

@test "parses pull URL and optional suffix and query" {
  parse_pr_url 'https://github.com/octo-org/widget/pull/42/files?tab=files'
  [ "$owner" = octo-org ]
  [ "$repo_name" = widget ]
  [ "$pr_number" = 42 ]
  [ "$repo" = octo-org/widget ]
}

@test "rejects non PR URLs and shell syntax" {
  run parse_pr_url 'https://github.com/octo-org/widget/issues/42'
  [ "$status" -eq 2 ]
  run parse_pr_url 'https://github.com/o/r/pull/4;touch /tmp/pwned'
  [ "$status" -eq 2 ]
}

@test "completion notifier emits success and failure titles when installed" {
  cat > "$TEST_TMP/terminal-notifier" <<'MOCK'
#!/usr/bin/env bash
printf '%s\n' "$*" >> "$NOTIFIER_LOG"
MOCK
  chmod +x "$TEST_TMP/terminal-notifier"
  NOTIFIER_LOG="$TEST_TMP/notifications"
  export NOTIFIER_LOG
  notify_completion 0 org repo 12
  notify_completion 1 org repo 12
  grep -F '✅ PR review complete' "$NOTIFIER_LOG"
  grep -F '⚠️ PR review stopped' "$NOTIFIER_LOG"
}

@test "completion notifier is optional and notifier errors are ignored" {
  PATH=/usr/bin:/bin
  export PATH
  notify_completion 0 org repo 12
  PATH="$TEST_TMP:$PATH"
  export PATH
  printf '#!/bin/sh\nexit 1\n' > "$TEST_TMP/terminal-notifier"
  chmod +x "$TEST_TMP/terminal-notifier"
  notify_pr_event org/repo 12 '🔄 PR updated' 'updated'
}

@test "event notifier includes PR identity" {
  cat > "$TEST_TMP/terminal-notifier" <<'MOCK'
#!/usr/bin/env bash
printf '%s\n' "$*" >> "$NOTIFIER_LOG"
MOCK
  chmod +x "$TEST_TMP/terminal-notifier"
  NOTIFIER_LOG="$TEST_TMP/notifications"
  export NOTIFIER_LOG
  notify_pr_event org/repo 12 '🎉 PR merged' merged
  grep -F 'org/repo · PR #12' "$NOTIFIER_LOG"
}

@test "background watcher reports a changed head or base" {
  cat > "$TEST_TMP/gh" <<'MOCK'
#!/usr/bin/env bash
printf 'main\tTitle\tOPEN\thead-new\tbase-new\t\n'
MOCK
  chmod +x "$TEST_TMP/gh"
  REVIEW_POLL_INTERVAL=1
  export REVIEW_POLL_INTERVAL
  cat > "$TEST_TMP/terminal-notifier" <<'MOCK'
#!/usr/bin/env bash
printf '%s\n' "$*" >> "$NOTIFIER_LOG"
kill -TERM "$PPID" 2>/dev/null || true
MOCK
  chmod +x "$TEST_TMP/terminal-notifier"
  NOTIFIER_LOG="$TEST_TMP/notifications"
  export NOTIFIER_LOG
  watch_pr_in_background 12 org/repo old-head old-base main 1 &
  watcher=$!
  wait "$watcher" 2>/dev/null || true
  grep -F 'PR updated' "$NOTIFIER_LOG"
}

@test "background watcher notifies merged and closed states" {
  cat > "$TEST_TMP/gh" <<'MOCK'
#!/usr/bin/env bash
if [[ $GH_STATE == MERGED ]]; then
  printf 'main\tTitle\tCLOSED\thead\tbase\t2026-01-01T00:00:00Z\n'
else
  printf 'main\tTitle\tCLOSED\thead\tbase\t\n'
fi
MOCK
  chmod +x "$TEST_TMP/gh"
  cat > "$TEST_TMP/terminal-notifier" <<'MOCK'
#!/usr/bin/env bash
printf '%s\n' "$*" >> "$NOTIFIER_LOG"
kill -TERM "$PPID" 2>/dev/null || true
MOCK
  chmod +x "$TEST_TMP/terminal-notifier"
  NOTIFIER_LOG="$TEST_TMP/notifications"
  REVIEW_POLL_INTERVAL=1
  GH_STATE=MERGED
  export NOTIFIER_LOG REVIEW_POLL_INTERVAL GH_STATE
  watch_pr_in_background 12 org/repo head base main 1 &
  watcher=$!
  wait "$watcher" 2>/dev/null || true
  grep -F 'PR merged' "$NOTIFIER_LOG"
  : > "$NOTIFIER_LOG"
  GH_STATE=CLOSED
  export GH_STATE
  watch_pr_in_background 12 org/repo head base main 1 &
  watcher=$!
  wait "$watcher" 2>/dev/null || true
  grep -F 'PR closed' "$NOTIFIER_LOG"
}

@test "snapshot requests all fields needed for refresh and status checks" {
  cat > "$TEST_TMP/gh" <<'MOCK'
#!/usr/bin/env bash
printf '%s\n' "$*" > "$GH_LOG"
printf 'main\tTitle\tOPEN\thead123\tbase456\t\n'
MOCK
  chmod +x "$TEST_TMP/gh"
  GH_LOG="$TEST_TMP/gh-args"
  export GH_LOG
  result=$(read_pr_snapshot 12 org/repo)
  [[ "$result" == $'main\tTitle\tOPEN\thead123\tbase456\t' ]]
  grep -F -- '--json baseRefName,title,state,headRefOid,baseRefOid,mergedAt' "$GH_LOG"
}

@test "ref fetch updates both base and PR refs and checks out new head" {
  cat > "$TEST_TMP/git" <<'MOCK'
#!/usr/bin/env bash
printf '%s\n' "$*" >> "$GIT_LOG"
MOCK
  chmod +x "$TEST_TMP/git"
  GIT_LOG="$TEST_TMP/git-args"
  export GIT_LOG
  fetch_review_refs /tmp/checkout main 12
  grep -F 'refs/heads/main:refs/remotes/origin/main' "$GIT_LOG"
  grep -F 'refs/pull/12/head:refs/remotes/origin/review-pr-12' "$GIT_LOG"
  grep -F 'checkout --quiet --detach refs/remotes/origin/review-pr-12' "$GIT_LOG"
}

@test "report path identifies repo, PR, head, and base" {
  result=$(report_path org repo 12 abcdef123456789 basefedcba98765 /reports)
  [ "$result" = /reports/review-org-repo-pr-12-abcdef123456-basefedcba98.md ]
}

@test "Codex gets inline prompt, sandbox policy, and Markdown output destination" {
  cat > "$TEST_TMP/codex" <<'MOCK'
#!/usr/bin/env bash
printf '%s\n' "$*" > "$CODEX_LOG"
MOCK
  chmod +x "$TEST_TMP/codex"
  CODEX_LOG="$TEST_TMP/codex-args"
  export CODEX_LOG
  run_codex_review /tmp/repo org/repo 12 base head /tmp/report.md 'Review playbook'
  grep -F -- '--output-last-message /tmp/report.md' "$CODEX_LOG"
  grep -F 'Merge-base: base. Head: head.' "$CODEX_LOG"
  grep -F 'Review playbook' "$CODEX_LOG"
}

@test "usage exits with status two" {
  run usage
  [ "$status" -eq 2 ]
}
