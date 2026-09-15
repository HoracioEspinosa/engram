# `engram project` — the engram-projects CLI

`engram project <slug> …` is the terminal surface of engram-projects: project cards, tasks, evidence, the runbook index, the code-graph pointer, and the per-task context pack. Every subcommand calls the same store functions the `projects` MCP profile calls, so what an agent sees through `mem_task_list` and what you see in a shell can never disagree.

Two commands, one letter apart, do different things:

| Command | Scope |
| --- | --- |
| `engram projects …` (plural) | Project **names**: `list`, `merge`, `consolidate`, `prune` |
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

The first argument is read as a subcommand when it is one (`card`, `upsert`, `graph`, `tasks`, `evidence`, `runbooks`, `context`, `promote`, `tree`, `set-parent`, `alias`, `bench`, `import-vault`, `search`), and as a slug otherwise. A project whose slug collides with a subcommand name has to be passed through `ENGRAM_PROJECT`.

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
| `project_cycle` | The parent would make the project its own ancestor |
| `project_depth_exceeded` | The parent would push the project, or something below it, past three levels |
| `set_parent_failed` | The move was refused for a reason none of the above names |
| `alias_owns_rows` | The name already holds memories of its own — merge the projects instead |
| `ambiguous_task` | The task reference resolves in more than one project |
| `evidence_root_unresolved` | No folder under the vault root matches the task |
| `vault_root_unresolved` | No argument, card or `ENGRAM_VAULT_ROOT` says where the vault is |
| `vault_readme_unparsed` | The vault README carries no "Mapa de tareas" section, and states were asked for |
| `path_escapes_vault` | A recorded vault path climbs out of the vault root |
| `restricted_path_rejected` | The path resolves, through every symlink, under a restricted root |
| `run_not_found` | The benchmark run file is not there |
| `not_engram_benchmark_v1` | The run is in the harness's own format and no pointer map was given |
| `pointer_unresolved` | A JSON Pointer in the map does not address anything in the run |
| `query_too_short` | `search` needs at least two characters |
| `unknown_theme` | No theme answers to that name (`engram theme`) |
| `invalid_theme` | The document is not a theme: a missing role, a bad name, a short gradient |
| `invalid_palette` | The palette parses but fails validation; `--force` stores it anyway |
| `merge_into_self` | `projects merge` was given a source that is already the target |
| `merge_failed` | The store refused the merge (`projects merge`) |

## `engram projects` — the plural family

`engram projects …` acts on project **names**, not on one project's data. It shares the `--json` contract above: `--json` prints machine-readable output on stdout, an error prints `{"error","code"}` and exits non-zero.

### `projects list`

Lists every project the store holds memories for, with its card details when it has a card.

```
$ engram projects list
Projects (2):
  nextcloud                       412 obs    18 sessions    7 prompts
  drive-argentina                  96 obs     4 sessions    1 prompt
```

`--json` prints an array — one object per project, in the same order — rather than the envelope the singular commands use, because there is no single project in scope:

```json
[
  {
    "name": "nextcloud",
    "slug": "nextcloud",
    "display_name": "Nextcloud server + apps amx_*",
    "parent": "clarodrive",
    "kind": "repo",
    "counts": { "observations": 412, "sessions": 18, "prompts": 7 }
  }
]
```

`slug`, `display_name`, `parent` and `kind` come from the project card and are absent for a project that has none. `counts` is always present.

### `projects merge <from>[,<from>…] <to>`

Moves every row of each source project into `<to>`, in one transaction, across every table with a `project` column — the same store call `mem_merge_projects` makes. This is the command `alias add` points at when a name already holds memories of its own.

```
$ engram projects merge nextcloud_00 nextcloud
Merged 1 source(s) into "nextcloud":
  observations: 118 moved
  project_cards: 1 moved
  sessions: 4 moved
  tasks: 2 moved
```

`<from>` also takes a comma-separated list, so a cluster of names that drifted apart collapses in one call. A source name is matched byte-for-byte, which is what makes `Engram` and `engram` mergeable; `<to>` is normalized the way every write path normalizes a project name.

The command exits non-zero when a source is not backed by a card or memories (`unknown_project`), when a source is already the target (`merge_into_self`), and when the store refuses the merge (`merge_failed`). A merge that moves nothing is never reported as success.

| Flag | Effect |
| --- | --- |
| `--json` | JSON envelope, with the full `MergeResult` — `table_rows_moved`, `table_rows_dropped`, `sources_merged`, `sources_skipped` — as its `result` |

### `projects consolidate` / `projects prune`

Interactive helpers that find similar names (`consolidate`) and empty projects (`prune`). Both accept `--dry-run`; `consolidate` also accepts `--all`. They are prompts, not scripts: a rollout drives `projects merge` instead.

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

It also takes what a person chooses about a project, and where it sits in the tree:

```bash
engram project nextcloud-00 upsert \
  --parent nextcloud --kind instance \
  --description "Instancia base de producción" --icon repo --color primary \
  --tag instancia --tag produccion --alias nextcloud_00
```

| Flag | Effect |
| --- | --- |
| `--parent <slug>` | Move the card under another project |
| `--root` | Move the card to the top of the tree (mutually exclusive with `--parent`) |
| `--kind` | `umbrella`, `repo`, `instance`, `service`, `dataset` or `knowledge` |
| `--description` | One line about what the project is |
| `--icon` | Icon token |
| `--color` | Palette role token (`primary`, `accent`, …) or `#rrggbb` |
| `--tag T` | Repeatable; stored as a JSON array |
| `--alias A` | Repeatable; each one becomes a name that resolves to this project |

The parent is a second write, and deliberately so: `set-parent` is the only path that validates the cycle and rewrites the depth of everything below the card. When it is refused, the card the upsert already wrote is still reported alongside the error, so a rejected move never reads as a rejected upsert.

The code graph is not touched here; that is `graph sync`.

### `tree [<root>]`

Walks the project tree in preorder — the whole forest, or one subtree when `<root>` names a project.

```
$ engram project tree --counts
▣ nextcloud
  ▫ nextcloud-00  12 obs · 1/3 tasks · 4 evidence
  ▫ nextcloud-01  3 obs · 0/1 tasks · 0 evidence
▪ engram
```

| Flag | Effect |
| --- | --- |
| `--counts` | Include observations, tasks and evidence per project |
| `--json` | JSON envelope; `result.nodes[]` carries `slug`, `parent_slug`, `depth`, `children`, `kind` and, with `--counts`, `counts` |

The glyph is the project's `kind`. With no `<root>`, the envelope's `project` is empty: a walk spanning every project has no single one to report.

### `set-parent <slug>`

Moves one project in the tree. Exactly one of `--to` and `--root` is required.

```bash
engram project set-parent nextcloud-00 --to nextcloud
engram project set-parent nextcloud-00 --root
```

A move that would close a cycle fails with `project_cycle`; one that would push the subtree past three levels fails with `project_depth_exceeded`.

### `tree suggest` / `tree apply` / `tree doctor`

`tree suggest` proposes a parent for each group of cards that reads as numbered instances of one product, folding the separator each slug happens to use so `nextcloud_00` and `nextcloud-02` land in the same family. **It never writes.**

```
$ engram project tree suggest
PARENT     EXISTS  CHILDREN                                  REASON
nextcloud  no      nextcloud-00, nextcloud_01, nextcloud-02  shared_prefix

nothing was changed; run: engram project tree apply --from-suggest
```

`tree apply --from-suggest` carries out what the suggestion proposes, asking first unless `--yes` (or `--json`) is given. A parent with no card is created as an `umbrella`, and the report says which ones it had to invent. One refused move never abandons the rest: each is reported under its own code in `result.conflicts[]`.

```bash
engram project tree apply --from-suggest --yes --json
```

`tree doctor` reports every card whose parent pointer or depth does not hold up — an orphan left by a retired umbrella, a cycle, a depth that disagrees with the chain. `--fix` detaches each of them to the top of the tree, which is the only repair it makes: where a broken card belongs is a question about intent.

```bash
engram project tree doctor --json
engram project tree doctor --fix
```

### `alias list|add|rm`

An alias is a name that resolves to a project without anything being stored under it. Resolution order is fixed: a real project is never redirected, a declared alias beats a coincidence of spelling.

```bash
engram project alias add nextcloud_00 --to nextcloud-00
engram project alias list nextcloud-00
engram project alias rm nextcloud_00
```

```
$ engram project alias add nextcloud_00 --to nextcloud-00
nextcloud_00 now resolves to nextcloud-00 (via alias)

$ engram project alias list nextcloud-00
ALIAS         PROJECT       SOURCE  UPDATED
nextcloud_00  nextcloud-00  manual  2026-09-14 10:02:11
```

`alias list` with no slug lists every alias. `--source` is one of `git_remote`, `dir`, `env`, `manual` (the default), `normalizer`.

`add` always prints what the name now resolves to, which is how you see an alias that is shadowed: a name that is itself a live project resolves `via card`, because a real project is never redirected.

A name that already holds memories of its own cannot become an alias — redirecting it would leave those memories reachable under a name the resolver no longer returns. The refusal is `alias_owns_rows` and it names the command that does the job properly:

```
$ engram project alias add nextcloud --to nextcloud-00
engram: "nextcloud" holds 412 observation(s) of its own and cannot become an alias of "nextcloud-00"; merge the projects instead
  hint: run: engram projects merge nextcloud nextcloud-00
```

### `graph check [<slug>]`

Compares the graph the card points at against `HEAD` and stamps the verdict on the card. Unlike `graph sync`, it reads no `graph.json`: it asks which files changed since `graph_commit` and counts only the ones the graph covers.

```
$ engram project nextcloud graph check --repo-dir ~/Projects/clarodrive
graph:   fresh (docs_only)
changed: 0 file(s) the graph covers
head:    7a79ef43…
checked: 2026-09-14 10:04:52
```

`reason` is one of `no_graph`, `code_changed`, `docs_only` or `graph_commit_unreachable`. A documentation-only change is **not** stale: the graph still describes the code, only its provenance is behind `HEAD`.

| Flag | Effect |
| --- | --- |
| `--repo-dir <dir>` | Repository to compare against (default: the detected project path, else cwd) |
| `--json` | JSON envelope |

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

### `evidence scan <task>`

Walks the task's folder in the knowledge vault and registers what it holds as evidence — one category, or all eleven. **It is a dry run by default**, and the dry run is a real one: the same walk, the same hashes, the same decisions, and not one write.

The vault root is resolved from `ENGRAM_VAULT_ROOT`, or from the project card's `knowledge_hub_path`. The task's folder is looked up by its recorded `vault_path`, then `<project>/<TICKET>-<slug>`, then `<project>/<slug>`, and finally any folder whose name leads with the ticket — which is what makes the command usable before anything has been imported.

```
$ export ENGRAM_VAULT_ROOT=~/.clarodrive
$ engram project nextcloud evidence scan CDBS-10336
planned: 11 new · 0 already known · 0 skipped · 2.4 MiB
folder:  /Users/me/.clarodrive/nextcloud/CDBS-10336-hardening-autologin
runs:    benchmarks/baseline-run1.json
         import them with: engram project bench import <task> <run.json>
nothing was written; re-run with --apply
```

```bash
engram project nextcloud evidence scan CDBS-10336 --category evidences --apply --json
```

| Flag | Effect |
| --- | --- |
| `--category X` | One of `analysis`, `plans`, `runbooks`, `reports`, `patches`, `evidences`, `evidences-qa`, `benchmarks`, `scripts`, `assets`, `exports`; default: all eleven |
| `--apply` | Register what the scan finds |
| `--max-bytes N` | Size beyond which a file is recorded but not hashed (default 64 MiB) |
| `--json` | JSON envelope |

Registration is idempotent by `(task, sha256)`: the same bytes found under a new name update the row's path and category instead of duplicating it, which is why a second scan reports `0 new · N already known`.

Every path is resolved through every symlink and compared against the restricted roots by containment, so a symlink planted inside the vault cannot smuggle a restricted tree into the store. Such an entry is reported in `skipped[]` with reason `restricted` and is never opened.

### `bench add|list|import`

A benchmark is the number a change was argued with. Each measurement is a row, one per metric is the baseline, and the comparison is a read.

```bash
engram project nextcloud bench add CDBS-10336 \
  --name lookup --metric lookup.p95 --unit ms --value 1512 \
  --baseline --captured-at 2026-08-20T10:00:00Z
engram project nextcloud bench add CDBS-10336 \
  --name lookup --metric lookup.p95 --unit ms --value 756 \
  --captured-at 2026-08-21T10:00:00Z
```

```
$ engram project nextcloud bench list CDBS-10336
METRIC      VALUE  UNIT  BASELINE  Δ               RUN     CAPTURED
lookup.p95  756    ms    1512      -50.00% better  lookup  2026-08-21T10:00:00Z
lookup.p95  1512   ms    * self    -               lookup  2026-08-20T10:00:00Z

2 of 2
```

The sign of Δ is read against the metric's own direction, so a number that improved reads as an improvement whichever way its unit points. Units are `ms`, `s`, `count`, `bytes`, `kib`, `mib`, `pct`, `ops`, `rps`, `usd`, `score`; `--direction lower|higher` overrides what the unit implies.

`bench add` flags: `--name`, `--metric`, `--unit`, `--value` (all required), `--baseline`, `--direction`, `--run-path`, `--config-stamp`, `--captured-at`, `--notes`, `--json`.

A measurement is keyed by `(task, name, metric, captured-at)`. Writing the same value under that key again is a retry: it exits zero, prints `already recorded` and reports `created:false` with `duplicate:true`, so a script that re-runs is not punished for it. A *different* value under the same key is never overwritten — that one fails with `duplicate_benchmark` and a non-zero exit, carrying the row it collided with, because the alternative is losing a number in silence. Capture it under its own `--captured-at`.
`bench list` flags: `--metric`, `--include-children`, `--limit`, `--offset`, `--json`. With no `<task>` it lists the project.

`bench import` reads a run file into measurements. A file carrying the `engram.benchmark.v1` marker is read directly; anything else is the harness's own output and needs a JSON Pointer map — passed with `--map`, or found as `benchmark_map.json` beside the run. Without one it fails with `not_engram_benchmark_v1`, because nothing guesses metrics out of a shape it does not recognise. It is a dry run until `--apply`.

```bash
engram project nextcloud bench import CDBS-10336 \
  ~/.clarodrive/nextcloud/CDBS-10336-hardening-autologin/benchmarks/baseline-run1.json --apply
engram project nextcloud bench import CDBS-10336 \
  ~/.clarodrive/nextcloud/CDBS-10336-hardening-autologin/benchmarks/harness-run.json \
  --map ~/.clarodrive/nextcloud/CDBS-10336-hardening-autologin/benchmarks/benchmark_map.json --apply
```

The `engram.benchmark.v1` shape:

```json
{
  "engram_benchmark": "v1",
  "task": "CDBS-10336",
  "name": "lookup-timeout",
  "captured_at": "2026-08-20T10:00:00Z",
  "config_stamp": "pond-02 / cache off",
  "baseline": true,
  "notes": "corrida base",
  "metrics": [{ "metric": "lookup.p95", "unit": "ms", "value": 1512 }]
}
```

And the map that reads a foreign one, as `benchmark_map.json`:

```json
[
  { "metric": "warm.resolve", "unit": "count", "pointer": "/scenarios/warm/total/ops/resolve" },
  { "metric": "cold.wallClockMs", "unit": "ms", "pointer": "/wallClockMs" }
]
```

Every pointer must resolve to a number: a map that no longer matches its harness fails with `pointer_unresolved` rather than importing a shorter list than the user wrote.

### `import-vault [<root>]`

Reads a whole knowledge tree — `<root>/<project>/<task>/<category>/` — into projects, tasks, evidence and benchmarks. **Dry run by default.**

A task folder is one holding a `README.md`, which is what separates the tasks from the `Runbooks/` tree and everything else a vault root keeps beside them. Folders prefixed with `_` or `.` are not tasks; only `_arquitectura` can be asked for, with `--include-arch`, and it comes in as a `spike`.

```
$ engram project import-vault ~/.clarodrive
ACTION  PROJECT    TASK                              KIND       STATE
create  nextcloud  CDBS-10336 hardening-autologin    bugfix     done
create  nextcloud  CDBS-10555 lookup-timeout         incident   pending
create  nextcloud  split-filesharing                 refactor   unverified

root:       /Users/me/.clarodrive
projects:   nextcloud
evidence:   14 new · 0 known · 0 skipped
benchmarks: 3 new · 0 known · 1 skipped
nothing was written; re-run with --apply
```

```bash
engram project import-vault ~/.clarodrive --project nextcloud --apply --json
```

| Flag | Effect |
| --- | --- |
| `--project X` | Import one project folder only |
| `--apply` | Write the plan |
| `--no-states` | Leave task states as the store records them |
| `--include-arch` | Import `_arquitectura` as a `spike` |
| `--max-bytes N` | Size beyond which a file is recorded but not hashed |
| `--json` | JSON envelope |

States come from the `## Mapa de tareas` section of the vault's root README, falling back to the task README's own `**Estado:**` line: `Cerrado (fecha)` → `done`, `Con pendientes` → `pending`, `Histórico` → `archived`, `Sin confirmar` → `unverified`. The state is read from the start of the cell, so a row that qualifies it — `Con pendientes — falta la QA`, `Sin confirmar cuál corre hoy` — still maps; a cell opening with none of the four leaves the state untouched. With `--no-states` the section is not required; without it, a README carrying none fails with `vault_readme_unparsed`.

The section runs until the next heading at its own level, and may be split into one subheading and one table per project:

```markdown
## Mapa de tareas

### `nextcloud/` — ingeniería de la plataforma

| Tarea | Qué resuelve | Estado | Archivos |
|---|---|---|---|
| [CDBS-10449 — migración de usuario](./nextcloud/CDBS-10449-migracion-usuario/README.md) | Migra un usuario entre instancias. | Cerrado (2026-06-22) | 195 |
```

A row is matched to a task folder by **the path its link points at** — `<project>/<task>` — never by its text, which is prose a person edits. The link text supplies the title, with any `TICKET — ` prefix dropped since the folder already carries the ticket. Lookup tables further down the file link to the same folders and are not part of the map.

The import is idempotent, and only in one direction: the title, the summary, the folder and the kind the folder name suggests are written **once, at creation**, so re-running never undoes a correction somebody made in the store. State is the exception, and `--no-states` is how you refuse even that. A second run over an unchanged tree therefore reports every task as `skip` and zero new rows.

### `search <query>`

One query across observations, tasks, evidence, runbooks, project cards and benchmarks, capped per kind so one loud kind cannot fill a result set six kinds are meant to share.

```
$ engram project search timeout --project nextcloud --subtree
KIND         REF          PROJECT    TITLE                             UPDATED
observation  obs-1a2b3c   nextcloud  El lookup agota su timeout        2026-09-05 16:13:31
task         CDBS-10555   nextcloud  Timeout de lookup en el estanque  2026-09-06 09:41:02

observation: 4
task: 1
```

| Flag | Effect |
| --- | --- |
| `--project X` | Scope to one project (default: the resolved project) |
| `--subtree` | Widen `--project` to the project and everything under it |
| `--kind K` | Repeatable: `observation`, `task`, `evidence`, `runbook`, `card`, `benchmark` |
| `--per-kind N` | Hits per kind, 1–25 (default 5) |
| `--json` | JSON envelope; `result.totals` carries how many there were before the cap |

A query shorter than two characters is refused with `query_too_short`: one character matches most of a workspace, which is a listing with extra steps rather than a search result.

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

## `engram theme` — the palettes the TUI renders with

`engram theme` is a top-level command, not a subcommand of `engram project`: a palette belongs to the interface, not to a project. Its `--json` therefore prints the result object directly, with no `{project, project_source, project_path, result}` envelope around it; errors keep the same `{"error", "code", …}` shape every other command uses.

Themes live in SQLite rather than in a configuration file, because everything else the interface remembers does. The palettes the binary ships are seeded into the `themes` table on every read, idempotently: a row still marked `builtin` takes the palette a new build carries, and one somebody has edited is left exactly as it is.

```
$ engram theme list

   THEME             VARIANT  SOURCE   PROBLEMS
   catppuccin-mocha  dark     builtin  -
   elephant          dark     builtin  3
*  kanagawa          dark     builtin  6
```

The `PROBLEMS` column is what `Palette.Validate` found. A theme is listed whether or not it is legible — the picker draws a broken one in `Danger` rather than hiding it, and so does this.

```
$ engram theme show kanagawa
theme:   kanagawa (dark · builtin)

    ROLE       HEX      ON BASE   ON SURFACE
▓▓  base       #1f1f28  1.00:1    1.16:1
▓▓  surface    #2a2a37  1.16:1    1.00:1
▓▓  overlay    #54546d  2.23:1 !  1.93:1 !
▓▓  text       #dcd7ba  11.26:1   9.75:1
▓▓  subtext    #727169  3.33:1 !  2.88:1 !
▓▓  primary    #7e9cd8  5.94:1    5.14:1
…

    LOGO ROW  HEX
▓▓  0         #957fb8
…

problem: subtext on base: contrast 3.33:1, want >= 4.5:1
problem: overlay on base: contrast 2.23:1, want >= 3.0:1
```

The block at the start of each row is a truecolor swatch painted in the role's own colour, and a ratio under the bar the role answers to is marked with `!` — 4.5:1 for the ten text roles (WCAG 1.4.3), 3:1 for `overlay`, which is a border and not text (WCAG 1.4.11).

| Subcommand | What it does |
| --- | --- |
| `list` | Every theme, its variant, where its palette came from, and which one is active |
| `show <name>` | The thirteen roles with swatches, the five-stop logo gradient, and the contrast matrix |
| `use <name>` | Writes `tui.theme` in `settings`. A name nothing answers to exits 1 and lists the ones that do |
| `import <file.json>` | Adds a theme from a document. `[--name N]` stores it under another name; `[--force]` stores a palette that fails validation |
| `export <name>` | Writes the document to stdout, or to `[--out <path>]` |
| `reset <name>` | Puts a builtin back the way this build ships it; a theme this build does not ship is removed instead |

```bash
engram theme use kanagawa
engram theme export kanagawa --out kanagawa.json
engram theme import kanagawa.json --name kanagawa-mine --force
engram theme reset kanagawa
```

### `theme.json`

```json
{
  "name": "koi-pond",
  "variant": "dark",
  "palette": {
    "base": "#0d1b21",
    "surface": "#16272f",
    "overlay": "#57808c",
    "text": "#e6edef",
    "subtext": "#9fb6bd",
    "primary": "#ff9e5e",
    "secondary": "#f4a8c0",
    "accent": "#ecc369",
    "highlight": "#7fe0d4",
    "success": "#96cf7f",
    "warning": "#e9b949",
    "danger": "#f4787f",
    "info": "#74bde0"
  },
  "logo_gradient": ["#e6edef", "#ecc369", "#ff9e5e", "#f4787f", "#7fe0d4"]
}
```

`name` is kebab-case, up to 32 characters. `variant` is `dark` or `light`, defaulting to `dark`. All thirteen roles must be present, each as a lowercase `#rrggbb` literal, and the gradient is exactly five stops.

Two checks are kept apart on purpose. **Shape** — the name, the variant, all thirteen roles, five stops — is what `import` refuses outright: a file that fails it is not a theme. **Taste** — legibility and duplication — is what `--force` overrides, because a palette somebody chose with their eyes open is theirs to choose. Validation reports every problem at once rather than the first: told one at a time, a person fixing a hand-written theme edits, re-imports, and is told the next one.

The rules are: the ten text roles clear 4.5:1 against both `base` and `surface`; `overlay` clears 3:1 against `base`; no two roles render as the same hex.

A palette can also be edited in place, which is the reason it is stored as a JSON document rather than as columns:

```bash
sqlite3 ~/.engram/engram.db \
  "UPDATE themes SET palette = json_set(palette, '\$.palette.primary', '#ff8a3d'),
                     source = 'sql', updated_at = datetime('now')
   WHERE name = 'koi-pond';"
```

Marking the row `source = 'sql'` is what protects the edit from the next upgrade's reseed. `engram theme reset koi-pond` undoes it.

## Relationship to the other surfaces

| Surface | Use it when | Subcommands |
| --- | --- | --- |
| CLI `engram project …` | Shell, hooks, scripts, and anything that wants aligned columns or `--json` | All (including `promote list` / `promote stamp`) |
| CLI `engram theme …` | Choosing, inspecting and exchanging palettes | `list`, `show`, `use`, `import`, `export`, `reset` |
| MCP `--tools=projects` | An agent inside a session | `card`, `upsert`, `graph sync`, `tasks …`, `evidence …`, `runbooks …`, `context` |
| HTTP `/projects/{slug}/…` | Another service reading the same data | `card`, `upsert`, `graph sync`, `tasks …`, `evidence …`, `runbooks …`, `context` |

All three go through the same functions in `internal/store` and `internal/project` for their shared subcommands.

**Exception: `promote list` / `promote stamp`** are CLI-only and have no MCP or HTTP equivalents. The `promote` commands are part of the engram → vault bridge workflow described in the knowledge-mcp documentation, where `promote-memories.py` orchestrates the pull request and file generation. The engram side provides observation inspection and stamping; the knowledge repository owns file rendering and PR management.
