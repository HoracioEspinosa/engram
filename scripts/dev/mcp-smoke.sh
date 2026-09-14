#!/usr/bin/env bash
# mcp-smoke.sh — drive one MCP session against the dev container and assert it.
#
# What it does: pipes docker/dev/fixtures/mcp/session.jsonl into a single
# `engram mcp` process over stdin, captures the NDJSON it writes back, and
# checks one assertion per request id (fifteen of them), printing PASS or FAIL
# for each and an N/15 summary.
#
# What it guarantees:
#   * One process, one session. MCP over stdio is stateful — initialize
#     negotiates the protocol version for the connection — so a per-request
#     `docker exec` would test fifteen unrelated handshakes instead of one
#     conversation.
#   * The tool catalogue can only grow. The `tools/list` reply is stored as
#     tools-list-<label>.json and compared against tools-list-baseline.json:
#     a name that disappears between phases fails here, which is the whole
#     point of keeping the additive rule enforceable rather than aspirational.
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
# The agent and projects profiles together register 28 tools today (18 + 10).
# The floor is an inequality, not an equality, because later phases add tools
# to the session and must not have to edit this number to stay green.
MIN_TOOLS=28

require_cmd docker jq
assert_no_live_db

SESSION_IN="$ROOT_DIR/docker/dev/fixtures/mcp/session.jsonl"
[ -f "$SESSION_IN" ] || fail "missing MCP session fixture: $SESSION_IN"

MCP_OUT="$OUT_DIR/mcp"
mkdir -p "$MCP_OUT"
SESSION_OUT="$MCP_OUT/session.out.jsonl"
SESSION_ERR="$MCP_OUT/session.stderr"

log "running the MCP session (--tools=agent,projects --project $PROJECT)"
set +e
docker exec -i -e "ENGRAM_PROJECT=$PROJECT" "$CONTAINER" \
  engram mcp --tools=agent,projects --project "$PROJECT" \
  <"$SESSION_IN" >"$SESSION_OUT" 2>"$SESSION_ERR"
session_status=$?
set -e
log "session exited $session_status ($(wc -l <"$SESSION_OUT" | tr -d ' ') response line(s))"

PASS_COUNT=0
FAIL_COUNT=0

record() {
  local verdict="$1" name="$2" detail="${3:-}"
  if [ "$verdict" = "PASS" ]; then
    PASS_COUNT=$((PASS_COUNT + 1))
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

# assert_ok <id> <name> — the call returned a result and did not report a
# tool-level error.
assert_ok() {
  local id="$1" name="$2" r
  r="$(resp "$id")"
  if [ -z "$r" ]; then
    record FAIL "$name" "no response with id $id"
    return
  fi
  if printf '%s' "$r" | jq -e 'has("error")' >/dev/null; then
    record FAIL "$name" "jsonrpc error: $(printf '%s' "$r" | jq -c '.error')"
    return
  fi
  if printf '%s' "$r" | jq -e '.result.isError == true' >/dev/null; then
    record FAIL "$name" "isError=true: $(printf '%s' "$r" | jq -r '.result.content[0].text // ""' | head -c 200)"
    return
  fi
  record PASS "$name"
}

# assert_typed_error <id> <name> — the call is expected to fail, and to fail
# with a machine-readable code rather than prose. Both envelopes are accepted:
# the projects tools emit `code`, the core tools emit `error_code`.
assert_typed_error() {
  local id="$1" name="$2" r text code
  r="$(resp "$id")"
  if [ -z "$r" ]; then
    record FAIL "$name" "no response with id $id"
    return
  fi
  if ! printf '%s' "$r" | jq -e '.result.isError == true' >/dev/null; then
    record FAIL "$name" "expected .result.isError == true, got: $(printf '%s' "$r" | jq -c '.result // .error')"
    return
  fi
  text="$(printf '%s' "$r" | jq -r '.result.content[0].text // ""')"
  code="$(printf '%s' "$text" | jq -r '(.code // .error_code // "")' 2>/dev/null || true)"
  if [ -z "$code" ] || [ "$code" = "null" ]; then
    record FAIL "$name" "no code/error_code in the error payload: $(printf '%s' "$text" | head -c 200)"
    return
  fi
  record PASS "$name ($code)"
}

# id 1 — initialize: the connection negotiated a protocol version.
init_resp="$(resp 1)"
if [ -n "$init_resp" ] && printf '%s' "$init_resp" | jq -e '.result.protocolVersion | type == "string" and length > 0' >/dev/null; then
  record PASS "id 1 initialize negotiated protocolVersion $(printf '%s' "$init_resp" | jq -r '.result.protocolVersion')"
else
  record FAIL "id 1 initialize negotiated a protocolVersion" "got: $(printf '%s' "$init_resp" | jq -c '.result // .error // "no response"')"
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
  record PASS "id 2 tools/list is complete and additive (${tool_count:-0} tools)"
else
  record FAIL "id 2 tools/list is complete and additive" "$tools_detail"
fi

assert_ok 3 "id 3 mem_project_upsert"
assert_ok 4 "id 4 mem_task_upsert"
assert_ok 5 "id 5 mem_save"
assert_ok 6 "id 6 mem_search"
assert_ok 7 "id 7 mem_get_observation"
assert_ok 8 "id 8 mem_evidence_add"
assert_ok 9 "id 9 mem_evidence_list"
assert_ok 10 "id 10 mem_task_list"
assert_ok 11 "id 11 mem_context_pack"

# id 12 — mem_project_card: the projects envelope is intact. The three keys
# below are the contract every projects tool answers with, and the phase rule
# is that they keep their shape while new fields appear beside them.
card_resp="$(resp 12)"
if [ -n "$card_resp" ] && printf '%s' "$card_resp" \
  | jq -e '.result.content[0].text | fromjson | has("project") and has("project_source") and has("result")' >/dev/null 2>&1; then
  record PASS "id 12 mem_project_card keeps the {project, project_source, result} envelope"
else
  record FAIL "id 12 mem_project_card keeps the {project, project_source, result} envelope" \
    "got: $(printf '%s' "$card_resp" | jq -r '.result.content[0].text // .error // "no response"' | head -c 200)"
fi

assert_typed_error 13 "id 13 rejects the invalid slug with a typed code"
assert_typed_error 14 "id 14 rejects the absolute evidence path with a typed code"
assert_typed_error 15 "id 15 rejects the graph reference without a commit with a typed code"

TOTAL=$((PASS_COUNT + FAIL_COUNT))
printf '\nmcp-smoke: %d/%d\n' "$PASS_COUNT" "$TOTAL"
printf 'session:   %s\nstderr:    %s\n' "$SESSION_OUT" "$SESSION_ERR"

[ "$FAIL_COUNT" -eq 0 ] || exit 1
