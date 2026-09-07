#!/usr/bin/env bash
# rebase-onto-upstream.sh — replay the fork's custom commits on top of upstream
# main, inside a throwaway clone, without touching the caller's checkout.
#
# Why a clone instead of `git rebase` in place: this repository is worked on by
# more than one person and more than one agent at a time. A rebase in place
# moves HEAD, rewrites the index and can leave the tree mid-conflict for
# whoever else is in it. A clone can be thrown away, and the caller's branch
# is only updated on explicit request (--adopt), never by surprise, and never
# pushed.
#
# Usage:
#   scripts/release/rebase-onto-upstream.sh [options]
#
# Options:
#   --repo <dir>         Source repository (default: the repo this script is in)
#   --branch <ref>       Branch carrying the custom commits (default: main)
#   --base <ref>         Last commit the custom work sits on top of
#                        (default: the merge base of --branch and upstream main)
#   --upstream-url <url> Upstream remote, read only
#                        (default: https://github.com/Gentleman-Programming/engram.git)
#   --upstream-ref <ref> Upstream branch to rebase onto (default: main)
#   --workdir <dir>      Where to clone (default: a mktemp directory)
#   --keep               Keep the workdir even when the rebase succeeds
#   --no-tests           Skip `go build` and `go test` after a clean rebase
#   --adopt [<branch>]   On success, fetch the result into the source repository
#                        as <branch> (default: <branch>-rebased-YYYYmmddHHMMSS).
#                        Creates a ref only; never checks out, never pushes.
#   -h, --help           This text
#
# Exit codes:
#   0  rebase clean (and tests green, unless --no-tests)
#   1  usage error or a precondition that could not be checked
#   2  the rebase stopped on a conflict (inventory printed, workdir kept)
#   3  the rebase was clean but `go build` or `go test` failed
#
# Requires: git >= 2.20, and go when tests run. Written for the bash 3.2 that
# ships with macOS and verified on bash 5.
set -euo pipefail

# Pure parameter expansion: this name is used in every error message, so it
# must not depend on a PATH that the caller may have trimmed.
SCRIPT_NAME="${0##*/}"

REPO=""
BRANCH="main"
BASE=""
UPSTREAM_URL="https://github.com/Gentleman-Programming/engram.git"
UPSTREAM_REF="main"
WORKDIR=""
KEEP=0
RUN_TESTS=1
ADOPT=0
ADOPT_BRANCH=""
WORKDIR_IS_TEMP=0

die() { printf '%s: %s\n' "$SCRIPT_NAME" "$1" >&2; exit "${2:-1}"; }
note() { printf '%s\n' "$1"; }
section() { printf '\n== %s ==\n' "$1"; }

usage() {
  # The header comment is the only copy of this text; printing it keeps the two
  # from drifting apart.
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
    --branch) [ $# -ge 2 ] || die "--branch needs a value"; BRANCH="$2"; shift 2 ;;
    --base) [ $# -ge 2 ] || die "--base needs a value"; BASE="$2"; shift 2 ;;
    --upstream-url) [ $# -ge 2 ] || die "--upstream-url needs a value"; UPSTREAM_URL="$2"; shift 2 ;;
    --upstream-ref) [ $# -ge 2 ] || die "--upstream-ref needs a value"; UPSTREAM_REF="$2"; shift 2 ;;
    --workdir) [ $# -ge 2 ] || die "--workdir needs a value"; WORKDIR="$2"; shift 2 ;;
    --keep) KEEP=1; shift ;;
    --no-tests) RUN_TESTS=0; shift ;;
    --adopt)
      ADOPT=1
      if [ $# -ge 2 ] && [ "${2#-}" = "$2" ]; then ADOPT_BRANCH="$2"; shift 2; else shift; fi ;;
    -h|--help) usage; exit 0 ;;
    *) die "unknown argument: $1" ;;
  esac
done

command -v git >/dev/null 2>&1 || die "git is required"

if [ -z "$REPO" ]; then
  SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
  REPO="$(cd "$SCRIPT_DIR/../.." && pwd)"
fi
[ -d "$REPO/.git" ] || die "not a git repository: $REPO"

git -C "$REPO" rev-parse --verify --quiet "$BRANCH^{commit}" >/dev/null \
  || die "branch '$BRANCH' does not exist in $REPO"

# A rebase that could reach the upstream remote with a write is the one failure
# this whole task is not allowed to have. Refuse an upstream URL that is not
# recognisably upstream's, so a typo cannot turn it into the fork.
case "$UPSTREAM_URL" in
  *Gentleman-Programming/engram*|/*|file://*|.*) ;;
  *) die "refusing an unexpected upstream URL: $UPSTREAM_URL (expected Gentleman-Programming/engram or a local path)" ;;
esac

if [ -z "$WORKDIR" ]; then
  WORKDIR="$(mktemp -d "${TMPDIR:-/tmp}/engram-rebase.XXXXXX")"
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
note "source:  $REPO"
note "clone:   $CLONE"
git clone --quiet --no-hardlinks --no-checkout "$REPO" "$CLONE" || die "clone failed"
git -C "$CLONE" checkout --quiet -B "$BRANCH" "refs/remotes/origin/$BRANCH"

# Belt and braces: neither remote in the clone can be pushed to, so no path
# through this script (or through a shell left open in the workdir) reaches
# GitHub. "no-push" is not a URL, and git refuses it.
git -C "$CLONE" remote set-url --push origin no-push://blocked
git -C "$CLONE" remote add upstream "$UPSTREAM_URL"
git -C "$CLONE" remote set-url --push upstream no-push://blocked

section "fetch upstream"
note "url: $UPSTREAM_URL ($UPSTREAM_REF)"
git -C "$CLONE" fetch --quiet upstream "$UPSTREAM_REF" || die "cannot fetch $UPSTREAM_URL"
ONTO=""
ONTO="$(git -C "$CLONE" rev-parse "upstream/$UPSTREAM_REF")" || die "cannot resolve upstream/$UPSTREAM_REF"
note "upstream head: $ONTO"

if [ -z "$BASE" ]; then
  BASE="$(git -C "$CLONE" merge-base "$BRANCH" "upstream/$UPSTREAM_REF")" \
    || die "cannot compute the merge base of $BRANCH and upstream/$UPSTREAM_REF"
fi
BASE_SHA=""
BASE_SHA="$(git -C "$CLONE" rev-parse "$BASE")" || die "cannot resolve base '$BASE'"

CUSTOM_COUNT=""
CUSTOM_COUNT="$(git -C "$CLONE" rev-list --count "$BASE_SHA..$BRANCH")" || die "cannot count the custom commits"
BEHIND=""
BEHIND="$(git -C "$CLONE" rev-list --count "$BASE_SHA..upstream/$UPSTREAM_REF")" || die "cannot count the upstream commits"

section "plan"
note "base:              $BASE_SHA"
note "custom commits:    $CUSTOM_COUNT"
note "upstream ahead by: $BEHIND commits"
if [ "$CUSTOM_COUNT" -eq 0 ]; then
  note "nothing to replay: $BRANCH carries no commit that upstream does not have."
  exit 0
fi
git -C "$CLONE" log --oneline "$BASE_SHA..$BRANCH" | while IFS= read -r line; do
  printf '  %s\n' "$line"
done

section "rebase"
# rerere records the resolution of a conflict so the next run replays it. The
# registry and inventory conflicts this branch produces are the same ones every
# time, so this is what stops the third rebase from costing what the first did.
git -C "$CLONE" config rerere.enabled true
git -C "$CLONE" config rerere.autoupdate true

set +e
git -C "$CLONE" rebase --onto "$ONTO" "$BASE_SHA" "$BRANCH" > "$WORKDIR/rebase.log" 2>&1
REBASE_STATUS=$?
set -e
tail -n 20 "$WORKDIR/rebase.log" | while IFS= read -r line; do printf '  %s\n' "$line"; done

if [ "$REBASE_STATUS" -ne 0 ]; then
  section "conflict inventory"
  STOPPED=""
  if [ -f "$CLONE/.git/rebase-merge/stopped-sha" ]; then
    STOPPED="$(awk 'NR==1{print; exit}' "$CLONE/.git/rebase-merge/stopped-sha")"
    note "stopped on: $STOPPED $(git -C "$CLONE" log --format=%s -1 "$STOPPED" 2>/dev/null || printf '(unknown subject)')"
  fi

  UNMERGED="$WORKDIR/unmerged.txt"
  git -C "$CLONE" diff --name-only --diff-filter=U > "$UNMERGED" || die "cannot list the unmerged paths"
  FILE_COUNT=""
  FILE_COUNT="$(awk 'END{print NR}' "$UNMERGED")"
  HUNKS=0
  while IFS= read -r f; do
    [ -n "$f" ] || continue
    n=0
    if [ -f "$CLONE/$f" ]; then
      n="$(awk '/^<<<<<<< /{c++} END{print c+0}' "$CLONE/$f")"
    fi
    HUNKS=$((HUNKS + n))
    printf '  %2s hunk(s)  %s\n' "$n" "$f"
  done < "$UNMERGED"

  note ""
  note "unmerged paths: $FILE_COUNT   conflict hunks: $HUNKS"
  note ""
  note "Resolve them in the clone, then continue there:"
  note "  cd $CLONE"
  note "  git status"
  note "  git rebase --continue      # rerere will remember the resolution"
  note ""
  note "Nothing was changed in $REPO, and neither remote in the clone can push."
  KEEP=1
  exit 2
fi

RESULT_SHA=""
RESULT_SHA="$(git -C "$CLONE" rev-parse HEAD)" || die "cannot read the rebased head"
note "rebased head: $RESULT_SHA"

if [ "$RUN_TESTS" -eq 1 ]; then
  section "build and test"
  if ! command -v go >/dev/null 2>&1; then
    die "go is not installed, so the rebase could not be verified; re-run with --no-tests to accept it unverified" 1
  fi
  set +e
  ( cd "$CLONE" && go build ./... ) > "$WORKDIR/build.log" 2>&1
  BUILD_STATUS=$?
  set -e
  if [ "$BUILD_STATUS" -ne 0 ]; then
    tail -n 30 "$WORKDIR/build.log" | while IFS= read -r line; do printf '  %s\n' "$line"; done
    KEEP=1
    die "the rebased tree does not build" 3
  fi
  note "go build: ok"

  set +e
  ( cd "$CLONE" && go test ./... ) > "$WORKDIR/test.log" 2>&1
  TEST_STATUS=$?
  set -e
  if [ "$TEST_STATUS" -ne 0 ]; then
    awk '/^(FAIL|--- FAIL)/' "$WORKDIR/test.log" | head -n 30 | while IFS= read -r line; do printf '  %s\n' "$line"; done
    KEEP=1
    die "the rebased tree does not pass its tests" 3
  fi
  note "go test: ok"
else
  note "tests skipped by request (--no-tests): this rebase is UNVERIFIED"
fi

if [ "$ADOPT" -eq 1 ]; then
  section "adopt"
  if [ -z "$ADOPT_BRANCH" ]; then
    ADOPT_BRANCH="$BRANCH-rebased-$(date +%Y%m%d%H%M%S)"
  fi
  if git -C "$REPO" rev-parse --verify --quiet "$ADOPT_BRANCH^{commit}" >/dev/null; then
    die "branch '$ADOPT_BRANCH' already exists in $REPO; pass another name to --adopt"
  fi
  git -C "$REPO" fetch --quiet --no-tags "$CLONE" "$BRANCH:$ADOPT_BRANCH" \
    || die "could not fetch the result into $REPO"
  note "created branch '$ADOPT_BRANCH' in $REPO at $RESULT_SHA"
  note "HEAD in $REPO was not moved, and nothing was pushed."
  note ""
  note "When you are ready to publish it, you do that by hand:"
  note "  git -C $REPO push origin $ADOPT_BRANCH:$BRANCH"
else
  note ""
  note "Not adopted. The result lives only in the clone; re-run with --adopt to"
  note "create a branch in $REPO (still without pushing)."
  KEEP=1
fi

exit 0
