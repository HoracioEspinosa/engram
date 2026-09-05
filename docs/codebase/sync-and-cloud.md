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
- `GET /sync/pull`
- `GET /sync/pull/{chunkID}`
- `POST /sync/push`
- `POST /sync/mutations/push`
- `GET /sync/mutations/pull`
- `/dashboard/*`

`POST /sync/push` and `POST /sync/mutations/push` enforce the server-side push request body limit from `ENGRAM_CLOUD_MAX_PUSH_BYTES` (default 8 MiB).

For complete route details, use [DOCS.md — HTTP API Endpoints](../../DOCS.md#http-api-endpoints).

## Cloud store: `internal/cloud/cloudstore`

`internal/cloud/cloudstore/cloudstore.go` persists to Postgres, materializes chunks/mutations, and feeds dashboard read models. If an organizational policy matters, state lives here or is enforced from `cloudserver` against data from here.

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
