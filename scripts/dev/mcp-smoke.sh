#!/usr/bin/env bash
# mcp-smoke.sh — drive one MCP session against the dev container and assert it.
#
# What it does: runs two MCP sessions against the dev container, one request per
# response, captures the NDJSON each writes back, and checks one assertion per
# request id, printing PASS or FAIL for each and a summary of how many are plain
# successes and how many are failures this suite holds fixed.
#
# The sessions are separate processes on purpose. The first
# (docker/dev/fixtures/mcp/session.jsonl, --tools=agent,projects) is the
# catalogue every other script depends on; the second
# (session-workspace.jsonl, --tools=agent,projects,workspace) adds the workspace
# profile. Loading a bigger profile into the first would change the number the
# baseline comparison is built on, so the workspace tools get a session of their
# own — ids 100 and up — over the same store the first one left behind, which is
# also what lets a scan find the task the first session created.
#
# What it guarantees:
#   * One number, every run. The session runs against /data/smoke, a data
#     directory this script deletes and recreates first, so no other script's
#     writes can change the answer; and it runs in lockstep, so no response can
#     overtake the write it depends on.
#   * One process, one session. MCP over stdio is stateful — initialize
#     negotiates the protocol version for the connection — so a per-request
#     `docker exec` would test sixteen unrelated handshakes instead of one
#     conversation.
#   * The tool catalogue can only grow. The `tools/list` reply is stored as
#     tools-list-<label>.json and compared against tools-list-baseline.json:
#     a name that disappears between runs fails here, which is the whole
#     point of keeping the additive rule enforceable rather than aspirational.
#
# No behaviour is held fixed today: every call either succeeds or is rejected
# for an input that is invalid on purpose. When a defect this suite is not
# allowed to fix shows up, assert_pinned_failure below records it against the
# exact code it reports, so it stays measured instead of skipped — and a pinned
# expectation that starts succeeding is a FAIL, not a silent pass: the day the
# behaviour changes, the assertion has to be promoted rather than forgotten.
#
# The transport is NDJSON: mcp-go's stdio server reads one JSON value per line
# and writes one per line, so every line of the capture is independently
# parseable and the assertions below are plain jq selects by id.
#
# Usage: mcp-smoke.sh [label]   (label defaults to "baseline")

set -euo pipefail

# shellcheck source=scripts/dev/lib.sh
. "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"

LABEL="${1:-baseline}"
PROJECT="koi-garden"
# A data directory of its own, wiped on every run. The fixture store at /data is
# what seed.sh fills and what the rest of this directory asserts on; sharing it
# would make this suite's result depend on whether seed.sh ran first.
SMOKE_DATA_DIR=/data/smoke
# The agent and projects profiles together register 28 tools today (18 + 10).
# The floor is an inequality, not an equality: the tool catalogue only grows,
# so adding a tool to the session must not require editing this number to
# stay green.
MIN_TOOLS=28
# The second session asks for agent,projects,workspace, and that set is exact:
# 18 + 10 + 7 unique names, the three tools the workspace profile shares with
# projects counted once. An equality here is what makes "the recommended set is
# 35 tools" a number the docs can quote.
WORKSPACE_TOOLS=35

require_cmd docker jq rg
assert_no_live_db

SESSION_IN="$ROOT_DIR/docker/dev/fixtures/mcp/session.jsonl"
[ -f "$SESSION_IN" ] || fail "missing MCP session fixture: $SESSION_IN"
WORKSPACE_IN="$ROOT_DIR/docker/dev/fixtures/mcp/session-workspace.jsonl"
[ -f "$WORKSPACE_IN" ] || fail "missing MCP workspace session fixture: $WORKSPACE_IN"

MCP_OUT="$OUT_DIR/mcp"
mkdir -p "$MCP_OUT"
SESSION_OUT="$MCP_OUT/session.out.jsonl"
SESSION_ERR="$MCP_OUT/session.stderr"
WORKSPACE_OUT="$MCP_OUT/session-workspace.out.jsonl"
WORKSPACE_ERR="$MCP_OUT/session-workspace.stderr"
# The stored reference the tool catalogue may only grow away from. It is the
# agent,projects list, so the workspace session compares against it too and only
# ever adds to it.
BASELINE_FILE="$MCP_OUT/tools-list-baseline.json"

log "resetting $SMOKE_DATA_DIR so the session starts from an empty store"
in_container rm -rf "$SMOKE_DATA_DIR"
in_container mkdir -p "$SMOKE_DATA_DIR"

log "running the MCP session in lockstep (--tools=agent,projects --project $PROJECT over $SMOKE_DATA_DIR)"
set +e
mcp_session_lockstep "$SESSION_IN" "$SESSION_OUT" "$SESSION_ERR" -- \
  docker exec -i -e "ENGRAM_DATA_DIR=$SMOKE_DATA_DIR" -e "ENGRAM_PROJECT=$PROJECT" "$CONTAINER" \
  engram mcp --tools=agent,projects --project "$PROJECT"
session_status=$?
set -e
log "session exited $session_status ($(wc -l <"$SESSION_OUT" | tr -d ' ') response line(s))"

EVALUATED=0
OK_COUNT=0
PINNED_COUNT=0
FAIL_COUNT=0

# record <PASS|FAIL> <ok|pinned> <name> [detail]
# The kind splits the summary: an "ok" is a call that works, a "pinned" is a
# call that fails the same way it is documented to fail. Both are green today;
# only the first should still be green once the binary is fixed.
record() {
  local verdict="$1" kind="$2" name="$3" detail="${4:-}"
  EVALUATED=$((EVALUATED + 1))
  if [ "$verdict" = "PASS" ]; then
    case "$kind" in
      pinned) PINNED_COUNT=$((PINNED_COUNT + 1)) ;;
      *) OK_COUNT=$((OK_COUNT + 1)) ;;
    esac
    printf 'PASS  %s\n' "$name"
  else
    FAIL_COUNT=$((FAIL_COUNT + 1))
    printf 'FAIL  %s%s\n' "$name" "${detail:+ — $detail}"
  fi
}

# Every line has to be JSON before any assertion can mean anything: a single
# stray log line on stdout would otherwise make later jq selects silently
# return nothing, and an assertion that matches nothing looks like a pass.
assert_every_line_is_json() {
  local capture="$1" bad_line=0 line_no=0 line
  while IFS= read -r line; do
    line_no=$((line_no + 1))
    [ -n "$line" ] || continue
    if ! printf '%s' "$line" | jq -e . >/dev/null 2>&1; then
      bad_line="$line_no"
      break
    fi
  done <"$capture"
  if [ "$bad_line" -ne 0 ]; then
    fail "line $bad_line of $capture is not JSON; no assertion below can be trusted"
  fi
  log "every response line of $capture parses as JSON"
}

assert_every_line_is_json "$SESSION_OUT"

# CAPTURE is the response file the assertions below read. It is switched once,
# between the two sessions, so every helper keeps taking an id and nothing else.
CAPTURE="$SESSION_OUT"

# resp <id> prints the single response object carrying that id.
resp() {
  jq -c --argjson id "$1" 'select(.id? == $id)' "$CAPTURE"
}

# err_text <response> prints the tool-level error message the call reported.
err_text() {
  printf '%s' "$1" | jq -r '.result.content[0].text // ""'
}

# assert_ok <id> <name> — the call returned a result and did not report a
# tool-level error.
assert_ok() {
  local id="$1" name="$2" r
  r="$(resp "$id")"
  if [ -z "$r" ]; then
    record FAIL ok "$name" "no response with id $id"
    return
  fi
  if printf '%s' "$r" | jq -e 'has("error")' >/dev/null; then
    record FAIL ok "$name" "jsonrpc error: $(printf '%s' "$r" | jq -c '.error')"
    return
  fi
  if printf '%s' "$r" | jq -e '.result.isError == true' >/dev/null; then
    record FAIL ok "$name" "isError=true: $(err_text "$r" | head -c 200)"
    return
  fi
  record PASS ok "$name"
}

# assert_error_code <id> <name> <code> — the call is expected to fail, and to
# fail with exactly this machine-readable code. Both envelopes are accepted:
# the projects tools emit `code`, the core tools emit `error_code`.
#
# This is deliberately not assert_pinned_failure: the input here is invalid
# on purpose (an unresolvable project, a path traversal attempt), so the
# rejection is the binary working as designed, not a limitation this suite
# holds fixed. Recording it as "ok" keeps the summary's "pinned" count meaning
# what it says — calls that fail only because of a documented defect.
assert_error_code() {
  local id="$1" name="$2" want="$3" r text code
  r="$(resp "$id")"
  if [ -z "$r" ]; then
    record FAIL ok "$name" "no response with id $id"
    return
  fi
  if ! printf '%s' "$r" | jq -e '.result.isError == true' >/dev/null; then
    record FAIL ok "$name" "expected .result.isError == true, got: $(printf '%s' "$r" | jq -c '.result // .error')"
    return
  fi
  text="$(err_text "$r")"
  code="$(printf '%s' "$text" | jq -r '(.code // .error_code // "")' 2>/dev/null || true)"
  if [ "$code" = "$want" ]; then
    record PASS ok "$name ($code)"
  else
    record FAIL ok "$name" "expected $want, got ${code:-<no code>}: $(printf '%s' "$text" | head -c 200)"
  fi
}

# assert_pinned_failure <id> <name> <code-or-regex> — the call fails today, and
# this is the exact failure. The third argument is a typed code, or a regex
# anchored with ^ for the one call whose error is prose rather than an envelope.
#
# Succeeding is a failure of this assertion. The point of pinning is that the
# suite stays honest about what is broken: a pinned call that starts working
# has to be promoted to assert_ok in the same change that fixes it, or the
# suite goes back to reporting a number nobody can act on.
assert_pinned_failure() {
  local id="$1" name="$2" want="$3" r text code
  r="$(resp "$id")"
  if [ -z "$r" ]; then
    record FAIL pinned "$name" "no response with id $id"
    return
  fi
  if printf '%s' "$r" | jq -e 'has("error")' >/dev/null; then
    record FAIL pinned "$name" "jsonrpc error rather than a tool error: $(printf '%s' "$r" | jq -c '.error')"
    return
  fi
  if ! printf '%s' "$r" | jq -e '.result.isError == true' >/dev/null; then
    record FAIL pinned "$name" "pinned failure for id $id no longer reproduces; promote it to assert_ok"
    return
  fi
  text="$(err_text "$r")"
  case "$want" in
    '^'*)
      if printf '%s' "$text" | rg -q -- "$want"; then
        record PASS pinned "$name"
      else
        record FAIL pinned "$name" "expected a message matching /$want/, got: $(printf '%s' "$text" | head -c 200)"
      fi
      ;;
    *)
      code="$(printf '%s' "$text" | jq -r '(.code // .error_code // "")' 2>/dev/null || true)"
      if [ "$code" = "$want" ]; then
        record PASS pinned "$name ($code)"
      else
        record FAIL pinned "$name" "expected $want, got ${code:-<no code>}: $(printf '%s' "$text" | head -c 200)"
      fi
      ;;
  esac
}

# id 1 — initialize: the connection negotiated a protocol version.
init_resp="$(resp 1)"
if [ -n "$init_resp" ] && printf '%s' "$init_resp" | jq -e '.result.protocolVersion | type == "string" and length > 0' >/dev/null; then
  record PASS ok "id 1 initialize negotiated protocolVersion $(printf '%s' "$init_resp" | jq -r '.result.protocolVersion')"
else
  record FAIL ok "id 1 initialize negotiated a protocolVersion" "got: $(printf '%s' "$init_resp" | jq -c '.result // .error // "no response"')"
fi

# id 2 — tools/list: the catalogue is big enough, carries the names the rest of
# this suite depends on, and has not lost anything since the stored baseline.
tools_resp="$(resp 2)"
tools_ok=1
tools_detail=""
if [ -z "$tools_resp" ]; then
  tools_ok=0
  tools_detail="no response with id 2"
else
  tool_count="$(printf '%s' "$tools_resp" | jq -r '.result.tools | length')"
  if [ "${tool_count:-0}" -lt "$MIN_TOOLS" ]; then
    tools_ok=0
    tools_detail="only $tool_count tools, expected at least $MIN_TOOLS"
  fi

  TOOLS_FILE="$MCP_OUT/tools-list-${LABEL}.json"
  PREVIOUS=""
  if [ -f "$BASELINE_FILE" ]; then
    PREVIOUS="$MCP_OUT/.tools-list-baseline.previous.json"
    cp "$BASELINE_FILE" "$PREVIOUS"
  fi
  printf '%s' "$tools_resp" | jq '.result.tools | map(.name) | sort' >"$TOOLS_FILE"
  log "tool catalogue ($tool_count) stored at $TOOLS_FILE"

  for want in mem_context_pack mem_project_card mem_evidence_add; do
    if ! jq -e --arg n "$want" 'index($n) != null' "$TOOLS_FILE" >/dev/null; then
      tools_ok=0
      tools_detail="${tools_detail:+$tools_detail; }$want is not registered"
    fi
  done

  if [ -n "$PREVIOUS" ]; then
    missing="$(jq -r --slurpfile now "$TOOLS_FILE" '. - $now[0] | join(", ")' "$PREVIOUS")"
    if [ -n "$missing" ]; then
      tools_ok=0
      tools_detail="${tools_detail:+$tools_detail; }gone since the baseline: $missing"
    else
      log "no tool disappeared since $BASELINE_FILE"
    fi
    rm -f "$PREVIOUS"
  fi
fi
if [ "$tools_ok" -eq 1 ]; then
  record PASS ok "id 2 tools/list is complete and additive (${tool_count:-0} tools)"
else
  record FAIL ok "id 2 tools/list is complete and additive" "$tools_detail"
fi

assert_ok 3 "id 3 mem_project_upsert"

# id 16 — the card written by id 3 is read back before anything else has been
# written to this store, so this is the one call that isolates the read path
# from whether an observation happens to exist.
assert_ok 16 "id 16 mem_project_card reads a project that only has a card"

assert_ok 4 "id 4 mem_task_upsert"
assert_ok 5 "id 5 mem_save accepts the project the process was started with"
assert_ok 6 "id 6 mem_search"
assert_ok 7 "id 7 mem_get_observation reads back what id 5 wrote"
assert_ok 8 "id 8 mem_evidence_add"
assert_ok 9 "id 9 mem_evidence_list"
assert_ok 10 "id 10 mem_task_list"
assert_ok 11 "id 11 mem_context_pack"
assert_ok 12 "id 12 mem_project_card"

# The name carries an underscore on purpose: folding the separators is the last
# thing project resolution tries, and a name that is unknown even after folding
# is the one this guard has to keep refusing.
assert_error_code 13 "id 13 mem_project_upsert refuses an explicit project no spelling resolves" unknown_project
assert_error_code 14 "id 14 mem_evidence_add refuses an absolute evidence path" absolute_path_rejected
assert_error_code 15 "id 15 mem_task_link refuses a graph_ref without a graph_commit" graph_commit_required

# ─── Second session: the workspace profile ───────────────────────────────────
#
# It runs over the same /data/smoke the first session filled, which is what
# gives the scan a task to attach evidence to, and against the read-only /vault
# the compose file mounts. Every write here is a dry run except the one
# benchmark, so re-running the suite is the same answer twice.

log "running the workspace MCP session in lockstep (--tools=agent,projects,workspace over $SMOKE_DATA_DIR)"
set +e
mcp_session_lockstep "$WORKSPACE_IN" "$WORKSPACE_OUT" "$WORKSPACE_ERR" -- \
  docker exec -i -e "ENGRAM_DATA_DIR=$SMOKE_DATA_DIR" -e "ENGRAM_PROJECT=$PROJECT" "$CONTAINER" \
  engram mcp --tools=agent,projects,workspace --project "$PROJECT"
workspace_status=$?
set -e
log "workspace session exited $workspace_status ($(wc -l <"$WORKSPACE_OUT" | tr -d ' ') response line(s))"

assert_every_line_is_json "$WORKSPACE_OUT"
CAPTURE="$WORKSPACE_OUT"

# id 100 — initialize: the second connection negotiated its own protocol version.
ws_init="$(resp 100)"
if [ -n "$ws_init" ] && printf '%s' "$ws_init" | jq -e '.result.protocolVersion | type == "string" and length > 0' >/dev/null; then
  record PASS ok "id 100 initialize negotiated protocolVersion $(printf '%s' "$ws_init" | jq -r '.result.protocolVersion')"
else
  record FAIL ok "id 100 initialize negotiated a protocolVersion" "got: $(printf '%s' "$ws_init" | jq -c '.result // .error // "no response"')"
fi

# id 101 — tools/list with the workspace profile: exactly the recommended set,
# stored for the phase gate and compared against the baseline the first session
# wrote, which it may only add to.
ws_tools="$(resp 101)"
ws_tools_ok=1
ws_tools_detail=""
if [ -z "$ws_tools" ]; then
  ws_tools_ok=0
  ws_tools_detail="no response with id 101"
else
  ws_count="$(printf '%s' "$ws_tools" | jq -r '.result.tools | length')"
  if [ "${ws_count:-0}" -ne "$WORKSPACE_TOOLS" ]; then
    ws_tools_ok=0
    ws_tools_detail="$ws_count tools, expected exactly $WORKSPACE_TOOLS"
  fi

  # The labelled artifact carries the recommended set, which is what a phase
  # gate quotes. The one exception is the stored reference itself: overwriting
  # tools-list-baseline.json with a bigger profile would make every later
  # agent,projects run report the workspace tools as missing.
  WS_TOOLS_FILE="$MCP_OUT/tools-list-${LABEL}.json"
  if [ "$WS_TOOLS_FILE" = "$BASELINE_FILE" ]; then
    WS_TOOLS_FILE="$MCP_OUT/tools-list-${LABEL}-workspace.json"
  fi
  printf '%s' "$ws_tools" | jq '.result.tools | map(.name) | sort' >"$WS_TOOLS_FILE"
  log "workspace tool catalogue ($ws_count) stored at $WS_TOOLS_FILE"

  for want in mem_project_tree mem_evidence_scan mem_benchmark_add mem_benchmark_list \
    mem_benchmark_import mem_vault_sync mem_workspace_search; do
    if ! jq -e --arg n "$want" 'index($n) != null' "$WS_TOOLS_FILE" >/dev/null; then
      ws_tools_ok=0
      ws_tools_detail="${ws_tools_detail:+$ws_tools_detail; }$want is not registered"
    fi
  done

  if [ -f "$BASELINE_FILE" ]; then
    ws_missing="$(jq -r --slurpfile now "$WS_TOOLS_FILE" '. - $now[0] | join(", ")' "$BASELINE_FILE")"
    if [ -n "$ws_missing" ]; then
      ws_tools_ok=0
      ws_tools_detail="${ws_tools_detail:+$ws_tools_detail; }gone since the baseline: $ws_missing"
    else
      log "the workspace catalogue only adds to $BASELINE_FILE"
    fi
  fi
fi
if [ "$ws_tools_ok" -eq 1 ]; then
  record PASS ok "id 101 tools/list carries the workspace profile (${ws_count:-0} tools)"
else
  record FAIL ok "id 101 tools/list carries the workspace profile" "$ws_tools_detail"
fi

assert_ok 102 "id 102 mem_project_upsert records the knowledge hub"
assert_ok 103 "id 103 mem_task_upsert records the task's vault folder"
assert_ok 104 "id 104 mem_project_tree walks koi-garden with counts"
assert_ok 105 "id 105 mem_evidence_scan dry-runs KOI-1099 over /vault"
assert_ok 106 "id 106 mem_benchmark_add records the lookup.p95 baseline"
assert_ok 107 "id 107 mem_benchmark_list reads the measurement back"
assert_ok 108 "id 108 mem_benchmark_import dry-runs an engram.benchmark.v1 run"
assert_ok 109 "id 109 mem_vault_sync dry-runs the whole vault"
assert_ok 110 "id 110 mem_workspace_search answers across kinds"

assert_error_code 111 "id 111 mem_project_tree refuses a root with no card" unknown_project
assert_error_code 112 "id 112 mem_evidence_scan refuses a task nothing answers to" unknown_task
assert_error_code 113 "id 113 mem_benchmark_add refuses a unit nothing compares" invalid_enum
assert_error_code 114 "id 114 mem_benchmark_list refuses a task nothing answers to" unknown_task
assert_error_code 115 "id 115 mem_benchmark_import refuses a run that is not there" run_not_found
assert_error_code 116 "id 116 mem_benchmark_import refuses a foreign run with no pointer map" not_engram_benchmark_v1
assert_error_code 117 "id 117 mem_vault_sync refuses a root that is not a directory" vault_root_unresolved
assert_error_code 118 "id 118 mem_workspace_search refuses a one-character query" query_too_short
assert_error_code 119 "id 119 mem_benchmark_add refuses a second value under the key id 106 recorded" duplicate_benchmark

printf '\nmcp-smoke: %d/%d evaluated (%d ok, %d pinned)\n' \
  "$((OK_COUNT + PINNED_COUNT))" "$EVALUATED" "$OK_COUNT" "$PINNED_COUNT"
printf 'session:   %s\nstderr:    %s\n' "$SESSION_OUT" "$SESSION_ERR"
printf 'workspace: %s\nstderr:    %s\n' "$WORKSPACE_OUT" "$WORKSPACE_ERR"

[ "$FAIL_COUNT" -eq 0 ] || exit 1
