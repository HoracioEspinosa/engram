[← Back to README](../README.md)

# Dev Environment

Engram's own contributor tooling — the isolated Docker stack under `scripts/dev/` and
`docker-compose.dev.yml`, wired into `make` — never touches the host toolchain or a
contributor's real `~/.engram` data. Every script in this guide mounts a throwaway
volume or a disposable container instead.

## The rule: nothing reaches `~/.engram`

`scripts/dev/up.sh` refuses to start until the resolved compose config is proven not to
reach the host's `~/.engram` directory, and until `./setup.sh` has run in the checkout.
Do not add a bind mount to `docker-compose.dev.yml` that points at a real data
directory — the isolated store lives entirely inside named Docker volumes
(`engram-dev-*`), disposable by `make dev-down`.

## Quick start

```bash
./setup.sh          # once per checkout
make dev-up          # build and start the isolated container
make dev-seed         # fill it with the fixture workspace
make dev-smoke        # drive one MCP session against it and assert the responses
make dev-down         # tear it down and drop the volumes
```

`make dev-gate` chains `dev-up`, `dev-seed`, `dev-smoke`, `dev-down` — the smallest
local loop that proves the container starts, accepts writes, and answers MCP calls
correctly.

## Make targets

| Target       | What it runs                          | What it's for |
| ------------ | -------------------------------------- | -------------- |
| `make help`  | —                                       | Lists every target below |
| `make templ` | `go tool templ generate ./internal/cloud/dashboard/...` | Regenerate dashboard `.templ` → `_templ.go` files (run after editing a `.templ` file, see [DOCS.md — Dashboard templ regeneration](../DOCS.md#dashboard-templ-regeneration)) |
| `make test`  | `scripts/dev/test.sh`                   | Full suite in the pinned container: `go test ./...`, `go test -tags e2e ./internal/server/...`, and the `internal/tui` coverage gate |
| `make bench` | `scripts/dev/bench.sh $(LABEL)`         | Benchmark hot paths six times and compare against `docker/dev/out/bench/baseline.txt` with `benchstat`. `LABEL` defaults to `baseline`; run `make bench LABEL=my-change` to compare against it |
| `make lint`  | `gofmt -l` + `go vet ./...`             | Same formatting and vet check CI runs, in the pinned container |
| `make tui-golden` | `scripts/dev/golden.sh --approve`  | Regenerate TUI golden files. Never run without reviewing the diff it writes to `docker/dev/out/tui/golden-diff.txt` — a golden file is the record of what a screen is supposed to look like |
| `make dev-up` | `scripts/dev/up.sh`                    | Build and start the isolated dev container, wait for its health check, verify the published API port is loopback-only |
| `make dev-down` | `scripts/dev/down.sh`                | Stop every service (including the `cloud` and `shots` compose profiles) and remove the named volumes, so the next `dev-up` + `dev-seed` starts from an empty store |
| `make dev-seed` | `scripts/dev/seed.sh`                | Upsert the fixture workspace: 4 project cards, 5 tasks, 3 evidence rows, the runbook index |
| `make dev-smoke` | `scripts/dev/mcp-smoke.sh`           | Run one MCP session per tool profile against the dev container and assert each response |
| `make dev-gate` | `dev-up` → `dev-seed` → `dev-smoke` → `dev-down` | The smallest full local loop |

## Scripts not wired into `make`

These need arguments, a live copy of real data, or a piece of state the `make` targets
above deliberately don't assume, so they stay explicit shell invocations:

| Script | Purpose |
| ------ | ------- |
| `scripts/dev/backup-live.sh` | Take one consistent, read-only `VACUUM INTO` copy of `~/.engram/engram.db` under `docker/dev/out/live-copy/`. The only script here that ever reads the real store, and it never writes to it |
| `scripts/dev/migrate-check.sh` | Prove a schema migration loses no rows, run against a `backup-live.sh` copy — never the original |
| `scripts/dev/reorg-check.sh` | Prove a project reorganisation (`mem_merge_projects`, the project tools) loses no observations and is idempotent, run against a `backup-live.sh` copy installed at `/data/live` |
| `scripts/dev/cloud-roundtrip.sh` | Bring up the `cloud` compose profile and prove an `engram-projects` row survives a push/pull round trip between two throwaway stores, field by field |
| `scripts/dev/tui-shots.sh` | Capture every TUI screen at both supported terminal geometries and check no line overflows |

Run any of them the same way `make` does — `bash scripts/dev/<script>.sh [args]` — after
`make dev-up`.

## Where the evidence lands

Every script writes its own output under `docker/dev/out/`, one subdirectory per
script (`docker/dev/out/test/`, `docker/dev/out/bench/`, `docker/dev/out/tui/`,
`docker/dev/out/live-copy/`, …). Nothing under `docker/dev/out/` is committed; it is
the run's evidence, not a build artifact. A closeout or gate that needs a durable
record copies the relevant log out of there into the task's own evidence trail — the
card, the task, or the vault, per `ENGRAM_VAULT_ROOT` below — not into `docker/dev/out/`
itself, which the next run is free to overwrite.

## Environment variables

- `ENGRAM_VAULT_ROOT` — where `engram project <slug> evidence scan` and the vault
  importer look for the knowledge vault, when the project card has no
  `knowledge_hub_path` of its own. See
  [engram-projects CLI — evidence scan](ENGRAM-PROJECTS-CLI.md#evidence-scan-task).
- `CD_EVIDENCE_DIR` is no longer needed for day-to-day work: evidence paths resolve
  from the project card, the task, and the vault root instead of a separately
  configured evidence directory. It still gates `engram project <slug> evidence add
  --file`, which hashes and stores a capture relative to it (`~/.clarodrive/evidence`
  when unset) — set it only if you use that specific command.

## `.mcp.json` recommendation

Point a contributor's own `.mcp.json` (untracked, never edited by tooling in this repo)
at the `agent,projects,workspace` profile — the recommended set for work inside a
project repository, 35 tools with the `workspace`/`projects` overlap counted once:

```json
{
  "mcpServers": {
    "engram": {
      "command": "engram",
      "args": ["mcp", "--tools=agent,projects,workspace"]
    }
  }
}
```

See [README — MCP Tools](../README.md#mcp-tools) and
[DOCS.md — Tool profiles](../DOCS.md#tool-profiles) for the full profile breakdown.
