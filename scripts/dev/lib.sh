#!/usr/bin/env bash
# lib.sh — shared state and guards for every script under scripts/dev/.
#
# What it does: exports the paths, the container name and the compose
# invocation that the dev environment is addressed through, plus the guards
# every entry point runs before it touches Docker.
#
# What it guarantees:
#   * assert_no_live_db() refuses to continue if anything in this run could
#     reach ~/.engram. The dev environment validates schema migrations and
#     merges; a single mount of the live store would apply them to the only
#     copy of the user's memory.
#   * require_setup() refuses to continue when ./setup.sh has not been run in
#     this checkout, because .claude/skills/ and .claude/agents/ are generated,
#     are not tracked, and resolve to nothing — silently — when absent.
#   * assert_dev_port_local() refuses to continue when the published HTTP port
#     is listening on anything but the host's loopback address. The container
#     binds 0.0.0.0 inside its own namespace; the host-side bind is the only
#     thing keeping the dev store off the local network.
#
# This file is sourced, never executed. It defines no side effects beyond the
# variables and functions below.

set -euo pipefail

if [ "${BASH_SOURCE[0]}" = "${0}" ]; then
  printf 'lib.sh is sourced by the other scripts in this directory, not executed.\n' >&2
  exit 2
fi

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
COMPOSE_FILE="$ROOT_DIR/docker-compose.dev.yml"
# Compose project name, fixed by `name: engram-dev` in the compose file. It is
# the prefix Compose would put on a volume key that carries no explicit
# `name:`; every volume in docker-compose.dev.yml pins one, so today the real
# volume names are the bare ones. dev_volume() below keeps the prefixed lookup
# as a fallback in case a pin is ever dropped.
COMPOSE_PROJECT="engram-dev"
DC=(docker compose -f "$COMPOSE_FILE")
CONTAINER="engram-dev"
OUT_DIR="$ROOT_DIR/docker/dev/out"
# Pinned to the version in go.mod and in .github/workflows/ci.yml. The machine
# also carries golang:1.27.0; building against it would measure a toolchain the
# project does not ship.
GO_IMAGE="golang:1.25.10"
IMAGE="engram-dev:latest"
LIVE_DB_DIR="$HOME/.engram"
# Host-side port docker-compose.dev.yml publishes the container's 7437 on.
# Deliberately not 7437, so a dev stack never collides with the engram serve
# the developer already runs on the real store.
DEV_HTTP_PORT="17437"

export ROOT_DIR COMPOSE_FILE COMPOSE_PROJECT CONTAINER OUT_DIR GO_IMAGE IMAGE LIVE_DB_DIR DEV_HTTP_PORT

# log writes progress to stderr so a script's stdout stays machine-readable.
log() {
  printf '[%s] %s\n' "$(date -u '+%H:%M:%S')" "$*" >&2
}

# fail reports the reason and stops the script with a non-zero status.
fail() {
  printf 'FAIL: %s\n' "$*" >&2
  exit 1
}

# require_cmd stops with a named error instead of letting the script die on a
# "command not found" several lines later.
require_cmd() {
  local cmd
  for cmd in "$@"; do
    command -v "$cmd" >/dev/null 2>&1 || fail "missing required command: $cmd"
  done
}

# dev_version stamps the image with the working tree it was built from.
# The suffix is -dirty whenever `git status` reports anything at all, tracked
# or untracked: the build context is the working tree, not HEAD, so an
# untracked file is as much a part of the image as a modified one.
dev_version() {
  local sha="unknown" dirty=""
  if sha="$(git -C "$ROOT_DIR" rev-parse --short HEAD 2>/dev/null)"; then
    :
  else
    sha="unknown"
  fi
  if [ -n "$(git -C "$ROOT_DIR" status --porcelain 2>/dev/null)" ]; then
    dirty="-dirty"
  fi
  printf 'dev-%s%s' "$sha" "$dirty"
}

# in_container runs a command in the already-running dev container. Stdin is
# kept open (-i, no -t) so an NDJSON MCP session can be piped straight in.
in_container() {
  docker exec -i "$CONTAINER" "$@"
}

# mcp_session_lockstep <in.jsonl> <out.jsonl> <stderr> -- <command...>
# runs one MCP session, sending a request only once the previous one has been
# answered, and appends every response line to <out.jsonl> in arrival order.
#
# The stdio server runs tool calls on a worker pool, so a file piped whole lets
# a read overtake the write it depends on; sending one request per response
# keeps the fixture's order.
#
# A line carrying an `id` is followed by a wait for the response with that same
# id. A notification carries no id, is answered by nothing, and is therefore
# sent without waiting. The command talks to two FIFOs rather than to a pipe
# because a pipe is only writable while its reader lives: the session has to
# stay open across the whole fixture, one line at a time.
mcp_session_lockstep() {
  local in_file="$1" out_file="$2" err_file="$3"
  shift 3
  [ "${1:-}" = "--" ] || fail "mcp_session_lockstep: the command must follow a -- separator"
  shift
  [ "$#" -gt 0 ] || fail "mcp_session_lockstep: no command given after --"
  [ -f "$in_file" ] || fail "mcp_session_lockstep: missing session fixture: $in_file"

  require_cmd mkfifo jq

  local work
  work="$(mktemp -d)"
  mkfifo "$work/in" "$work/out"
  : >"$out_file"

  "$@" <"$work/in" >"$work/out" 2>"$err_file" &
  local pid=$!

  # Order matters: the command opens its stdin FIFO first and blocks until a
  # writer appears, then its stdout FIFO and blocks until a reader appears, so
  # the two ends are opened here in the same order.
  exec 3>"$work/in" 4<"$work/out"

  local line id reply reply_id rc
  while IFS= read -r line; do
    [ -n "$line" ] || continue
    printf '%s\n' "$line" >&3
    id="$(printf '%s' "$line" | jq -r '.id // empty' 2>/dev/null || true)"
    [ -n "$id" ] || continue
    while :; do
      if IFS= read -r -t 30 reply <&4; then
        [ -n "$reply" ] || continue
        printf '%s\n' "$reply" >>"$out_file"
        reply_id="$(printf '%s' "$reply" | jq -r '.id // empty' 2>/dev/null || true)"
        [ "$reply_id" = "$id" ] && break
      else
        rc=$?
        exec 3>&- 4<&-
        kill "$pid" 2>/dev/null || true
        rm -rf "$work"
        [ "$rc" -gt 128 ] && fail "no response for id $id within 30s"
        fail "the session ended before a response for id $id arrived; see $err_file"
      fi
    done
  done <"$in_file"

  exec 3>&-
  # Whatever the server writes on its way out is drained rather than left in
  # the FIFO: an unread line would block its exit on a full pipe, and the
  # capture is supposed to hold every line the session produced.
  while IFS= read -r -t 5 reply <&4; do
    [ -n "$reply" ] || continue
    printf '%s\n' "$reply" >>"$out_file"
  done
  exec 4<&-

  local status=0
  wait "$pid" || status=$?
  rm -rf "$work"
  return "$status"
}

# dev_volume resolves a declared volume name to the real Docker volume.
# docker-compose.dev.yml pins an explicit `name:` on every volume, so the real
# name is the bare one and this resolves to it. The prefixed lookup stays as a
# fallback for the case where those pins are dropped: Compose would then create
# <project>_<name> for its services while a bare `docker run -v <name>` would
# create a second, empty volume, and the shots profile would read the empty one.
dev_volume() {
  local short="$1"
  local qualified="${COMPOSE_PROJECT}_${short}"
  if docker volume inspect "$qualified" >/dev/null 2>&1; then
    printf '%s' "$qualified"
    return 0
  fi
  printf '%s' "$short"
}

# ensure_volume resolves a volume name and creates it if it does not exist.
# `docker volume create` is idempotent, so this is safe to call on every run.
# Creating it up front rather than letting `docker run -v` auto-create it means
# the name a script uses is the name that exists, even on the first run.
ensure_volume() {
  local name
  name="$(dev_volume "$1")"
  docker volume create "$name" >/dev/null
  printf '%s' "$name"
}

# assert_no_live_db refuses to run anything that could reach the live store.
# Two independent ways in are checked: the ENGRAM_DATA_DIR this shell would
# hand to a container, and every path Compose resolves in the merged config
# (which is where a relative `./`-mount or a ${HOME} interpolation would show
# up). The resolved config is also written out as the isolation evidence a
# reviewer checks before signing off on a run.
assert_no_live_db() {
  if [ "${ENGRAM_DATA_DIR:-}" = "$LIVE_DB_DIR" ]; then
    fail "ENGRAM_DATA_DIR points at the live store ($LIVE_DB_DIR); the dev environment never opens it"
  fi

  [ -f "$COMPOSE_FILE" ] || fail "compose file not found: $COMPOSE_FILE"

  local config
  config="$("${DC[@]}" config 2>&1)" || fail "docker compose config failed:
$config"

  case "$config" in
    *"$LIVE_DB_DIR"*)
      mkdir -p "$OUT_DIR/isolation"
      printf '%s\n' "$config" >"$OUT_DIR/isolation/compose-config.txt"
      fail "docker compose config resolves a path under $LIVE_DB_DIR; see $OUT_DIR/isolation/compose-config.txt"
      ;;
  esac

  mkdir -p "$OUT_DIR/isolation"
  printf '%s\n' "$config" >"$OUT_DIR/isolation/compose-config.txt"
  log "isolation ok: nothing in the compose config resolves under $LIVE_DB_DIR"
}

# assert_dev_port_local refuses to continue when the published HTTP port is
# reachable from anything but this machine.
#
# `docker compose config` proves what the file asks for; lsof proves what the
# kernel actually did. Docker opens the host-side socket itself, so a ports:
# entry that loses its "127.0.0.1:" prefix still produces a working stack —
# one that also answers the local network. Only the live socket shows that.
#
# Run it after the container reports healthy: before that, Docker may not have
# opened the host-side listener yet.
assert_dev_port_local() {
  require_cmd lsof

  local listeners
  listeners="$(lsof -nP -iTCP:"$DEV_HTTP_PORT" -sTCP:LISTEN 2>/dev/null || true)"
  [ -n "$listeners" ] || fail "nothing is listening on port $DEV_HTTP_PORT; the dev api is not published"

  local line addr bad=""
  while IFS= read -r line; do
    case "$line" in
      COMMAND* | "") continue ;;
    esac
    # The NAME column is the last field before the "(LISTEN)" marker, e.g.
    # "127.0.0.1:17437" or "[::1]:17437". Drop the port to get the bind
    # address on its own.
    addr="${line%% (LISTEN)*}"
    addr="${addr##* }"
    addr="${addr%:*}"
    case "$addr" in
      127.0.0.1 | localhost | ::1 | "[::1]") ;;
      *) bad="$bad $addr" ;;
    esac
  done <<<"$listeners"

  [ -z "$bad" ] || fail "port $DEV_HTTP_PORT listens on$bad, not only on the host's loopback address; fix the ports: entry in $COMPOSE_FILE"

  log "port ok: $DEV_HTTP_PORT listens on the loopback address only"
}

# require_setup refuses to run until ./setup.sh has populated .claude/.
# Both directories are generated and git-ignored, so a fresh checkout has
# neither: the agent's `skills:` field would resolve to nothing without any
# error, and koi-sensei would not exist as an agent at all.
#
# The skills directory entry is named after the skill's own directory under
# skills/, which is not the same string as the `name:` in its frontmatter
# (skills/architecture-guardrails/SKILL.md declares engram-architecture-
# guardrails). Both spellings are accepted so this guard keeps working if
# setup.sh ever links by frontmatter name instead.
require_setup() {
  local skills_dir="$ROOT_DIR/.claude/skills"
  local agent_file="$ROOT_DIR/.claude/agents/koi-sensei.md"
  local candidate found=""

  for candidate in \
    "$skills_dir/engram-architecture-guardrails" \
    "$skills_dir/architecture-guardrails"; do
    if [ -e "$candidate" ]; then
      found="$candidate"
      break
    fi
  done

  [ -n "$found" ] || fail "no architecture-guardrails skill under $skills_dir — run ./setup.sh first"
  [ -f "$agent_file" ] || fail "$agent_file is missing — run ./setup.sh first"
  log "setup ok: $found and $agent_file are in place"
}

# GIT_MOUNT_ARGS carries the extra `docker run -v` flags a checkout needs so the
# container can read its own git metadata. In an ordinary clone it stays empty:
# the mount of the working tree already carries .git. In a linked worktree .git
# is a file naming a directory outside the tree, so without this the container
# sees a checkout git refuses to read, and every test that resolves a project
# from the repository fails for a reason that has nothing to do with the code.
GIT_MOUNT_ARGS=()

set_git_mount_args() {
  GIT_MOUNT_ARGS=()
  [ -f "$ROOT_DIR/.git" ] || return 0

  local common
  common="$(git -C "$ROOT_DIR" rev-parse --path-format=absolute --git-common-dir 2>/dev/null || true)"
  [ -n "$common" ] && [ -d "$common" ] || return 0

  # Mounted at its own absolute path, because that is the path the .git file
  # names: anywhere else and the pointer still dangles.
  GIT_MOUNT_ARGS=(-v "$common:$common")
  log "linked worktree: mounting $common so git works inside the container"
}
