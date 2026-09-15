#!/usr/bin/env bash
# test.sh — run the whole test suite in a container, never on the host.
#
# What it does: mounts the working tree into golang:1.25.10 and runs the three
# things CI runs — the unit suite, the e2e-tagged server suite, and the
# internal/tui coverage gate — with the module cache and the build cache on
# named volumes so a second run is not a cold compile.
#
# What it guarantees:
#   * The host toolchain is never used. The pinned image is the one go.mod and
#     .github/workflows/ci.yml agree on; the machine also has golang:1.27.0,
#     and measuring against it would measure a toolchain the project does not
#     ship.
#   * The working tree comes out of the run unchanged. The mount is read-write
#     because the logs below are written into docker/dev/out/test/ — the
#     coverage gate itself writes nothing, it runs `go test -cover` without a
#     profile — so `git status` is captured before and after and any other
#     difference is reported.
#   * The Postgres-gated cloud suites run instead of skipping. A throwaway
#     Postgres container supplies CLOUDSTORE_TEST_DSN and is removed when the
#     script exits, so `ok` on those packages means the tests executed.

set -euo pipefail

# shellcheck source=scripts/dev/lib.sh
. "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"

require_cmd docker git
assert_no_live_db

TEST_OUT="$OUT_DIR/test"
mkdir -p "$TEST_OUT"

# No compose service mounts these, so compose never creates them; they are
# created here instead of relying on `docker run -v` to auto-create them.
GOMOD_VOLUME="$(ensure_volume engram-dev-gomod)"
GOBUILD_VOLUME="$(ensure_volume engram-dev-gobuild)"

# docker/dev/out is this script's own output area, so its contents are expected
# to change; everything else in the tree is not.
status_snapshot() {
  git -C "$ROOT_DIR" status --porcelain -- . ':(exclude)docker/dev/out'
}

BEFORE_STATUS="$(status_snapshot)"
set_git_mount_args

# The cloud suites — internal/cloud/cloudstore and the cmd/engram runtime
# wiring tests — skip themselves when CLOUDSTORE_TEST_DSN is unset, so without
# a database this run would report `ok` for tests that never executed. A
# throwaway Postgres on its own network, torn down with the script, makes them
# run instead of skip. It is a fresh container every time and holds nothing
# worth keeping, so it gets no volume.
TEST_PG_NETWORK="engram-dev-test-net"
TEST_PG_CONTAINER="engram-dev-test-pg"
TEST_PG_DSN="postgres://engram_test:test_only_pw@${TEST_PG_CONTAINER}:5432/engram_test?sslmode=disable"

cleanup_test_postgres() {
  docker rm -f "$TEST_PG_CONTAINER" >/dev/null 2>&1 || true
  docker network rm "$TEST_PG_NETWORK" >/dev/null 2>&1 || true
}
trap cleanup_test_postgres EXIT

cleanup_test_postgres
docker network create "$TEST_PG_NETWORK" >/dev/null
log "starting a throwaway Postgres for the DSN-gated cloud suites"
docker run -d --name "$TEST_PG_CONTAINER" --network "$TEST_PG_NETWORK" \
  -e POSTGRES_USER=engram_test \
  -e POSTGRES_PASSWORD=test_only_pw \
  -e POSTGRES_DB=engram_test \
  postgres:16-alpine >/dev/null

pg_ready=0
for _ in $(seq 1 60); do
  if docker exec "$TEST_PG_CONTAINER" pg_isready -U engram_test -d engram_test >/dev/null 2>&1; then
    pg_ready=1
    break
  fi
  sleep 1
done
[ "$pg_ready" -eq 1 ] || fail "the throwaway Postgres never became ready"

log "running the suite inside $GO_IMAGE"
set +e
docker run --rm \
  --network "$TEST_PG_NETWORK" \
  -e CLOUDSTORE_TEST_DSN="$TEST_PG_DSN" \
  -v "$ROOT_DIR:/src" \
  -v "${GOMOD_VOLUME}:/go/pkg/mod" \
  -v "${GOBUILD_VOLUME}:/root/.cache/go-build" \
  "${GIT_MOUNT_ARGS[@]+"${GIT_MOUNT_ARGS[@]}"}" \
  -w /src \
  "$GO_IMAGE" \
  bash -c '
    set -uo pipefail
    status=0
    mkdir -p /src/docker/dev/out/test
    echo "--- go test ./... ---"
    go test ./... 2>&1 | tee /src/docker/dev/out/test/unit.log || status=1
    echo "--- go test -tags e2e ./internal/server/... ---"
    go test -tags e2e ./internal/server/... 2>&1 | tee /src/docker/dev/out/test/e2e.log || status=1
    echo "--- scripts/tui-coverage-gate.sh ---"
    bash scripts/tui-coverage-gate.sh 2>&1 | tee /src/docker/dev/out/test/tui-coverage.log || status=1
    exit $status
  '
run_status=$?
set -e

AFTER_STATUS="$(status_snapshot)"

if [ "$BEFORE_STATUS" != "$AFTER_STATUS" ]; then
  printf 'FAIL  the test run changed the working tree outside docker/dev/out:\n' >&2
  printf '  before:\n%s\n  after:\n%s\n' "${BEFORE_STATUS:-(clean)}" "${AFTER_STATUS:-(clean)}" >&2
  printf 'logs: %s\n' "$TEST_OUT" >&2
  exit 1
fi
log "the working tree is unchanged outside docker/dev/out"

printf 'logs: %s\n' "$TEST_OUT"
if [ "$run_status" -ne 0 ]; then
  fail "the test run exited $run_status; see the logs above"
fi
log "test run green"
