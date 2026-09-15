#!/usr/bin/env bash
# tui-shots.sh — capture every workspace screen at both supported geometries.
#
# What it does: for each scene and each geometry, it starts `engram tui` in a
# detached tmux session inside the container, waits for the alternate screen to
# hold a real frame, walks to the scene with the workspace's own keys, and
# writes two captures: the plain text (.txt) and the same frame with its escape
# sequences (.ansi).
#
# What it guarantees, per scene and per geometry:
#   * no line overflows the terminal it was drawn for. That is the single
#     measurable statement behind "the layout does not break at 80 columns";
#   * the frame holds exactly the rows the terminal has, and the second of them
#     is still chrome — the tab bar, or the top edge of the panel covering it.
#     A frame taller than its terminal is cut from the top by the renderer, so
#     that pair is what "the layout does not break at 24 rows" measures;
#   * the same scene captured twice is the same picture. The workspace
#     remembers what a session does to it, so every capture starts from the
#     rows cleared rather than from whatever the scene before it left behind;
#   * every colour the frame emits, foreground and background alike, is a role
#     of the palette the run asked for. That is both the proof that the right
#     palette drew the frame — the koi palettes share no colour — and the
#     proof that nothing painted over it. glamour's Markdown styling, which
#     brings a highlighter with a fixed scheme of its own, is the case this
#     was written for.
#
# On top of that it proves once, across two runs, that the Runbooks Markdown
# view repaints its syntax-highlighted code block when the palette changes:
# the code block is the one region drawn by a third-party highlighter, and a
# highlighter that caches its style by name keeps the first palette forever.
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
# The .ansi capture keeps the raw escape sequences a frame was drawn with,
# which is where its colour information lives; the .txt capture is what the
# width assertion reads and what a reviewer actually looks at.

set -euo pipefail

# shellcheck source=scripts/dev/lib.sh
. "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"

require_cmd docker awk perl jq
assert_no_live_db

TUI_OUT="$OUT_DIR/tui"
mkdir -p "$TUI_OUT"

# The project every scene opens against. Seeded by scripts/dev/seed.sh.
PROJECT="koi-garden"

# Scenes: <name>|<keys>. The keys are a ";"-separated walk through the
# workspace, in the workspace's own bindings:
#   * a bare token is sent as a key name, so "C-p", "Enter", "j" and "?" all
#     mean what tmux means by them;
#   * a token starting with "L:" is sent literally, which is how a query is
#     typed into a search box without tmux reading "n" as a key name.
# The digits are the tab bar's own bindings: 0 Home, 1 Memory, 2 Tasks,
# 3 Evidence, 4 Benchmarks, 5 Runbooks, 6 Graph, 7 Settings. The overlays are
# C-p (project tree), C-k (search palette), C-t (theme picker) and ? (help).
POND_SCENES=(
  "tree|C-p"
  "home|"
  "memory|1"
  "memory-detail|1;j;Enter;Enter"
  "tasks|2"
  "tasks-detail|2;j;Enter"
  "evidence|3"
  "evidence-detail|3;Enter"
  "benchmarks|4"
  "runbooks|5"
  "runbooks-view|5;/;L:autologin;Enter;Enter;G"
  "graph|6"
  "settings|7"
  "palette|C-k;L:lookup"
  "theme-picker|C-t"
  "help|?"
  "search-scoped-memory|1;Enter;L:timeout;Enter;a"
  "context-pack|2;Enter;x"
)

# The light palette is captured on the three screens where a light ground
# changes the most: the one a reader lands on, one dense with badges, and the
# one that draws its own swatches.
DAY_SCENES=(
  "home|"
  "tasks-detail|2;j;Enter"
  "settings|7"
)

GEOMETRIES=("80 24" "120 40")

BOOT_TIMEOUT=20
SETTLE_SECONDS=2
NAV_SETTLE_SECONDS=1

PASS_COUNT=0
FAIL_COUNT=0

# ALLOWED_TRIPLETS holds the active palette as "r;g;b" lines, and PRIMARY_TRIPLET
# the one colour whose presence proves which palette drew the frame.
ALLOWED_TRIPLETS=""
PRIMARY_TRIPLET=""
ACTIVE_THEME=""

kill_session() {
  in_container tmux kill-session -t "$1" >/dev/null 2>&1 || true
}

# SETTINGS_THE_RUN_WRITES are the rows a capture session leaves behind: the tab
# it was last on, the width the scoped-Memory scene narrows to, and the icon
# vocabulary the Settings tab would cycle. The theme is deliberately not among
# them — use_theme sets it, and it is what the run is capturing.
SETTINGS_THE_RUN_WRITES="'tui.last_tab', 'tui.memory_scope', 'tui.icons'"

# reset_remembered_state puts the workspace's own memory back where every scene
# starts from.
#
# A capture session is a user session like any other: the scene that narrows
# Memory writes the width down, and every scene after it — in this run and in
# the next — then opens at whatever the last one left behind. The tab bar
# carries that width in Memory's own title, so the same scene captured twice
# produces two different pictures. Clearing the rows rather than setting them
# is what makes each capture a capture of the workspace's default.
reset_remembered_state() {
  in_container sqlite3 /data/engram.db \
    "DELETE FROM settings WHERE key IN (${SETTINGS_THE_RUN_WRITES});" ||
    fail "could not clear the remembered TUI settings"
}

# hex_to_triplet turns "#ff9e5e" into "255;158;94", which is how a true-colour
# SGR sequence spells it. Comparing decimal triplets rather than hex strings is
# the only honest way to look for a colour in a terminal capture: the escape
# sequence never carries the hex.
hex_to_triplet() {
  printf '%d;%d;%d' "0x${1:1:2}" "0x${1:3:2}" "0x${1:5:2}"
}

# load_palette reads the active theme through the binary's own exporter, so the
# expected colours are the ones the program resolves rather than a copy of them
# kept in this script. A tape or a test that hard-codes a hex drifts the first
# time a palette is edited; this cannot.
load_palette() {
  local name="$1" document hexes hex
  ACTIVE_THEME="$name"
  document="$(in_container engram theme export "$name")" ||
    fail "engram theme export $name failed"

  hexes="$(printf '%s' "$document" | jq -r '.palette | to_entries[] | .value')"
  ALLOWED_TRIPLETS=""
  while IFS= read -r hex; do
    [ -n "$hex" ] || continue
    ALLOWED_TRIPLETS="$ALLOWED_TRIPLETS$(hex_to_triplet "$hex")
"
  done <<<"$hexes"

  hex="$(printf '%s' "$document" | jq -r '.palette.primary')"
  [ -n "$hex" ] && [ "$hex" != "null" ] || fail "theme $name exports no primary role"
  PRIMARY_TRIPLET="$(hex_to_triplet "$hex")"
  log "palette $name: $(printf '%s' "$ALLOWED_TRIPLETS" | grep -c . || true) roles, primary $hex ($PRIMARY_TRIPLET)"
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

# send_walk plays one scene's key walk into the session.
send_walk() {
  local session="$1" walk="$2" step
  [ -n "$walk" ] || return 0
  local IFS=';'
  # shellcheck disable=SC2086
  for step in $walk; do
    [ -n "$step" ] || continue
    if [ "${step#L:}" != "$step" ]; then
      in_container tmux send-keys -t "$session" -l -- "${step#L:}" || true
    else
      in_container tmux send-keys -t "$session" -- "$step" || true
    fi
    sleep "$NAV_SETTLE_SECONDS"
  done
}

# assert_width reports every line wider than the terminal it was drawn for.
#
# The count is taken by perl under -CSD, not by awk: awk's length() counts
# bytes, and every frame here is full of box-drawing glyphs and accented
# Latin, each of which is one cell but two or three bytes. A line that fills
# its terminal exactly then measures as ~82 for an 80-column capture, and the
# assertion reports an overflow that the screen does not have.
assert_width() {
  perl -CSD -e '
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
    ' "$1" "$2"
}

# assert_height reports a capture that does not hold exactly the rows its
# terminal has.
#
# It is the other half of assert_width, and it is the half that catches the
# frame going missing rather than the row going wide: tmux pads a pane to its
# own height, so a capture with fewer rows is a pane that never filled, and one
# with more is a capture of something other than the pane.
assert_height() {
  local height="$1" file="$2" rows
  rows="$(awk 'END { print NR }' "$file")"
  [ "${rows:-0}" -eq "$height" ] && return 0
  printf '      the capture holds %s rows, not the %s the terminal has\n' "${rows:-0}" "$height"
  return 1
}

# OVERLAY_SCENES are the scenes captured with a panel composed over the
# workspace. A panel is centred on the terminal, so on a short one it starts on
# the very row the tab bar occupies and covers it — which is a modal panel
# doing its job, not a frame that overflowed.
OVERLAY_SCENES="tree palette theme-picker help"

# assert_chrome_is_on_screen requires the frame's second row to be the chrome:
# the tab bar, or the top edge of the panel covering it.
#
# This is what a frame taller than its terminal actually costs. The renderer
# drops the rows that do not fit from the TOP, so an overflowing screen does
# not lose its last list row — it loses the app frame's padding, then the tab
# bar, then the screen's own header, and the reader is left looking at the
# middle of a list with nothing on screen to say which tab it belongs to. A
# frame whose second row is still chrome is a frame nothing was dropped from.
#
# The bar is recognised by its structure, not by its glyphs: eight slots
# numbered in order, exactly one of them bracketed as the active one. That is
# the same reading internal/tui/app/golden_lint_test.go takes of it, and it
# holds in every icon mode, at both geometries, in either palette. A panel's
# top edge is recognised the same way, as a long run of one repeated
# non-alphanumeric cell, over an empty first row — the app frame's own padding,
# which is the row the renderer would have taken first.
assert_chrome_is_on_screen() {
  perl -CSD -e '
      my ($file, $overlay) = @ARGV;
      open my $fh, "<", $file or die "cannot read $file: $!\n";
      my ($first, $row) = ("", "");
      while (my $line = <$fh>) {
        $first = $line if $. == 1;
        if ($. == 2) { $row = $line; last }
      }
      close $fh;
      chomp $first;
      chomp $row;

      my $digits = join "", ($row =~ /(\d)/g);
      my $active = () = $row =~ /\[/g;
      exit 0 if $digits eq "01234567" && $active == 1;
      exit 0 if $overlay && $first =~ /^\s*$/ && $row =~ /([^\p{Alnum}\s])\1{19,}/;

      printf "      row 2 is neither the tab bar (slots %s, %d active) nor a panel edge:\n      | %s\n",
        ($digits eq "" ? "none" : $digits), $active, $row;
      exit 1;
    ' "$1" "$2"
}

# scene_has_overlay reports whether a scene name is one of OVERLAY_SCENES.
scene_has_overlay() {
  local scene
  for scene in $OVERLAY_SCENES; do
    [ "$scene" = "$1" ] && return 0
  done
  return 1
}

# assert_colours_are_the_palettes reports every true-colour sequence the frame
# emits that is not a role of the active palette, foreground and background
# alike, and refuses a frame that carries almost no colour at all.
#
# The koi palettes share no colour, so "every colour is a role of this one" is
# also what proves which palette drew the frame. Asking instead for one
# specific role to appear would be weaker and wrong: a role is drawn where the
# screen has something to say with it, and a screen that never reaches for the
# primary — a table of benchmark numbers, a Markdown body — is not thereby
# drawn in the wrong theme.
#
# A colour is matched with one unit of tolerance per channel. lipgloss resolves
# a hex through a float and truncates on the way back out, so a channel of 0xf4
# leaves the program as 243 rather than 244. That is the renderer's arithmetic,
# not a different colour, and a capture is the wrong place to relitigate it.
assert_colours_are_the_palettes() {
  local file="$1" found
  found="$(grep -oE '[34]8;2;[0-9]+;[0-9]+;[0-9]+' "$file" 2>/dev/null |
    sed -E 's/^[34]8;2;//' | sort -u || true)"

  local count
  count="$(printf '%s\n' "$found" | grep -c . || true)"
  if [ "${count:-0}" -lt 3 ]; then
    printf '      only %s distinct colours in the frame; it was not drawn in true colour\n' "${count:-0}"
    return 1
  fi

  local foreign
  foreign="$(printf '%s\n' "$found" | awk -v allowed="$ALLOWED_TRIPLETS" '
    BEGIN {
      n = split(allowed, rows, "\n")
      for (i = 1; i <= n; i++) {
        if (rows[i] == "") continue
        split(rows[i], c, ";")
        ar[i] = c[1]; ag[i] = c[2]; ab[i] = c[3]; total = i
      }
    }
    NF {
      split($0, c, ";")
      for (i = 1; i <= total; i++) {
        if (ar[i] == "") continue
        dr = c[1] - ar[i]; if (dr < 0) dr = -dr
        dg = c[2] - ag[i]; if (dg < 0) dg = -dg
        db = c[3] - ab[i]; if (db < 0) db = -db
        if (dr <= 1 && dg <= 1 && db <= 1) next
      }
      print $0
    }')"

  [ -z "$foreign" ] || {
    printf '      colour(s) outside the %s palette: %s\n' "$ACTIVE_THEME" "$(printf '%s' "$foreign" | tr '\n' ' ')"
    return 1
  }
  return 0
}

# assert_theme_left_its_mark checks, once per theme rather than once per
# frame, that the theme's own primary reached at least one capture. It is what
# catches a run where every scene was drawn by the same wrong palette and the
# per-frame audit therefore agreed with itself.
assert_theme_left_its_mark() {
  local theme="$1" triplet="$2"
  if grep -qlF "38;2;$triplet" "$TUI_OUT"/*-"$theme"-*.ansi >/dev/null 2>&1; then
    log "$theme left its primary ($triplet) on at least one capture"
    return 0
  fi
  printf 'FAIL  %s: not one capture carries the palette primary %s\n' "$theme" "$triplet"
  FAIL_COUNT=$((FAIL_COUNT + 1))
}

# shot captures one scene and, unless it is called as a bare capture, judges
# it. counted="no" is how the extra Runbooks capture the repaint comparison
# needs is taken without being counted a second time as a scene.
shot() {
  local name="$1" walk="$2" width="$3" height="$4" theme="$5" counted="${6:-yes}"
  local label="${name}-${theme}-${width}x${height}"
  local session="shot-${label}"
  local txt="$TUI_OUT/${label}.txt"
  local ansi="$TUI_OUT/${label}.ansi"
  # The container sees docker/dev/out as /out, so the program's stderr lands
  # next to its captures without a second copy step.
  local stderr_path="/out/tui/${label}.stderr"

  kill_session "$session"
  reset_remembered_state

  # COLORTERM is what tells lipgloss the terminal takes 24-bit colour. Without
  # it the palette is quantised to 256 indexed colours on its way out and the
  # capture cannot be compared against a hex at all.
  #
  # LANG is set for the same kind of reason on the glyph side: the icon mode
  # falls back to ascii for a locale that does not say UTF-8, and the image
  # carries no locale at all. A capture taken without it freezes the fallback
  # rather than the workspace, and the two do not look alike.
  local cmd="ENGRAM_PROJECT= COLORTERM=truecolor TERM=xterm-256color LANG=C.UTF-8 engram tui --project ${PROJECT} 2>${stderr_path}"
  log "capturing $label"
  if ! docker exec -i "$CONTAINER" \
    tmux new-session -d -s "$session" -x "$width" -y "$height" "sh -c '${cmd}'"; then
    printf 'FAIL  %s: tmux could not start the session\n' "$label"
    FAIL_COUNT=$((FAIL_COUNT + 1))
    return
  fi
  # tmux stores a cell's colour at full depth only when its terminal
  # description admits it; without this the pane downgrades every RGB colour
  # to the nearest of 256 before it is ever captured.
  in_container tmux set-option -t "$session" -g default-terminal "tmux-256color" >/dev/null 2>&1 || true
  in_container tmux set-option -t "$session" -ga terminal-features ",xterm-256color:RGB" >/dev/null 2>&1 || true

  if ! wait_for_frame "$session"; then
    printf 'FAIL  %s: no frame appeared within %ss\n' "$label" "$BOOT_TIMEOUT"
    in_container tmux capture-pane -t "$session" -p 2>/dev/null | awk '{ print "      | " $0 }' || true
    kill_session "$session"
    FAIL_COUNT=$((FAIL_COUNT + 1))
    return
  fi
  sleep "$SETTLE_SECONDS"

  send_walk "$session" "$walk"

  in_container tmux capture-pane -t "$session" -p >"$txt"
  in_container tmux capture-pane -t "$session" -p -e >"$ansi"
  kill_session "$session"

  if [ "$counted" != "yes" ]; then
    log "captured $label for the repaint comparison"
    return
  fi

  local overlay=""
  scene_has_overlay "$name" && overlay="overlay"

  local problems=""
  assert_width "$width" "$txt" || problems="$problems width"
  assert_height "$height" "$txt" || problems="$problems height"
  assert_chrome_is_on_screen "$txt" "$overlay" || problems="$problems chrome"
  assert_colours_are_the_palettes "$ansi" || problems="$problems colour"

  if [ -z "$problems" ]; then
    printf 'PASS  %s (fits %sx%s, chrome on screen, every colour a %s role)\n' \
      "$label" "$width" "$height" "$ACTIVE_THEME"
    PASS_COUNT=$((PASS_COUNT + 1))
  else
    printf 'FAIL  %s:%s\n' "$label" "$problems"
    FAIL_COUNT=$((FAIL_COUNT + 1))
  fi
}

use_theme() {
  log "switching the stored theme to $1"
  in_container engram theme use "$1" >/dev/null || fail "engram theme use $1 failed"
  load_palette "$1"
}

run_set() {
  local theme="$1"
  shift
  local scene geometry
  for scene in "$@"; do
    for geometry in "${GEOMETRIES[@]}"; do
      # shellcheck disable=SC2086
      set -- $geometry
      shot "${scene%%|*}" "${scene#*|}" "$1" "$2" "$theme"
    done
  done
}

# ─── the runs ────────────────────────────────────────────────────────────────

use_theme koi-pond
POND_PRIMARY="$PRIMARY_TRIPLET"
run_set koi-pond "${POND_SCENES[@]}"
assert_theme_left_its_mark koi-pond "$POND_PRIMARY"

use_theme koi-day
run_set koi-day "${DAY_SCENES[@]}"
assert_theme_left_its_mark koi-day "$PRIMARY_TRIPLET"

# The Runbooks Markdown view is captured a second time in the light palette
# for one reason only: the comparison below. It is not a scene of its own —
# the same screen is already frozen in koi-pond at both geometries — so it is
# taken without being counted.
shot "runbooks-view" "5;/;L:autologin;Enter;Enter;G" 120 40 koi-day no

log "restoring koi-pond"
in_container engram theme use koi-pond >/dev/null || fail "engram theme use koi-pond failed"

# ─── the code block repaints ─────────────────────────────────────────────────
#
# The Markdown view's code block is drawn by chroma through glamour, and
# chroma registers a style globally under a name. A build that names the style
# after the palette's identity rather than its contents keeps whichever palette
# drew first, for the rest of the process — the theme picker would then repaint
# the whole workspace except its code. Two captures of the same runbook under
# two palettes are what tells the two apart.
log "checking that the code block is repainted with the palette"
POND_VIEW="$TUI_OUT/runbooks-view-koi-pond-120x40.ansi"
DAY_VIEW="$TUI_OUT/runbooks-view-koi-day-120x40.ansi"

# code_block_colours reads the colours of the highlighted block alone, not of
# the whole frame. Comparing whole frames would prove nothing: the workspace
# around the block is repainted by the theme either way, so the two captures
# would differ even with the block frozen — which is the exact defect this
# exists to catch. The block is located by the identifiers the fixture runbook
# puts in it.
code_block_colours() {
  grep -E 'autologinTTL|issueAutologinToken|Token\{Subject' "$1" |
    grep -oE '38;2;[0-9]+;[0-9]+;[0-9]+' | sort -u
}

if [ ! -s "$POND_VIEW" ] || [ ! -s "$DAY_VIEW" ]; then
  printf 'FAIL  code-block repaint: one of the two Runbooks captures is missing\n'
  FAIL_COUNT=$((FAIL_COUNT + 1))
else
  pond_colours="$(code_block_colours "$POND_VIEW")"
  day_colours="$(code_block_colours "$DAY_VIEW")"
  if [ -z "$pond_colours" ]; then
    printf 'FAIL  code-block repaint: the koi-pond capture does not show the code block at all\n'
    FAIL_COUNT=$((FAIL_COUNT + 1))
  elif [ -z "$day_colours" ]; then
    printf 'FAIL  code-block repaint: the koi-day capture does not show the code block at all\n'
    FAIL_COUNT=$((FAIL_COUNT + 1))
  elif [ "$pond_colours" = "$day_colours" ]; then
    printf 'FAIL  code-block repaint: both palettes drew the runbook with the same colours\n'
    FAIL_COUNT=$((FAIL_COUNT + 1))
  else
    printf 'PASS  code-block repaint (%d token colours in koi-pond, %d in koi-day, sets differ)\n' \
      "$(printf '%s\n' "$pond_colours" | grep -c .)" \
      "$(printf '%s\n' "$day_colours" | grep -c .)"
  fi
fi

printf '\ntui-shots: %d passed, %d failed\ncaptures: %s\n' "$PASS_COUNT" "$FAIL_COUNT" "$TUI_OUT"
[ "$FAIL_COUNT" -eq 0 ] || exit 1
