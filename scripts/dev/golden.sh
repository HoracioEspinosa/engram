#!/usr/bin/env bash
# golden.sh — regenerate the TUI golden files, on an explicit approval only.
#
# What it does: with --approve, runs both golden mechanisms inside
# golang:1.25.10 and writes the resulting diff to
# docker/dev/out/tui/golden-diff.txt for review.
#
#   internal/tui/app  TestGoldenScreens        -update
#   internal/tui/e2e  TestTeatestGoldenScreens -e2e-update
#
# The two flags are deliberately different names. internal/tui/e2e imports the
# app package, which registers flag.Bool("update", ...) at package init purely
# by being imported; a second flag of the same name in e2e would collide at
# init time, so that package registers "-e2e-update" instead.
#
# What it guarantees: golden files never move by accident. A golden file is the
# record of what the screen is supposed to look like — regenerating it makes
# any rendering test pass by definition, so the regeneration is gated behind a
# flag nobody types without meaning it, and the diff is written out so the
# change is reviewed as a change rather than absorbed silently.
#
# Usage: golden.sh --approve

set -euo pipefail

# shellcheck source=scripts/dev/lib.sh
. "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"

APPROVED=0
while [ "$#" -gt 0 ]; do
  case "$1" in
    --approve)
      APPROVED=1
      ;;
    -h | --help)
      printf 'usage: golden.sh --approve\n' >&2
      exit 0
      ;;
    *)
      fail "unknown argument: $1 (usage: golden.sh --approve)"
      ;;
  esac
  shift
done

if [ "$APPROVED" -ne 1 ]; then
  fail "refusing to rewrite the golden files without --approve"
fi

require_cmd docker git
assert_no_live_db

TUI_OUT="$OUT_DIR/tui"
mkdir -p "$TUI_OUT"

# No compose service mounts these, so compose never creates them; they are
# created here instead of relying on `docker run -v` to auto-create them.
GOMOD_VOLUME="$(ensure_volume engram-dev-gomod)"
GOBUILD_VOLUME="$(ensure_volume engram-dev-gobuild)"
set_git_mount_args

log "regenerating the golden files inside $GO_IMAGE"
docker run --rm \
  -v "$ROOT_DIR:/src" \
  "${GIT_MOUNT_ARGS[@]+"${GIT_MOUNT_ARGS[@]}"}" \
  -v "${GOMOD_VOLUME}:/go/pkg/mod" \
  -v "${GOBUILD_VOLUME}:/root/.cache/go-build" \
  -w /src \
  "$GO_IMAGE" \
  bash -c '
    set -eu
    go test ./internal/tui/app/... -run TestGoldenScreens -update
    go test ./internal/tui/e2e/... -run TestTeatestGoldenScreens -e2e-update
  '

DIFF_FILE="$TUI_OUT/golden-diff.txt"
git -C "$ROOT_DIR" diff -- internal/tui/app/testdata internal/tui/e2e/testdata >"$DIFF_FILE"

if [ -s "$DIFF_FILE" ]; then
  log "golden files changed; review $DIFF_FILE before committing"
else
  log "golden files are unchanged"
fi
printf '%s\n' "$DIFF_FILE"
