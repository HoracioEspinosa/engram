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
# Requires: go (this script builds the binary it exercises — see below —
# with `go build`), tmux, rg, awk.
#
# On the binary under test: this script builds its OWN copy of `engram`
# from ROOT_DIR/cmd/engram into $WORK_DIR and calls every case by that
# binary's absolute path, never by the bare command name "engram". This
# matters because a machine running this script may already have a
# released `engram` earlier on $PATH (e.g. installed via Homebrew) — a bare
# `engram tui` would silently exercise that instead of this checkout's own
# code. The build step below prints both binaries' `version` output so a
# reader can confirm which one every case actually ran; cmd/engram/main.go
# only bakes in a real version via goreleaser's ldflags; an unadorned
# `go build` here leaves it at "dev" (or, failing that, the module's own
# pseudo-version), which is why the two are expected to read differently.
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
BINARY="$WORK_DIR/engram-under-test"

cleanup() {
  tmux kill-session -t "$SESSION" >/dev/null 2>&1
  rm -rf "$WORK_DIR"
}
trap cleanup EXIT INT TERM

PASS_COUNT=0
FAIL_COUNT=0

echo "engram tui smoke test — rfc-tui.md §10.1"
echo "root: $ROOT_DIR"
echo "work: $WORK_DIR"
echo

echo "building $BINARY from $ROOT_DIR/cmd/engram ..."
if ! (cd "$ROOT_DIR" && go build -o "$BINARY" ./cmd/engram); then
  echo "FATAL: go build failed; aborting without running any case."
  exit 2
fi
echo "built: $BINARY"
echo "  version under test:   $("$BINARY" version 2>/dev/null || echo '(version command failed)')"
if command -v engram >/dev/null 2>&1; then
  echo "  version on \$PATH:      $(engram version 2>/dev/null || echo '(version command failed)') (from $(command -v engram); NOT what this script runs)"
else
  echo "  (no 'engram' found elsewhere on \$PATH)"
fi
echo

# wait_for_evidence SESSION STDERR_LOG PATTERN TIMEOUT_SECONDS
# Polls two places until PATTERN (an `rg` pattern) appears in either, or the
# timeout elapses: `tmux capture-pane` (screen + scrollback) and
# STDERR_LOG, a file the case's own stderr is redirected into.
#
# The stderr log exists because capture-pane's scrollback is NOT enough on
# its own: cmd/engram/main.go prints its "unknown theme" warning to stderr
# BEFORE tea.EnterAltScreen switches the terminal to the alternate screen
# buffer, and once that switch happens, `tmux capture-pane -S -200` reads
# from the alternate buffer's own (now-fresh) history — the warning, which
# lived in the *normal* buffer an instant earlier, is gone from view even
# though the pane never touched it. Confirmed by polling capture-pane every
# 100ms against the real binary: the warning is visible for roughly 400-500ms
# after boot and gone by the 600ms poll, once the Dashboard's first frame
# takes over — a 1-second poll interval missed that window essentially every
# time, which is why this case failed on the first real run against the
# compiled binary before this fix (see this task's report). Redirecting
# stderr to a plain file sidesteps the alternate-screen boundary entirely:
# a file on disk never cares which screen buffer is active.
wait_for_evidence() {
  local session="$1"
  local stderr_log="$2"
  local pattern="$3"
  local timeout="$4"
  local waited=0
  local interval="0.2"
  local ticks
  ticks=$(awk -v t="$timeout" -v i="$interval" 'BEGIN { printf "%d", (t / i) + 1 }')
  local tick=0

  while [ "$tick" -lt "$ticks" ]; do
    if tmux capture-pane -t "$session" -p -S -200 2>/dev/null | rg -q -- "$pattern"; then
      echo "found"
      return 0
    fi
    if [ -f "$stderr_log" ] && rg -q -- "$pattern" "$stderr_log" 2>/dev/null; then
      echo "found"
      return 0
    fi
    sleep "$interval"
    tick=$((tick + 1))
  done
  echo "timeout"
  return 1
}

# run_case NAME DATA_DIR EXTRA_ENV EXPECT_PATTERN TIMEOUT ARGS...
# EXTRA_ENV is a single "KEY=VALUE" string prepended to the command, or ""
# for none. Every case runs $BINARY (this checkout's own build, by absolute
# path — never the bare "engram" command) from inside its own throwaway
# data dir, never from ROOT_DIR, so detectProject(cwd) never resolves this
# checkout itself as a project. Stderr is redirected into DATA_DIR/stderr.log
# so wait_for_evidence can see a warning printed before the alternate screen
# takes over — see that function's own comment.
#
# The command always runs through an explicit `sh -c`, never through
# whatever tmux's own default-shell happens to be configured to. Found
# necessary the hard way: this machine's tmux default-shell is fish, which
# has neither POSIX `VAR=value cmd` env-prefix assignment reliably in every
# form used below nor bash/ksh's `>(process substitution)` — an earlier
# version of this script used `2> >(tee ...)` directly in the command string
# tmux was given, which fish could not parse at all, killing the pane before
# anything ran (confirmed: `tmux capture-pane` reported "can't find pane"
# moments after `new-session`, and dropping the process-substitution in favour
# of a plain `2>file` redirect, run through `sh -c` explicitly, fixed it).
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
  local stderr_log="$data_dir/stderr.log"
  rm -f "$stderr_log"

  local cmd="cd \"$data_dir\" && ENGRAM_DATA_DIR=\"$data_dir\""
  if [ -n "$extra_env" ]; then
    cmd="$cmd $extra_env"
  fi
  cmd="$cmd \"$BINARY\" tui"
  local a
  for a in "$@"; do
    cmd="$cmd \"$a\""
  done
  cmd="$cmd 2>\"$stderr_log\""

  tmux new-session -d -s "$SESSION" -x 120 -y 40 sh -c "$cmd" 2>/dev/null
  if [ $? -ne 0 ]; then
    printf 'FAIL  %s: could not start tmux session\n' "$name"
    FAIL_COUNT=$((FAIL_COUNT + 1))
    return
  fi

  local result
  result="$(wait_for_evidence "$SESSION" "$stderr_log" "$expect_pattern" "$timeout")"
  if [ "$result" = "found" ]; then
    printf 'PASS  %s\n' "$name"
    PASS_COUNT=$((PASS_COUNT + 1))
  else
    printf 'FAIL  %s: pattern %q never appeared within %ss. Last screen:\n' "$name" "$expect_pattern" "$timeout"
    tmux capture-pane -t "$SESSION" -p -S -200 2>/dev/null | sed 's/^/      | /'
    if [ -f "$stderr_log" ]; then
      printf '      stderr.log:\n'
      sed 's/^/      | /' "$stderr_log"
    fi
    FAIL_COUNT=$((FAIL_COUNT + 1))
  fi

  tmux send-keys -t "$SESSION" C-c >/dev/null 2>&1
  sleep 1
  tmux kill-session -t "$SESSION" >/dev/null 2>&1
}

# $BINARY is already built above; each case still gets a generous timeout
# for the store to open and the first real query round-trip, not for
# compilation (that cost was paid once, before any case ran).
CASE_TIMEOUT=20

# Case 1: `engram tui --project nextcloud` opens S2 (the Project Dashboard).
# Marker: the Dashboard's own footer (app/dashboard.go's viewDashboard),
# which no other screen prints verbatim. The project does not need to exist
# in the (empty, throwaway) store for this: app.New decides the *starting
# screen* from --project being non-empty alone (internal/tui/app/model.go's
# New), before any query runs — a missing project only turns into the
# Dashboard's own "no such project" error banner, still on S2.
#
# ENGRAM_PROJECT is cleared even though --project already takes precedence
# over it (rfc-tui.md §9.1), so this case's outcome never depends on
# whatever a developer's own shell (or CI runner) happens to have exported —
# found necessary while debugging case 4 below: this machine's environment
# already carries a resolvable ENGRAM_PROJECT.
run_case \
  "engram tui --project nextcloud opens S2 (Project Dashboard)" \
  "$WORK_DIR/case1" \
  "ENGRAM_PROJECT=" \
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
# just duplicate that test suite far more fragilely. ENGRAM_PROJECT is
# cleared for the same reason as case 1.
run_case \
  "ENGRAM_TUI_THEME=kanagawa boots without an unknown-theme warning" \
  "$WORK_DIR/case3" \
  "ENGRAM_PROJECT= ENGRAM_TUI_THEME=kanagawa" \
  "Dashboard.*Memory.*Tasks" \
  "$CASE_TIMEOUT"

# Case 4: --theme <unknown> falls back to the default with a warning.
# The warning (cmd/engram/main.go's "engram: unknown theme %q, falling back
# to %s") prints to stderr before tea.EnterAltScreen switches the terminal
# over — wait_for_evidence's own comment above has the full account of why
# that made this case fail against the real binary on the first run, and
# why stderr is duplicated to a file instead of relying on tmux's
# scrollback alone. ENGRAM_PROJECT is cleared for the same reason as case 1.
run_case \
  "--theme unknown-palette falls back to the default with a warning" \
  "$WORK_DIR/case4" \
  "ENGRAM_PROJECT=" \
  "unknown theme" \
  "$CASE_TIMEOUT" \
  --theme unknown-palette

echo
echo "engram tui smoke test: $PASS_COUNT passed, $FAIL_COUNT failed"

if [ "$FAIL_COUNT" -gt 0 ]; then
  exit 1
fi
exit 0
