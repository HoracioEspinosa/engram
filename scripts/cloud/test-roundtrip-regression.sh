#!/bin/bash
# Regression test for T-04.05: detect bugs in projects-roundtrip-rehearsal.sh
# FAILS if the bugs are fixed, PASSES if bugs are present
# This proves our fixes actually address real problems

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REHEARSAL_SCRIPT="${SCRIPT_DIR}/projects-roundtrip-rehearsal.sh"

log() { echo "[test-regression] $*"; }
pass() { echo "[test-regression] ✓ PASS: $*"; }
fail() { echo "[test-regression] ✗ FAIL: $*"; exit 1; }

# Test 1: BUG - jq with dangling quote must be present in the script
log "TEST 1: Dangling quote in jq exists in script"

if grep -q "jq -r // empty'" "${REHEARSAL_SCRIPT}"; then
	pass "BUG DETECTED: Script contains jq -r // empty' (dangling quote)"
else
	fail "BUG FIXED: jq -r // empty' not found (quote bug is fixed)"
fi

# Test 2: BUG - False commands must be present in the script
log "TEST 2: False commands exist in script"

false_cmd_found=0

if grep -q "project card create" "${REHEARSAL_SCRIPT}"; then
	pass "BUG DETECTED: Script calls 'project card create' (false command)"
	false_cmd_found=$((false_cmd_found + 1))
else
	fail "BUG FIXED: 'project card create' not found in script"
fi

if grep -q "task create" "${REHEARSAL_SCRIPT}"; then
	pass "BUG DETECTED: Script calls 'task create' (false command)"
	false_cmd_found=$((false_cmd_found + 1))
else
	fail "BUG FIXED: 'task create' not found in script"
fi

if grep -q "evidence create" "${REHEARSAL_SCRIPT}"; then
	pass "BUG DETECTED: Script calls 'evidence create' (false command)"
	false_cmd_found=$((false_cmd_found + 1))
else
	fail "BUG FIXED: 'evidence create' not found in script"
fi

[[ ${false_cmd_found} -eq 3 ]] || fail "Not all false commands found in script"

# Test 3: BUG - Toothless assertions must be present
log "TEST 3: Toothless assertions exist in script"

toothless_count=$(grep -n "log \"Note:" "${REHEARSAL_SCRIPT}" | wc -l)

if [[ ${toothless_count} -gt 0 ]]; then
	pass "BUG DETECTED: Script has ${toothless_count} 'Note:' fallback messages (toothless assertions)"
else
	fail "BUG FIXED: No 'Note:' fallback messages found (assertions are now real)"
fi

log ""
pass "ALL BUGS DETECTED: Script is still broken in all 3 ways"
exit 0
