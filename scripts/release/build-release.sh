#!/usr/bin/env bash
# build-release.sh — build the fork's release artifacts locally, publishing
# nothing, and prove the built binary reports the version it claims.
#
# It works on a throwaway clone, never on the caller's checkout: goreleaser
# writes dist/ and runs `go mod tidy`, and neither belongs in a tree somebody
# else is working in.
#
# Usage:
#   scripts/release/build-release.sh [options]
#
# Options:
#   --repo <dir>     Source repository (default: the repo this script is in)
#   --ref <ref>      Commit or branch to build (default: main)
#   --tag <tag>      Build as this tag, shaped v<version>-cd.<n>. Without it the
#                    build is a snapshot and the version is goreleaser's own.
#   --image          Also build the container image for the host platform and
#                    check the version inside it (requires docker)
#   --keep           Keep the workdir on success
#   --workdir <dir>  Where to clone (default: a mktemp directory)
#   -h, --help       This text
#
# A snapshot build (no --tag) automatically produces version_template from
# .goreleaser.custom.yaml (--snapshot is passed to goreleaser internally).
#
# Publishing is not reachable from here: every goreleaser invocation carries
# --skip=publish,announce and there is no passthrough for extra arguments.
#
# Exit codes:
#   0  artifacts built and the version check passed
#   1  usage error, or a tool that was needed to check something is missing
#   2  goreleaser failed
#   3  the built artifact reported a version other than the one asked for
#
# Requires: git, go, goreleaser (>= 2.12); docker only with --image. Written for
# the bash 3.2 that ships with macOS and verified on bash 5.
set -euo pipefail

# Pure parameter expansion: this name is used in every error message, so it
# must not depend on a PATH that the caller may have trimmed.
SCRIPT_NAME="${0##*/}"

REPO=""
REF="main"
TAG=""
WITH_IMAGE=0
KEEP=0
WORKDIR=""
WORKDIR_IS_TEMP=0
CONFIG=".goreleaser.custom.yaml"

die() { printf '%s: %s\n' "$SCRIPT_NAME" "$1" >&2; exit "${2:-1}"; }
note() { printf '%s\n' "$1"; }
section() { printf '\n== %s ==\n' "$1"; }

usage() {
  while IFS= read -r line; do
    case "$line" in
      '#!'*) continue ;;
      '#') printf '\n' ;;
      '# '*) printf '%s\n' "${line#\# }" ;;
      *) break ;;
    esac
  done < "$0"
}

while [ $# -gt 0 ]; do
  case "$1" in
    --repo) [ $# -ge 2 ] || die "--repo needs a value"; REPO="$2"; shift 2 ;;
    --ref) [ $# -ge 2 ] || die "--ref needs a value"; REF="$2"; shift 2 ;;
    --tag) [ $# -ge 2 ] || die "--tag needs a value"; TAG="$2"; shift 2 ;;
    --workdir) [ $# -ge 2 ] || die "--workdir needs a value"; WORKDIR="$2"; shift 2 ;;
    --image) WITH_IMAGE=1; shift ;;
    --keep) KEEP=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) die "unknown argument: $1" ;;
  esac
done

if [ -n "$TAG" ]; then
  case "$TAG" in
    v*-cd.*) ;;
    *) die "tag '$TAG' is not shaped v<version>-cd.<n>, which is the only shape release-custom.yml releases" ;;
  esac
fi

for tool in git go goreleaser; do
  command -v "$tool" >/dev/null 2>&1 || die "$tool is required and is not installed"
done
if [ "$WITH_IMAGE" -eq 1 ] && ! command -v docker >/dev/null 2>&1; then
  die "--image was requested but docker is not installed, so the image could not be checked"
fi

if [ -z "$REPO" ]; then
  SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
  REPO="$(cd "$SCRIPT_DIR/../.." && pwd)"
fi
[ -d "$REPO/.git" ] || die "not a git repository: $REPO"
git -C "$REPO" rev-parse --verify --quiet "$REF^{commit}" >/dev/null \
  || die "ref '$REF' does not exist in $REPO"

if [ -z "$WORKDIR" ]; then
  WORKDIR="$(mktemp -d "${TMPDIR:-/tmp}/engram-release.XXXXXX")"
  WORKDIR_IS_TEMP=1
else
  [ -e "$WORKDIR" ] && die "workdir already exists: $WORKDIR"
  mkdir -p "$WORKDIR" || die "cannot create workdir: $WORKDIR"
fi
CLONE="$WORKDIR/engram"

# shellcheck disable=SC2329 # invoked by the EXIT trap below
cleanup() {
  status=$?
  if [ "$WORKDIR_IS_TEMP" -eq 1 ] && [ "$KEEP" -eq 0 ] && [ "$status" -eq 0 ]; then
    rm -rf "$WORKDIR"
  elif [ -d "$WORKDIR" ]; then
    printf '\nworkdir kept at: %s\n' "$WORKDIR"
  fi
}
trap cleanup EXIT

section "clone"
note "source: $REPO ($REF)"
note "clone:  $CLONE"
git clone --quiet --no-hardlinks --no-checkout "$REPO" "$CLONE" || die "clone failed"
git -C "$CLONE" checkout --quiet --detach "$REF" || die "cannot check out '$REF' in the clone"
# Same guard as the rebase script: no remote here can reach GitHub.
git -C "$CLONE" remote set-url --push origin no-push://blocked

[ -f "$CLONE/$CONFIG" ] || die "$CONFIG is missing at $REF; there is nothing to build with"

section "validate the configuration"
# HOMEBREW_TAP_TOKEN is templated in the config. It is only read here, never
# printed, and an empty value is enough for validation and for a local build,
# which is the point: a dry run must not need the real secret.
HOMEBREW_TAP_TOKEN="${HOMEBREW_TAP_TOKEN:-}"
export HOMEBREW_TAP_TOKEN
( cd "$CLONE" && goreleaser check -f "$CONFIG" ) || die "goreleaser check rejected $CONFIG" 2

section "build"
EXPECTED=""
if [ -n "$TAG" ]; then
  EXPECTED="${TAG#v}"
  note "building as tag $TAG (expected version: $EXPECTED)"
  git -C "$CLONE" tag -f "$TAG" HEAD >/dev/null 2>&1 || die "cannot create the local tag $TAG in the clone"
  set +e
  ( cd "$CLONE" && GORELEASER_CURRENT_TAG="$TAG" goreleaser release -f "$CONFIG" \
      --clean --skip=publish,announce,validate ) > "$WORKDIR/goreleaser.log" 2>&1
  GR_STATUS=$?
  set -e
else
  note "building a snapshot (no tag)"
  set +e
  ( cd "$CLONE" && goreleaser release -f "$CONFIG" \
      --snapshot --clean --skip=publish,announce ) > "$WORKDIR/goreleaser.log" 2>&1
  GR_STATUS=$?
  set -e
fi

if [ "$GR_STATUS" -ne 0 ]; then
  tail -n 30 "$WORKDIR/goreleaser.log" | while IFS= read -r line; do printf '  %s\n' "$line"; done
  KEEP=1
  die "goreleaser failed (full log: $WORKDIR/goreleaser.log)" 2
fi

DIST="$CLONE/dist"
[ -d "$DIST" ] || die "goreleaser reported success but produced no dist/ directory" 2

section "artifacts"
ARCHIVES=0
for f in "$DIST"/*.tar.gz "$DIST"/*.zip; do
  [ -e "$f" ] || continue
  ARCHIVES=$((ARCHIVES + 1))
  printf '  %s\n' "$(basename "$f")"
done
note "archives: $ARCHIVES"
[ "$ARCHIVES" -gt 0 ] || die "no archive was produced" 2

CASK="$DIST/homebrew/Casks/engram-custom.rb"
if [ -f "$CASK" ]; then
  note "cask:     $CASK"
else
  note "cask:     NOT generated (goreleaser skipped the homebrew step)"
fi

section "version check"
HOST_OS=""
case "$(uname -s)" in
  Darwin) HOST_OS=darwin ;;
  Linux) HOST_OS=linux ;;
  *) die "unsupported host OS $(uname -s): the built binary could not be run, so the version is UNVERIFIED" ;;
esac
HOST_ARCH=""
case "$(uname -m)" in
  arm64|aarch64) HOST_ARCH=arm64 ;;
  x86_64|amd64) HOST_ARCH=amd64 ;;
  *) die "unsupported host architecture $(uname -m): the built binary could not be run, so the version is UNVERIFIED" ;;
esac

BIN=""
for d in "$DIST"/engram_"$HOST_OS"_"$HOST_ARCH"*/; do
  [ -x "$d/engram" ] || continue
  BIN="$d/engram"
  break
done
[ -n "$BIN" ] || die "no binary for ${HOST_OS}_${HOST_ARCH} in $DIST, so the version is UNVERIFIED" 2

# `engram --version` prints its update notice on stderr, but reading only the
# line that starts with "engram " keeps this check about the version and not
# about which stream a future release decides to warn on.
GOT=""
GOT="$("$BIN" --version | awk '/^engram /{print; exit}')" || die "the built binary could not be run" 3
[ -n "$GOT" ] || die "the built binary printed no version line" 3
note "binary:  $GOT"
if [ -n "$EXPECTED" ]; then
  [ "$GOT" = "engram $EXPECTED" ] || { KEEP=1; die "binary reports '$GOT', expected 'engram $EXPECTED'" 3; }
  note "binary version matches $TAG"
else
  case "$GOT" in
    "engram dev"|"engram ") KEEP=1; die "snapshot binary reports '$GOT': the ldflags stamp did not take" 3 ;;
    *) note "snapshot version stamped (no tag to compare against)" ;;
  esac
fi

if [ "$WITH_IMAGE" -eq 1 ]; then
  section "container image"
  IMG_VERSION="${EXPECTED:-0.0.0-local}"
  IMG_TAG="engram-custom-local:$IMG_VERSION"
  REV=""
  REV="$(git -C "$CLONE" rev-parse HEAD)"
  set +e
  ( cd "$CLONE" && docker buildx build --platform "linux/$HOST_ARCH" \
      -f docker/custom/Dockerfile \
      --build-arg ENGRAM_VERSION="$IMG_VERSION" \
      --build-arg ENGRAM_REVISION="$REV" \
      -t "$IMG_TAG" --load . ) > "$WORKDIR/docker.log" 2>&1
  DK_STATUS=$?
  set -e
  if [ "$DK_STATUS" -ne 0 ]; then
    tail -n 20 "$WORKDIR/docker.log" | while IFS= read -r line; do printf '  %s\n' "$line"; done
    KEEP=1
    die "the image build failed (full log: $WORKDIR/docker.log)" 2
  fi
  IMG_GOT=""
  IMG_GOT="$(docker run --rm "$IMG_TAG" --version | awk '/^engram /{print; exit}')" \
    || { KEEP=1; die "the built image could not be run" 3; }
  [ -n "$IMG_GOT" ] || { KEEP=1; die "the built image printed no version line" 3; }
  note "image:   $IMG_GOT"
  [ "$IMG_GOT" = "engram $IMG_VERSION" ] \
    || { KEEP=1; die "image reports '$IMG_GOT', expected 'engram $IMG_VERSION'" 3; }
  note "image version matches"
  note "local tag left behind: $IMG_TAG (remove it with: docker image rm $IMG_TAG)"
fi

section "done"
note "Nothing was published: every goreleaser run carried --skip=publish,announce."
exit 0
