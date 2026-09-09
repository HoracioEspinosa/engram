#!/usr/bin/env bash
#
# container-recovery-rehearsal.sh
#
# Closes the pending half of the projects cloud-replication rehearsal
# (scripts/cloud/projects-roundtrip-rehearsal.sh): recovery from a SECOND
# MACHINE. That script explicitly declares the second physical machine out
# of scope and runs both replicas as two directories on the same host, which
# is also why its `created_by` check could never mean anything — two
# processes on the same host inherit the same $USER.
#
# A Docker container is not a simulation of a second machine: it has its own
# hostname, its own (empty) $USER/$USERNAME, and none of the host's
# ~/.engram, installed binary, or shell configuration. Nothing here is
# inherited from the host by accident — the container only gets what this
# script explicitly hands it over the network (the cloud server URL) and
# through environment variables (the cloud token, a GitHub token to fetch
# the private release).
#
# What this proves, end to end:
#   1. A clean container with no ~/.engram and no engram binary fetches the
#      binary from the fork's GitHub release, verifies its checksum, and
#      configures itself against the cloud server.
#   2. It recovers a project's project card, task, evidence, and observation
#      that exist ONLY on the cloud server (the container never had them).
#   3. The recovered content is compared FIELD BY FIELD against the origin's
#      content, not by row counts.
#   4. The container pushes a new observation of its own, and the resulting
#      sync manifest's `created_by` (cloud_chunks.created_by, populated
#      client-side by internal/sync.GetUsername()) is compared against the
#      origin's. If the two ever coincide, this script reports it as a
#      genuine finding — it does not pass silently.
#   5. The origin machine pulls that new observation back, closing the round
#      trip both ways.
#   6. A negative case: a container with an invalid cloud token must fail to
#      recover anything, and is proven to have recovered nothing — not just
#      to have printed an error.
#
# Explicitly OUT OF SCOPE, even after this passes:
#   - Two distinct network paths/firewalls. The container reaches the cloud
#     server over the same Docker network as the compose stack (or, on
#     Docker Desktop, through the host's loopback) — not over two separate
#     networks the way two physical machines on different sites would.
#   - Two distinct clocks. The container shares the Docker Desktop Linux
#     VM's kernel clock; it does not exercise clock drift or skew between
#     independent machines.
#   - Whether ENGRAM_CLOUD_ALLOWED_PROJECTS or the token pepper survive a
#     real network partition or latency — this rehearsal runs on a healthy,
#     low-latency link only.
#
# Prerequisites on the host:
#   - Docker (tested with 29.7.2) with a working `docker compose`.
#   - `gh`, authenticated with read access to the fork
#     (github.com/HoracioEspinosa/engram is PRIVATE — this is itself a
#     finding worth carrying into the Homebrew tap work: an unauthenticated
#     `curl` against its public release-asset URL 404s. The tap formula
#     needs the same authenticated download strategy, documented in its own
#     task).
#   - `sqlite3`, `curl`, `openssl` (already required by
#     projects-roundtrip-rehearsal.sh, which this script otherwise mirrors).
#
# Usage:
#   bash scripts/cloud/container-recovery-rehearsal.sh
#
# Overrides (env):
#   CONTAINER_REHEARSAL_REPO         GitHub repo to fetch the release from
#                                     (default: HoracioEspinosa/engram)
#   CONTAINER_REHEARSAL_RELEASE_TAG  Release tag to install in the container
#                                     (default: v1.20.0-cd.2)
#
# Exit codes:
#   0: every check passed, including the created_by distinction and the
#      negative case
#   1: at least one check failed — read the PASS/FAIL log for which one
#
set -euo pipefail

# ============================================================================
# Configuration
# ============================================================================

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
ENGRAM_BIN="${REPO_ROOT}/bin/engram"
DOCKER_COMPOSE_FILE="${REPO_ROOT}/docker-compose.cloud-auth.yml"

CLOUD_URL="http://localhost:18081"
CLOUD_HEALTH_TIMEOUT=30

TOKEN_PEPPER="engram-auth-token-pepper-distinct-e2e-9876543210"
ADMIN_USERNAME="container-rehearsal-admin"
ADMIN_EMAIL="admin@test.local"
ADMIN_RAW_TOKEN="egc_admin_container_rehearsal_12345"

PROJECT="container-recovery-rehearsal"
DB_USER="engram"
DB_NAME="engram_cloud_auth"

GH_REPO="${CONTAINER_REHEARSAL_REPO:-HoracioEspinosa/engram}"
RELEASE_TAG="${CONTAINER_REHEARSAL_RELEASE_TAG:-v1.20.0-cd.2}"

export ENGRAM_CLOUD_TOKEN_PEPPER="${TOKEN_PEPPER}"
export ENGRAM_CLOUD_SERVER="${CLOUD_URL}"
export ENGRAM_DATABASE_URL="postgres://engram:engram_auth_dev@127.0.0.1:5434/engram_cloud_auth?sslmode=disable"

PASS_COUNT=0
FAIL_COUNT=0
WORK_DIR=$(mktemp -d)
COMPOSE_UP=0
NETWORK_NAME=""
trap 'cleanup' EXIT

# ============================================================================
# Logging & assertions (same contract as projects-roundtrip-rehearsal.sh)
# ============================================================================

log() { echo "[container-rehearsal] $*" >&2; }
pass() { echo "[container-rehearsal] ✓ $*" >&2; PASS_COUNT=$((PASS_COUNT + 1)); }
fail() { echo "[container-rehearsal] ✗ $*" >&2; FAIL_COUNT=$((FAIL_COUNT + 1)); }

managed_token_hash() {
	local raw_token="$1"
	local domain_sep="engram-cloud-token:v1:"
	local digest
	digest=$(printf '%s%s' "${domain_sep}" "${raw_token}" \
		| openssl dgst -sha256 -mac HMAC -macopt "key:${TOKEN_PEPPER}" -binary \
		| base64 | tr '+/' '-_' | tr -d '=\n')
	echo "hmac-sha256:v1:${digest}"
}

cleanup() {
	log "Cleaning up..."
	if [[ "${COMPOSE_UP}" -eq 1 ]]; then
		docker compose -f "${DOCKER_COMPOSE_FILE}" -f "${WORK_DIR}/compose.override.yml" down -v 2>/dev/null || true
	fi
	rm -rf "${WORK_DIR}" 2>/dev/null || true
	echo "[container-rehearsal] Results: PASS=${PASS_COUNT} FAIL=${FAIL_COUNT}" >&2
	if [[ ${FAIL_COUNT} -gt 0 ]]; then
		echo "[container-rehearsal] FAIL: one or more checks did not hold (exit code: 1)" >&2
		exit 1
	fi
	echo "[container-rehearsal] OK: recovery from a clean container, verified end to end (exit code: 0)" >&2
	exit 0
}

# dump_project_content DB_PATH — deterministic, comparable-by-diff dump of
# every entity the rehearsal seeds. Used identically for the origin replica
# (host sqlite3) and the container replica (container's own sqlite3), so a
# mismatch in the diff is a real content mismatch and not a query mismatch.
dump_project_content() {
	local db="$1"
	{
		echo "== project_card =="
		sqlite3 "${db}" "SELECT slug,display_name,repo_url,default_branch FROM project_cards WHERE slug='${PROJECT}';"
		echo "== task =="
		sqlite3 "${db}" "SELECT jira_key,title,kind,state FROM tasks WHERE project='${PROJECT}';"
		echo "== evidence =="
		sqlite3 "${db}" "SELECT sha256,kind,proves FROM evidence WHERE project='${PROJECT}';"
		echo "== observation (origin-authored) =="
		sqlite3 "${db}" "SELECT title,type,content FROM observations WHERE project='${PROJECT}' AND title='origin-machine-observation';"
	}
}

# ============================================================================
# 0. Preconditions
# ============================================================================

command -v docker > /dev/null 2>&1 || { fail "docker is not available"; exit 1; }
command -v gh > /dev/null 2>&1 || { fail "gh is not available"; exit 1; }
GH_TOKEN="$(gh auth token 2>/dev/null || true)"
if [[ -z "${GH_TOKEN}" ]]; then
	fail "gh is not authenticated (gh auth token returned nothing); cannot fetch the private release"
	exit 1
fi
pass "Preconditions: docker present, gh authenticated"

# ============================================================================
# 1. Resolve the release asset the container will download
#
# github.com/HoracioEspinosa/engram is a PRIVATE repository. Its release
# assets are not reachable by an unauthenticated `curl` against the public
# download URL (that 404s) — every download, including the one the
# container performs below, goes through the authenticated REST API and
# carries the bearer token as a header, never written to a file.
# ============================================================================

log "Resolving release ${RELEASE_TAG} assets for ${GH_REPO}..."

DOCKER_ARCH="$(docker info --format '{{.Architecture}}' 2>/dev/null || true)"
case "${DOCKER_ARCH}" in
	aarch64 | arm64) TARGET_ARCH="arm64" ;;
	x86_64 | amd64) TARGET_ARCH="amd64" ;;
	*)
		fail "unrecognized docker architecture: ${DOCKER_ARCH:-<empty>}"
		exit 1
		;;
esac

VERSION_NO_V="${RELEASE_TAG#v}"
TARBALL_NAME="engram_${VERSION_NO_V}_linux_${TARGET_ARCH}.tar.gz"

TARBALL_ASSET_ID="$(gh api "repos/${GH_REPO}/releases/tags/${RELEASE_TAG}" --jq ".assets[] | select(.name==\"${TARBALL_NAME}\") | .id" 2>/dev/null || true)"
CHECKSUMS_ASSET_ID="$(gh api "repos/${GH_REPO}/releases/tags/${RELEASE_TAG}" --jq '.assets[] | select(.name=="checksums.txt") | .id' 2>/dev/null || true)"

if [[ -z "${TARBALL_ASSET_ID}" ]]; then
	fail "release ${RELEASE_TAG} has no asset named ${TARBALL_NAME}"
	exit 1
fi
if [[ -z "${CHECKSUMS_ASSET_ID}" ]]; then
	fail "release ${RELEASE_TAG} has no checksums.txt asset"
	exit 1
fi
pass "Resolved release assets: ${TARBALL_NAME} (id ${TARBALL_ASSET_ID}), checksums.txt (id ${CHECKSUMS_ASSET_ID})"

# ============================================================================
# 2. Start the cloud server, scoped to this rehearsal's own project name
#
# docker-compose.cloud-auth.yml is shared with
# projects-roundtrip-rehearsal.sh and hardcodes ENGRAM_CLOUD_ALLOWED_PROJECTS
# to test-project. Rather than editing that shared fixture (or colliding
# with its project name), this script layers a compose override that adds
# its own project to the allowlist, in its own throwaway work directory.
# ============================================================================

cat > "${WORK_DIR}/compose.override.yml" <<OVERRIDE_EOF
services:
  cloud:
    environment:
      ENGRAM_CLOUD_ALLOWED_PROJECTS: ${PROJECT}
OVERRIDE_EOF

log "Starting cloud server with authentication..."
if ! docker compose -f "${DOCKER_COMPOSE_FILE}" -f "${WORK_DIR}/compose.override.yml" up -d --build 2>&1; then
	fail "Failed to start docker compose"
	exit 1
fi
COMPOSE_UP=1
pass "Cloud server started"

NETWORK_NAME="$(docker compose -f "${DOCKER_COMPOSE_FILE}" -f "${WORK_DIR}/compose.override.yml" ps --format '{{.Networks}}' cloud 2>/dev/null | head -1)"
if [[ -z "${NETWORK_NAME}" ]]; then
	fail "could not resolve the compose network the cloud service joined"
	exit 1
fi
pass "Cloud service is reachable from other containers on network: ${NETWORK_NAME}"

log "Waiting for cloud server to be ready..."
for i in $(seq 1 ${CLOUD_HEALTH_TIMEOUT}); do
	if curl -s "${CLOUD_URL}/health" > /dev/null 2>&1; then
		pass "Cloud server is ready"
		break
	fi
	sleep 1
	if [[ ${i} -eq ${CLOUD_HEALTH_TIMEOUT} ]]; then
		fail "Cloud server did not become ready after ${CLOUD_HEALTH_TIMEOUT}s"
		exit 1
	fi
done

# ============================================================================
# 3. Bootstrap admin and grant it access to this rehearsal's project
# ============================================================================

log "Bootstrapping admin user..."
if ! bootstrap_output=$("${ENGRAM_BIN}" cloud bootstrap admin \
	--username "${ADMIN_USERNAME}" \
	--email "${ADMIN_EMAIL}" 2>&1); then
	fail "Bootstrap admin command failed: ${bootstrap_output}"
	exit 1
fi
ADMIN_PRINCIPAL_ID=$(echo "${bootstrap_output}" | sed -n 's/.*principal_id=\([^ ]*\).*/\1/p' || true)
if [[ -z "${ADMIN_PRINCIPAL_ID}" ]]; then
	fail "Failed to extract admin principal_id from bootstrap"
	exit 1
fi
pass "Admin user bootstrapped: principal_id=${ADMIN_PRINCIPAL_ID}"

ADMIN_TOKEN_HASH=$(managed_token_hash "${ADMIN_RAW_TOKEN}")
if ! docker exec -i engram-cloud-postgres-auth psql -U "${DB_USER}" -d "${DB_NAME}" > /dev/null 2>&1 <<INSERT_SQL
INSERT INTO cloud_principal_tokens (principal_id, token_prefix, token_hash, name, created_by_principal_id, created_at)
VALUES ('${ADMIN_PRINCIPAL_ID}', 'egc_admin', '${ADMIN_TOKEN_HASH}', 'bootstrap-token', '${ADMIN_PRINCIPAL_ID}', NOW())
ON CONFLICT DO NOTHING;
INSERT INTO cloud_project_grants (principal_id, project, granted_by_principal_id, created_at)
VALUES ('${ADMIN_PRINCIPAL_ID}', '${PROJECT}', '${ADMIN_PRINCIPAL_ID}', NOW())
ON CONFLICT DO NOTHING;
INSERT_SQL
then
	fail "Failed to insert admin token/grant into database"
	exit 1
fi
pass "Admin token and project grant inserted"

# ============================================================================
# 4. Origin machine (this host): seed a project that will exist ONLY on the
#    cloud server as far as the container is concerned.
# ============================================================================

log "Seeding data on the origin machine (this host)..."
ORIGIN_HOME="${WORK_DIR}/origin"
mkdir -p "${ORIGIN_HOME}/.engram"
export ENGRAM_DATA_DIR="${ORIGIN_HOME}/.engram"
export ENGRAM_PROJECTS_SYNC=1
export ENGRAM_CLOUD_TOKEN="${ADMIN_RAW_TOKEN}"
export CD_EVIDENCE_DIR="${WORK_DIR}/evidence-origin"
mkdir -p "${CD_EVIDENCE_DIR}"

"${ENGRAM_BIN}" cloud enroll "${PROJECT}" > /dev/null 2>&1 && pass "Origin enrolled for cloud sync" || { fail "Origin enroll failed"; exit 1; }

"${ENGRAM_BIN}" project "${PROJECT}" upsert \
	--display-name "Container Recovery Rehearsal" \
	--repo-url "https://github.com/test/repo" \
	--default-branch "main" > /dev/null 2>&1 && pass "Project card created on origin" || { fail "Project card creation failed"; exit 1; }

"${ENGRAM_BIN}" project "${PROJECT}" tasks upsert \
	--jira "CONT-1" \
	--title "Container Recovery Task" \
	--kind "feature" \
	--state "open" > /dev/null 2>&1 && pass "Task created on origin" || { fail "Task creation failed"; exit 1; }

echo "Evidence created on the origin machine, before any container existed." > "${CD_EVIDENCE_DIR}/origin-evidence.txt"
TASK_ID=$(sqlite3 "${ENGRAM_DATA_DIR}/engram.db" "SELECT sync_id FROM tasks WHERE project='${PROJECT}' LIMIT 1" 2>/dev/null || true)
if [[ -z "${TASK_ID}" ]]; then
	fail "Could not retrieve task ID for evidence linking"
	exit 1
fi
"${ENGRAM_BIN}" project "${PROJECT}" evidence add "${TASK_ID}" \
	--file "${CD_EVIDENCE_DIR}/origin-evidence.txt" \
	--kind "txt" \
	--proves "container recovery works" > /dev/null 2>&1 && pass "Evidence created on origin" || { fail "Evidence creation failed"; exit 1; }

"${ENGRAM_BIN}" save "origin-machine-observation" \
	"This observation was created on the origin machine, before any container existed." \
	--type discovery --project "${PROJECT}" > /dev/null 2>&1 && pass "Observation created on origin" || { fail "Observation creation failed"; exit 1; }

dump_project_content "${ENGRAM_DATA_DIR}/engram.db" > "${WORK_DIR}/origin-content.txt"
if [[ ! -s "${WORK_DIR}/origin-content.txt" ]]; then
	fail "Origin content dump is empty; nothing to compare against"
	exit 1
fi

log "Pushing origin data to the cloud..."
if ! "${ENGRAM_BIN}" sync --cloud --project "${PROJECT}" > /dev/null 2>&1; then
	fail "Failed to push origin data to the cloud"
	exit 1
fi
pass "Origin data pushed to the cloud"

ORIGIN_CREATED_BY="$(docker exec -i engram-cloud-postgres-auth psql -U "${DB_USER}" -d "${DB_NAME}" -t -A \
	-c "SELECT created_by FROM cloud_chunks WHERE project_name='${PROJECT}' ORDER BY created_at ASC LIMIT 1;" 2>/dev/null | tr -d '[:space:]')"
if [[ -z "${ORIGIN_CREATED_BY}" ]]; then
	fail "Could not read the origin chunk's created_by from cloud_chunks"
	exit 1
fi
pass "Origin manifest created_by = '${ORIGIN_CREATED_BY}'"

# ============================================================================
# 5. Clean container ("the second machine"): fetch the binary, configure
#    against the cloud server, and recover the project that, from its point
#    of view, has never existed anywhere but the server.
# ============================================================================

log "Building the container's recovery script..."
CONTAINER_ENTRYPOINT="${WORK_DIR}/container-entrypoint.sh"
cat > "${CONTAINER_ENTRYPOINT}" <<'CONTAINER_EOF'
#!/bin/sh
set -eu

echo "[container] hostname:  $(hostname)"
echo "[container] USER=[${USER:-}] USERNAME=[${USERNAME:-}]"

apk add --no-cache curl tar sqlite ca-certificates > /tmp/apk.log 2>&1

curl -fsSL -H "Authorization: Bearer ${GH_TOKEN}" -H "Accept: application/octet-stream" \
	"https://api.github.com/repos/${GH_REPO}/releases/assets/${CHECKSUMS_ASSET_ID}" \
	-o /tmp/checksums.txt

curl -fsSL -H "Authorization: Bearer ${GH_TOKEN}" -H "Accept: application/octet-stream" \
	"https://api.github.com/repos/${GH_REPO}/releases/assets/${TARBALL_ASSET_ID}" \
	-o "/tmp/${TARBALL_NAME}"

EXPECTED_SHA=$(awk -v f="${TARBALL_NAME}" '$2==f {print $1}' /tmp/checksums.txt)
ACTUAL_SHA=$(sha256sum "/tmp/${TARBALL_NAME}" | awk '{print $1}')
if [ -z "${EXPECTED_SHA}" ] || [ "${EXPECTED_SHA}" != "${ACTUAL_SHA}" ]; then
	echo "[container] CHECKSUM MISMATCH: expected=[${EXPECTED_SHA}] actual=[${ACTUAL_SHA}]" >&2
	exit 1
fi
echo "[container] checksum verified: ${ACTUAL_SHA}"

tar -xzf "/tmp/${TARBALL_NAME}" -C /tmp engram
mv /tmp/engram /usr/local/bin/engram
chmod +x /usr/local/bin/engram
cp /usr/local/bin/engram /out/engram-binary-verified
echo "[container] binary: $(/usr/local/bin/engram --help 2>&1 | head -1)"

export ENGRAM_DATA_DIR=/root/.engram
export ENGRAM_PROJECTS_SYNC=1

engram cloud enroll "${PROJECT}"
engram sync --cloud --project "${PROJECT}" --import

{
	echo "== project_card =="
	sqlite3 "${ENGRAM_DATA_DIR}/engram.db" "SELECT slug,display_name,repo_url,default_branch FROM project_cards WHERE slug='${PROJECT}';"
	echo "== task =="
	sqlite3 "${ENGRAM_DATA_DIR}/engram.db" "SELECT jira_key,title,kind,state FROM tasks WHERE project='${PROJECT}';"
	echo "== evidence =="
	sqlite3 "${ENGRAM_DATA_DIR}/engram.db" "SELECT sha256,kind,proves FROM evidence WHERE project='${PROJECT}';"
	echo "== observation (origin-authored) =="
	sqlite3 "${ENGRAM_DATA_DIR}/engram.db" "SELECT title,type,content FROM observations WHERE project='${PROJECT}' AND title='origin-machine-observation';"
} > /out/container-content.txt

hostname > /out/container-hostname.txt

# Round trip back: push one new artifact of the container's own, so its
# sync manifest's created_by can be compared against the origin's.
engram save "container-machine-marker" \
	"Written from the clean container replica, to test whether the sync manifest's created_by can tell the two machines apart." \
	--type discovery --project "${PROJECT}"
engram sync --cloud --project "${PROJECT}"
CONTAINER_EOF

log "Running the clean container (no ~/.engram, no binary, no prior config)..."
mkdir -p "${WORK_DIR}/container-out"
if ! docker run --rm \
	--network "${NETWORK_NAME}" \
	--hostname "engram-second-machine" \
	-e GH_TOKEN="${GH_TOKEN}" \
	-e GH_REPO="${GH_REPO}" \
	-e TARBALL_NAME="${TARBALL_NAME}" \
	-e TARBALL_ASSET_ID="${TARBALL_ASSET_ID}" \
	-e CHECKSUMS_ASSET_ID="${CHECKSUMS_ASSET_ID}" \
	-e PROJECT="${PROJECT}" \
	-e ENGRAM_CLOUD_SERVER="http://cloud:18081" \
	-e ENGRAM_CLOUD_TOKEN="${ADMIN_RAW_TOKEN}" \
	-v "${CONTAINER_ENTRYPOINT}:/entrypoint.sh:ro" \
	-v "${WORK_DIR}/container-out:/out" \
	alpine:3.20 sh /entrypoint.sh; then
	fail "Container recovery run failed"
	exit 1
fi
pass "Container fetched the release binary, verified its checksum, and recovered the project"

if [[ ! -f "${WORK_DIR}/container-out/container-content.txt" ]]; then
	fail "Container produced no content dump to compare"
	exit 1
fi

# ============================================================================
# 6. Compare recovered content field by field (not row counts)
# ============================================================================

log "Comparing recovered content against the origin's..."
if diff -u "${WORK_DIR}/origin-content.txt" "${WORK_DIR}/container-out/container-content.txt" > "${WORK_DIR}/content.diff" 2>&1; then
	pass "Recovered content matches the origin's byte for byte (project card, task, evidence, observation)"
else
	fail "Recovered content DIFFERS from the origin's:"
	cat "${WORK_DIR}/content.diff" >&2
fi

# ============================================================================
# 7. Measure whether created_by distinguishes the two replicas
# ============================================================================

CONTAINER_CREATED_BY="$(docker exec -i engram-cloud-postgres-auth psql -U "${DB_USER}" -d "${DB_NAME}" -t -A \
	-c "SELECT created_by FROM cloud_chunks WHERE project_name='${PROJECT}' ORDER BY created_at DESC LIMIT 1;" 2>/dev/null | tr -d '[:space:]')"
CONTAINER_HOSTNAME="$(cat "${WORK_DIR}/container-out/container-hostname.txt" 2>/dev/null || echo '<unknown>')"

log "origin created_by='${ORIGIN_CREATED_BY}'  container created_by='${CONTAINER_CREATED_BY}'  container hostname='${CONTAINER_HOSTNAME}'"
if [[ -z "${CONTAINER_CREATED_BY}" ]]; then
	fail "Could not read the container chunk's created_by from cloud_chunks"
elif [[ "${ORIGIN_CREATED_BY}" == "${CONTAINER_CREATED_BY}" ]]; then
	fail "created_by does NOT distinguish the two replicas even across a container boundary: both resolved to '${ORIGIN_CREATED_BY}'. This is a real finding, not a script defect: GetUsername() (internal/sync/sync.go) falls through \$USER -> \$USERNAME -> hostname, and something in this environment made both sides land on the same value."
else
	pass "created_by DOES distinguish the two replicas across a container boundary: origin='${ORIGIN_CREATED_BY}' (resolved from \$USER on the host) vs container='${CONTAINER_CREATED_BY}' (resolved from the container's own hostname, since \$USER/\$USERNAME are unset there). This only holds because the container never inherits the host's \$USER — two directories on the SAME host, as in projects-roundtrip-rehearsal.sh, would still collide."
fi

# ============================================================================
# 8. Close the round trip: origin pulls the container's new observation back
# ============================================================================

log "Pulling the container's new observation back into the origin..."
export ENGRAM_DATA_DIR="${ORIGIN_HOME}/.engram"
if ! "${ENGRAM_BIN}" sync --cloud --project "${PROJECT}" --import > /dev/null 2>&1; then
	fail "Origin failed to pull the container's mutation back"
else
	ROUNDTRIP_TITLE=$(sqlite3 "${ENGRAM_DATA_DIR}/engram.db" "SELECT title FROM observations WHERE project='${PROJECT}' AND title='container-machine-marker';" 2>/dev/null || true)
	if [[ "${ROUNDTRIP_TITLE}" == "container-machine-marker" ]]; then
		pass "Origin recovered the container's observation: the round trip closes both ways"
	else
		fail "Origin did not recover the container's observation after --import"
	fi
fi

# ============================================================================
# 9. Negative case: an invalid cloud token must fail, and fail to recover
#    anything — not silently succeed with zero rows.
# ============================================================================

log "Running the negative case: a clean container with an invalid cloud token..."
NEGATIVE_LOG="${WORK_DIR}/negative-case.log"
set +e
docker run --rm \
	--network "${NETWORK_NAME}" \
	--hostname "engram-invalid-token-machine" \
	-v "${WORK_DIR}/container-out/engram-binary-verified:/usr/local/bin/engram:ro" \
	-e ENGRAM_CLOUD_SERVER="http://cloud:18081" \
	-e ENGRAM_CLOUD_TOKEN="egc_totally_invalid_token_0000" \
	-e ENGRAM_PROJECTS_SYNC=1 \
	-e ENGRAM_DATA_DIR=/root/.engram \
	-e PROJECT="${PROJECT}" \
	alpine:3.20 sh -c '
		apk add --no-cache sqlite > /dev/null 2>&1
		engram cloud enroll "${PROJECT}" > /dev/null 2>&1
		engram sync --cloud --project "${PROJECT}" --import
		sync_rc=$?
		echo "EXIT_CODE=${sync_rc}"
		echo "ROWS=$(sqlite3 "${ENGRAM_DATA_DIR}/engram.db" "SELECT COUNT(*) FROM project_cards WHERE slug=\"${PROJECT}\";" 2>/dev/null || echo 0)"
		exit "${sync_rc}"
	' > "${NEGATIVE_LOG}" 2>&1
NEGATIVE_RUN_EXIT=$?
set -e

if [[ ${NEGATIVE_RUN_EXIT} -eq 0 ]]; then
	fail "Negative case: recovery with an invalid token unexpectedly succeeded (exit 0)"
elif rg -q '^ROWS=0$' "${NEGATIVE_LOG}" 2>/dev/null || grep -q '^ROWS=0$' "${NEGATIVE_LOG}"; then
	pass "Negative case: an invalid cloud token failed to authenticate AND recovered zero rows (see ${NEGATIVE_LOG} for the exact error)"
else
	fail "Negative case: the run failed but rows were still recovered — that would be worse than a clean rejection:"
	cat "${NEGATIVE_LOG}" >&2
fi
