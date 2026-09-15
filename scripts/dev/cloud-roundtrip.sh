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
# The hierarchy, the aliases and the benchmarks are written here with the
# shipped CLI — `project set-parent`, `project alias add`, `project bench add` —
# so the round trip over them exercises the product rather than a SQL statement
# this script wrote. A card is also given a non-default kind, description, icon,
# colour and tags, because a column that only ever replicates its default proves
# nothing about whether it replicates.
#
# Three shapes the real database was found in are planted on purpose, because no
# writer produces them any more and each one used to stop the push silently:
#   * a project whose rows carry capitals while its enrolled slug is lower case;
#   * an observation of an enrolled project with no sync_mutations row at all,
#     invisible to the push while the pending counters read zero;
#   * a chunk an older client left in cloud_chunks whose project_card was never
#     materialized into cloud_mutations, so the pull stream never carried it.
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

# koi-garden-pond-01 is the mixed-case half of the rehearsal: its rows are
# written as "Koi-Garden-Pond-01" while enrollment normalizes the slug to lower
# case, which is the shape the real database is in for every project named with
# capitals.
MIXED_CASE_PROJECT="koi-garden-pond-01"
MIXED_CASE_ROW_PROJECT="Koi-Garden-Pond-01"
PROJECTS=(koi-garden koi-garden-pond-02 "$MIXED_CASE_PROJECT")
EVIDENCE_REL="koi-garden/KOI-1099/01-traza.png"

# psql runs a statement against the dev cloud's Postgres. It exists so the
# rehearsal can plant a chunk the way an older client left one — inside
# cloud_chunks with nothing materialized into cloud_mutations — which no client
# can produce any more.
psql() {
  docker exec -i engram-dev-postgres psql -U engram_dev -d engram_dev -tAc "$1"
}

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

# The server is wiped first. rt-a and rt-b are recreated on every run and mint
# fresh sync ids, so a cloud that kept the last run's rows would hand rt-b both
# generations: two cards for one slug, two tasks holding one Jira key, and a
# comparison that fails for a reason that has nothing to do with this run.
log "resetting the cloud database so the rehearsal starts from an empty server"
"${DC[@]}" --profile cloud rm -sf postgres-dev cloud-dev >/dev/null 2>&1 || true
docker volume rm engram-dev-pg >/dev/null 2>&1 || true

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
# Every optional column is given a value that is not its default: a comparison
# over defaults cannot tell replication from two databases agreeing by accident.
rt "$DIR_A" koi-garden-pond-02 project koi-garden-pond-02 upsert \
  --display-name "Koi Garden · Pond 02" \
  --jira-project KOI \
  --knowledge-hub koi-garden/README.md \
  --kind instance \
  --description "El segundo estanque del jardin" \
  --icon pond \
  --color accent \
  --tag instance --tag pond \
  --json >"$CLOUD_OUT/rt-a-card-koi-garden-pond-02.json"

log "rt-a: hierarchy"
# set-parent is the only writer that walks the ancestors, refuses a cycle and
# rewrites the depth of the subtree, so it is also the only honest way to get a
# non-NULL parent_slug into the round trip.
rt "$DIR_A" koi-garden-pond-02 project set-parent koi-garden-pond-02 --to koi-garden \
  --json >"$CLOUD_OUT/rt-a-set-parent.json"

log "rt-a: alias"
rt "$DIR_A" koi-garden project alias add koi_garden --to koi-garden \
  --json >"$CLOUD_OUT/rt-a-alias.json"

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

log "rt-a: slug-only tasks from the vault"
# The third identity the tasks table accepts, and the only one the CLI cannot
# write: a task known by its own slug. The vault holds two folders with no
# ticket in their name — mantenimiento-del-estanque and split-pond-filesharing
# — and the importer is the shipped writer that turns those into tasks. One
# such task is enough to make an unfixed codec refuse the chunk, which aborts
# the push of the card, the observations and the evidence along with it.
rt "$DIR_A" koi-garden project koi-garden import-vault /vault \
  --project koi-garden --apply \
  --json >"$CLOUD_OUT/rt-a-import-vault.json"

log "rt-a: a session with no directory"
# A session saved against an explicit project was never opened in a checkout,
# so it has no directory to record. Written straight to SQLite because no CLI
# flag produces one: the point of the rehearsal is that the push carries such a
# row, not how it came to exist. The next store open backfills its sync
# mutation, which is the path the real database took.
in_container sqlite3 "$DIR_A/engram.db" \
  "INSERT INTO sessions (id, project, directory, started_at)
   VALUES ('manual-save-koi-garden-nodir', 'koi-garden', '', datetime('now'));"

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

log "rt-a: benchmark"
rt "$DIR_A" koi-garden project koi-garden bench add KOI-1099 \
  --name lookup \
  --metric lookup.p95 \
  --unit ms \
  --value 1512 \
  --baseline \
  --json >"$CLOUD_OUT/rt-a-bench-koi-1099.json"

log "rt-a: observation"
rt "$DIR_A" koi-garden save \
  "round trip por la nube" \
  "rt-a escribe la tarjeta, la tarea y la evidencia; rt-b las recibe sin tocar el disco de rt-a" \
  --type discovery --project koi-garden --topic koi-workspace/fase-1/cloud-roundtrip >/dev/null

log "rt-a: a project whose rows carry capitals"
# Written straight to SQLite because every writer normalizes: the rows of a
# project named with capitals predate that normalization, and the export used
# to compare the column exactly, so the typed collections of the chunk came out
# empty and the server rejected it for citing a session it was never sent.
in_container sqlite3 "$DIR_A/engram.db" \
  "INSERT INTO sessions (id, project, directory, started_at)
   VALUES ('manual-save-$MIXED_CASE_PROJECT', '$MIXED_CASE_ROW_PROJECT', '/work/pond-01', datetime('now'));
   INSERT INTO observations (sync_id, session_id, type, title, content, project, scope, topic_key)
   VALUES ('obs-mixed-case', 'manual-save-$MIXED_CASE_PROJECT', 'discovery',
           'la fila guarda mayusculas', 'el slug inscrito esta en minusculas',
           '$MIXED_CASE_ROW_PROJECT', 'project', 'koi-workspace/fase-1/mixed-case');"

# ─── 4. rt-a enrolls and pushes ──────────────────────────────────────────────

for project in "${PROJECTS[@]}"; do
  log "rt-a: enrolling $project"
  rt "$DIR_A" "$project" cloud enroll "$project" >/dev/null
done

log "rt-a: an observation written with no journal row"
# The shape the real database was found in: 167 observations of enrolled
# projects with no sync_mutations row at all, invisible to the push while the
# pending counters read zero. Written after enrollment so the row is a genuine
# gap rather than something the first enrollment swept up.
in_container sqlite3 "$DIR_A/engram.db" \
  "INSERT INTO observations (sync_id, session_id, type, title, content, project, scope, topic_key)
   VALUES ('obs-unjournaled', 'manual-save-koi-garden-nodir', 'discovery',
           'fila sin journal', 'nunca se encolo una mutacion para esta fila',
           'koi-garden', 'project', 'koi-workspace/fase-1/sin-journal');"

log "rt-a: re-enrolling koi-garden to journal what was written outside the journal"
rt "$DIR_A" koi-garden cloud enroll koi-garden >/dev/null
rt "$DIR_A" koi-garden cloud upgrade doctor --project koi-garden >"$CLOUD_OUT/rt-a-doctor-koi-garden.txt" 2>&1 || true
doctor_gap="$(awk -F': ' '/^unjournaled_rows/ { print $2 }' "$CLOUD_OUT/rt-a-doctor-koi-garden.txt")"
if [ "${doctor_gap:-missing}" = "0" ]; then
  pass "engram cloud upgrade doctor reports no unjournaled rows for koi-garden"
else
  report_fail "engram cloud upgrade doctor reports unjournaled_rows=${doctor_gap:-<missing>} for koi-garden"
  sed 's/^/      | /' "$CLOUD_OUT/rt-a-doctor-koi-garden.txt"
fi

for project in "${PROJECTS[@]}"; do
  log "rt-a: pushing $project"
  rt "$DIR_A" "$project" sync --cloud --project "$project" >"$CLOUD_OUT/rt-a-push-$project.log" 2>&1 \
    || { cat "$CLOUD_OUT/rt-a-push-$project.log"; fail "rt-a could not push $project"; }
  sed 's/^/      | /' "$CLOUD_OUT/rt-a-push-$project.log"
done

# ─── 5. rt-b enrolls and pulls ───────────────────────────────────────────────

for project in "${PROJECTS[@]}"; do
  log "rt-b: enrolling $project"
  # rt-b is the machine that has never seen any of this, which is exactly what
  # --allow-empty declares: enrolling a project in order to pull it, rather
  # than a name that matches nothing because it was mistyped.
  rt "$DIR_B" "$project" cloud enroll "$project" --allow-empty >/dev/null
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

# project is folded here and nowhere else: the chunk codec canonicalizes the
# project of every row it carries, so an observation whose local column holds
# capitals arrives at the replica under the normalized slug. That is the
# contract, not a loss — the two spellings are the same project — and the
# original casing on rt-a is asserted on its own below.
OBSERVATION_COLUMNS="sync_id, type, title, content, lower(ifnull(project, '')), scope, topic_key"

ALIAS_COLUMNS="alias, sync_id, slug, source"

# baseline_set_at is excluded: the flag is what travels, and the moment it was
# raised is stamped by whichever machine raised it.
BENCHMARK_COLUMNS="sync_id, project, task_sync_id, name, metric, unit, direction, value, baseline,
  run_path, sha256, config_stamp, captured_at, notes, source"

log "comparing the two stores field by field"
compare_table project_cards "$CARD_COLUMNS" "project_cards WHERE deleted_at IS NULL ORDER BY slug"
compare_table tasks "$TASK_COLUMNS" "tasks WHERE deleted_at IS NULL ORDER BY sync_id"
compare_table evidence "$EVIDENCE_COLUMNS" "evidence WHERE deleted_at IS NULL ORDER BY sync_id"
compare_table observations "$OBSERVATION_COLUMNS" "observations WHERE deleted_at IS NULL ORDER BY sync_id"
compare_table project_aliases "$ALIAS_COLUMNS" "project_aliases WHERE deleted_at IS NULL ORDER BY alias"
compare_table benchmarks "$BENCHMARK_COLUMNS" "benchmarks WHERE deleted_at IS NULL ORDER BY sync_id"

# ─── 6b. the values, not only the agreement ──────────────────────────────────
#
# compare_table proves the two stores say the same thing. These prove that what
# they say is what rt-a wrote: a round trip that dropped the hierarchy on both
# sides would agree perfectly and be worthless.

expect_value() {
  local label="$1" want="$2" got="$3"
  if [ "$got" = "$want" ]; then
    pass "$label = $got"
  else
    report_fail "$label = ${got:-<empty>}, expected $want"
  fi
}

pond_row="$(sql "$DIR_B" "SELECT parent_slug || '|' || depth || '|' || kind || '|' || icon || '|' || color || '|' || tags
  FROM project_cards WHERE slug = 'koi-garden-pond-02';")"
expect_value "rt-b koi-garden-pond-02 hierarchy and metadata" \
  'koi-garden|1|instance|pond|accent|["instance","pond"]' "$pond_row"

pond_description="$(sql "$DIR_B" "SELECT description FROM project_cards WHERE slug = 'koi-garden-pond-02';")"
expect_value "rt-b koi-garden-pond-02 description" "El segundo estanque del jardin" "$pond_description"

alias_rows="$(sql "$DIR_B" "SELECT count(*) FROM project_aliases WHERE deleted_at IS NULL;")"
expect_value "rt-b project_aliases rows" "1" "$alias_rows"
alias_row="$(sql "$DIR_B" "SELECT alias || ' -> ' || slug FROM project_aliases WHERE deleted_at IS NULL;")"
expect_value "rt-b alias" "koi_garden -> koi-garden" "$alias_row"

# Scoped to the benchmark this script wrote by hand: the vault import brings
# its own, and counting all of them would measure the fixture rather than the
# round trip.
bench_rows="$(sql "$DIR_B" "SELECT count(*) FROM benchmarks
  WHERE deleted_at IS NULL AND name = 'lookup' AND metric = 'lookup.p95';")"
expect_value "rt-b benchmarks rows for lookup.p95" "1" "$bench_rows"
bench_row="$(sql "$DIR_B" "SELECT metric || '|' || unit || '|' || direction || '|' || baseline
  FROM benchmarks WHERE deleted_at IS NULL AND name = 'lookup' AND metric = 'lookup.p95';")"
expect_value "rt-b benchmark" 'lookup.p95|ms|lower|1' "$bench_row"

# A task whose only identity is a slug has to arrive with that slug intact and
# without a key invented for it on the way.
slug_only_count="$(sql "$DIR_B" "SELECT count(*) FROM tasks
  WHERE deleted_at IS NULL AND jira_key IS NULL AND sdd_change IS NULL AND slug IS NOT NULL;")"
if [ "${slug_only_count:-0}" -ge 1 ]; then
  pass "rt-b holds $slug_only_count slug-only task(s)"
else
  report_fail "rt-b holds no slug-only task; the vault import did not survive the round trip"
fi

nodir_session="$(sql "$DIR_B" "SELECT id || '|' || ifnull(directory, '<NULL>')
  FROM sessions WHERE id = 'manual-save-koi-garden-nodir';")"
expect_value "rt-b session with no directory" "manual-save-koi-garden-nodir|" "$nodir_session"

# The push used to drop this row entirely: the export compared the project
# column exactly, so the typed collections came out empty and the server
# rejected the chunk for citing a session it was never sent.
mixed_case_obs="$(sql "$DIR_B" "SELECT ifnull(project, '<NULL>') FROM observations
  WHERE sync_id = 'obs-mixed-case' AND deleted_at IS NULL;")"
expect_value "rt-b observation of a project whose rows carry capitals" "$MIXED_CASE_PROJECT" "$mixed_case_obs"

# And the push does not rewrite what it reads: rt-a keeps the casing it was
# written with.
mixed_case_local="$(sql "$DIR_A" "SELECT ifnull(project, '<NULL>') FROM observations
  WHERE sync_id = 'obs-mixed-case' AND deleted_at IS NULL;")"
expect_value "rt-a keeps the original casing of its own row" "$MIXED_CASE_ROW_PROJECT" "$mixed_case_local"

unjournaled_obs="$(sql "$DIR_B" "SELECT ifnull(title, '<NULL>') FROM observations
  WHERE sync_id = 'obs-unjournaled' AND deleted_at IS NULL;")"
expect_value "rt-b observation that had no journal row on rt-a" "fila sin journal" "$unjournaled_obs"

# The chunk is not the only way a replica reads the server: autosync pulls from
# cloud_mutations through /sync/mutations/pull. An entity that reaches
# cloud_chunks and not cloud_mutations is invisible on that path, and the client
# acks the push with nothing to report.
curl -fsS --max-time 10 -H "Authorization: Bearer $CLOUD_TOKEN" \
  "http://127.0.0.1:28081/sync/mutations/pull?since_seq=0&limit=100" \
  >"$CLOUD_OUT/mutations-pull.json" \
  || report_fail "could not read /sync/mutations/pull"
if [ -s "$CLOUD_OUT/mutations-pull.json" ]; then
  for entity in project_card task evidence project_alias benchmark; do
    count="$(jq --arg e "$entity" '[.mutations[]? | select(.entity == $e)] | length' "$CLOUD_OUT/mutations-pull.json")"
    if [ "${count:-0}" -ge 1 ]; then
      pass "/sync/mutations/pull serves $count $entity mutation(s)"
    else
      report_fail "/sync/mutations/pull serves no $entity mutation; a pulling replica would never see one"
    fi
  done
fi

# The three staleness columns are a local verdict about a local checkout, so a
# replica that has never seen the repository must hold none of them.
stale_rows="$(sql "$DIR_B" "SELECT count(*) FROM project_cards
  WHERE graph_stale_reason IS NOT NULL OR graph_changed_files IS NOT NULL OR graph_checked_at IS NOT NULL;")"
if [ "$stale_rows" = "0" ]; then
  pass "rt-b holds no staleness verdict (graph_stale_reason, graph_changed_files, graph_checked_at all NULL)"
else
  report_fail "rt-b carries a staleness verdict on $stale_rows card(s); those three columns are local and must not travel"
fi

# ─── 6c. a chunk an older client left behind ─────────────────────────────────
#
# An entity without a typed collection travels only as a mutation inside the
# chunk, and a push materializes it into cloud_mutations as it writes. Chunks
# written before that behavior existed still hold theirs and nothing else does,
# so the pull stream never carried them. No client can produce such a chunk any
# more, so it is planted directly and the server is restarted over it.

STALE_CHUNK_CARD="card-left-inside-an-older-chunk"
log "planting a chunk whose project_card was never materialized"
psql "INSERT INTO cloud_chunks (project_name, chunk_id, created_by, payload, sessions_count, observations_count, prompts_count)
      VALUES ('koi-garden', 'legacy-unmaterialized-chunk', 'legacy-push',
        '{\"sessions\":[],\"observations\":[],\"prompts\":[],\"mutations\":[{\"entity\":\"project_card\",\"entity_key\":\"$STALE_CHUNK_CARD\",\"op\":\"upsert\",\"project\":\"koi-garden\",\"payload\":\"{\\\"slug\\\":\\\"koi-garden\\\",\\\"sync_id\\\":\\\"$STALE_CHUNK_CARD\\\"}\"}]}'::jsonb,
        0, 0, 0)
      ON CONFLICT (project_name, chunk_id) DO NOTHING;" >/dev/null

stream_before="$(psql "SELECT count(*) FROM cloud_mutations WHERE entity_key = '$STALE_CHUNK_CARD';" | tr -d ' ')"
if [ "${stream_before:-0}" = "0" ]; then
  pass "the planted chunk starts out invisible to the mutation stream"
else
  report_fail "the planted chunk was already materialized before the restart (rows=$stream_before)"
fi

log "restarting the cloud server so its start-up pass drains the chunk"
"${DC[@]}" --profile cloud restart cloud-dev >/dev/null
waited=0
while :; do
  if curl -fsS --max-time 3 "$CLOUD_HEALTH_URL" >"$CLOUD_OUT/health-after-restart.json" 2>/dev/null; then
    break
  fi
  waited=$((waited + 1))
  [ "$waited" -lt "$CLOUD_BOOT_TIMEOUT" ] || fail "the cloud server did not come back within ${CLOUD_BOOT_TIMEOUT}s"
  sleep 1
done

stream_after="$(psql "SELECT count(*) FROM cloud_mutations WHERE entity_key = '$STALE_CHUNK_CARD';" | tr -d ' ')"
if [ "${stream_after:-0}" = "1" ]; then
  pass "the server start materialized the project_card the older chunk held"
else
  report_fail "the project_card stuck inside the older chunk is still missing from cloud_mutations (rows=${stream_after:-0})"
fi

# And it is a start-up pass, not a one-shot migration: a second restart must
# not duplicate the row.
log "restarting once more to prove the pass is idempotent"
"${DC[@]}" --profile cloud restart cloud-dev >/dev/null
waited=0
while :; do
  if curl -fsS --max-time 3 "$CLOUD_HEALTH_URL" >/dev/null 2>&1; then
    break
  fi
  waited=$((waited + 1))
  [ "$waited" -lt "$CLOUD_BOOT_TIMEOUT" ] || fail "the cloud server did not come back within ${CLOUD_BOOT_TIMEOUT}s"
  sleep 1
done
stream_twice="$(psql "SELECT count(*) FROM cloud_mutations WHERE entity_key = '$STALE_CHUNK_CARD';" | tr -d ' ')"
if [ "${stream_twice:-0}" = "1" ]; then
  pass "a second start left the materialized mutation alone"
else
  report_fail "a second start changed the materialized mutation count to ${stream_twice:-0}"
fi

# ─── 6d. a chunk whose typed collection does not cover its mutations ─────────
#
# A chunk's two halves come from different places: the typed collections from a
# timestamp window over the local tables, the mutation array from the journal.
# A session whose row predates the window travels only as a mutation, so the
# collection is not a superset of the array. Deduplicating the array by entity
# treated it as one and dropped exactly those rows — the client acked them and
# the pull stream never carried them. Both halves are rehearsed: ingestion on a
# live push, and the start-up pass on a chunk planted the way an older client
# left one.

GAP_SESSIONS="'gap-session-in-window','gap-session-before-window','gap-session-older-still'"

log "pushing a chunk whose sessions array covers one of its three session mutations"
cat >"$CLOUD_OUT/typed-gap-chunk.json" <<'JSON'
{
  "created_by": "roundtrip-typed-gap",
  "client_created_at": "2026-04-29T10:03:00Z",
  "data": {
    "sessions": [
      {"id":"gap-session-in-window","project":"koi-garden","directory":"/data/rt-a","started_at":"2026-04-29T10:00:00Z"}
    ],
    "observations": [],
    "prompts": [],
    "mutations": [
      {"entity":"session","entity_key":"gap-session-in-window","op":"upsert","project":"koi-garden","payload":"{\"id\":\"gap-session-in-window\",\"directory\":\"/data/rt-a\",\"started_at\":\"2026-04-29T10:00:00Z\"}"},
      {"entity":"session","entity_key":"gap-session-before-window","op":"upsert","project":"koi-garden","payload":"{\"id\":\"gap-session-before-window\",\"directory\":\"/data/rt-a\",\"started_at\":\"2026-03-01T10:00:00Z\"}"},
      {"entity":"session","entity_key":"gap-session-older-still","op":"upsert","project":"koi-garden","payload":"{\"id\":\"gap-session-older-still\",\"directory\":\"/data/rt-a\",\"started_at\":\"2026-02-01T10:00:00Z\"}"}
    ]
  }
}
JSON

push_status="$(curl -sS -o "$CLOUD_OUT/typed-gap-push.json" -w '%{http_code}' \
  -X POST \
  -H "Authorization: Bearer $CLOUD_TOKEN" \
  -H 'Content-Type: application/json' \
  --data-binary @"$CLOUD_OUT/typed-gap-chunk.json" \
  "http://127.0.0.1:28081/sync/push?project=koi-garden" 2>>"$LOG_FILE")"
if [ "$push_status" = "200" ]; then
  pass "the cloud server accepted the chunk (HTTP 200)"
else
  report_fail "the cloud server answered $push_status on the push; expected 200"
  awk '{ print "      | " $0 }' "$CLOUD_OUT/typed-gap-push.json"
fi

pushed_rows="$(psql "SELECT count(*) FROM cloud_mutations WHERE entity = 'session' AND entity_key IN ($GAP_SESSIONS);" | tr -d ' ')"
if [ "${pushed_rows:-0}" = "3" ]; then
  pass "all three session mutations reached cloud_mutations, not only the one the sessions array carried"
else
  report_fail "the push materialized ${pushed_rows:-0} of its 3 session mutations; the rest are acked and unreachable"
  psql "SELECT entity_key FROM cloud_mutations WHERE entity = 'session' AND entity_key IN ($GAP_SESSIONS) ORDER BY entity_key;" \
    | awk '{ print "      | " $0 }'
fi

# The repair half. An older server materialized the typed session and dropped
# the other two, so the planted chunk is paired with the one row that ingestion
# did write at the time.
LEGACY_GAP_SESSIONS="'legacy-gap-session-typed','legacy-gap-session-a','legacy-gap-session-b'"

log "planting a chunk an older ingestion would have half-materialized"
docker exec -i engram-dev-postgres psql -U engram_dev -d engram_dev -tA >/dev/null <<'SQL'
INSERT INTO cloud_chunks (project_name, chunk_id, created_by, payload, sessions_count, observations_count, prompts_count)
VALUES ('koi-garden', 'legacy-typed-gap-chunk', 'legacy-push', $chunk${
  "sessions": [{"id":"legacy-gap-session-typed","project":"koi-garden","directory":"/data/rt-a","started_at":"2026-04-29T10:00:00Z"}],
  "observations": [],
  "prompts": [],
  "mutations": [
    {"entity":"session","entity_key":"legacy-gap-session-typed","op":"upsert","project":"koi-garden","payload":"{\"id\":\"legacy-gap-session-typed\"}"},
    {"entity":"session","entity_key":"legacy-gap-session-a","op":"upsert","project":"koi-garden","payload":"{\"id\":\"legacy-gap-session-a\"}"},
    {"entity":"session","entity_key":"legacy-gap-session-b","op":"upsert","project":"koi-garden","payload":"{\"id\":\"legacy-gap-session-b\"}"}
  ]
}$chunk$::jsonb, 1, 0, 0)
ON CONFLICT (project_name, chunk_id) DO NOTHING;

INSERT INTO cloud_mutations (project, entity, entity_key, op, payload)
VALUES ('koi-garden', 'session', 'legacy-gap-session-typed', 'upsert', $row${"id":"legacy-gap-session-typed"}$row$::jsonb);
SQL

legacy_before="$(psql "SELECT count(*) FROM cloud_mutations WHERE entity = 'session' AND entity_key IN ($LEGACY_GAP_SESSIONS);" | tr -d ' ')"
if [ "${legacy_before:-0}" = "1" ]; then
  pass "the planted chunk starts with only the session its typed collection carried"
else
  report_fail "the planted chunk did not start in the half-materialized state (rows=${legacy_before:-0})"
fi

log "restarting the cloud server so its start-up pass recovers the two dropped sessions"
"${DC[@]}" --profile cloud restart cloud-dev >/dev/null
waited=0
while :; do
  if curl -fsS --max-time 3 "$CLOUD_HEALTH_URL" >/dev/null 2>&1; then
    break
  fi
  waited=$((waited + 1))
  [ "$waited" -lt "$CLOUD_BOOT_TIMEOUT" ] || fail "the cloud server did not come back within ${CLOUD_BOOT_TIMEOUT}s"
  sleep 1
done

legacy_after="$(psql "SELECT count(*) FROM cloud_mutations WHERE entity = 'session' AND entity_key IN ($LEGACY_GAP_SESSIONS);" | tr -d ' ')"
if [ "${legacy_after:-0}" = "3" ]; then
  pass "the server start recovered the two sessions the older chunk left out"
else
  report_fail "the sessions stuck inside the older chunk are still missing from cloud_mutations (rows=${legacy_after:-0})"
fi

# The pass says what it recovered per entity, so an operator reading the log
# knows which rule was dropping rows rather than only how many.
"${DC[@]}" --profile cloud logs --no-color cloud-dev >"$CLOUD_OUT/cloud-dev-start.log" 2>&1 || true
if grep -q 'materialize-chunks: project=koi-garden .*by_entity=session=2' "$CLOUD_OUT/cloud-dev-start.log"; then
  pass "the start-up pass logged the recovery broken down per entity"
else
  report_fail "the start-up pass did not log by_entity=session=2 for koi-garden"
  grep 'materialize-chunks' "$CLOUD_OUT/cloud-dev-start.log" | awk '{ print "      | " $0 }'
fi

log "restarting once more to prove the recovery is idempotent"
"${DC[@]}" --profile cloud restart cloud-dev >/dev/null
waited=0
while :; do
  if curl -fsS --max-time 3 "$CLOUD_HEALTH_URL" >/dev/null 2>&1; then
    break
  fi
  waited=$((waited + 1))
  [ "$waited" -lt "$CLOUD_BOOT_TIMEOUT" ] || fail "the cloud server did not come back within ${CLOUD_BOOT_TIMEOUT}s"
  sleep 1
done
legacy_twice="$(psql "SELECT count(*) FROM cloud_mutations WHERE entity = 'session' AND entity_key IN ($LEGACY_GAP_SESSIONS);" | tr -d ' ')"
if [ "${legacy_twice:-0}" = "3" ]; then
  pass "a second start left the recovered sessions alone"
else
  report_fail "a second start changed the recovered session count to ${legacy_twice:-0}"
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

Still outside this rehearsal:
  * A conflict. Both replicas are written by one machine, so last-write-wins is
    never asked to choose between two edits of the same row.
  * A subtree deeper than one level, and a reparenting that arrives before the
    card it points at. sync_apply_deferred is asserted empty above, which is
    what would catch the second; neither is provoked on purpose.
  * The three staleness columns beyond "they do not travel", asserted above.
NOTCOVERED

printf '\ncloud-roundtrip: %d passed, %d failed\nartifacts: %s\n' "$PASS_COUNT" "$FAIL_COUNT" "$CLOUD_OUT"
[ "$FAIL_COUNT" -eq 0 ] || exit 1
