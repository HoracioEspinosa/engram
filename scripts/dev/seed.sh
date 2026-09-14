#!/usr/bin/env bash
# seed.sh — fill the dev store with the fixture workspace.
#
# What it does: upserts the four project cards, the five tasks, the three
# evidence rows and the runbook index, then saves `engram doctor --json`.
#
# What it guarantees: idempotence. Every write below goes through a subcommand
# whose upsert key is stable (the slug for a card, the Jira key or sdd_change
# for a task, (task, sha256) for evidence), so running it twice leaves the same
# rows and the same counters. That is what lets a gate re-seed without first
# tearing the volume down.
#
# Why ENGRAM_PROJECT is set on every call: `upsert` and `tasks upsert` refuse
# an explicit slug that is neither backed by an existing card nor equal to what
# ENGRAM_PROJECT or the cwd resolve to (docs/ENGRAM-PROJECTS-CLI.md), so the
# very first upsert of a brand-new fixture project can only be made by
# declaring that project in the environment.
#
# Usage:
#   seed.sh
#   seed.sh --with-live-copy <path/to/engram-<UTC>.db>
#
# --with-live-copy places a copy produced by backup-live.sh at
# /data/live/engram.db inside the container, in a data directory of its own.
# /data/live is never the container's own ENGRAM_DATA_DIR: the fixtures and the
# real-data copy have to stay separate stores, because reorg-check.sh mutates
# the second one and must not touch the first.

set -euo pipefail

# shellcheck source=scripts/dev/lib.sh
. "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"

LIVE_COPY=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    --with-live-copy)
      [ "$#" -ge 2 ] || fail "--with-live-copy requires a path"
      LIVE_COPY="$2"
      shift
      ;;
    -h | --help)
      printf 'usage: seed.sh [--with-live-copy <copy.db>]\n' >&2
      exit 0
      ;;
    *)
      fail "unknown argument: $1"
      ;;
  esac
  shift
done

require_cmd docker jq
assert_no_live_db

SEED_OUT="$OUT_DIR/seed"
mkdir -p "$SEED_OUT"

REPO_URL="https://example.invalid/koi/garden.git"

# card <slug> <display name> <repo url> <default branch> <jira project> <knowledge hub>
card() {
  local slug="$1" display="$2" repo="$3" branch="$4" jira="$5" hub="$6"
  log "card: $slug"
  docker exec -i -e "ENGRAM_PROJECT=$slug" "$CONTAINER" \
    engram project "$slug" upsert \
    --display-name "$display" \
    --repo-url "$repo" \
    --default-branch "$branch" \
    --jira-project "$jira" \
    --knowledge-hub "$hub" \
    --json >"$SEED_OUT/card-$slug.json"
}

# task <project> <key flag> <key> <title> <kind> <state>
task() {
  local project="$1" keyflag="$2" key="$3" title="$4" kind="$5" state="$6"
  log "task: $project $key"
  docker exec -i -e "ENGRAM_PROJECT=$project" "$CONTAINER" \
    engram project "$project" tasks upsert \
    "$keyflag" "$key" \
    --title "$title" \
    --kind "$kind" \
    --state "$state" \
    --json >"$SEED_OUT/task-$project-$(printf '%s' "$key" | tr '[:upper:]' '[:lower:]').json"
}

# evidence <project> <task> <relative path under CD_EVIDENCE_DIR> <proves>
# The digest is computed inside the container, against the file the container
# actually sees, rather than being pinned here: a fixture regenerated with a
# different byte for byte content would otherwise be registered under a digest
# that no longer describes it.
evidence() {
  local project="$1" task="$2" rel="$3" proves="$4"
  local sha
  sha="$(in_container sh -c "sha256sum '/vault/evidence/$rel' | cut -d' ' -f1")"
  [ -n "$sha" ] || fail "could not hash /vault/evidence/$rel inside the container"
  log "evidence: $project $task $rel ($sha)"
  docker exec -i -e "ENGRAM_PROJECT=$project" "$CONTAINER" \
    engram project "$project" evidence add "$task" \
    --path "$rel" \
    --sha256 "$sha" \
    --kind png \
    --proves "$proves" \
    --json >"$SEED_OUT/evidence-$project-$task.json"
}

# knowledge_hub_path names the hub document, not the project folder; the doctor
# treats a directory as dangling. The three koi-garden cards share one hub, the
# way a set of sibling deployments shares the page that documents all of them.
card koi-garden "Koi Garden" "$REPO_URL" master KOI koi-garden/README.md
card koi-garden-pond-01 "Koi Garden · Pond 01" "$REPO_URL" pond-01 KOI koi-garden/README.md
card koi-garden-pond-02 "Koi Garden · Pond 02" "$REPO_URL" pond-02 KOI koi-garden/README.md
card tsukimi-bridge "Tsukimi Bridge" "https://example.invalid/koi/tsukimi-bridge.git" master TSU tsukimi-bridge/README.md

task koi-garden --jira KOI-1042 "hardening autologin" feature "done"
task koi-garden --jira KOI-1099 "lookup timeout en el estanque" bugfix "in_progress"
# The two ticketless tasks carry the vault's "Sin confirmar" and "Histórico"
# states, which have no equivalent in the eight states the schema accepts
# today; they are seeded as `open` and the mapping to the new states lands
# with the task rebuild.
task koi-garden --sdd-change split-pond-filesharing "split pond filesharing" spike open
task koi-garden --sdd-change mantenimiento-del-estanque "mantenimiento del estanque" spike open
task tsukimi-bridge --jira TSU-204 "migración de thumbnails" migration in_progress

evidence koi-garden KOI-1042 "koi-garden/KOI-1042/01-pantalla-login.png" "la pantalla de login responde tras el hardening"
evidence koi-garden KOI-1099 "koi-garden/KOI-1099/01-traza.png" "la traza muestra el timeout del lookup"
evidence tsukimi-bridge TSU-204 "tsukimi-bridge/TSU-204/01-galeria.png" "la galería carga los thumbnails migrados"

# Runbooks are seeded from the prepared entries file rather than by scanning
# the vault, once per project that owns entries. The sync drops an entry whose
# project is not the one the command runs for, so a single call would reject
# three of the five as other_project and report a number about the call rather
# than about the entries. koi-garden-pond-01 owns none, so it is not in the loop.
#
# --entries-file resolves each `service:` through the fixed map in
# internal/runbooks/service_map.go (internal/runbooks/index.go:24), exactly as
# --vault-dir does, and that map only knows fifteen real service slugs. Every
# fixture service sits outside it on purpose, so the whole fixture is skipped as
# unknown_service and runbook_index stays empty: the map is a fixed list, so
# every fictional service is rejected until the map becomes data-driven, and
# that rejection is not a seeding failure.
rm -f "$SEED_OUT"/runbooks-sync-*.json
for slug in koi-garden koi-garden-pond-02 tsukimi-bridge; do
  log "runbooks: syncing $slug from /vault/Runbooks/.entries.json"
  set +e
  docker exec -i -e "ENGRAM_PROJECT=$slug" "$CONTAINER" \
    engram project "$slug" runbooks sync \
    --entries-file /vault/Runbooks/.entries.json \
    --json >"$SEED_OUT/runbooks-sync-$slug.json" 2>"$SEED_OUT/runbooks-sync-$slug.stderr"
  runbooks_status=$?
  set -e
  if [ "$runbooks_status" -ne 0 ]; then
    log "WARN runbooks sync for $slug exited $runbooks_status (see $SEED_OUT/runbooks-sync-$slug.json and .stderr); recorded, not fatal for seeding"
  fi
done

# One line for the whole fixture: what was indexed, and why the rest was not.
# other_project is left out because it says nothing about an entry — it is what
# the two projects that do not own it report, and every entry is already counted
# once, by the project that does.
jq -s '{
    upserted: (map(.result.sync.upserted // 0) | add),
    skipped: (
      map(.result.sync.skipped[]? | select(.reason != "other_project") | .reason)
      | group_by(.) | map({(.[0]): length}) | add // {}
    )
  }' "$SEED_OUT"/runbooks-sync-*.json >"$SEED_OUT/runbooks-baseline.json"
log "runbooks baseline: $(jq -c . "$SEED_OUT/runbooks-baseline.json")"

if [ -n "$LIVE_COPY" ]; then
  [ -f "$LIVE_COPY" ] || fail "live copy not found: $LIVE_COPY"
  log "installing the live copy at /data/live/engram.db"
  in_container mkdir -p /data/live
  # docker cp lands the file owned by root; the container runs as uid 10001,
  # so ownership is handed over explicitly. Done by uid rather than by name so
  # it does not depend on the account name the image happens to use.
  docker cp "$LIVE_COPY" "$CONTAINER:/data/live/engram.db"
  docker exec -u 0:0 "$CONTAINER" chown 10001:10001 /data/live /data/live/engram.db \
    || log "WARN could not chown /data/live inside the container; a later write to the copy may fail"

  integrity="$(in_container sqlite3 /data/live/engram.db 'PRAGMA integrity_check;' | head -1)"
  if [ "$integrity" != "ok" ]; then
    fail "the installed live copy does not pass integrity_check: $integrity"
  fi
  log "live copy installed and verified (integrity_check = ok)"
  printf '%s\n' "$integrity" >"$SEED_OUT/live-copy-integrity.txt"
fi

log "saving doctor report"
in_container engram doctor --json >"$SEED_OUT/doctor.json"

log "seed complete; artifacts under $SEED_OUT"
