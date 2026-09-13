#!/bin/bash
# tui-coverage-gate.sh — CI gate for the coverage clause on internal/tui/**.
#
# Runs `go test -cover` over every internal/tui package and fails, by
# package name and percentage, if any package with production code drops
# below THRESHOLD. This is the enforcement mechanism for that threshold:
# without it, the number only exists as documentation that nothing checks.
#
# THRESHOLD is the one place this number lives. Nothing else in this repo —
# not the CI workflow, not a doc — repeats it; raising or lowering the bar
# only ever touches the line below.
THRESHOLD=80
#
# Per-package, not averaged across internal/tui/** as a whole: an aggregate
# mean can hide one sunk package behind several sitting near 100%, and the
# criterion this gate enforces is a per-package floor, not a whole-tree
# average.
#
# internal/tui/e2e is pure test harness with no production code, so `go
# test -cover` reports it as "coverage: [no statements]" instead of a
# percentage. That is not a failure and must never be scored as 0%: this
# script recognizes that exact phrase and skips it rather than trying to
# parse a number out of it.
#
# A package under internal/tui/** with no test files at all (`go test`
# reports it as "?  <pkg>  [no test files]", never as "ok") is treated as a
# hard failure for the same reason [no statements] is treated as a pass:
# both are edge cases `go test`'s normal "ok ... coverage: NN.N%" line
# does not cover, and silently ignoring either would let a real gap through
# a gate whose entire purpose is to not do that.
#
# Runs under bash 3.2 (macOS's shipped /bin/bash): no mapfile, no
# readarray, no associative arrays (declare -A), no ${var,,}.

set -uo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT_DIR" || exit 2

OUTPUT="$(go test -count=1 -cover ./internal/tui/... 2>&1)"
GO_TEST_STATUS=$?

echo "$OUTPUT"

if [ "$GO_TEST_STATUS" -ne 0 ]; then
  echo
  echo "FAIL: go test ./internal/tui/... exited $GO_TEST_STATUS; the coverage gate cannot trust these numbers, so it did not evaluate any of them."
  exit "$GO_TEST_STATUS"
fi

FAIL_COUNT=0
CHECKED_COUNT=0

# Line-by-line, without mapfile/readarray (bash 3.2 has neither).
while IFS= read -r line; do
  case "$line" in
    ok*"[no statements]"*)
      # Pure test-harness package (e.g. internal/tui/e2e): not scored, not a failure.
      continue
      ;;
    \?*"[no test files]"*)
      PKG=$(echo "$line" | awk '{print $2}')
      echo "FAIL: coverage gate: $PKG has no test files under internal/tui/**"
      FAIL_COUNT=$((FAIL_COUNT + 1))
      CHECKED_COUNT=$((CHECKED_COUNT + 1))
      ;;
    ok*"coverage:"*"% of statements"*)
      PKG=$(echo "$line" | awk '{print $2}')
      PCT=$(echo "$line" | awk '{print $5}' | tr -d '%')
      CHECKED_COUNT=$((CHECKED_COUNT + 1))
      BELOW=$(awk -v pct="$PCT" -v threshold="$THRESHOLD" 'BEGIN { print (pct < threshold) ? "1" : "0" }')
      if [ "$BELOW" = "1" ]; then
        echo "FAIL: coverage gate: $PKG is at ${PCT}%, below the required ${THRESHOLD}% threshold for internal/tui/**"
        FAIL_COUNT=$((FAIL_COUNT + 1))
      fi
      ;;
  esac
done <<EOF
$OUTPUT
EOF

if [ "$CHECKED_COUNT" -eq 0 ]; then
  echo "FAIL: coverage gate found no scoreable packages under internal/tui/...; go test's output format may have changed."
  exit 2
fi

echo
if [ "$FAIL_COUNT" -gt 0 ]; then
  echo "coverage gate: $FAIL_COUNT of $CHECKED_COUNT checked package(s) under internal/tui/** are below ${THRESHOLD}%."
  exit 1
fi

echo "coverage gate: all $CHECKED_COUNT checked package(s) under internal/tui/** are at or above ${THRESHOLD}%."
exit 0
