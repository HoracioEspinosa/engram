#!/usr/bin/env bash
# Fictitious benchmark harness for KOI-1099. It does not call a real service —
# this fixture only documents the shape a real run-bench.sh would have, so the
# importer test data stays self-contained.
set -euo pipefail

TARGET="${1:-koi-garden-pond-02}"
OUT_DIR="$(dirname "${BASH_SOURCE[0]}")"

echo "Running 200 cross-pond lookups against ${TARGET}..."
echo "This is a fixture placeholder: see baseline-run1.json / baseline-run2.json"
echo "for the metrics a real run would have produced, and BASELINE.md for the"
echo "methodology."

echo "Would write: ${OUT_DIR}/baseline-run$(date +%s).json"
