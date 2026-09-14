#!/usr/bin/env bash
# bench.sh — measure the hot paths and compare against the baseline.
#
# What it does: runs the store and project-detection benchmarks six times
# inside golang:1.25.10 and writes the raw output to
# docker/dev/out/bench/<label>.txt. When a baseline.txt already exists and this
# run is not itself the baseline, it also runs benchstat and writes
# <label>.benchstat.txt next to it.
#
# What it guarantees: the numbers come from the pinned toolchain and the same
# caches every run uses, and six samples are taken so benchstat has something
# to compute a confidence interval from — a single sample cannot tell a real
# regression from scheduler noise.
#
# The names asked for by BENCH_PATTERN are the paths the performance work
# targets. A store with no Benchmark function at all makes `bench.sh baseline`
# say so and exit clean, because code that has not written a benchmark has not
# regressed one either. Any other label stops instead: comparing against a
# baseline that holds no sample is not a green run, it is benchstat given
# nothing to read.
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

BENCH_PATTERN='Search|ListTasks|ProjectCardCounts|ListProjectCards|FindRunbooks|Stats|DetectProjectFull'
BENCH_PACKAGES='./internal/store/... ./internal/project/...'
BASELINE="$BENCH_OUT/baseline.txt"
BENCHSTAT_VERSION='golang.org/x/perf/cmd/benchstat@v0.0.0-20260908200009-22c9c6c9d4da'

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
set_git_mount_args

log "benchmarking $BENCH_PACKAGES (6 samples) into $TARGET"
docker run --rm \
  -v "$ROOT_DIR:/src" \
  -v "${GOMOD_VOLUME}:/go/pkg/mod" \
  -v "${GOBUILD_VOLUME}:/root/.cache/go-build" \
  "${GIT_MOUNT_ARGS[@]+"${GIT_MOUNT_ARGS[@]}"}" \
  -w /src \
  "$GO_IMAGE" \
  go test -run '^$' -bench "$BENCH_PATTERN" -benchmem -count=6 $BENCH_PACKAGES \
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
#
# It is pinned: @latest would change the comparison tool between two runs whose
# whole point is to be comparable. The pinned version declares a newer go
# directive than the image, so GOTOOLCHAIN=auto is set for the install alone —
# the samples above are already measured, and they were measured by $GO_IMAGE.
docker run --rm \
  -v "$ROOT_DIR:/src" \
  -v "${GOMOD_VOLUME}:/go/pkg/mod" \
  -v "${GOBUILD_VOLUME}:/root/.cache/go-build" \
  -v "${BENCH_OUT}:/bench" \
  -w /src \
  -e GOTOOLCHAIN=auto \
  "$GO_IMAGE" \
  bash -c "set -e; go install '$BENCHSTAT_VERSION'; /go/bin/benchstat /bench/baseline.txt '/bench/${LABEL}.txt'" \
  >"$COMPARISON"

printf '%s\n%s\n' "$TARGET" "$COMPARISON"
