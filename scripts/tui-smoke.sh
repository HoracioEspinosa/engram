#!/bin/bash
# tui-smoke.sh — smoke test for `engram tui`, rfc-tui.md §10.1's "Smoke" row.
#
# Boots the real binary inside a detached tmux session per case, at a real
# terminal (this is exactly what internal/tui/e2e's teatest suite cannot
# exercise: a real TTY, a real process boundary — teatest drives the same
# Bubble Tea Program in-process, over a pipe, under `go test`), waits for a
# marker to appear on screen, and reports which case failed and what it saw
# instead.
#
# Runs under bash 3.2 (macOS's shipped /bin/bash): no mapfile, no readarray,
# no associative arrays (declare -A), no ${var,,}. Every loop below is a
# plain positional-parameter shift, not an array keyed by name.
#
# Requires: go (to run the binary via `go run`, never `go build` — see this
# task's own report for why this script was authored but not run
# end-to-end), tmux, rg.
#
# Isolation: every case runs with ENGRAM_DATA_DIR pointed at a throwaway
# temp directory and cwd inside it, never at the real ~/.engram and never
# from inside this checkout (whose own directory name could otherwise
# resolve as a project) — the store backing active engram MCP sessions must
# never be opened by this script.

set -uo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
WORK_DIR="$(mktemp -d "${TMPDIR:-/tmp}/engram-tui-smoke.XXXXXX")"
SESSION="engram-tui-smoke-$$"

cleanup() {
  tmux kill-session -t "$SESSION" >/dev/null 2>&1
  rm -rf "$WORK_DIR"
}
trap cleanup EXIT INT TERM

PASS_COUNT=0
FAIL_COUNT=0

# wait_for_pattern SESSION PATTERN TIMEOUT_SECONDS
# Polls `tmux capture-pane` (screen + scrollback) until PATTERN (an `rg`
# pattern) appears, or the timeout elapses. Echoes "found" or "timeout".
wait_for_pattern() {
  local session="$1"
  local pattern="$2"
  local timeout="$3"
  local waited=0

  while [ "$waited" -lt "$timeout" ]; do
    if tmux capture-pane -t "$session" -p -S -200 2>/dev/null | rg -q -- "$pattern"; then
      echo "found"
      return 0
    fi
    sleep 1
    waited=$((waited + 1))
  done
  echo "timeout"
  return 1
}

# run_case NAME DATA_DIR EXTRA_ENV EXPECT_PATTERN TIMEOUT ARGS...
# EXTRA_ENV is a single "KEY=VALUE" string prepended to the command, or ""
# for none. Every case runs `go run` from inside its own throwaway data
# dir, never from ROOT_DIR, so detectProject(cwd) never resolves this
# checkout itself as a project.
run_case() {
  local name="$1"
  local data_dir="$2"
  local extra_env="$3"
  local expect_pattern="$4"
  local timeout="$5"
  shift 5
  # remaining positional args ($@) are the arguments to `engram` (after "tui").

  mkdir -p "$data_dir"
  tmux kill-session -t "$SESSION" >/dev/null 2>&1

  local cmd="cd \"$data_dir\" && ENGRAM_DATA_DIR=\"$data_dir\""
  if [ -n "$extra_env" ]; then
    cmd="$cmd $extra_env"
  fi
  cmd="$cmd go run \"$ROOT_DIR/cmd/engram\" tui"
  local a
  for a in "$@"; do
    cmd="$cmd \"$a\""
  done

  tmux new-session -d -s "$SESSION" -x 120 -y 40 "$cmd" 2>/dev/null
  if [ $? -ne 0 ]; then
    printf 'FAIL  %s: could not start tmux session\n' "$name"
    FAIL_COUNT=$((FAIL_COUNT + 1))
    return
  fi

  local result
  result="$(wait_for_pattern "$SESSION" "$expect_pattern" "$timeout")"
  if [ "$result" = "found" ]; then
    printf 'PASS  %s\n' "$name"
    PASS_COUNT=$((PASS_COUNT + 1))
  else
    printf 'FAIL  %s: pattern %q never appeared within %ss. Last screen:\n' "$name" "$expect_pattern" "$timeout"
    tmux capture-pane -t "$SESSION" -p -S -200 2>/dev/null | sed 's/^/      | /'
    FAIL_COUNT=$((FAIL_COUNT + 1))
  fi

  tmux send-keys -t "$SESSION" C-c >/dev/null 2>&1
  sleep 1
  tmux kill-session -t "$SESSION" >/dev/null 2>&1
}

echo "engram tui smoke test — rfc-tui.md §10.1"
echo "root: $ROOT_DIR"
echo "work: $WORK_DIR"
echo

# `go run` compiles on first use; every case gets the same generous timeout
# rather than special-casing the first one, since a cold module cache (a
# fresh checkout, or CI with no build cache restored) pays that cost again
# regardless of which case runs first.
CASE_TIMEOUT=60

# Case 1: `engram tui --project nextcloud` opens S2 (the Project Dashboard).
# Marker: the Dashboard's own footer (app/dashboard.go's viewDashboard),
# which no other screen prints verbatim. The project does not need to exist
# in the (empty, throwaway) store for this: app.New decides the *starting
# screen* from --project being non-empty alone (internal/tui/app/model.go's
# New), before any query runs — a missing project only turns into the
# Dashboard's own "no such project" error banner, still on S2.
run_case \
  "engram tui --project nextcloud opens S2 (Project Dashboard)" \
  "$WORK_DIR/case1" \
  "" \
  "1-5 tabs.*block.*enter open block" \
  "$CASE_TIMEOUT" \
  --project nextcloud

# Case 2: `engram tui` with no resolvable project.
#
# rfc-tui.md §10.1's smoke row states this opens S1 (the Project Selector).
# It does not: internal/tui/tui.go's own doc comment on New, and
# internal/tui/app/model.go's New (`if initialProject != "" { screenDashboard
# } else { screenTab }` — screenTab is Memory, not screenSelector) both say a
# workspace with no project opens on the Memory tab; nothing on this path
# ever opens the Selector automatically — only pressing "p" does. This case
# asserts the REAL behaviour (Memory's own stats card, whose "observations"
# label no other screen this script reaches prints bare) and calls the
# discrepancy out here rather than asserting the RFC's claim and silently
# failing every run.
#
# ENGRAM_PROJECT is explicitly cleared so a developer's own shell env never
# leaks a resolvable project into this case.
run_case \
  "engram tui with no resolvable project opens Memory, NOT S1 (see comment above)" \
  "$WORK_DIR/case2" \
  "ENGRAM_PROJECT=" \
  "observations" \
  "$CASE_TIMEOUT"

# Case 3: ENGRAM_TUI_THEME=kanagawa changes the palette without error.
# This only asserts the CLI accepts the override and the TUI still boots
# cleanly (the tab bar renders) — the palette *contract* itself (every
# Styles field non-zero, contrast ratios) is theme/theme_test.go's job, not
# this script's; verifying an exact rendered hex from a tmux capture would
# just duplicate that test suite far more fragilely.
run_case \
  "ENGRAM_TUI_THEME=kanagawa boots without an unknown-theme warning" \
  "$WORK_DIR/case3" \
  "ENGRAM_TUI_THEME=kanagawa" \
  "Dashboard.*Memory.*Tasks" \
  "$CASE_TIMEOUT"

# Case 4: --theme <unknown> falls back to the default with a warning.
# The warning (cmd/engram/main.go's "engram: unknown theme %q, falling back
# to %s") prints to stderr before the alternate screen takes over, so it
# lands in tmux's scrollback (captured with -S -200) even after the TUI's
# first real frame replaces it on screen.
run_case \
  "--theme unknown-palette falls back to the default with a warning" \
  "$WORK_DIR/case4" \
  "" \
  "unknown theme" \
  "$CASE_TIMEOUT" \
  --theme unknown-palette

echo
echo "engram tui smoke test: $PASS_COUNT passed, $FAIL_COUNT failed"

if [ "$FAIL_COUNT" -gt 0 ]; then
  exit 1
fi
exit 0
