#!/usr/bin/env bash
# cloud-roundtrip.sh — rehearse a projects round trip against the dev cloud.
#
# What it does: brings up the `cloud` profile (Postgres plus `engram cloud
# serve` on 28081), builds two throwaway stores inside engram-dev — /data/rt-a,
# the machine that writes, and /data/rt-b, the machine that has never seen any
# of it — writes the koi fixture into rt-a with the shipped CLI, pushes it to
# the cloud, pulls it into rt-b, and then compares the two stores field by
# field.
#
# What it guarantees: that `engram sync --cloud` carries an engram-projects row
# across two machines without losing or inventing a field. The comparison is
# column by column over project_cards, tasks, evidence and observations rather
# than a row count, because a round trip that replicates a card but drops its
# `kind` or resets its `depth` would pass a count and fail a reader.
#
# Local ids and local timestamps are excluded on purpose: `id`, `created_at`,
# `updated_at` and `task_id` describe the row's life on one machine, not the
# fact it carries. `sync_id` is the identity that travels, so it is compared.
#
# Auth: the dev cloud runs with a legacy sync token rather than under
# ENGRAM_CLOUD_INSECURE_NO_AUTH=1. The insecure mode serves the sync routes too
# — with no authenticator to mint principals, newCloudRuntime leaves the
# principal-project authorizer out and the allowlist alone scopes the request —
# but a token rehearses the authenticated path the product ships. The token
# below is the one docker-compose.dev.yml hands the server; it is local-only
# and is not a secret.
#
# Not covered here, and deliberately left to the phase 2 gate: a card with
# `parent_slug` actually set, `project_aliases` and `benchmarks`. No shipped
# CLI writes any of the three today, so a round trip over them would be a test
# of a SQL statement this script wrote, not of the product.
#
# Usage (from the repository root):
#   bash scripts/dev/cloud-roundtrip.sh

set -euo pipefail

# shellcheck source=scripts/dev/lib.sh
. "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"

# sqlite3 is not required on the host: every read below runs inside engram-dev,
# against a store that only exists there.
require_cmd docker jq curl diff
assert_no_live_db

CLOUD_OUT="$OUT_DIR/cloud"
mkdir -p "$CLOUD_OUT"

LOG_FILE="$CLOUD_OUT/roundtrip.log"
: >"$LOG_FILE"
exec > >(tee -a "$LOG_FILE") 2>&1

# Must match ENGRAM_CLOUD_TOKEN in docker-compose.dev.yml's cloud profile.
CLOUD_TOKEN="${ENGRAM_DEV_CLOUD_TOKEN:-engram-dev-workspace-local-only-sync-token}"
# The address rt-a and rt-b reach the server at: both run inside engram-dev,
# which shares the compose network with cloud-dev, so the service name resolves.
CLOUD_URL="http://cloud-dev:28081"
CLOUD_HEALTH_URL="http://127.0.0.1:28081/health"
CLOUD_BOOT_TIMEOUT=60

DIR_A="/data/rt-a"
DIR_B="/data/rt-b"

PROJECTS=(koi-garden koi-garden-pond-02)
EVIDENCE_REL="koi-garden/KOI-1099/01-traza.png"

PASS_COUNT=0
FAIL_COUNT=0

pass() {
  printf 'PASS  %s\n' "$*"
  PASS_COUNT=$((PASS_COUNT + 1))
}

report_fail() {
  printf 'FAIL  %s\n' "$*"
  FAIL_COUNT=$((FAIL_COUNT + 1))
}

# stop_cloud_profile stops the two cloud services without touching engram-dev,
# which the rest of the gate keeps using. Registered as a trap so an assertion
# that exits early still leaves the stack the way it found it.
stop_cloud_profile() {
  log "stopping the cloud profile"
  "${DC[@]}" --profile cloud stop postgres-dev cloud-dev >/dev/null 2>&1 || true
}

# rt runs an engram command against one of the two throwaway stores. Every
# variable the round trip depends on is passed per call rather than baked into
# the container, so the same engram-dev keeps serving the rest of the gate with
# its own defaults.
rt() {
  local data_dir="$1" project="$2"
  shift 2
  docker exec -i \
    -e "ENGRAM_DATA_DIR=$data_dir" \
    -e "ENGRAM_PROJECTS_SYNC=1" \
    -e "ENGRAM_CLOUD_TOKEN=$CLOUD_TOKEN" \
    -e "ENGRAM_PROJECT=$project" \
    "$CONTAINER" engram "$@"
}

# sql reads one of the two stores. -nullvalue keeps NULL distinguishable from
# the empty string, which is the difference between "the column did not travel"
# and "it travelled carrying an empty value".
sql() {
  local data_dir="$1" query="$2"
  in_container sqlite3 -noheader -nullvalue '<NULL>' "$data_dir/engram.db" "$query"
}

# compare_table runs the same projection on both stores and reports the diff.
# The projection is spelled out per table (never SELECT *) so a new column shows
# up as a deliberate edit here rather than as a silent hole in the comparison.
compare_table() {
  local label="$1" columns="$2" from="$3"
  local query="SELECT $columns FROM $from;"
  local file_a="$CLOUD_OUT/$label-rt-a.tsv"
  local file_b="$CLOUD_OUT/$label-rt-b.tsv"

  sql "$DIR_A" "$query" >"$file_a"
  sql "$DIR_B" "$query" >"$file_b"

  local rows
  rows="$(awk 'NF' "$file_a" | wc -l | tr -d ' ')"

  if diff -u "$file_a" "$file_b" >"$CLOUD_OUT/$label.diff"; then
    if [ "$rows" -eq 0 ]; then
      report_fail "field-by-field $label: both stores are empty, so nothing was compared"
      return
    fi
    pass "field-by-field $label: $rows rows"
    return
  fi
  report_fail "field-by-field $label: the two stores disagree"
  awk '{ print "      | " $0 }' "$CLOUD_OUT/$label.diff"
}

# ─── 1. the cloud profile ────────────────────────────────────────────────────

trap stop_cloud_profile EXIT

log "starting the cloud profile (postgres-dev, cloud-dev)"
"${DC[@]}" --profile cloud up -d postgres-dev cloud-dev

log "waiting up to ${CLOUD_BOOT_TIMEOUT}s for the cloud server to answer /health"
waited=0
while :; do
  if curl -fsS --max-time 3 "$CLOUD_HEALTH_URL" >"$CLOUD_OUT/health.json" 2>/dev/null; then
    break
  fi
  waited=$((waited + 1))
  [ "$waited" -lt "$CLOUD_BOOT_TIMEOUT" ] || fail "the cloud server did not answer $CLOUD_HEALTH_URL within ${CLOUD_BOOT_TIMEOUT}s"
  sleep 1
done
log "cloud health: $(jq -c . "$CLOUD_OUT/health.json")"

# The health endpoint is unauthenticated, so it says the process is up and
# nothing about whether a client can sync. This one does both.
pull_status="$(in_container sh -c "wget -q -S -O /dev/null --header='Authorization: Bearer $CLOUD_TOKEN' '$CLOUD_URL/sync/pull?project=koi-garden' 2>&1 | awk '/HTTP\\//{ code = \$2 } END { print code }'")"
[ "$pull_status" = "200" ] || fail "the cloud server answered $pull_status on an authenticated /sync/pull; expected 200"
pass "the cloud server accepts an authenticated /sync/pull (HTTP 200)"

# ─── 2. two clean stores ─────────────────────────────────────────────────────

log "resetting $DIR_A and $DIR_B"
in_container sh -c "rm -rf '$DIR_A' '$DIR_B' && mkdir -p '$DIR_A' '$DIR_B'"

for dir in "$DIR_A" "$DIR_B"; do
  rt "$dir" "" cloud config --server "$CLOUD_URL" >/dev/null
done
log "both stores point at $CLOUD_URL"

# ─── 3. rt-a writes the fixture ──────────────────────────────────────────────

log "rt-a: project cards"
rt "$DIR_A" koi-garden project koi-garden upsert \
  --display-name "Koi Garden" \
  --jira-project KOI \
  --knowledge-hub koi-garden/README.md \
  --json >"$CLOUD_OUT/rt-a-card-koi-garden.json"
rt "$DIR_A" koi-garden-pond-02 project koi-garden-pond-02 upsert \
  --display-name "Koi Garden · Pond 02" \
  --jira-project KOI \
  --knowledge-hub koi-garden/README.md \
  --json >"$CLOUD_OUT/rt-a-card-koi-garden-pond-02.json"

log "rt-a: tasks"
rt "$DIR_A" koi-garden project koi-garden tasks upsert \
  --jira KOI-1099 \
  --title "lookup timeout en el estanque" \
  --kind bugfix --state in_progress \
  --json >"$CLOUD_OUT/rt-a-task-koi-1099.json"
# The ticketless half of the fixture: a task keyed by its SDD change alone,
# which is the other upsert key the schema accepts.
rt "$DIR_A" koi-garden project koi-garden tasks upsert \
  --sdd-change split-pond-filesharing \
  --title "split pond filesharing" \
  --kind spike --state open \
  --json >"$CLOUD_OUT/rt-a-task-split-pond-filesharing.json"

log "rt-a: evidence"
# Hashed inside the container, against the file the container actually sees, so
# a regenerated fixture is never registered under a digest that no longer
# describes it.
EVIDENCE_SHA="$(in_container sh -c "sha256sum '/vault/evidence/$EVIDENCE_REL' | cut -d' ' -f1")"
[ -n "$EVIDENCE_SHA" ] || fail "could not hash /vault/evidence/$EVIDENCE_REL inside the container"
log "rt-a: evidence sha256 $EVIDENCE_SHA"
rt "$DIR_A" koi-garden project koi-garden evidence add KOI-1099 \
  --path "$EVIDENCE_REL" \
  --sha256 "$EVIDENCE_SHA" \
  --kind png \
  --proves "la traza muestra el timeout del lookup" \
  --json >"$CLOUD_OUT/rt-a-evidence-koi-1099.json"

log "rt-a: observation"
rt "$DIR_A" koi-garden save \
  "round trip por la nube" \
  "rt-a escribe la tarjeta, la tarea y la evidencia; rt-b las recibe sin tocar el disco de rt-a" \
  --type discovery --project koi-garden --topic koi-workspace/fase-1/cloud-roundtrip >/dev/null

# ─── 4. rt-a enrolls and pushes ──────────────────────────────────────────────

for project in "${PROJECTS[@]}"; do
  log "rt-a: enrolling $project"
  rt "$DIR_A" "$project" cloud enroll "$project" >/dev/null
done

for project in "${PROJECTS[@]}"; do
  log "rt-a: pushing $project"
  rt "$DIR_A" "$project" sync --cloud --project "$project" >"$CLOUD_OUT/rt-a-push-$project.log" 2>&1 \
    || { cat "$CLOUD_OUT/rt-a-push-$project.log"; fail "rt-a could not push $project"; }
  sed 's/^/      | /' "$CLOUD_OUT/rt-a-push-$project.log"
done

# ─── 5. rt-b enrolls and pulls ───────────────────────────────────────────────

for project in "${PROJECTS[@]}"; do
  log "rt-b: enrolling $project"
  rt "$DIR_B" "$project" cloud enroll "$project" >/dev/null
done

for project in "${PROJECTS[@]}"; do
  log "rt-b: pulling $project"
  rt "$DIR_B" "$project" sync --cloud --project "$project" --import >"$CLOUD_OUT/rt-b-pull-$project.log" 2>&1 \
    || { cat "$CLOUD_OUT/rt-b-pull-$project.log"; fail "rt-b could not pull $project"; }
  sed 's/^/      | /' "$CLOUD_OUT/rt-b-pull-$project.log"
done

# ─── 6. field by field ───────────────────────────────────────────────────────

# Every column the card carries except the ones that describe its life on one
# machine (created_at, updated_at, deleted_at) and the three staleness columns,
# which are asserted separately below because they are local by design.
CARD_COLUMNS="slug, sync_id, display_name, repo_url, default_branch, jira_project, jira_component,
  knowledge_hub_path, graph_path, graph_commit, graph_built_at, graph_summary, owner,
  parent_slug, depth, kind, description, icon, color, tags"

TASK_COLUMNS="sync_id, project, jira_key, sdd_change, slug, title, summary, pending_note, vault_path,
  kind, state, jira_status, jira_status_category, branch, pr_url, knowledge_ref, assignee,
  parent_task_sync_id"

# task_sync_id rather than task_id: the local integer differs between replicas
# by construction, the sync id is the pointer that has to survive the trip.
EVIDENCE_COLUMNS="sync_id, project, task_sync_id, path, sha256, category, kind, proves, config_stamp,
  captured_at, attached_jira, attached_confluence_url, size_bytes, manifest_path"

OBSERVATION_COLUMNS="sync_id, type, title, content, project, scope, topic_key"

log "comparing the two stores field by field"
compare_table project_cards "$CARD_COLUMNS" "project_cards WHERE deleted_at IS NULL ORDER BY slug"
compare_table tasks "$TASK_COLUMNS" "tasks WHERE deleted_at IS NULL ORDER BY sync_id"
compare_table evidence "$EVIDENCE_COLUMNS" "evidence WHERE deleted_at IS NULL ORDER BY sync_id"
compare_table observations "$OBSERVATION_COLUMNS" "observations WHERE deleted_at IS NULL ORDER BY sync_id"

# The three staleness columns are a local verdict about a local checkout, so a
# replica that has never seen the repository must hold none of them.
stale_rows="$(sql "$DIR_B" "SELECT count(*) FROM project_cards
  WHERE graph_stale_reason IS NOT NULL OR graph_changed_files IS NOT NULL OR graph_checked_at IS NOT NULL;")"
if [ "$stale_rows" = "0" ]; then
  pass "rt-b holds no staleness verdict (graph_stale_reason, graph_changed_files, graph_checked_at all NULL)"
else
  report_fail "rt-b carries a staleness verdict on $stale_rows card(s); those three columns are local and must not travel"
fi

# ─── 7. the queues both ends keep ────────────────────────────────────────────

pending_a="$(sql "$DIR_A" "SELECT count(*) FROM sync_mutations WHERE acked_at IS NULL;")"
if [ "$pending_a" = "0" ]; then
  pass "rt-a has no unacknowledged mutation left"
else
  report_fail "rt-a still holds $pending_a unacknowledged mutation(s); the push did not drain its outbox"
  sql "$DIR_A" "SELECT seq, entity, entity_key, project FROM sync_mutations WHERE acked_at IS NULL ORDER BY seq;" \
    | awk '{ print "      | " $0 }'
fi

deferred_b="$(sql "$DIR_B" "SELECT count(*) FROM sync_apply_deferred;")"
if [ "$deferred_b" = "0" ]; then
  pass "rt-b parked nothing in sync_apply_deferred"
else
  report_fail "rt-b parked $deferred_b row(s) in sync_apply_deferred; the pull could not apply them in order"
  sql "$DIR_B" "SELECT sync_id, entity, apply_status, retry_count, last_error FROM sync_apply_deferred;" \
    | awk '{ print "      | " $0 }'
fi

# ─── 8. what this rehearsal does not cover ───────────────────────────────────

cat <<'NOTCOVERED'

Deferred to the phase 2 gate — no shipped CLI writes any of these today, so a
round trip over them would exercise a SQL statement this script wrote rather
than the product:
  * project_cards.parent_slug carrying a value (and the depth it implies).
    `engram project <slug> upsert` has no --parent-slug flag, so every card
    here replicates at depth 0 with a NULL parent. The column itself is
    compared above and does travel; what is untested is a non-NULL one.
  * project_aliases. No subcommand writes a row, so the table is empty on both
    ends and its replication is unproven.
  * benchmarks. Same: empty on both ends, replication unproven.
  * kind, description, icon, color and tags likewise have no flag on `upsert`,
    so they replicate at their defaults ('repo', NULL, NULL, NULL, NULL). The
    columns are compared, the non-default values are not.
NOTCOVERED

printf '\ncloud-roundtrip: %d passed, %d failed\nartifacts: %s\n' "$PASS_COUNT" "$FAIL_COUNT" "$CLOUD_OUT"
[ "$FAIL_COUNT" -eq 0 ] || exit 1
