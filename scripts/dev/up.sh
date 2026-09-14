#!/usr/bin/env bash
# up.sh — build and start the isolated dev container.
#
# What it does: stamps the image with the working tree's short SHA, builds
# engram-dev, starts it, waits for Docker's own health check to report
# healthy, publishes the compiled binary into the engram-dev-bin volume so the
# shots profile can run it without rebuilding, and prints `engram version`.
#
# What it guarantees: nothing starts until ./setup.sh has been run in this
# checkout and until the resolved compose config is proven not to reach
# ~/.engram. Readiness is decided by `docker inspect`, never by connecting to
# a port: internal/server/server.go binds 127.0.0.1 inside the container, so
# no port is published and a port probe from the host would report a refused
# connection on a perfectly healthy container.

set -euo pipefail

# shellcheck source=scripts/dev/lib.sh
. "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"

HEALTH_TIMEOUT=60

require_cmd docker git
require_setup
assert_no_live_db

ENGRAM_DEV_VERSION="$(dev_version)"
export ENGRAM_DEV_VERSION
log "building $IMAGE as $ENGRAM_DEV_VERSION"
"${DC[@]}" build engram-dev

log "starting engram-dev"
"${DC[@]}" up -d engram-dev

log "waiting up to ${HEALTH_TIMEOUT}s for engram-dev to report healthy"
waited=0
health=""
while [ "$waited" -lt "$HEALTH_TIMEOUT" ]; do
  # A container with no HEALTHCHECK has no .State.Health at all; the template
  # below then yields "none", which is treated as "not healthy yet" rather
  # than as success, so a Dockerfile that loses its health check fails loudly
  # here instead of letting every later script race the container's startup.
  health="$(docker inspect -f '{{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}' "$CONTAINER" 2>/dev/null || echo "absent")"
  case "$health" in
    healthy)
      log "engram-dev healthy after ${waited}s"
      break
      ;;
    unhealthy)
      docker inspect -f '{{json .State.Health}}' "$CONTAINER" >&2 || true
      fail "engram-dev reported unhealthy after ${waited}s"
      ;;
  esac
  sleep 2
  waited=$((waited + 2))
done

if [ "$health" != "healthy" ]; then
  "${DC[@]}" logs --tail 50 engram-dev >&2 || true
  fail "engram-dev did not become healthy within ${HEALTH_TIMEOUT}s (last state: $health)"
fi

# Publish the binary into the shared volume. Done with a throwaway `docker run`
# against the image itself rather than `docker cp` out of the running
# container, so the copy never depends on the container's current state and
# the binary's path is resolved by the image's own $PATH instead of being
# hardcoded here.
#
# The copy runs as root (--user 0:0) because Docker creates an empty named
# volume owned by root:root with mode 0755, and the image's own user is
# engram (uid 10001), which cannot write into it. Only this one-shot copy is
# privileged; the service container keeps running as engram. The binary is
# written 0755 so that same unprivileged user can execute it from the volume.
BIN_VOLUME="$(ensure_volume engram-dev-bin)"
log "publishing the engram binary into volume $BIN_VOLUME"
docker run --rm \
  --user 0:0 \
  -v "${BIN_VOLUME}:/bin-out" \
  --entrypoint sh \
  "$IMAGE" \
  -c 'set -eu; src="$(command -v engram)"; cp "$src" /bin-out/engram; chmod 0755 /bin-out/engram'

log "version reported by the container:"
in_container engram version
