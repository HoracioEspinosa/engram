#!/usr/bin/env bash
# bench.sh — measure the store's hot paths and compare against the baseline.
#
# What it does: runs the store benchmarks six times inside golang:1.25.10 and
# writes the raw output to docker/dev/out/bench/<label>.txt. When a
# baseline.txt already exists and this run is not itself the baseline, it also
# runs benchstat and writes <label>.benchstat.txt next to it.
#
# What it guarantees: the numbers come from the pinned toolchain and the same
# caches every run uses, and six samples are taken so benchstat has something
# to compute a confidence interval from — a single sample cannot tell a real
# regression from scheduler noise.
#
# internal/store carries no Benchmark functions yet: they arrive with the
# performance work, and the ones this script asks for by name are the paths
# that work targets. Until then `bench.sh baseline` says so and exits clean,
# because code that has not written a benchmark has not regressed one either.
# Any other label stops instead: comparing against a baseline that holds no
# sample is not a green run, it is benchstat given nothing to read.
#
# Usage: bench.sh <label>     e.g. bench.sh baseline, bench.sh store-hot-paths

set -euo pipefail

# shellcheck source=scripts/dev/lib.sh
. "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"

[ "$#" -ge 1 ] || fail "usage: bench.sh <label>"
LABEL="$1"

require_cmd docker rg
assert_no_live_db

BENCH_OUT="$OUT_DIR/bench"
mkdir -p "$BENCH_OUT"

BENCH_PATTERN='Search|ListTasks|ProjectCardCounts|FindRunbooks|Stats'
BASELINE="$BENCH_OUT/baseline.txt"

# A comparison needs something to compare against. A baseline file that holds no
# sample is not a measurement of zero regression, it is the absence of a
# measurement, and benchstat fed one produces output that looks like a result.
if [ "$LABEL" != "baseline" ] && ! rg -q '^Benchmark' "$BASELINE" 2>/dev/null; then
  fail "bench/baseline.txt carries no benchmark samples; run bench.sh baseline once benchmarks exist"
fi

if ! rg -q '^func Benchmark' "$ROOT_DIR/internal/store"; then
  log "no benchmarks found under internal/store; nothing to measure yet"
  printf 'no benchmarks found\n' >"$BENCH_OUT/${LABEL}.txt"
  exit 0
fi

# No compose service mounts these, so compose never creates them; they are
# created here instead of relying on `docker run -v` to auto-create them.
GOMOD_VOLUME="$(ensure_volume engram-dev-gomod)"
GOBUILD_VOLUME="$(ensure_volume engram-dev-gobuild)"
TARGET="$BENCH_OUT/${LABEL}.txt"

log "benchmarking internal/store (6 samples) into $TARGET"
docker run --rm \
  -v "$ROOT_DIR:/src" \
  -v "${GOMOD_VOLUME}:/go/pkg/mod" \
  -v "${GOBUILD_VOLUME}:/root/.cache/go-build" \
  -w /src \
  "$GO_IMAGE" \
  go test -run '^$' -bench "$BENCH_PATTERN" -benchmem -count=6 ./internal/store/... \
  >"$TARGET"

if [ "$LABEL" = "baseline" ]; then
  log "this run is the baseline; nothing to compare against"
  printf '%s\n' "$TARGET"
  exit 0
fi

COMPARISON="$BENCH_OUT/${LABEL}.benchstat.txt"
log "comparing against the baseline with benchstat"
# benchstat is not in the toolchain image, so it is installed into the
# throwaway container's own GOPATH. The module cache volume makes that a
# download once rather than once per run.
docker run --rm \
  -v "$ROOT_DIR:/src" \
  -v "${GOMOD_VOLUME}:/go/pkg/mod" \
  -v "${GOBUILD_VOLUME}:/root/.cache/go-build" \
  -v "${BENCH_OUT}:/bench" \
  -w /src \
  "$GO_IMAGE" \
  bash -c "set -e; go install golang.org/x/perf/cmd/benchstat@latest; /go/bin/benchstat /bench/baseline.txt '/bench/${LABEL}.txt'" \
  >"$COMPARISON"

printf '%s\n%s\n' "$TARGET" "$COMPARISON"
