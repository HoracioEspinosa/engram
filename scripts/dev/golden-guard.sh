#!/usr/bin/env bash
# golden-guard.sh — a golden file never moves without the picture of it.
#
# What it does: given a git range, reports whether it rewrites any TUI golden
# file without touching docs/tui/img/.
#
# What it guarantees: a regenerated golden is reviewed as a change to a screen
# rather than absorbed as 1500 lines of diff. A .golden file is ASCII with the
# colour stripped and the layout flattened; nobody can tell from it whether the
# screen it froze is one a person would want to look at. The capture in
# docs/tui/img/ is what makes that reviewable, and the only way to be sure one
# exists is to require it in the same change.
#
# What it does NOT do: check that the capture matches the golden. Nothing can,
# short of rendering both; what this enforces is that the author looked.
#
# Usage: golden-guard.sh [--warn] [<range>]
#   <range>   any git range, default HEAD~1..HEAD. In CI, the PR's own range.
#   --warn    report the violation and exit 0 instead of failing.
#
# --warn stays for a local run where the captures are still being rendered.
# CI runs it armed: docs/tui/img/ holds a capture of every screen, so a
# regenerated golden always has one to be reviewed against.

set -euo pipefail

# shellcheck source=scripts/dev/lib.sh
. "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"

WARN_ONLY=0
RANGE=""

while [ "$#" -gt 0 ]; do
  case "$1" in
    --warn)
      WARN_ONLY=1
      ;;
    -h | --help)
      printf 'usage: golden-guard.sh [--warn] [<range>]\n' >&2
      exit 0
      ;;
    -*)
      fail "unknown option: $1 (usage: golden-guard.sh [--warn] [<range>])"
      ;;
    *)
      if [ -n "$RANGE" ]; then
        fail "more than one range given: $RANGE and $1"
      fi
      RANGE="$1"
      ;;
  esac
  shift
done

require_cmd git

RANGE="${RANGE:-HEAD~1..HEAD}"

if ! git -C "$ROOT_DIR" rev-parse --quiet --verify "${RANGE%%..*}" >/dev/null 2>&1; then
  fail "the range $RANGE names a commit this checkout does not have (a shallow clone needs fetch-depth: 0)"
fi

# A guard that points at an empty directory passes everything. The captures
# are what makes a golden reviewable, so their absence is a failure of the
# guard itself rather than a clean run.
IMG_DIR="$ROOT_DIR/docs/tui/img"
if [ "$WARN_ONLY" -eq 0 ]; then
  if [ -z "$(find "$IMG_DIR" -name '*.png' -print -quit 2>/dev/null)" ]; then
    fail "no capture under docs/tui/img/ — render the tapes before arming this guard (scripts/dev/tapes.sh, then the shots profile)"
  fi
fi

CHANGED="$(git -C "$ROOT_DIR" diff --name-only "$RANGE")"

GOLDEN="$(printf '%s\n' "$CHANGED" | grep -E '^internal/tui/.*/testdata/.*\.golden$' || true)"
if [ -z "$GOLDEN" ]; then
  log "no golden file changed in $RANGE"
  exit 0
fi

IMAGES="$(printf '%s\n' "$CHANGED" | grep -E '^docs/tui/img/' || true)"
if [ -n "$IMAGES" ]; then
  log "golden files changed in $RANGE, with captures alongside them"
  exit 0
fi

printf 'these golden files were rewritten with no capture in docs/tui/img/:\n' >&2
printf '%s\n' "$GOLDEN" | sed 's/^/  /' >&2
printf 'regenerate the captures for the screens that moved and commit them in the same change.\n' >&2

if [ "$WARN_ONLY" -eq 1 ]; then
  log "warn-only: not failing"
  exit 0
fi
exit 1
