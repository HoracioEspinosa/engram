#!/usr/bin/env bash
# Regression test for T-04.08 hueco 1: guard job must check origin/main, not origin/custom/main
#
# The guard job in release-custom.yml validates that a tag points to a commit on
# origin/main. Before commit 22dc38e, the awk pattern incorrectly searched for
# origin/custom/main, which no longer exists after 7924c26. Any release tag would
# fail silently, making the whole release pipeline impossible.
#
# This test verifies that the pattern:
# 1. Searches for 'origin/main' (current, correct behavior)
# 2. Does NOT search for 'origin/custom/main' (the bug)
# 3. Correctly accepts commits on origin/main
# 4. Correctly rejects commits NOT on origin/main

set -euo pipefail

repo="${1:-.}"
cd "$repo"

echo "=== Regression Test: Guard branch check pattern (T-04.08) ==="

# Extract the awk program from release-custom.yml
awk_prog=$(sed -n "s/.*awk '\([^']*\)'.*/\1/p" .github/workflows/release-custom.yml | head -1)

if [ -z "$awk_prog" ]; then
  echo "✗ Failed to extract awk program from release-custom.yml"
  exit 1
fi

echo "Pattern: $awk_prog"

# Test 1: Must not contain 'custom'
if echo "$awk_prog" | grep -q "custom"; then
  echo "✗ REGRESSION: Pattern still references 'custom' — release guard is broken"
  exit 1
fi
echo "✓ Pattern does not reference 'custom'"

# Test 2: Must contain 'origin/main' (with escaping)
if ! echo "$awk_prog" | grep -q "origin.*main"; then
  echo "✗ REGRESSION: Pattern does not reference 'origin/main'"
  exit 1
fi
echo "✓ Pattern references 'origin/main'"

# Test 3: Pattern must accept commits on origin/main
mock_branches_on_main="  origin/main
  origin/feature/test"

if printf '%s\n' "$mock_branches_on_main" | awk "$awk_prog"; then
  echo "✓ Pattern accepts commits on origin/main"
else
  echo "✗ REGRESSION: Pattern rejects commits on origin/main"
  exit 1
fi

# Test 4: Pattern must reject commits NOT on origin/main
mock_branches_not_on_main="  origin/feature/only
  origin/hotfix/temporary"

if ! printf '%s\n' "$mock_branches_not_on_main" | awk "$awk_prog"; then
  echo "✓ Pattern rejects commits not on origin/main"
else
  echo "✗ REGRESSION: Pattern incorrectly accepts commits not on origin/main"
  exit 1
fi

echo "=== All regression tests passed ==="
