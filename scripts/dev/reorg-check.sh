#!/usr/bin/env bash
# reorg-check.sh — prove a project reorganisation loses nothing.
#
# What it does: against the copy of the real store installed at /data/live by
# `seed.sh --with-live-copy`, it counts observations per project, runs the
# reorg MCP session (mem_merge_projects and the project tools, over
# --tools=admin,projects), counts again, snapshots the unacknowledged outbox on
# both sides, runs the session a second time and requires the second result to
# be byte for byte identical to the first.
#
# What it guarantees:
#   * No observation is created or destroyed by a merge. The total before and
#     after has to match exactly; only its distribution across projects may
#     change.
#   * The reorganisation is idempotent, so it can be replayed on the real store
#     later without compounding.
#   * Every unacknowledged outbox row is still valid JSON afterwards, which is
#     what a replica pulling these mutations will actually parse.
#
# It runs against /data/live and never against /data: /data holds the fixture
# store the rest of this directory asserts on, and this script mutates what it
# points at. The original ~/.engram/engram.db is not reachable from here at
# all — only the copy backup-live.sh produced.
#
# The orphan count (observations whose project has no card) is reported, not
# enforced: on the real data most observations predate project cards entirely,
# so a threshold here would fail on day one for a reason that has nothing to do
# with the merge.

set -euo pipefail

# shellcheck source=scripts/dev/lib.sh
. "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"

require_cmd docker jq
assert_no_live_db

LIVE_DIR=/data/live
LIVE_DB="$LIVE_DIR/engram.db"
SESSION_IN="$ROOT_DIR/docker/dev/fixtures/mcp/reorg.jsonl"
[ -f "$SESSION_IN" ] || fail "missing reorg session fixture: $SESSION_IN"

REORG_OUT="$OUT_DIR/reorg"
mkdir -p "$REORG_OUT"

in_container test -f "$LIVE_DB" \
  || fail "$LIVE_DB is not installed; run seed.sh --with-live-copy <copy.db> first"

# sql runs one statement against the copy inside the container.
sql() {
  in_container sqlite3 "$LIVE_DB" "$1"
}

# sql_tsv runs one statement with tab-separated output, for the count files.
sql_tsv() {
  in_container sqlite3 -noheader -separator '	' "$LIVE_DB" "$1"
}

COUNTS_SQL="SELECT project, COUNT(*) FROM observations WHERE deleted_at IS NULL GROUP BY project ORDER BY project;"
TOTAL_SQL="SELECT COUNT(*) FROM observations WHERE deleted_at IS NULL;"
# sync_mutations carries project as its own column (backfilled from the JSON
# payload), so the outbox snapshot does not have to reach into the payload to
# say which project a pending mutation belongs to.
OUTBOX_SQL="SELECT json_object('seq', seq, 'project', project, 'entity', entity, 'payload', payload) FROM sync_mutations WHERE acked_at IS NULL ORDER BY seq;"

log "counting observations before the reorg"
sql_tsv "$COUNTS_SQL" >"$REORG_OUT/before-projects.tsv"
TOTAL_BEFORE="$(sql "$TOTAL_SQL")"
sql "$OUTBOX_SQL" >"$REORG_OUT/outbox-before.jsonl"
log "before: $TOTAL_BEFORE observation(s) across $(wc -l <"$REORG_OUT/before-projects.tsv" | tr -d ' ') project(s)"

# mem_doctor is named on its own alongside the two profiles because it belongs
# to the agent profile (internal/mcp/mcp.go:113), and the reorg session calls
# it. ResolveTools (mcp.go:156-185) resolves a token that is not a profile name
# as an individual tool, so mixing the two forms is what the flag is for and
# the agent profile does not have to be pulled in wholesale to reach one tool.
run_session() {
  local out="$1"
  docker exec -i -e "ENGRAM_DATA_DIR=$LIVE_DIR" "$CONTAINER" \
    engram mcp --tools=admin,projects,mem_doctor \
    <"$SESSION_IN" >"$out" 2>"${out%.jsonl}.stderr"
}

log "running the reorg session"
set +e
run_session "$REORG_OUT/session.out.jsonl"
first_status=$?
set -e
log "reorg session exited $first_status"

log "counting observations after the reorg"
sql_tsv "$COUNTS_SQL" >"$REORG_OUT/after-projects.tsv"
TOTAL_AFTER="$(sql "$TOTAL_SQL")"
sql "$OUTBOX_SQL" >"$REORG_OUT/outbox-after.jsonl"
log "after: $TOTAL_AFTER observation(s) across $(wc -l <"$REORG_OUT/after-projects.tsv" | tr -d ' ') project(s)"

FAILURES=0
note_failure() {
  FAILURES=$((FAILURES + 1))
  printf 'FAIL  %s\n' "$*"
}

if [ "$TOTAL_BEFORE" = "$TOTAL_AFTER" ]; then
  printf 'PASS  observation total unchanged (%s)\n' "$TOTAL_BEFORE"
else
  note_failure "observation total changed: $TOTAL_BEFORE before, $TOTAL_AFTER after"
fi

# Every pending outbox row has to stay parseable: a replica applies these by
# decoding the payload, so a merge that rewrote it into invalid JSON would only
# fail on the other side of the sync, far from its cause.
bad_payloads=0
while IFS= read -r row; do
  [ -n "$row" ] || continue
  if ! printf '%s' "$row" | jq -e '.payload | fromjson' >/dev/null 2>&1; then
    bad_payloads=$((bad_payloads + 1))
  fi
done <"$REORG_OUT/outbox-after.jsonl"
if [ "$bad_payloads" -eq 0 ]; then
  printf 'PASS  every unacknowledged outbox payload is valid JSON (%s row(s))\n' \
    "$(wc -l <"$REORG_OUT/outbox-after.jsonl" | tr -d ' ')"
else
  note_failure "$bad_payloads unacknowledged outbox row(s) no longer carry valid JSON payloads"
fi

# Reported, never enforced — see the header.
has_cards="$(sql "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='project_cards';")"
if [ "$has_cards" = "1" ]; then
  ORPHANS="$(sql "SELECT COUNT(*) FROM observations o LEFT JOIN project_cards c ON c.slug = o.project WHERE c.slug IS NULL AND o.deleted_at IS NULL;")"
  printf 'INFO  %s observation(s) sit in a project with no project_cards row\n' "$ORPHANS"
  printf '%s\n' "$ORPHANS" >"$REORG_OUT/orphan-observations.txt"
else
  printf 'INFO  project_cards is absent from this copy; orphan count skipped\n'
fi

log "running the reorg session a second time to check idempotence"
set +e
run_session "$REORG_OUT/session-rerun.out.jsonl"
second_status=$?
set -e
log "second reorg session exited $second_status"
sql_tsv "$COUNTS_SQL" >"$REORG_OUT/after-projects-rerun.tsv"

if cmp -s "$REORG_OUT/after-projects.tsv" "$REORG_OUT/after-projects-rerun.tsv"; then
  printf 'PASS  the second run produced an identical distribution\n'
else
  note_failure "the second run changed the distribution; see after-projects.tsv vs after-projects-rerun.tsv"
fi

log "running doctor against the copy"
docker exec -i -e "ENGRAM_DATA_DIR=$LIVE_DIR" "$CONTAINER" \
  engram doctor --json >"$REORG_OUT/doctor.json"

# internal/diagnostic has no "fail" status: a check reports ok, warning,
# blocked or error, and the report's own status is the worst of them. Blocked
# and error are the two that mean something is actually broken.
BAD_CHECKS="$(jq -r '[.checks[] | select(.result == "error" or .result == "blocked")] | length' "$REORG_OUT/doctor.json")"
if [ "$BAD_CHECKS" = "0" ]; then
  printf 'PASS  doctor reports no blocked or error check (status: %s)\n' "$(jq -r '.status' "$REORG_OUT/doctor.json")"
else
  note_failure "doctor reports $BAD_CHECKS blocked/error check(s): $(jq -c '[.checks[] | select(.result == "error" or .result == "blocked") | {check_id, result, reason_code}]' "$REORG_OUT/doctor.json")"
fi

printf '\nartifacts: %s\n' "$REORG_OUT"
[ "$FAILURES" -eq 0 ] || exit 1
