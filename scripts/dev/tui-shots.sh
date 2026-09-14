#!/usr/bin/env bash
# tui-shots.sh — capture every TUI screen at both supported geometries.
#
# What it does: for each scene and each geometry, it starts `engram tui` in a
# detached tmux session inside the container, waits for the alternate screen to
# hold a real frame, navigates to the scene's tab, and writes two captures:
# the plain text (.txt) and the same frame with its escape sequences (.ansi).
#
# What it guarantees: no line overflows the terminal it was drawn for. That is
# the single measurable statement behind "the layout does not break at 80
# columns", and it is checked per line, per scene, per geometry.
#
# Two details are inherited from scripts/tui-smoke.sh, both learned the hard
# way there:
#   * Readiness is polled, not assumed. The program enters the alternate
#     screen a few hundred milliseconds after launch; capturing before that
#     records the normal buffer, which is empty or still shows the shell.
#   * Every command handed to tmux is wrapped in `sh -c` and passed as a
#     single argument. tmux joins its remaining arguments with spaces before
#     running them, so a command split across several arguments loses its
#     quoting silently.
#
# The .ansi capture is what the colour and contrast assertions of later phases
# read; the .txt capture is what the width assertion below reads and what a
# reviewer actually looks at.

set -euo pipefail

# shellcheck source=scripts/dev/lib.sh
. "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"

require_cmd docker awk perl
assert_no_live_db

TUI_OUT="$OUT_DIR/tui"
mkdir -p "$TUI_OUT"

# Scenes: <name>:<engram tui arguments>:<tab digit to press, empty for none>.
# The digits are the tab bar's own bindings (internal/tui/app/update.go's
# digitTabs): 1 Memory, 2 Tasks, 3 Evidence, 4 Runbooks. The Dashboard needs no
# key because --project lands on it, and the Selector needs no project at all.
SCENES=(
  "selector::"
  "dashboard:--project koi-garden:"
  "memory:--project koi-garden:1"
  "tasks:--project koi-garden:2"
  "evidence:--project koi-garden:3"
  "runbooks:--project koi-garden:4"
)

GEOMETRIES=("80 24" "120 40")

BOOT_TIMEOUT=20
SETTLE_SECONDS=2
NAV_SETTLE_SECONDS=1

PASS_COUNT=0
FAIL_COUNT=0

kill_session() {
  in_container tmux kill-session -t "$1" >/dev/null 2>&1 || true
}

# wait_for_frame polls the pane until it holds a non-blank line, which is the
# first evidence that the program has drawn into the alternate screen.
wait_for_frame() {
  local session="$1" waited=0 content=""
  while [ "$waited" -lt "$BOOT_TIMEOUT" ]; do
    content="$(in_container tmux capture-pane -t "$session" -p 2>/dev/null || true)"
    if printf '%s' "$content" | awk 'NF { found = 1 } END { exit (found ? 0 : 1) }'; then
      return 0
    fi
    sleep 1
    waited=$((waited + 1))
  done
  return 1
}

shot() {
  local name="$1" args="$2" key="$3" width="$4" height="$5"
  local label="${name}-${width}x${height}"
  local session="shot-${label}"
  local txt="$TUI_OUT/${label}.txt"
  local ansi="$TUI_OUT/${label}.ansi"
  # The container sees docker/dev/out as /out, so the program's stderr lands
  # next to its captures without a second copy step.
  local stderr_path="/out/tui/${label}.stderr"

  kill_session "$session"

  local cmd="ENGRAM_PROJECT= engram tui ${args} 2>${stderr_path}"
  log "capturing $label"
  if ! docker exec -i "$CONTAINER" \
    tmux new-session -d -s "$session" -x "$width" -y "$height" "sh -c '${cmd}'"; then
    printf 'FAIL  %s: tmux could not start the session\n' "$label"
    FAIL_COUNT=$((FAIL_COUNT + 1))
    return
  fi

  if ! wait_for_frame "$session"; then
    printf 'FAIL  %s: no frame appeared within %ss\n' "$label" "$BOOT_TIMEOUT"
    in_container tmux capture-pane -t "$session" -p 2>/dev/null | awk '{ print "      | " $0 }' || true
    kill_session "$session"
    FAIL_COUNT=$((FAIL_COUNT + 1))
    return
  fi
  sleep "$SETTLE_SECONDS"

  if [ -n "$key" ]; then
    in_container tmux send-keys -t "$session" "$key" || true
    sleep "$NAV_SETTLE_SECONDS"
  fi

  in_container tmux capture-pane -t "$session" -p >"$txt"
  in_container tmux capture-pane -t "$session" -p -e >"$ansi"
  kill_session "$session"

  # Width is measured in characters, which equals cells for the ASCII and
  # box-drawing glyphs the current screens use. A palette with double-width
  # glyphs needs x/ansi.StringWidth to be measured honestly; that belongs in a
  # Go test, not here, and is called out so this assertion is not mistaken for
  # a full cell-width guarantee.
  #
  # The count is taken by perl under -CSD, not by awk: awk's length() counts
  # bytes, and every frame here is full of box-drawing glyphs and accented
  # Latin, each of which is one cell but two or three bytes. A line that fills
  # its terminal exactly then measures as ~82 for an 80-column capture, and the
  # assertion reports an overflow that the screen does not have.
  if perl -CSD -e '
      my ($w, $file) = @ARGV;
      open my $fh, "<", $file or die "cannot read $file: $!\n";
      my $bad = 0;
      while (my $line = <$fh>) {
        chomp $line;
        my $len = length $line;
        next if $len <= $w;
        printf "      line %d is %d characters wide\n", $., $len;
        $bad++;
      }
      close $fh;
      exit($bad ? 1 : 0);
    ' "$width" "$txt"; then
    printf 'PASS  %s (no line exceeds %s columns)\n' "$label" "$width"
    PASS_COUNT=$((PASS_COUNT + 1))
  else
    printf 'FAIL  %s: at least one line exceeds %s columns\n' "$label" "$width"
    FAIL_COUNT=$((FAIL_COUNT + 1))
  fi
}

for scene in "${SCENES[@]}"; do
  scene_name="${scene%%:*}"
  scene_rest="${scene#*:}"
  scene_args="${scene_rest%%:*}"
  scene_key="${scene_rest#*:}"
  for geometry in "${GEOMETRIES[@]}"; do
    # shellcheck disable=SC2086
    set -- $geometry
    shot "$scene_name" "$scene_args" "$scene_key" "$1" "$2"
  done
done

printf '\ntui-shots: %d passed, %d failed\ncaptures: %s\n' "$PASS_COUNT" "$FAIL_COUNT" "$TUI_OUT"
[ "$FAIL_COUNT" -eq 0 ] || exit 1
