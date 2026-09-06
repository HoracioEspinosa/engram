#!/usr/bin/env bash
# run.sh — canary battery for the release scripts.
#
# Every case fixes an exit code by exact equality and, where the message is the
# deliverable, a substring of the output. The rebase cases run end to end
# against synthetic git repositories on local paths, so the battery needs no
# network and no GitHub.
#
# Usage:
#   scripts/release/tests/run.sh [--shell /path/to/bash]
#
# Without --shell it runs the whole battery once per bash it can find
# (/bin/bash and any other bash on PATH), because these scripts have to work on
# the bash 3.2 that macOS ships as well as on a modern one.
#
# Exit code: 0 when every case passes, 1 otherwise.
set -u

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
RELEASE_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
REBASE="$RELEASE_DIR/rebase-onto-upstream.sh"
BUILD="$RELEASE_DIR/build-release.sh"

SHELLS=""
while [ $# -gt 0 ]; do
  case "$1" in
    --shell) SHELLS="$2"; shift 2 ;;
    -h|--help) printf 'usage: run.sh [--shell /path/to/bash]\n'; exit 0 ;;
    *) printf 'unknown argument: %s\n' "$1" >&2; exit 1 ;;
  esac
done

if [ -z "$SHELLS" ]; then
  SHELLS="/bin/bash"
  OTHER="$(command -v bash 2>/dev/null || printf '')"
  if [ -n "$OTHER" ] && [ "$OTHER" != "/bin/bash" ]; then
    SHELLS="$SHELLS $OTHER"
  fi
fi

PASS=0
FAIL=0

pass() { PASS=$((PASS + 1)); printf '[PASS] %s\n' "$1"; }
fail() { FAIL=$((FAIL + 1)); printf '[FAIL] %s\n' "$1"; }

# run_case <name> <expected-exit> <expected-substring-or-empty> -- <cmd...>
run_case() {
  name="$1"; want_code="$2"; want_text="$3"; shift 3
  [ "$1" = "--" ] && shift
  out=""
  out="$("$@" 2>&1)"
  code=$?
  if [ "$code" -ne "$want_code" ]; then
    fail "$name: exit $code, expected $want_code"
    printf '%s\n' "$out" | while IFS= read -r l; do printf '       %s\n' "$l"; done
    return
  fi
  if [ -n "$want_text" ]; then
    case "$out" in
      *"$want_text"*) ;;
      *)
        fail "$name: exit $code as expected, but the output does not contain: $want_text"
        printf '%s\n' "$out" | while IFS= read -r l; do printf '       %s\n' "$l"; done
        return ;;
    esac
  fi
  pass "$name"
}

git_q() { git -c user.name=canary -c user.email=canary@invalid -c commit.gpgsign=false "$@" >/dev/null 2>&1; }

# make_fixture <dir> <mode>
# mode=clean    the custom commit touches a file upstream never touched
# mode=conflict the custom commit touches the same line upstream moved
# mode=empty    the fork carries no commit upstream does not have
make_fixture() {
  root="$1"; mode="$2"
  up="$root/upstream"
  fork="$root/fork"
  mkdir -p "$up"
  git_q init -b main "$up"
  printf 'base\n' > "$up/shared.txt"
  printf 'untouched\n' > "$up/other.txt"
  git_q -C "$up" add -A
  git_q -C "$up" commit -m "base"

  git_q clone "$up" "$fork"
  git_q -C "$fork" checkout -b custom/main

  if [ "$mode" != "empty" ]; then
    if [ "$mode" = "conflict" ]; then
      printf 'fork change\n' > "$fork/shared.txt"
    else
      printf 'fork feature\n' > "$fork/feature.txt"
    fi
    git_q -C "$fork" add -A
    git_q -C "$fork" commit -m "custom work"
  fi

  # Upstream moves on after the fork branched.
  printf 'upstream change\n' > "$up/shared.txt"
  git_q -C "$up" add -A
  git_q -C "$up" commit -m "upstream advance"
}

battery() {
  sh_bin="$1"
  # shellcheck disable=SC2016 # $BASH_VERSION must be expanded by the shell under test, not by this one
  sh_ver="$("$sh_bin" -c 'printf %s "$BASH_VERSION"')"
  printf '\n===== battery under %s (%s) =====\n' "$sh_bin" "$sh_ver"

  TMP="$(mktemp -d "${TMPDIR:-/tmp}/engram-release-canary.XXXXXX")" || { fail "mktemp"; return; }

  # ---------- rebase-onto-upstream.sh: argument handling ----------
  run_case "rebase --help exits 0 and prints the usage" 0 "Options:" -- \
    "$sh_bin" "$REBASE" --help

  run_case "rebase rejects an unknown argument" 1 "unknown argument: --nope" -- \
    "$sh_bin" "$REBASE" --nope

  run_case "rebase rejects an option without its value" 1 "--branch needs a value" -- \
    "$sh_bin" "$REBASE" --branch

  run_case "rebase rejects a directory that is not a repository" 1 "not a git repository" -- \
    "$sh_bin" "$REBASE" --repo "$TMP"

  # ---------- fixtures ----------
  mkdir -p "$TMP/clean" "$TMP/conflict" "$TMP/empty"
  make_fixture "$TMP/clean" clean
  make_fixture "$TMP/conflict" conflict
  make_fixture "$TMP/empty" empty

  run_case "rebase rejects a branch that does not exist" 1 "does not exist" -- \
    "$sh_bin" "$REBASE" --repo "$TMP/clean/fork" --branch no/such/branch \
      --upstream-url "$TMP/clean/upstream"

  run_case "rebase refuses an upstream URL that is not upstream" 1 "refusing an unexpected upstream URL" -- \
    "$sh_bin" "$REBASE" --repo "$TMP/clean/fork" --upstream-url "https://github.com/someone-else/engram.git"

    # ---------- release workflow: verify release runbook checkpoint logic ----------
  REPO_ROOT="$(cd "$RELEASE_DIR/../.." && pwd)"
  run_case "release runbook has checkout, merge, and push in order in section 3.2" 0 "" -- \
    "$sh_bin" -c '
      file="'"$REPO_ROOT"'/docs/RELEASE-FORK.md"

      # Verify all three commands are present
      if ! grep -q "git -C ~/Projects/engram checkout custom/main" "$file"; then
        printf "runbook missing: git checkout custom/main\n" >&2
        exit 1
      fi
      if ! grep -q "git -C ~/Projects/engram merge --ff-only" "$file"; then
        printf "runbook missing: git merge --ff-only\n" >&2
        exit 1
      fi
      if ! grep -q "git -C ~/Projects/engram push origin custom/main" "$file"; then
        printf "runbook missing: git push origin custom/main\n" >&2
        exit 1
      fi

      # Get line numbers and verify order (checkout < merge < push)
      checkout_line=$(grep -n "git -C ~/Projects/engram checkout custom/main" "$file" | sed "s/:.*//")
      merge_line=$(grep -n "git -C ~/Projects/engram merge --ff-only" "$file" | sed "s/:.*//")
      push_line=$(grep -n "git -C ~/Projects/engram push origin custom/main" "$file" | sed "s/:.*//")

      if [ "$checkout_line" -ge "$merge_line" ]; then
        printf "checkout (line %s) must come before merge (line %s)\n" "$checkout_line" "$merge_line" >&2
        exit 1
      fi
      if [ "$merge_line" -ge "$push_line" ]; then
        printf "merge (line %s) must come before push (line %s)\n" "$merge_line" "$push_line" >&2
        exit 1
      fi
    '


    # ---------- rebase: the clean path ----------
  run_case "rebase replays a clean branch and reports the plan" 0 "custom commits:    1" -- \
    "$sh_bin" "$REBASE" --repo "$TMP/clean/fork" --upstream-url "$TMP/clean/upstream" \
      --no-tests --workdir "$TMP/wd-clean"

  run_case "rebase says so when it did not run the tests" 0 "this rebase is UNVERIFIED" -- \
    "$sh_bin" "$REBASE" --repo "$TMP/clean/fork" --upstream-url "$TMP/clean/upstream" \
      --no-tests --workdir "$TMP/wd-clean2"

  # The clone must not be able to reach a remote with a write.
  if [ -d "$TMP/wd-clean/engram" ]; then
    pushurl="$(git -C "$TMP/wd-clean/engram" config --get remote.origin.pushurl 2>/dev/null || printf 'MISSING')"
    if [ "$pushurl" = "no-push://blocked" ]; then
      pass "rebase blocks the push url on origin in the clone"
    else
      fail "rebase blocks the push url on origin in the clone: got '$pushurl'"
    fi
    upushurl="$(git -C "$TMP/wd-clean/engram" config --get remote.upstream.pushurl 2>/dev/null || printf 'MISSING')"
    if [ "$upushurl" = "no-push://blocked" ]; then
      pass "rebase blocks the push url on upstream in the clone"
    else
      fail "rebase blocks the push url on upstream in the clone: got '$upushurl'"
    fi
  else
    fail "rebase blocks the push url on origin in the clone: the workdir was not kept, nothing to inspect"
    fail "rebase blocks the push url on upstream in the clone: the workdir was not kept, nothing to inspect"
  fi

  run_case "rebase refuses a workdir that already exists" 1 "workdir already exists" -- \
    "$sh_bin" "$REBASE" --repo "$TMP/clean/fork" --upstream-url "$TMP/clean/upstream" \
      --no-tests --workdir "$TMP/wd-clean"

  # ---------- rebase: nothing to replay ----------
  run_case "rebase reports an empty branch instead of pretending to work" 0 "nothing to replay" -- \
    "$sh_bin" "$REBASE" --repo "$TMP/empty/fork" --upstream-url "$TMP/empty/upstream" \
      --no-tests --workdir "$TMP/wd-empty"

  # ---------- rebase: the conflict path ----------
  run_case "rebase exits 2 on a conflict" 2 "conflict inventory" -- \
    "$sh_bin" "$REBASE" --repo "$TMP/conflict/fork" --upstream-url "$TMP/conflict/upstream" \
      --no-tests --workdir "$TMP/wd-conflict"

  run_case "rebase counts the unmerged paths and the hunks" 2 "unmerged paths: 1   conflict hunks: 1" -- \
    "$sh_bin" "$REBASE" --repo "$TMP/conflict/fork" --upstream-url "$TMP/conflict/upstream" \
      --no-tests --workdir "$TMP/wd-conflict2"

  run_case "rebase names the commit it stopped on" 2 "stopped on:" -- \
    "$sh_bin" "$REBASE" --repo "$TMP/conflict/fork" --upstream-url "$TMP/conflict/upstream" \
      --no-tests --workdir "$TMP/wd-conflict3"

  # ---------- rebase: the source repository is never touched ----------
  before_head="$(git -C "$TMP/conflict/fork" rev-parse HEAD)"
  before_branch="$(git -C "$TMP/conflict/fork" symbolic-ref --short HEAD)"
  before_status="$(git -C "$TMP/conflict/fork" status --porcelain=v1)"
  "$sh_bin" "$REBASE" --repo "$TMP/conflict/fork" --upstream-url "$TMP/conflict/upstream" \
    --no-tests --workdir "$TMP/wd-conflict4" >/dev/null 2>&1
  after_head="$(git -C "$TMP/conflict/fork" rev-parse HEAD)"
  after_branch="$(git -C "$TMP/conflict/fork" symbolic-ref --short HEAD)"
  after_status="$(git -C "$TMP/conflict/fork" status --porcelain=v1)"
  if [ "$before_head" = "$after_head" ] && [ "$before_branch" = "$after_branch" ] && [ "$before_status" = "$after_status" ]; then
    pass "a conflicting rebase leaves the source repository exactly as it was"
  else
    fail "a conflicting rebase leaves the source repository exactly as it was: head $before_head -> $after_head, branch $before_branch -> $after_branch"
  fi

  # ---------- rebase: adopt ----------
  run_case "rebase --adopt creates the branch it names" 0 "created branch 'rebased-ok'" -- \
    "$sh_bin" "$REBASE" --repo "$TMP/clean/fork" --upstream-url "$TMP/clean/upstream" \
      --no-tests --adopt rebased-ok --workdir "$TMP/wd-adopt"

  if git -C "$TMP/clean/fork" rev-parse --verify --quiet "rebased-ok^{commit}" >/dev/null 2>&1; then
    pass "the adopted branch exists in the source repository"
  else
    fail "the adopted branch exists in the source repository"
  fi

  head_after_adopt="$(git -C "$TMP/clean/fork" symbolic-ref --short HEAD)"
  if [ "$head_after_adopt" = "custom/main" ]; then
    pass "adopting does not move HEAD in the source repository"
  else
    fail "adopting does not move HEAD in the source repository: HEAD is $head_after_adopt"
  fi

  run_case "rebase --adopt refuses to overwrite an existing branch" 1 "already exists" -- \
    "$sh_bin" "$REBASE" --repo "$TMP/clean/fork" --upstream-url "$TMP/clean/upstream" \
      --no-tests --adopt rebased-ok --workdir "$TMP/wd-adopt2"

  # ---------- build-release.sh ----------
  run_case "build --help exits 0 and prints the usage" 0 "Options:" -- \
    "$sh_bin" "$BUILD" --help

  run_case "build rejects an unknown argument" 1 "unknown argument: --publish" -- \
    "$sh_bin" "$BUILD" --publish

  run_case "build rejects a tag that is not shaped for this fork" 1 "is not shaped v<version>-cd.<n>" -- \
    "$sh_bin" "$BUILD" --tag v1.20.0

  run_case "build rejects a directory that is not a repository" 1 "not a git repository" -- \
    "$sh_bin" "$BUILD" --repo "$TMP"

  # A missing tool must be named, not silently skipped.
  # The shim comes first and carries only what the case is allowed to find;
  # /usr/bin and /bin keep the ordinary utilities available so the case
  # isolates the missing release tool and nothing else.
  SHIM="$TMP/shim"
  mkdir -p "$SHIM"
  for t in git go; do
    p="$(command -v "$t" 2>/dev/null || printf '')"
    [ -n "$p" ] && ln -sf "$p" "$SHIM/$t"
  done
  run_case "build names the release tool it cannot find" 1 "goreleaser is required" -- \
    env PATH="$SHIM:/usr/bin:/bin" "$sh_bin" "$BUILD" --repo "$TMP/clean/fork"

  for t in git go goreleaser; do
    p="$(command -v "$t" 2>/dev/null || printf '')"
    [ -n "$p" ] && ln -sf "$p" "$SHIM/$t"
  done
  if [ -e "$SHIM/goreleaser" ]; then
    run_case "build refuses --image when docker is missing" 1 "docker is not installed" -- \
      env PATH="$SHIM:/usr/bin:/bin" "$sh_bin" "$BUILD" --repo "$TMP/clean/fork" --image
    run_case "build refuses a ref without the release configuration" 1 "is missing at" -- \
      env PATH="$SHIM:/usr/bin:/bin" "$sh_bin" "$BUILD" --repo "$TMP/clean/fork" --ref custom/main \
        --workdir "$TMP/wd-build"
  else
    fail "build refuses --image when docker is missing: goreleaser is not installed here, case not run"
    fail "build refuses a ref without the release configuration: goreleaser is not installed here, case not run"
  fi

  rm -rf "$TMP"
}

for s in $SHELLS; do
  if [ ! -x "$s" ]; then
    fail "shell $s is not executable"
    continue
  fi
  battery "$s"
done

printf '\n== summary ==\nPASS: %s  FAIL: %s\n' "$PASS" "$FAIL"
[ "$FAIL" -eq 0 ] && exit 0 || exit 1
