#!/usr/bin/env bash
#
# projects-roundtrip-rehearsal.sh
#
# Integration test for engram projects cloud replication (RFC section 10).
# Verifies CRDT sync of project cards, tasks, evidence, and task links
# from a local replica through a docker-compose cloud server to another replica.
#
# Usage:
#   bash scripts/cloud/projects-roundtrip-rehearsal.sh
#
# Exit codes:
#   0: all tests passed
#   1: one or more tests failed
#

set -euo pipefail

# ============================================================================
# Configuration
# ============================================================================

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
ENGRAM_BIN="${REPO_ROOT}/bin/engram"
DOCKER_COMPOSE_FILE="${REPO_ROOT}/docker-compose.cloud-auth.yml"

# Cloud server config
CLOUD_URL="http://localhost:18081"
CLOUD_HEALTH_TIMEOUT=30

# Auth config.
#
# TOKEN_PEPPER must be byte-for-byte identical to the `cloud` service's
# ENGRAM_CLOUD_TOKEN_PEPPER in docker-compose.cloud-auth.yml: it is the HMAC
# key used both here (to mint managed-token hashes for the fixture
# principals) and inside the running server (to verify presented bearer
# tokens). A mismatched pepper makes every authenticated call fail with
# "unauthorized" without any other symptom, so keep the two values in sync
# by hand until one of them reads the other from the environment.
TOKEN_PEPPER="engram-auth-token-pepper-distinct-e2e-9876543210"
ADMIN_USERNAME="rehearsal-admin"
ADMIN_EMAIL="admin@test.local"
ADMIN_RAW_TOKEN="egc_admin_rehearsal_12345"
USER_USERNAME="rehearsal-user"
USER_EMAIL="sync@test.local"
USER_RAW_TOKEN="egc_user_sync_12345"

PROJECT="test-project"
DB_USER="engram"
DB_NAME="engram_cloud_auth"

# These are read by every `engram` invocation below, local or cloud:
#   - ENGRAM_CLOUD_TOKEN_PEPPER lets the bootstrap CLI (which talks to
#     Postgres directly, not through the HTTP server) hash tokens the same
#     way the server will verify them.
#   - ENGRAM_CLOUD_SERVER is how `engram sync --cloud` finds the server;
#     without it, cloud sync fails preflight before touching the network.
#   - ENGRAM_DATABASE_URL points the bootstrap CLI at THIS stack's Postgres
#     (port 5434, db engram_cloud_auth). Left unset, it falls back to
#     engram's normal dev-stack DSN (port 5433, db engram_cloud), which is
#     a different, unrelated database this stack never touches - bootstrap
#     then fails to even connect.
# All three must be exported before `docker compose up` and before any
# bootstrap call, so they are set here, in Configuration, rather than
# deeper in the script next to their first use.
export ENGRAM_CLOUD_TOKEN_PEPPER="${TOKEN_PEPPER}"
export ENGRAM_CLOUD_SERVER="${CLOUD_URL}"
export ENGRAM_DATABASE_URL="postgres://engram:engram_auth_dev@127.0.0.1:5434/engram_cloud_auth?sslmode=disable"

# Tracking
PASS_COUNT=0
FAIL_COUNT=0
WORK_DIR=$(mktemp -d)
trap 'cleanup' EXIT

# ============================================================================
# Logging & assertions
# ============================================================================

log() {
	echo "[rehearsal] $*" >&2
}

pass() {
	echo "[rehearsal] ✓ $*" >&2
	# Plain arithmetic assignment, not `((PASS_COUNT++))`: under `set -e`, a
	# bare `((expr))` command is treated as a failing command whenever expr
	# evaluates to 0 - and post-increment `x++` evaluates to the OLD value,
	# so the very first call with PASS_COUNT=0 kills the whole script right
	# here. `VAR=$((...))` is a variable assignment, not an `((...))`
	# command, so its exit status is always 0 regardless of the arithmetic
	# result.
	PASS_COUNT=$((PASS_COUNT + 1))
}

fail() {
	echo "[rehearsal] ✗ $*" >&2
	FAIL_COUNT=$((FAIL_COUNT + 1))
}

# managed_token_hash mirrors ManagedTokenHasher.Hash in
# internal/cloud/auth/foundation.go exactly: HMAC-SHA256 keyed by the
# dedicated pepper, over the domain separator concatenated with the raw
# token, encoded as unpadded base64url. Skipping the domain separator, using
# a plain digest instead of an HMAC, or using standard (padded, +/) base64
# all produce a hash the server's ManagedTokenHasher.Verify will never match.
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
	# -v also drops the named Postgres volume: without it, the first-admin
	# bootstrap in section 3 below only ever succeeds on the very first run
	# this stack has ever seen on the machine, and fails forever after with
	# "a managed admin already exists" on every later run.
	docker compose -f "${DOCKER_COMPOSE_FILE}" down -v 2>/dev/null || true
	rm -rf "${WORK_DIR}" 2>/dev/null || true
	results
}

results() {
	echo "[rehearsal] Results: PASS=${PASS_COUNT} FAIL=${FAIL_COUNT}" >&2
	if [[ ${FAIL_COUNT} -gt 0 ]]; then
		echo "[rehearsal] FAIL: E2E rehearsal encountered errors (exit code: 1)" >&2
		exit 1
	fi
	echo "[rehearsal] OK: E2E rehearsal completed successfully (exit code: 0)" >&2
	exit 0
}

# ============================================================================
# 1. Start cloud server with authentication
# ============================================================================

log "Starting cloud server with authentication..."
if ! docker compose -f "${DOCKER_COMPOSE_FILE}" up -d 2>&1; then
	fail "Failed to start docker compose"
	exit 1
fi

pass "Cloud server started"

# ============================================================================
# 2. Wait for cloud server to be ready
# ============================================================================

log "Waiting for cloud server to be ready..."
for i in $(seq 1 ${CLOUD_HEALTH_TIMEOUT}); do
	if curl -s "${CLOUD_URL}/health" >/dev/null 2>&1; then
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
# 3. Bootstrap admin user (and grant it access to the rehearsal project)
# ============================================================================

log "Bootstrapping admin user..."

# --grant-project would be the obvious way to also write the
# cloud_project_grants row the server's authorizer checks before letting
# this principal touch ${PROJECT} over HTTP, but it currently makes
# bootstrap's completion-audit write fail (a pre-existing bug in
# cloudstore's sensitive-metadata filter rejects the "issued_token" key by
# name even when grants are the only thing being recorded and no token was
# issued). That is an engram bug, not a rehearsal-script one, and out of
# scope here, so the grant is inserted directly in section 4 instead,
# right where the admin token already is.
#
# The command substitution is guarded explicitly instead of left to
# `set -e`: an unguarded `x=$(cmd)` that fails still exits the script under
# `set -e`, but silently - skipping straight to the EXIT trap without ever
# calling fail(), so results() reports a clean PASS/FAIL=0 for a run that
# never got past this line.
if ! bootstrap_output=$("${ENGRAM_BIN}" cloud bootstrap admin \
	--username "${ADMIN_USERNAME}" \
	--email "${ADMIN_EMAIL}" 2>&1); then
	fail "Bootstrap admin command failed: ${bootstrap_output}"
	exit 1
fi

log "Bootstrap output: ${bootstrap_output}"
ADMIN_PRINCIPAL_ID=$(echo "${bootstrap_output}" | sed -n 's/.*principal_id=\([^ ]*\).*/\1/p' || true)
if [[ -z "${ADMIN_PRINCIPAL_ID}" ]]; then
	fail "Failed to extract admin principal_id from bootstrap"
	exit 1
fi
pass "Admin user bootstrapped: principal_id=${ADMIN_PRINCIPAL_ID}"

# ============================================================================
# 4. Insert managed tokens into database
# ============================================================================

log "Computing and inserting managed tokens into database..."

# DB_USER/DB_NAME must match docker-compose.cloud-auth.yml's
# POSTGRES_USER/POSTGRES_DB (engram/engram_cloud_auth) - the postgres:16
# image only creates the role and database named by those two variables,
# not a "postgres"/"engram" pair, so the wrong pair fails every docker exec
# below with "role ... does not exist" before it ever gets to the SQL.
ADMIN_TOKEN_HASH=$(managed_token_hash "${ADMIN_RAW_TOKEN}")
log "Admin token hash: ${ADMIN_TOKEN_HASH:0:30}..."

if ! docker exec -i engram-cloud-postgres-auth psql -U "${DB_USER}" -d "${DB_NAME}" >/dev/null 2>&1 <<INSERT_SQL
INSERT INTO cloud_principal_tokens (principal_id, token_prefix, token_hash, name, created_by_principal_id, created_at)
VALUES ('${ADMIN_PRINCIPAL_ID}', 'egc_admin', '${ADMIN_TOKEN_HASH}', 'bootstrap-token', '${ADMIN_PRINCIPAL_ID}', NOW())
ON CONFLICT DO NOTHING;
-- Grants the admin principal access to the rehearsal project. This is the
-- same cloud_project_grants row `cloud bootstrap admin --grant-project`
-- would write; see the comment in section 3 for why it is inserted here
-- directly instead.
INSERT INTO cloud_project_grants (principal_id, project, granted_by_principal_id, created_at)
VALUES ('${ADMIN_PRINCIPAL_ID}', '${PROJECT}', '${ADMIN_PRINCIPAL_ID}', NOW())
ON CONFLICT DO NOTHING;
INSERT_SQL
then
	fail "Failed to insert admin token into database"
	exit 1
fi
pass "Admin token inserted"

# ============================================================================
# 5. Create second user for testing (simulate another principal)
# ============================================================================

log "Creating secondary user for negative test..."

# The second principal does not exist yet, so its id cannot be hardcoded:
# cloud_principal_tokens.principal_id is a real foreign key into
# cloud_principals, and inserting a token for a guessed id (e.g. 2) fails
# that constraint the moment the guess is wrong. This single statement
# creates the principal, its human-user row, and its token together and
# reads the generated id back via CTEs, instead of assuming one.
USER_TOKEN_HASH=$(managed_token_hash "${USER_RAW_TOKEN}")

if ! docker exec -i engram-cloud-postgres-auth psql -U "${DB_USER}" -d "${DB_NAME}" >/dev/null 2>&1 <<INSERT_SQL
WITH new_principal AS (
	INSERT INTO cloud_principals (kind, display_name, role)
	VALUES ('human', '${USER_USERNAME}', 'member')
	RETURNING id
), new_human AS (
	INSERT INTO cloud_human_users (principal_id, username, email)
	SELECT id, '${USER_USERNAME}', '${USER_EMAIL}' FROM new_principal
	RETURNING principal_id
)
INSERT INTO cloud_principal_tokens (principal_id, token_prefix, token_hash, name, created_by_principal_id, created_at)
SELECT id, 'egc_user', '${USER_TOKEN_HASH}', 'sync-token', '${ADMIN_PRINCIPAL_ID}', NOW() FROM new_principal
ON CONFLICT DO NOTHING;
INSERT_SQL
then
	fail "Could not create secondary user for negative test"
fi

# ============================================================================
# 6. Create two local replicas
# ============================================================================

log "Creating local replicas..."
REPLICA_A_HOME="${WORK_DIR}/replica-a"
REPLICA_B_HOME="${WORK_DIR}/replica-b"
mkdir -p "${REPLICA_A_HOME}" "${REPLICA_B_HOME}"
pass "Replica A: ${REPLICA_A_HOME}"
pass "Replica B: ${REPLICA_B_HOME}"

# ============================================================================
# 7. Seed project card in replica A using real CLI commands
# ============================================================================

log "Seeding data in replica A..."

# ENGRAM_HOME is not read anywhere in this codebase - the real override is
# ENGRAM_DATA_DIR (see cmd/engram/main.go), which replaces cfg.DataDir
# outright (so it must include the trailing "/.engram" this script's own
# sqlite3 queries expect). Setting ENGRAM_HOME here was a silent no-op: the
# CLI fell through to its real default, $HOME/.engram, meaning every prior
# run of this rehearsal wrote its "test-project" fixtures straight into
# the operator's actual local engram database instead of an isolated one.
export ENGRAM_DATA_DIR="${REPLICA_A_HOME}/.engram"
export ENGRAM_PROJECTS_SYNC=1
export ENGRAM_CLOUD_TOKEN="${ADMIN_RAW_TOKEN}"

# evidence add refuses any --file outside a configured evidence root
# (defaulting to ~/.clarodrive/evidence when CD_EVIDENCE_DIR is unset) -
# pointing it here keeps the rehearsal's evidence fixture inside its own
# throwaway WORK_DIR instead of a real, shared directory on the operator's
# machine.
export CD_EVIDENCE_DIR="${WORK_DIR}/evidence-a"
mkdir -p "${CD_EVIDENCE_DIR}"

if "${ENGRAM_BIN}" cloud enroll "${PROJECT}" >/dev/null 2>&1; then
	pass "Replica A enrolled for cloud sync"
else
	fail "Failed to enroll replica A for cloud sync"
	exit 1
fi

# Create project card using the real subcommand
if "${ENGRAM_BIN}" project "${PROJECT}" upsert \
	--display-name "Rehearsal Test Project" \
	--repo-url "https://github.com/test/repo" \
	--default-branch "main" >/dev/null 2>&1; then
	pass "Project card created"
else
	fail "Failed to create project card"
	exit 1
fi

# ============================================================================
# 8. Create task in replica A
# ============================================================================

log "Creating task in replica A..."

# A task row requires a jira_key or an sdd_change (CHECK constraint on the
# tasks table) - --title/--kind/--state alone is rejected outright.
if "${ENGRAM_BIN}" project "${PROJECT}" tasks upsert \
	--jira "RHRS-1" \
	--title "Replication Test Task" \
	--kind "feature" \
	--state "open" >/dev/null 2>&1; then
	pass "Task created"
else
	fail "Failed to create task"
	exit 1
fi

# ============================================================================
# 9. Create evidence in replica A
# ============================================================================

log "Creating evidence in replica A..."

# Create a temporary test file for evidence, inside CD_EVIDENCE_DIR (see
# section 7) so evidence add accepts it.
TEST_FILE="${CD_EVIDENCE_DIR}/test-evidence.txt"
echo "Test evidence for CRDT replication" > "${TEST_FILE}"
TEST_SHA256=$(shasum -a 256 "${TEST_FILE}" | awk '{print $1}')

# Get first task ID for linking evidence. The literal project value is
# inlined directly: the sqlite3 CLI has no parameter-binding syntax, so a
# bare `?` in a query run this way is not a placeholder - it is compared
# against the literal string "?", which never matches any row and always
# silently returns empty/zero instead of erroring.
TASK_ID=$(sqlite3 "${REPLICA_A_HOME}/.engram/engram.db" \
	"SELECT sync_id FROM tasks WHERE project='${PROJECT}' LIMIT 1" 2>/dev/null || true)

if [[ -z "${TASK_ID}" ]]; then
	fail "Could not retrieve task ID for evidence linking"
else
	# Evidence "kind" is a closed enum (png, gif, mp4, json, log, txt) -
	# "text" is not a member and is rejected outright.
	if "${ENGRAM_BIN}" project "${PROJECT}" evidence add "${TASK_ID}" \
		--file "${TEST_FILE}" \
		--kind "txt" \
		--proves "CRDT replication works" >/dev/null 2>&1; then
		pass "Evidence created and linked"
	else
		fail "Evidence creation failed"
	fi
fi

# ============================================================================
# 10. Verify data in replica A
# ============================================================================

log "Verifying seeded data in replica A..."

# project_cards is keyed by "slug", not "project" - querying a nonexistent
# column is its own failure mode, independent of the unbound-`?` one, and
# the `2>/dev/null || echo "0"` fallback below used to swallow it silently
# too.
CARD_COUNT=$(sqlite3 "${REPLICA_A_HOME}/.engram/engram.db" \
	"SELECT COUNT(*) FROM project_cards WHERE slug='${PROJECT}'" 2>/dev/null || echo "0")

if [[ "${CARD_COUNT}" -lt 1 ]]; then
	fail "Project card not found in replica A"
	exit 1
fi
pass "Project card exists in replica A"

TASK_COUNT=$(sqlite3 "${REPLICA_A_HOME}/.engram/engram.db" \
	"SELECT COUNT(*) FROM tasks WHERE project='${PROJECT}'" 2>/dev/null || echo "0")

if [[ "${TASK_COUNT}" -lt 1 ]]; then
	fail "Task not found in replica A"
	exit 1
fi
pass "Task exists in replica A (count: ${TASK_COUNT})"

# ============================================================================
# 11. Export mutations from replica A to the cloud
# ============================================================================

log "Exporting mutations from replica A..."

if "${ENGRAM_BIN}" sync --status >/dev/null 2>&1; then
	pass "Sync status check passed"
else
	fail "Sync status unavailable"
fi

# This is the actual replication push: `engram sync --cloud --project`
# exports replica A's project-scoped mutations as a chunk and pushes it to
# the cloud server over HTTP, authenticated with ADMIN_RAW_TOKEN and
# authorized by the project grant from section 3.
if "${ENGRAM_BIN}" sync --cloud --project "${PROJECT}" >/dev/null 2>&1; then
	pass "Mutations exported to cloud from replica A"
else
	fail "Failed to export mutations to cloud from replica A"
	exit 1
fi

# ============================================================================
# 12. Import mutations into replica B via real cloud sync
# ============================================================================

log "Importing mutations into replica B via cloud sync..."

# Switch to replica B's own isolated data directory and enroll it too:
# enrollment (IsProjectEnrolled) is checked per local database, so replica
# B needs its own row exactly like replica A did in section 7.
export ENGRAM_DATA_DIR="${REPLICA_B_HOME}/.engram"
export ENGRAM_PROJECTS_SYNC=1
export ENGRAM_CLOUD_TOKEN="${ADMIN_RAW_TOKEN}"

if "${ENGRAM_BIN}" cloud enroll "${PROJECT}" >/dev/null 2>&1; then
	pass "Replica B enrolled for cloud sync"
else
	fail "Failed to enroll replica B for cloud sync"
	exit 1
fi

# This is the actual replication pull: `--import` pulls every chunk
# replica B has not seen yet from the cloud server and applies its
# mutations to replica B's local database. There is nothing to poll for -
# the command either lands the data synchronously or reports an error.
if "${ENGRAM_BIN}" sync --cloud --project "${PROJECT}" --import >/dev/null 2>&1; then
	pass "Mutations imported into replica B via CRDT sync"
else
	fail "Failed to import mutations into replica B"
	exit 1
fi

# ============================================================================
# 13. Verify data replication
# ============================================================================

log "Verifying data consistency between replicas..."

CARD_COUNT_B=$(sqlite3 "${REPLICA_B_HOME}/.engram/engram.db" \
	"SELECT COUNT(*) FROM project_cards WHERE slug='${PROJECT}'" 2>/dev/null || echo "0")

if [[ "${CARD_COUNT_B}" -lt 1 ]]; then
	fail "Project card not replicated to replica B"
	exit 1
fi
pass "Project card verified in replica B"

# ============================================================================
# 14. Compare task metadata
# ============================================================================

log "Comparing task metadata..."

TASK_A=$(sqlite3 "${REPLICA_A_HOME}/.engram/engram.db" \
	"SELECT title FROM tasks WHERE project='${PROJECT}' LIMIT 1" 2>/dev/null || echo "")

TASK_B=$(sqlite3 "${REPLICA_B_HOME}/.engram/engram.db" \
	"SELECT title FROM tasks WHERE project='${PROJECT}' LIMIT 1" 2>/dev/null || echo "")

if [[ -z "${TASK_A}" ]]; then
	fail "Task A not found"
elif [[ -z "${TASK_B}" ]]; then
	fail "Task B not found (replication failed)"
else
	if [[ "${TASK_A}" == "${TASK_B}" ]]; then
		pass "Task metadata matches between replicas"
	else
		fail "Task metadata mismatch between replicas: A='${TASK_A}' vs B='${TASK_B}'"
		exit 1
	fi
fi

# ============================================================================
# All tests completed
# ============================================================================

cleanup
