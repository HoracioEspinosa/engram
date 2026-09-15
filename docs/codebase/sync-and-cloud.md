[← Codebase Guide](../CODEBASE-GUIDE.md) | [← Previous: Interfaces](interfaces.md) | [Next: Dashboard →](dashboard.md)

# Sync and Cloud

**Sync moves local-first memory without changing ownership: local SQLite stays authoritative.** Engram has git-friendly chunk sync and opt-in cloud sync for explicit projects.

## Local sync and cloud sync

Engram has two related but distinct ideas:

1. **Git sync through chunks**: export/import to `.engram/manifest.json` and `.engram/chunks/*.jsonl.gz`.
2. **Opt-in cloud sync**: push/pull against `engram cloud serve` for an explicit project.

```text
Local SQLite
   │
   ├── engram sync
   │     └── .engram/manifest.json + gzip JSONL chunks
   │
   └── engram sync --cloud --project <name>
         └── internal/cloud/remote
                └── HTTP /sync/*
                       └── internal/cloud/cloudserver
                              └── internal/cloud/cloudstore Postgres
```

## Git-friendly chunks: `internal/sync`

`internal/sync/sync.go` avoids one large shared JSON file. Each sync creates new chunks and a small manifest. That reduces merge conflicts and lets multiple machines generate memory in parallel.

Project-scoped chunks carry sessions, observations, prompts, and the non-orphaned `memory_relations` graph for observations in that project. Relation rows travel as existing `relation` sync mutations inside the chunk so imports reuse the same idempotent relation apply path as cloud sync.

Guardrails:

- Do not modify old chunks to “update” them.
- Do not assume every project is exported unless the command says so.
- Keep imported-chunk tracking to avoid duplicates.

## Cloud autosync: `internal/cloud/autosync`

`internal/cloud/autosync/manager.go` runs in long-lived processes and coordinates:

- SQLite lease to avoid duplicate workers,
- pending mutation push,
- cursor-based pull,
- deferred replay,
- backoff with jitter,
- degraded state with `reason_code` and message.

Business rule: **if sync is blocked, fail loudly and visibly**. No silent drops.

## Cloud transport: `internal/cloud/remote` + `internal/cloud/cloudserver`

`internal/cloud/remote/transport.go` is the client. `internal/cloud/cloudserver/cloudserver.go` is the server. The server mounts:

- `GET /health`
- `GET /version`
- `GET /sync/pull` — the **manifest** of a project's chunks (`project` is required); it lists ids and counts, never chunk content
- `GET /sync/pull/{chunkID}` — **one chunk**, by chunk id. The path segment is an id, not a cursor: `/sync/pull/0` is a 404, not "start from the beginning"
- `POST /sync/push`
- `POST /sync/mutations/push`
- `GET /sync/mutations/pull` — the **materialized stream** read from `cloud_mutations`, paged with `since_seq` (not `since`) and `limit`. It is global: a `project` parameter is ignored, and scoping comes from the caller's authorization
- `/dashboard/*`

`POST /sync/push` and `POST /sync/mutations/push` enforce the server-side push request body limit from `ENGRAM_CLOUD_MAX_PUSH_BYTES` (default 8 MiB).

A pushed session requires an `id` and nothing else. `directory` records where a session was opened, and a session saved against an explicit project was never opened in a checkout — the local store writes those on purpose, so the codec, the server and the local backfill all carry them as they are. A push rejection is chunk-wide, so every required-field rule on this path has to match a rule the local schema actually enforces.

For complete route details, use [DOCS.md — HTTP API Endpoints](../../DOCS.md#http-api-endpoints).

### Project names are compared folded

Enrollment normalizes a project name to lower case (`store.NormalizeProject`); the rows keep whatever case they were written with, which is why reads across the store compare `lower(project)` and `core-0003-fn-indexes` / `core-0004-sync-project-fn-indexes` index that expression on observations, sessions, prompts and `sync_mutations`. The whole push path follows the same rule end to end: `ExportProject`'s typed collections, the backfill selects, `ProjectJournalGaps`, the enrolled-projects join on both pending-mutation reads, and `SkipAckNonEnrolledMutations`. An exact-equality filter anywhere on that path is a silent data-loss bug — the export returns no rows for a project named with capitals while the mutation selector still picks them, so the chunk carries mutations citing sessions it does not include and the server rejects it wholesale.

`engram cloud enroll` refuses a name that owns no local row at all (exit 1, `reason_code: enroll_project_has_no_local_rows`). Enrollment is the one place where a typo is indistinguishable from a healthy project that simply has nothing new. A fresh replica enrolling a project in order to pull it is the legitimate version of the same state, and says so with `--allow-empty`.

### A chunk carries every session it cites

The cloud validates each observation and prompt upsert against the sessions the chunk carries plus the ones it has already indexed for that project (`cloud_project_sessions`), and that index is per project. A session can be cited across projects — the manual-save fallback is — so the chunk builder reads the citation from every upsert it carries and loads whatever session the per-project export did not provide. Deletes are left out: the cloud does not resolve their session, and attaching one would turn a mutation-only chunk into a snapshot.

New manual saves get a per-project session id (`manual-save-<slug>`) for the same reason: one shared `manual-save` row cited from many projects is a reference the server's model cannot express.

### A row with no journal entry is not replicated

`pending mutations = 0` means the journal is drained, not that every row reached the cloud. A row with no `sync_mutations` entry at all never enters a push. `Store.ProjectJournalGaps` counts those per enrolled project, folded, split in two:

- **journalable** — a backfill pass can enqueue them. `engram cloud enroll` (every run, not only the first) and `engram cloud upgrade repair --apply` do exactly that, idempotently.
- **blocked** — the cloud upsert contract rejects them (an observation needs `session_id`, `type`, `title`, `content` and `scope`; a prompt needs `session_id` and `content`), so no backfill can deliver them. Only completing or removing the row clears the count.

`engram cloud upgrade doctor` prints `unjournaled_rows` and `unjournaled_detail`, and never answers `ready` while either number is above zero.

## Cloud store: `internal/cloud/cloudstore`

`internal/cloud/cloudstore/cloudstore.go` persists to Postgres, materializes chunks/mutations, and feeds dashboard read models. If an organizational policy matters, state lives here or is enforced from `cloudserver` against data from here.

A chunk carries three entities in typed arrays — `sessions`, `observations`, `prompts` — and everything else only inside `mutations`. `WriteChunk` materializes the typed three from their arrays and every other entity from its mutation, so `relation` and all seven engram-projects entities reach `cloud_mutations`. That table is what `ListMutationsSince` serves, so an entity missing from it is invisible to every pulling replica while the pushing client still acks the chunk — a silent, reason-code-less hole. The predicate is the complement (`hasTypedChunkCollection`) rather than a list of entities to carry, so a newly replicated entity is carried by default.

`WriteChunk` only covers chunks written after that rule existed. Everything an older client pushed still sits in `cloud_chunks` alone, and no client can re-push a chunk the server already holds. `CloudStore.MaterializeChunkMutations` walks the stored chunks of a project and writes the missing entity mutations into `cloud_mutations`; `cloud serve` runs it over every project at start, and `engram cloud repair materialize-chunks [--project <name>] (--dry-run|--apply)` runs it on demand. It is idempotent, keyed on entity, key and op — never on the payload, so a later push carrying a newer payload stays a genuine update instead of being treated as a duplicate.

The two repair commands move data in opposite directions and are not interchangeable: `materialize-mutations` rebuilds `cloud_chunks` from `cloud_mutations` (so the dashboard, which counts chunks, stops reading short); `materialize-chunks` fills `cloud_mutations` from `cloud_chunks` (so the pull stream stops missing entities).

`ENGRAM_CLOUD_ALLOWED_PROJECTS=*` is a wildcard, never a project name: nothing is ever stored, authorized or materialized under `*`. Every consumer of the allowlist asks `cloud.AllowsAllProjects` before it iterates the list — the project authorizer, the dashboard scope, and the startup materialization in `cmd/engram/cloud.go`, which expands the wildcard through `CloudStore.ListMutationProjects`. Iterating the list literally means running per-project work against one project that holds nothing, which is invisible: no error, and only the dashboard's `cloud_chunks` count reads short of `cloud_mutations`.

## engram-projects replication: `internal/store/projects_sync.go`

Five entities travel besides the upstream four: `project_card`, `task`, `evidence`, `task_link` and `observation_ref`. Two rules shape the code.

**Enqueue happens inside the writing transaction.** `enqueueProjectCardTx`, `enqueueTaskTx`, `enqueueEvidenceTx`, `enqueueTaskLinkTx` and `enqueueObservationRefTx` are called from the same `withTx` that writes the row, so a row and the mutation that replicates it are never committed apart. `ENGRAM_PROJECTS_SYNC` gates this half only, and defaults to off: a mutation for an entity the other replicas do not understand would halt their pull. Applying what arrives is never gated.

**Apply is deterministic, not order-sensitive.** `applyProjectsMutationTx` merges each entity under clocks that do not overlap, so two replicas that receive the same mutations in different orders end up byte-identical:

| Entity | Rule |
|---|---|
| `project_card` | Descriptive fields last-writer-wins on `updated_at`; the code-graph group (`graph_commit`, `graph_built_at`, `graph_summary`) only moves forward on `graph_built_at` |
| `task` | Three clocks: `updated_at` for what a person edits, `state_synced_at` for the two fields copied from Jira, and `taskStateClock` for `state`, which both writers touch. A `jira_key` claimed by two tasks stays with the older one; the loser keeps its row under a quarantined key plus a dead `sync_apply_deferred` row |
| `evidence` | Immutable once created, except `attached_jira` (0 → 1 only), the first `attached_confluence_url`, and a monotone `deleted_at` |
| `task_link` | LWW-element-set: an unlink is remembered in `task_link_tombstones` so an older link cannot resurrect the pair; delete wins an exact tie |
| `observation_ref` | Grow-only set, no delete in v1 |

Ties on any clock are broken by the SHA-256 of that group's own fields, never of the whole payload — the local side of a comparison may already carry another group's values merged in from a third replica.

**The wire contract accepts exactly the rows the store accepts.** `internal/cloud/chunkcodec/projects.go` keeps its own copy of the payload structs, so its required-field rules have to be read against the store's CHECK constraints rather than assumed to follow them. A task is identified by `jira_key`, `sdd_change` **or** `slug` — the vault importer writes tasks carrying only the last one. A rejection here aborts the whole chunk, not just the offending mutation, so the error names the entity and the row's own identity (`sync_id`, or the alias for `project_alias`, or the observation for `observation_ref`) instead of only its index inside the chunk.

A projects mutation whose parent row has not arrived is parked in `sync_apply_deferred` and the chunk still succeeds; upstream entities keep their strict behavior. Parked rows are keyed by entity plus payload digest, because several distinct payloads for one task can be in flight and keying them by entity alone loses whichever arrived first.

The push splits each project's batch in two: upstream entities first, engram-projects second. A cloud image that predates these entities rejects the second chunk with `400 unsupported mutation`; the client keeps them pending under `reason_code = unsupported_entity` and the first chunk stays acked.

## Sync/cloud guardrails

- Local SQLite remains the source of truth.
- Cloud sync is project-scoped.
- Push and pull are covered if the sync contract changes.
- Blocks/policies fail loudly with reason code.
- Cloud docs (`docs/engram-cloud/*`, `DOCS.md#cloud-cli-opt-in`, `DOCS.md#cloud-autosync`) stay aligned.

## Sync/cloud change checklist

- [ ] Local SQLite remains the source of truth.
- [ ] Cloud sync is project-scoped.
- [ ] Push and pull are covered if the sync contract changes.
- [ ] Blocks/policies fail loudly with reason code.
- [ ] `internal/cloud/autosync/*_test.go`, `internal/cloud/remote/*_test.go`, `internal/cloud/cloudserver/*_test.go`, or `internal/cloud/cloudstore/*_test.go` cover the affected boundary.
- [ ] Cloud docs (`docs/engram-cloud/*`, `DOCS.md#cloud-cli-opt-in`, `DOCS.md#cloud-autosync`) stay aligned.

---

[← Previous: Interfaces](interfaces.md) | [Next: Dashboard →](dashboard.md)
