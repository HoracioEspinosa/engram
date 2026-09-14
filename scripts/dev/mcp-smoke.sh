#!/usr/bin/env bash
# mcp-smoke.sh — drive one MCP session against the dev container and assert it.
#
# What it does: runs docker/dev/fixtures/mcp/session.jsonl through a single
# `engram mcp` process, one request per response, captures the NDJSON it writes
# back, and checks one assertion per request id (sixteen of them), printing PASS
# or FAIL for each and a summary of how many are plain successes and how many
# are failures this suite holds fixed.
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
# One behaviour of the current binary is held fixed rather than worked around,
# because a suite that skips what it cannot fix stops measuring it:
#   * Read tools do not recognise a project that only has a card (ids 6, 9-12
#     and 16).
# It is pinned to the exact code it reports today. A pinned expectation that
# starts succeeding is a FAIL, not a silent pass: the day the behaviour changes,
# the assertion has to be promoted rather than forgotten.
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

require_cmd docker jq rg
assert_no_live_db

SESSION_IN="$ROOT_DIR/docker/dev/fixtures/mcp/session.jsonl"
[ -f "$SESSION_IN" ] || fail "missing MCP session fixture: $SESSION_IN"

MCP_OUT="$OUT_DIR/mcp"
mkdir -p "$MCP_OUT"
SESSION_OUT="$MCP_OUT/session.out.jsonl"
SESSION_ERR="$MCP_OUT/session.stderr"

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
bad_line=0
line_no=0
while IFS= read -r line; do
  line_no=$((line_no + 1))
  [ -n "$line" ] || continue
  if ! printf '%s' "$line" | jq -e . >/dev/null 2>&1; then
    bad_line="$line_no"
    break
  fi
done <"$SESSION_OUT"
if [ "$bad_line" -ne 0 ]; then
  fail "line $bad_line of $SESSION_OUT is not JSON; no assertion below can be trusted"
fi
log "every response line parses as JSON"

# resp <id> prints the single response object carrying that id.
resp() {
  jq -c --argjson id "$1" 'select(.id? == $id)' "$SESSION_OUT"
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
  BASELINE_FILE="$MCP_OUT/tools-list-baseline.json"
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
assert_pinned_failure 16 "id 16 mem_project_card does not see a project that only has a card" unknown_project

assert_ok 4 "id 4 mem_task_upsert"
assert_ok 5 "id 5 mem_save accepts the project the process was started with"
assert_pinned_failure 6 "id 6 mem_search does not see the project" unknown_project
assert_ok 7 "id 7 mem_get_observation reads back what id 5 wrote"
assert_ok 8 "id 8 mem_evidence_add"
assert_pinned_failure 9 "id 9 mem_evidence_list does not see the project" unknown_project
assert_pinned_failure 10 "id 10 mem_task_list does not see the project" unknown_project
assert_pinned_failure 11 "id 11 mem_context_pack does not see the project" unknown_project
assert_pinned_failure 12 "id 12 mem_project_card does not see the project" unknown_project

assert_error_code 13 "id 13 mem_project_upsert refuses an explicit project it does not already know" unknown_project
assert_error_code 14 "id 14 mem_evidence_add refuses an absolute evidence path" absolute_path_rejected
assert_error_code 15 "id 15 mem_task_link refuses a graph_ref without a graph_commit" graph_commit_required

printf '\nmcp-smoke: %d/%d evaluated (%d ok, %d pinned)\n' \
  "$((OK_COUNT + PINNED_COUNT))" "$EVALUATED" "$OK_COUNT" "$PINNED_COUNT"
printf 'session:   %s\nstderr:    %s\n' "$SESSION_OUT" "$SESSION_ERR"

[ "$FAIL_COUNT" -eq 0 ] || exit 1
