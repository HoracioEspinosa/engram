#!/usr/bin/env bash
# One-off cache cleanup for koi-garden. Fixture-only: no real target exists,
# this just documents the shape of the maintenance scripts that land here.
set -euo pipefail

CACHE_DIR="${1:-/var/cache/koi-garden}"

echo "Would clear stale pond thumbnails cache under ${CACHE_DIR}"
echo "Would clear stale lookup resolution cache (see KOI-1099 for the real fix)"
