# `engram project` — the engram-projects CLI

`engram project <slug> …` is the terminal surface of engram-projects: project cards, tasks, evidence, the runbook index, the code-graph pointer, and the per-task context pack. Every subcommand calls the same store functions the `projects` MCP profile calls, so what an agent sees through `mem_task_list` and what you see in a shell can never disagree.

Two commands, one letter apart, do different things:

| Command | Scope |
| --- | --- |
| `engram projects …` (plural) | Project **names**: `list`, `consolidate`, `prune` |
| `engram project …` (singular) | One project's **engram-projects data**: card, tasks, evidence, runbooks, graph, context |

## Which project a command acts on

`<slug>` is optional. When it is omitted, the project is resolved by precedence:

1. the explicit `<slug>` positional argument,
2. `ENGRAM_PROJECT`,
3. the project detected from the current directory (the same 5-case detection every other engram command uses).

```bash
engram project nextcloud card      # explicit
ENGRAM_PROJECT=nextcloud engram project card
cd ~/Projects/clarodrive && engram project card
```

The first argument is read as a subcommand when it is one (`card`, `upsert`, `graph`, `tasks`, `evidence`, `runbooks`, `context`), and as a slug otherwise. A project whose slug collides with a subcommand name has to be passed through `ENGRAM_PROJECT`.

Read subcommands refuse a project the store has never seen. Subcommands that can create a row — `upsert` and `tasks upsert` — additionally refuse an explicit slug that is neither backed by an existing card or observations nor equal to what `ENGRAM_PROJECT`/cwd resolve to, so a typo cannot silently open a new project.

## Output contract

Every subcommand prints aligned columns by default and accepts `--json`.

`--json` prints the same envelope the MCP tools return:

```json
{
  "project": "nextcloud",
  "project_source": "explicit_override",
  "project_path": "",
  "result": { "…": "…" }
}
```

Errors under `--json` print `{"error": "...", "code": "...", …}` on stdout and exit non-zero, so a script can branch on `code` instead of parsing prose. Without `--json` the message goes to stderr.

One deliberate difference from the MCP tools: `card --graph-summary --json` emits `graph_summary` as a nested JSON object rather than the JSON string the column stores, so it can be walked directly:

```bash
engram project nextcloud card --graph-summary --json \
  | jq '.result.card.graph_commit, .result.card.graph_summary.god_nodes[0:3]'
```

### Error codes

| Code | Meaning |
| --- | --- |
| `ambiguous_project` | No slug given and the current directory does not resolve to one |
| `unknown_project` | The slug is not backed by a card or observations |
| `invalid_slug` | The slug is not `[a-z0-9][a-z0-9-]{0,63}`, or is reserved (`migrate`, `current`) |
| `no_card` | The project has no `project_cards` row yet — run `engram project <slug> upsert` |
| `missing_field` | A required flag or positional argument is absent |
| `invalid_enum` | A flag value is outside its enumeration |
| `unknown_task` | The task reference does not resolve in this project |
| `unknown_observation` | The observation id or sync id does not exist |
| `cross_project_link` | The observation and the task belong to different projects |
| `graph_commit_required` | `--graph-ref` was given without `--graph-commit` |
| `graph_not_found` | No `graph.json` under the repository directory |
| `graph_missing_commit` | `graph.json` carries no `built_at_commit`; nothing was persisted |
| `task_key_conflict` | The Jira key already belongs to a task in another project |
| `invalid_sha256` | `--sha256` is not 64 lowercase hex characters |
| `invalid_file` | `--file` is missing, is a directory, or sits outside the evidence directory |
| `absolute_path_rejected` | `--path` must be relative to the evidence directory |
| `vault_dir_not_found` | `--vault-dir` has no `Runbooks/` folder |
| `entries_rejected` | No indexable runbook entry survived parsing and filtering |

## Subcommands

### `card`

Prints the project's dashboard card: pointers, counters and cloud-sync status.

```
$ engram project nextcloud card
project:   nextcloud (Nextcloud server + apps amx_*)
repo:      git@bitbucket.org:example/clarodrive.git (branch master)
jira:      CDBS
knowledge: -
graph:     graphify-out/graph.json @ 7a79ef43 (built 2026-09-04 19:22:51)
owner:     dev-nextcloud
updated:   2026-09-05 16:13:25
counts:    0 obs · 0 pinned · 1/1 tasks active · 1 evidence (0 unattached) · 3 runbooks (2 stale)
sync:      enrolled no · lifecycle disabled · last acked seq 0
```

| Flag | Effect |
| --- | --- |
| `--graph-summary` | Also render the stored graph summary (node/edge/community counts and top god nodes) |
| `--json` | JSON envelope |

### `upsert`

Creates or updates the project card. It is idempotent: a flag you do not pass is left untouched, so a second `upsert --owner X` never clears `display_name`.

```bash
engram project nextcloud upsert \
  --display-name "Nextcloud server + apps amx_*" \
  --repo-url "git@bitbucket.org:example/clarodrive.git" \
  --jira-project CDBS --owner dev-nextcloud
```

Flags: `--display-name`, `--repo-url`, `--default-branch`, `--jira-project`, `--jira-component`, `--knowledge-hub`, `--owner`, `--graph-path`, `--json`.

The code graph is not touched here; that is `graph sync`.

### `graph sync`

Reads `graph.json` (and `GRAPH_REPORT.md` when present) and stamps `graph_commit`, `graph_built_at` and `graph_summary` onto the card. The graph itself is never copied — only the pointer and a bounded summary.

```
$ engram project nextcloud graph sync --repo-dir ~/Projects/clarodrive
graph:   graphify-out/graph.json
commit:  7a79ef43a9575b8f2bfd7c0034811c7516ea33fc (HEAD matches)
summary: 922 nodes · 900 edges · 89 communities
stamped project_cards.nextcloud (graph_commit, graph_built_at, graph_summary)
```

| Flag | Effect |
| --- | --- |
| `--repo-dir <dir>` | Repository root holding `graphify-out/` (default: the detected project path, else cwd) |
| `--graph-path <rel>` | Repo-relative path of `graph.json` (default: the card's `graph_path`) |
| `--json` | JSON envelope |

When `graph.json` has no `built_at_commit`, nothing is written: a summary is never stored without the commit it was computed from.

### `tasks list`

```
$ engram project nextcloud tasks list --state active --stale-after 24h
ID  KEY         KIND      STATE        JIRA STATUS  SYNCED  STALE  OBS  EVD  TITLE
1   CDBS-10336  incident  in_progress  -            -       yes    0    1    Previews return 503 on object-store files

1 of 1 task(s) · offset 0
```

| Flag | Effect |
| --- | --- |
| `--state` | `active` (default — every state except `done` and `cancelled`) or one exact state |
| `--kind` | `feature`, `bugfix`, `refactor`, `incident`, `migration`, `spike` |
| `--jira` | Exact Jira key |
| `--q` | FTS5 query over title, Jira key, SDD change and branch |
| `--limit` / `--offset` | Paging (limit 1-100, default 20) |
| `--stale-after` | Jira-mirror freshness window: `24h`, `90m`, or a bare number of hours |
| `--json` | JSON envelope |

`STALE` says the Jira mirror has not been refreshed within the window — not that the task itself is stale.

### `tasks upsert`

Creates or updates a task. The upsert key precedence is `--sync-id` → `--jira` → (project, `--sdd-change`) → new row; at least one of the three is required, and `--title` and `--kind` are required when creating.

```bash
engram project nextcloud tasks upsert \
  --jira CDBS-10336 --title "Previews return 503 on object-store files" \
  --kind incident --state in_progress --branch fix/CDBS-10336
```

Flags: `--sync-id`, `--jira`, `--sdd-change`, `--title`, `--kind`, `--state`, `--jira-status`, `--jira-status-category`, `--branch`, `--pr`, `--knowledge-ref`, `--assignee`, `--json`.

### `tasks link <task>`

Links an existing observation to a task and optionally records external references. This is the only way to stamp graph facts and `knowledge_ref` without going through `mem_save`.

```bash
engram project nextcloud tasks link CDBS-10336 \
  --observation 1423 --role root_cause \
  --graph-ref 'OCP\IConfig' --graph-commit 7a79ef43a9575b8f2bfd7c0034811c7516ea33fc
```

`<task>` accepts a Jira key, a `task-<hex>` sync id, `#42` for a local id, or `change:<sdd_change>`. `--observation` accepts a numeric id or an `obs-<hex>` sync id. `--graph-ref` requires `--graph-commit`: a structural claim without the commit it was read at is not recorded.

Flags: `--observation` (required), `--role`, `--knowledge-ref`, `--graph-ref`, `--graph-commit`, `--runbook`, `--jira-ref`, `--json`.

### `evidence add <task>`

Registers an already-captured file. Idempotent by `(task, sha256)`.

```
$ engram project nextcloud evidence add CDBS-10336 \
    --file ~/.clarodrive/evidence/nextcloud/CDBS-10336/01-preview-200.png \
    --kind png --proves "GET /core/preview returns 200 after the fix" \
    --config-stamp "imaginary -concurrency 200; objectstore=gcs" --attached-jira
evidence #1 evd-c605d22b1bcd1a57 · sha256 89761071f42e… · 40 B · limits ok · attached to Jira
path:    nextcloud/CDBS-10336/01-preview-200.png
proves:  GET /core/preview returns 200 after the fix
```

`--file` hashes the capture and derives `--path` and `--size-bytes` from it, so you never run `shasum` by hand. The file must live under the evidence directory — `$CD_EVIDENCE_DIR`, or `~/.clarodrive/evidence` when that is unset — because the stored path is always relative to it. Absolute `--path` values are rejected.

Flags: `--file` or (`--path` + `--sha256`), `--kind` (required), `--proves` (required), `--config-stamp`, `--captured-at`, `--size-bytes`, `--manifest`, `--attached-jira`, `--confluence-url`, `--json`.

Per-file and per-task size limits are reported, never enforced: `limits ok` or `limits exceeded: …` tells the caller whether to fall back to a Confluence link.

### `evidence list [<task>]`

```
$ engram project nextcloud evidence list
ID  TASK        KIND  SIZE  JIRA  CAPTURED             PATH                                     PROVES
1   CDBS-10336  png   40 B  yes   2026-09-05 16:13:31  nextcloud/CDBS-10336/01-preview-200.png  GET /core/preview returns 200 after the fix

1 of 1 file(s) · 40 B total
```

Flags: `--attached-jira` (use `--attached-jira=false` to list what is still unattached), `--kind`, `--limit`, `--offset`, `--json`.

### `runbooks sync`

Rebuilds the runbook index for the project. Exactly one source is required.

```
$ engram project nextcloud runbooks sync --vault-dir ~/vault/clarodrive
scanned 18 runbook document(s) · upserted 3 · unchanged 0 · skipped 15 (not_runbook x3, other_project x5, template x6, unknown_service x1) · stale 4 · exec recomputed 3
```

| Flag | Effect |
| --- | --- |
| `--vault-dir <dir>` | Walk `<dir>/Runbooks/**/*.md` and read each note's YAML header. Needs no MCP server. Source `vault-fs`, where a note older than 90 days is stale |
| `--entries-file <file>` | Read entries from JSON — either a bare array or an object with an `entries` key, so a `mem_runbook_index_sync` payload can be replayed verbatim. Source `knowledge-mcp`, where `needs_review` drives staleness |
| `--prune-missing` | Delete index rows of this project that the entries no longer contain |
| `--json` | JSON envelope |

The vault is only ever read; the derived index lives in the local SQLite store.

`skipped` explains every rejected note with a stable reason: `not_runbook`, `template`, `unknown_service`, `invalid_id`, `invalid_status`, `missing_service`, and `other_project` for notes that belong to a different service — this command is project-scoped, so a vault holding many services indexes only the one being synced.

### `runbooks find <symptom>`

Ranks candidate runbooks by BM25 over the index.

```
$ engram project nextcloud runbooks find "preview 503 object store"
RB-003  Preview endpoint slow, returning 503, or serving incorrect sizes     performance     verified  STALE(113d)  exec 0  rank -0.49
        Runbooks/Performance/RB-003 Preview Endpoint Slow Or Failing.md
RB-002  Public preview returns 503 for files mounted from a federated share  data-integrity  verified  STALE(115d)  exec 0  rank -0.00
        Runbooks/RB-002 Public Preview Crashes On Federated Storage File.md

2 of 2 runbook(s)
```

Flags: `--category`, `--pattern`, `--include-stale` (default true; `--include-stale=false` hides stale ones), `--match-mode` (`any` default, or `all`), `--limit` (1-20, default 5), `--json`.

### `context <task>`

Composes the task's context pack — card, pointers, pinned decisions, linked observations, evidence, candidate runbooks and refs — budgeted to a character count, so a new session starts from what previous ones already found.

```bash
engram project nextcloud context CDBS-10336 --max-chars 6000 --copy
```

| Flag | Effect |
| --- | --- |
| `--max-chars` | Budget, 2000-40000 (default 12000) |
| `--format` | `markdown` (default) or `json` |
| `--json` | Shorthand for `--format json` |
| `--sections` | Comma-separated subset of `header,card,pointers,pinned,observations,evidence,runbooks,refs,footer` |
| `--observations-limit` | Linked observations to include, 1-30 (default 8) |
| `--observation-chars` | Characters per observation, 200-4000 (default 600) |
| `--include-runbooks` | `--include-runbooks=false` omits the runbook section |
| `--repo-dir` | Repository root; enables the graph-vs-HEAD staleness check |
| `--copy` | Also copy the pack to the system clipboard via OSC 52 |

`--copy` writes the escape sequence to the controlling terminal, never to stdout, so `engram project nextcloud context CDBS-10336 --copy > pack.md` both saves a clean file and fills the clipboard. The confirmation line goes to stderr for the same reason.

### `promote list` / `promote stamp`

The engram half of the engram -> vault promotion bridge: a pinned observation
becomes a curated document in the knowledge vault through a pull request.

engram never writes to the vault. `promote list` says which observations are
eligible; the knowledge repository's bridge renders the candidate documents and
opens the pull request; `promote stamp` records the merged document back on the
observation. `mem_task_link` can already stamp a `knowledge_ref`, but only on an
observation linked to a task, and a pinned decision often has no ticket at all.

```
$ engram project nextcloud promote list
pinned inspected: 12 · candidates: 3 · already promoted: 8 · type-excluded: 1
allowlist: decision, discovery
ID   SYNC_ID          TYPE       CREATED     JIRA         TITLE
417  obs-d1e4e54bff…  discovery  2026-08-31  -            Unique .part files avoid write collisions
402  obs-15e9889557…  decision   2026-08-24  CDBS-10336   Coalesce preview requests by fileId
```

The counters are the point. An empty candidate list has four distinct causes —
nothing pinned, nothing pinned of an allowed type, everything eligible already
promoted, or an eligible observation that cannot carry the stamp — and a bridge
that reports "0 candidates" without naming the cause reads as success in all
four. `pinned_inspected` is how many units the scan actually judged; zero of
them is never a pass.

| Flag (`promote list`) | Effect |
| --- | --- |
| `--types` | Comma-separated subset of the allowlist. It can only narrow: naming a type outside `decision,discovery` is `type_not_promotable`, never a widening |
| `--limit` | Caps the candidate list without falsifying the counters |
| `--json` | The full scan, counters included |

```bash
engram project nextcloud promote stamp obs-15e9889557f2d845 \
  --knowledge-ref "Services/Nextcloud/Previews.md"
```

`--knowledge-ref` goes through the same shape rule as everywhere else (RFC
§9.1/§9.2), so a pasted `[[Work/Claro drive/…]]` wikilink is accepted and a
pointer into `90 - Engram/` is refused. Re-running over an already merged batch
is a success that reports `stamped: false`; a *different* second reference is
`knowledge_ref_conflict`, because the export path keeps the earliest one and
would silently ignore the newcomer.

Order matters: the stamp belongs **after** the pull request is merged. Stamped
earlier it points at a document no checkout has, which is exactly what
`engram doctor --check knowledge_ref_dangling` reports as a defect.

| Flag (`promote stamp`) | Effect |
| --- | --- |
| `--knowledge-ref` | Required. Vault-relative path of the merged document |
| `--allow-unpinned` | Repair case: the observation was unpinned after the document merged |
| `--allow-any-type` | Repair case: give an existing document its backlink even when the observation's type would not have started a promotion |

## Relationship to the other surfaces

| Surface | Use it when | Subcommands |
| --- | --- | --- |
| CLI `engram project …` | Shell, hooks, scripts, and anything that wants aligned columns or `--json` | All (including `promote list` / `promote stamp`) |
| MCP `--tools=projects` | An agent inside a session | `card`, `upsert`, `graph sync`, `tasks …`, `evidence …`, `runbooks …`, `context` |
| HTTP `/projects/{slug}/…` | Another service reading the same data | `card`, `upsert`, `graph sync`, `tasks …`, `evidence …`, `runbooks …`, `context` |

All three go through the same functions in `internal/store` and `internal/project` for their shared subcommands.

**Exception: `promote list` / `promote stamp`** are CLI-only and have no MCP or HTTP equivalents. The `promote` commands are part of the engram → vault bridge workflow described in the knowledge-mcp documentation, where `promote-memories.py` orchestrates the pull request and file generation. The engram side provides observation inspection and stamping; the knowledge repository owns file rendering and PR management.
