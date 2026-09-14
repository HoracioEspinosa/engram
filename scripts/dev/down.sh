#!/usr/bin/env bash
# down.sh — stop the dev environment and, by default, throw its state away.
#
# What it does: brings down every service in the compose file, including the
# ones behind the cloud and shots profiles, and removes the named volumes.
#
# What it guarantees: a following `up.sh` + `seed.sh` starts from an empty
# store. The dev state is disposable on purpose — every assertion in this
# directory is written against a store that seed.sh built, so a leftover
# volume from an earlier schema is a source of false passes, not a saving.
#
# Usage:
#   down.sh          remove containers and volumes (default)
#   down.sh --keep   remove containers, keep the volumes (store, caches, bin)

set -euo pipefail

# shellcheck source=scripts/dev/lib.sh
. "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"

KEEP_VOLUMES=0
while [ "$#" -gt 0 ]; do
  case "$1" in
    --keep)
      KEEP_VOLUMES=1
      ;;
    -h | --help)
      printf 'usage: down.sh [--keep]\n' >&2
      exit 0
      ;;
    *)
      fail "unknown argument: $1 (usage: down.sh [--keep])"
      ;;
  esac
  shift
done

require_cmd docker

# The profiles are listed explicitly because `docker compose down` only stops
# services in the profiles it is told about: without them, postgres-dev,
# cloud-dev and vhs survive a "down" and keep their volumes attached, which
# then makes the volume removal below a partial no-op.
args=(--profile cloud --profile shots down --remove-orphans)
if [ "$KEEP_VOLUMES" -eq 0 ]; then
  args+=(-v)
  log "stopping the dev environment and removing its volumes"
else
  log "stopping the dev environment, keeping its volumes"
fi

"${DC[@]}" "${args[@]}"

if [ "$KEEP_VOLUMES" -eq 0 ]; then
  # `docker compose config` drops any top-level volume no service mounts, so
  # the Go module and build caches — used by test.sh and bench.sh through a
  # plain `docker run`, never by a compose service — are invisible to
  # `down -v` and would survive it. engram-dev-bin is listed too: it is
  # populated by up.sh's own `docker run`, so removing it by name does not
  # depend on the shots profile having been resolved during the down.
  #
  # -f makes a missing volume a no-op, which is the normal case for whichever
  # of these compose already removed.
  log "removing the volumes compose does not manage"
  docker volume rm -f \
    engram-dev-gomod \
    engram-dev-gobuild \
    engram-dev-bin \
    >/dev/null 2>&1 || true
fi
