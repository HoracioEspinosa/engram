#!/usr/bin/env bash
# backup-live.sh — take one consistent, read-only copy of the live store.
#
# What it does: opens ~/.engram/engram.db through a read-only URI and writes a
# self-contained copy under docker/dev/out/live-copy/, verifies it, and prints
# its path as the last line of stdout so a caller can capture it.
#
# What it guarantees: the live store is never written to and never locked for
# writing. `mode=ro` opens the file read-only at the OS level, so even a bug in
# this script cannot modify it, and VACUUM INTO runs inside a read transaction,
# so a concurrent MCP session keeps writing while the copy is taken.
#
# This is the ONLY script in this directory that reads the live database at
# all, and nothing downstream of it ever does: migrate-check.sh and
# reorg-check.sh consume the copy it produces, never the original.
#
# NEVER copy engram.db together with its -wal and -shm files by hand. Those
# three files are one logical database read at three different instants: a
# writer can commit between the `cp` of the main file and the `cp` of the WAL,
# after which the WAL's frames no longer line up with the header and salt of
# the copied main file. SQLite then either replays frames onto the wrong page
# image or refuses the file outright as malformed — and the failure surfaces
# later, on the restored copy, where it looks like a migration bug instead of a
# bad copy. VACUUM INTO (and the .backup fallback) both produce a single file
# that is already checkpointed and internally consistent, with no sidecars.
#
# Opt-in: ENGRAM_ALLOW_LIVE_BACKUP=1 must be set. Reading the live store is a
# deliberate act, not something a script should do because it was sourced by
# another one.

set -euo pipefail

# shellcheck source=scripts/dev/lib.sh
. "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"

if [ "${ENGRAM_ALLOW_LIVE_BACKUP:-}" != "1" ]; then
  fail "refusing to read $LIVE_DB_DIR/engram.db without ENGRAM_ALLOW_LIVE_BACKUP=1"
fi

require_cmd sqlite3

SOURCE_DB="$LIVE_DB_DIR/engram.db"
[ -f "$SOURCE_DB" ] || fail "live store not found at $SOURCE_DB"

DEST_DIR="$OUT_DIR/live-copy"
mkdir -p "$DEST_DIR"
DEST="$DEST_DIR/engram-$(date -u '+%Y%m%dT%H%M%SZ').db"

[ -e "$DEST" ] && fail "destination already exists: $DEST"

RO_URI="file:${SOURCE_DB}?mode=ro"

log "copying $SOURCE_DB with VACUUM INTO"
if ! sqlite3 "$RO_URI" "VACUUM INTO '${DEST}';" 2>"$DEST_DIR/.vacuum.err"; then
  log "VACUUM INTO failed ($(tr -d '\n' <"$DEST_DIR/.vacuum.err")); falling back to .backup"
  rm -f "$DEST"
  # .backup is the older mechanism and copies page by page under the backup
  # API, which is equally safe against a concurrent writer; it just produces a
  # copy that is not compacted.
  sqlite3 "$RO_URI" ".backup '${DEST}'" || fail "both VACUUM INTO and .backup failed against $SOURCE_DB"
fi
rm -f "$DEST_DIR/.vacuum.err"

[ -f "$DEST" ] || fail "no copy was produced at $DEST"

log "verifying the copy"
integrity="$(sqlite3 "$DEST" 'PRAGMA integrity_check;' | head -1)"
if [ "$integrity" != "ok" ]; then
  fail "the copy failed integrity_check: $integrity"
fi

# A sidecar next to the copy means the copy was opened for writing at some
# point; that is exactly what must never happen to a snapshot other scripts
# treat as ground truth.
if [ -e "${DEST}-wal" ] || [ -e "${DEST}-shm" ]; then
  fail "the copy grew a -wal/-shm sidecar; it is no longer a clean snapshot"
fi

log "copy verified (integrity_check = ok), $(du -h "$DEST" | cut -f1)"
printf '%s\n' "$DEST"
