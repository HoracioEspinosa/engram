#!/usr/bin/env bash
# migrate-check.sh — prove a migration loses no rows on real data.
#
# What it does: takes a copy produced by backup-live.sh, counts every table and
# reads PRAGMA user_version, opens the copy with the dev image (which applies
# whatever migrations the build carries), counts and reads the version again,
# and fails if any table came out with fewer rows than it went in with.
#
# What it guarantees:
#   * Row counts are compared per table, not in total. A migration that drops
#     one table while another grows would balance out in a single number and
#     is exactly the failure this check exists to catch.
#   * The original is untouched. The copy is copied again into a working
#     directory first, so a run that corrupts the database still leaves the
#     snapshot backup-live.sh produced intact for the next attempt.
#   * `engram doctor --json` reports no blocked or error check afterwards.
#     internal/diagnostic has no "fail" state: a check reports ok, warning,
#     blocked or error, and the report's status is the worst of them.
#
# It also measures `runbooks sync --vault-dir /vault` against the fixtures and
# stores the result without judging it. The vault carries its own service map
# at Runbooks/services.json, so the fixture services resolve through it rather
# than through the compatibility default in internal/runbooks/service_map.go;
# recording the number is how a regression in that wiring becomes visible.
#
# Usage: migrate-check.sh <copy.db>

set -euo pipefail

# shellcheck source=scripts/dev/lib.sh
. "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"

[ "$#" -ge 1 ] || fail "usage: migrate-check.sh <copy.db>"
SOURCE_COPY="$1"
[ -f "$SOURCE_COPY" ] || fail "copy not found: $SOURCE_COPY"

require_cmd docker jq awk join sort
assert_no_live_db

MIGRATE_OUT="$OUT_DIR/migrate"
WORK_DIR="$MIGRATE_OUT/work"
mkdir -p "$MIGRATE_OUT"
rm -rf "$WORK_DIR"
mkdir -p "$WORK_DIR"

# The store resolves its database as <ENGRAM_DATA_DIR>/engram.db, so the copy
# has to carry that exact name inside the directory that gets mounted.
cp "$SOURCE_COPY" "$WORK_DIR/engram.db"
# The container runs as uid 10001 and the host file belongs to the host user;
# SQLite also needs to create -wal/-shm beside the database, so the directory
# itself has to be writable.
chmod 0777 "$WORK_DIR"
chmod 0666 "$WORK_DIR/engram.db"

# sqlite_in_image runs a SQL script against the working copy using the dev
# image, which already carries sqlite3. Keeping it in a container means this
# script needs nothing installed on the host beyond Docker.
sqlite_in_image() {
  docker run --rm -i \
    -v "${WORK_DIR}:/restore" \
    --entrypoint sh \
    "$IMAGE" \
    -c 'sqlite3 -noheader -separator "	" /restore/engram.db'
}

# Tables only: the FTS shadow tables (every name containing _fts) and SQLite's
# own sqlite_* tables are excluded, because their row counts are derived and
# change legitimately whenever an index is rebuilt.
TABLE_LIST_SQL="SELECT name FROM sqlite_master
WHERE type = 'table'
  AND name NOT LIKE '%\\_fts%' ESCAPE '\\'
  AND name NOT LIKE 'sqlite\\_%' ESCAPE '\\'
ORDER BY name;"

# Emitting one COUNT statement per table and feeding it back in keeps the
# counting generic: a table added by a future migration is counted without
# editing this script.
COUNT_SCRIPT_SQL="SELECT 'SELECT ''' || name || ''' || char(9) || COUNT(*) FROM \"' || name || '\";'
FROM sqlite_master
WHERE type = 'table'
  AND name NOT LIKE '%\\_fts%' ESCAPE '\\'
  AND name NOT LIKE 'sqlite\\_%' ESCAPE '\\'
ORDER BY name;"

table_counts() {
  local script
  script="$(printf '%s\n' "$COUNT_SCRIPT_SQL" | sqlite_in_image)"
  printf '%s\n' "$script" | sqlite_in_image | sort
}

log "reading the copy before the migration"
printf '%s\n' "$TABLE_LIST_SQL" | sqlite_in_image >"$MIGRATE_OUT/tables-before.txt"
table_counts >"$MIGRATE_OUT/before.tsv"
USER_VERSION_BEFORE="$(printf 'PRAGMA user_version;\n' | sqlite_in_image)"
log "before: $(wc -l <"$MIGRATE_OUT/before.tsv" | tr -d ' ') table(s), user_version=$USER_VERSION_BEFORE"

log "opening the copy with $IMAGE (this applies the migrations the build carries)"
set +e
docker run --rm \
  -v "${WORK_DIR}:/restore" \
  -e ENGRAM_DATA_DIR=/restore \
  "$IMAGE" \
  doctor --json >"$MIGRATE_OUT/doctor.json" 2>"$MIGRATE_OUT/doctor.stderr"
doctor_status=$?
set -e
log "doctor exited $doctor_status"

log "reading the copy after the migration"
table_counts >"$MIGRATE_OUT/after.tsv"
USER_VERSION_AFTER="$(printf 'PRAGMA user_version;\n' | sqlite_in_image)"
INTEGRITY="$(printf 'PRAGMA integrity_check;\n' | sqlite_in_image | head -1)"
log "after: $(wc -l <"$MIGRATE_OUT/after.tsv" | tr -d ' ') table(s), user_version=$USER_VERSION_AFTER, integrity_check=$INTEGRITY"

FAILURES=0
note_failure() {
  FAILURES=$((FAILURES + 1))
  printf 'FAIL  %s\n' "$*"
}

printf 'user_version: %s -> %s\n' "$USER_VERSION_BEFORE" "$USER_VERSION_AFTER" >"$MIGRATE_OUT/user-version.txt"

if [ "$INTEGRITY" = "ok" ]; then
  printf 'PASS  integrity_check = ok\n'
else
  note_failure "integrity_check = $INTEGRITY"
fi

# -a 1 keeps a table that existed before and is gone after; -e MISSING makes
# that case visible to awk instead of collapsing into an empty field.
join -t '	' -a 1 -e MISSING -o '0,1.2,2.2' "$MIGRATE_OUT/before.tsv" "$MIGRATE_OUT/after.tsv" \
  >"$MIGRATE_OUT/row-delta.tsv"

if awk -F '\t' '
    $3 == "MISSING" { printf "      table %s disappeared (%s row(s) before)\n", $1, $2; bad++; next }
    ($3 + 0) < ($2 + 0) { printf "      table %s went from %s to %s row(s)\n", $1, $2, $3; bad++ }
    END { exit (bad ? 1 : 0) }
  ' "$MIGRATE_OUT/row-delta.tsv"; then
  printf 'PASS  no table lost rows (%s table(s) compared)\n' "$(wc -l <"$MIGRATE_OUT/row-delta.tsv" | tr -d ' ')"
else
  note_failure "at least one table lost rows; see $MIGRATE_OUT/row-delta.tsv"
fi

if jq -e . "$MIGRATE_OUT/doctor.json" >/dev/null 2>&1; then
  BAD_CHECKS="$(jq -r '[.checks[] | select(.result == "error" or .result == "blocked")] | length' "$MIGRATE_OUT/doctor.json")"
  if [ "$BAD_CHECKS" = "0" ]; then
    printf 'PASS  doctor reports no blocked or error check (status: %s)\n' "$(jq -r '.status' "$MIGRATE_OUT/doctor.json")"
  else
    note_failure "doctor reports $BAD_CHECKS blocked/error check(s): $(jq -c '[.checks[] | select(.result == "error" or .result == "blocked") | {check_id, result, reason_code}]' "$MIGRATE_OUT/doctor.json")"
  fi
else
  note_failure "doctor did not produce parseable JSON (exit $doctor_status); see $MIGRATE_OUT/doctor.stderr"
fi

# Measured, never enforced — see the header.
log "measuring runbooks sync against the fixture vault"
set +e
docker exec -i -e ENGRAM_PROJECT=koi-garden "$CONTAINER" \
  engram project koi-garden runbooks sync --json --vault-dir /vault \
  >"$MIGRATE_OUT/vault-scan.json" 2>"$MIGRATE_OUT/vault-scan.stderr"
vault_status=$?
set -e
printf 'INFO  runbooks sync --vault-dir /vault exited %s; see %s\n' "$vault_status" "$MIGRATE_OUT/vault-scan.json"

printf '\nartifacts: %s\n' "$MIGRATE_OUT"
[ "$FAILURES" -eq 0 ] || exit 1
