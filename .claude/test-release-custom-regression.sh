#!/usr/bin/env bash
# Regression test: Validates that release-custom.yml has the correct branch pattern.
#
# This test ensures the guard job checks for origin/main (not origin/custom/main).
# If someone reverts the fix, this test MUST fail.
#
# Execution: bash .claude/test-release-custom-regression.sh
# Expected: exit code 0 (PASS)

set -euo pipefail

echo "=== Regression Test: release-custom.yml guard branch check ==="
echo

# Allow override via environment variable for testing
WORKFLOW_FILE="${TEST_WORKFLOW_FILE:-.github/workflows/release-custom.yml}"

if [ ! -f "$WORKFLOW_FILE" ]; then
  echo "✗ FAIL: $WORKFLOW_FILE not found"
  exit 1
fi

echo "File: $WORKFLOW_FILE"
echo "Searching for the guard branch check pattern..."
echo

# The guard job should check for origin/main (current main branch)
# NOT origin/custom/main (retired in commit 7924c26)

# Extract the guard check line
guard_line=$(sed -n '59,70p' "$WORKFLOW_FILE" | grep -E "awk.*origin.*main" | head -1)

if [ -z "$guard_line" ]; then
  echo "✗ FAIL: Could not find guard branch check pattern in lines 59-70"
  exit 1
fi

echo "Found guard check:"
echo "  $guard_line"
echo

# Validation
# The pattern MUST have origin/main (in any form: escaped or not)
# The pattern MUST NOT have origin/custom/main

if echo "$guard_line" | grep -q "origin/custom/main\|origin\\\\/custom\\\\/main"; then
  echo "✗ FAIL: Found retired pattern origin/custom/main in guard check"
  echo "       This branch was removed in commit 7924c26."
  exit 1
fi

if ! echo "$guard_line" | grep -q "origin.*main"; then
  echo "✗ FAIL: Did not find origin/main pattern in guard check"
  exit 1
fi

echo "Pattern analysis:"
if echo "$guard_line" | grep -q "origin\\\\/main"; then
  echo "  ✓ Found escaped origin\\/main in awk pattern"
elif echo "$guard_line" | grep -q "origin/main"; then
  echo "  ✓ Found unescaped origin/main in pattern"
else
  echo "  ✗ FAIL: Pattern structure unexpected"
  exit 1
fi
echo "  ✓ No retired origin/custom/main found"
echo

echo "✓ PASS: Guard check has correct branch pattern (origin/main)"
echo "✓ REGRESSION TEST PASSED"
exit 0
