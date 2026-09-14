// The seven `workspace` profile tools: the project tree, the vault scanners
// and importers, benchmarks, and the one search that answers across every kind
// of row at once.
//
// They are the MCP face of internal/workspace and internal/store, and they hold
// no logic of their own beyond reading arguments and naming failures: the same
// operations are reachable from `engram project …`, and a rule implemented
// twice is a rule that will be enforced differently on the two surfaces. Every
// refusal a caller can act on carries the code the operation declared it under,
// so a script branches on `code` rather than on prose.
package mcp

import (
	"context"
	"errors"
	"fmt"
	"strings"

	projectpkg "github.com/HoracioEspinosa/engram/internal/project"
	"github.com/HoracioEspinosa/engram/internal/store"
	"github.com/HoracioEspinosa/engram/internal/vault"
	"github.com/HoracioEspinosa/engram/internal/workspace"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

var workspaceKindEnum = []string{
	store.WorkspaceKindObservation, store.WorkspaceKindTask, store.WorkspaceKindEvidence,
	store.WorkspaceKindRunbook, store.WorkspaceKindCard, store.WorkspaceKindBenchmark,
}

var benchmarkUnitEnum = []string{"ms", "s", "bytes", "kib", "mib", "usd", "ops", "rps", "score", "count", "pct"}

var benchmarkDirectionEnum = []string{store.BenchmarkDirectionLower, store.BenchmarkDirectionHigher}

// maxProjectTreeDepth is the deepest the tree is ever rendered, mirroring the
// depth the store enforces on every write.
const maxProjectTreeDepth = store.MaxProjectDepth

// workspaceToolError republishes a workspace failure under the code that
// package declared it with, and returns nil for anything it did not name so the
// caller can go on with its own mapping.
func workspaceToolError(err error) *mcp.CallToolResult {
	if code := workspace.CodeOf(err); code != "" {
		return projectToolError(code, err.Error(), nil)
	}
	return nil
}

// namedProjectResult is the envelope of an answer about one named project. The
// name came from the argument or from the row the operation acted on, never
// from the working directory: a vault scan is about the task it was given, and
// reporting the caller's cwd next to it would describe the caller.
func namedProjectResult(project string) projectpkg.DetectionResult {
	if strings.TrimSpace(project) == "" {
		return projectpkg.DetectionResult{}
	}
	return projectpkg.DetectionResult{Project: project, Source: projectpkg.SourceExplicitOverride}
}

// registerWorkspaceTools registers the seven `workspace` profile tools.
//
// All but mem_workspace_search are deferred: they are the deliberate steps of a
// reorganisation, asked for by name. The search is what a session reaches for
// without planning to, so it stays in context.
func registerWorkspaceTools(srv *server.MCPServer, s *store.Store, cfg MCPConfig, allowlist map[string]bool, writeQueue *writeQueue) {
	if shouldRegister("mem_project_tree", allowlist) {
		srv.AddTool(
			mcp.NewTool("mem_project_tree",
				mcp.WithDescription("Walk the project hierarchy in preorder: a root and everything under it, or the whole forest when no root is given. Call this to find out which project a name belongs under before filing work against it."),
				mcp.WithDeferLoading(true),
				mcp.WithTitleAnnotation("Project Tree"),
				mcp.WithReadOnlyHintAnnotation(true),
				mcp.WithDestructiveHintAnnotation(false),
				mcp.WithIdempotentHintAnnotation(true),
				mcp.WithOpenWorldHintAnnotation(false),
				mcp.WithString("root", mcp.Description("Slug to start from; omit for the whole forest")),
				mcp.WithBoolean("include_counts", mcp.DefaultBool(false),
					mcp.Description("Include each node's observation, task, evidence and runbook counters")),
				mcp.WithNumber("depth", mcp.Min(1), mcp.Max(maxProjectTreeDepth), mcp.DefaultNumber(maxProjectTreeDepth),
					mcp.Description("How many levels to return, counting the root as one")),
			),
			handleProjectTree(s),
		)
	}

	if shouldRegister("mem_evidence_scan", allowlist) {
		srv.AddTool(
			mcp.NewTool("mem_evidence_scan",
				mcp.WithDescription("Walk a task's vault folder and register what it holds as evidence, hashing each file. Dry run by default: it reports the exact plan an apply would carry out."),
				mcp.WithDeferLoading(true),
				mcp.WithTitleAnnotation("Scan Evidence"),
				mcp.WithReadOnlyHintAnnotation(false),
				mcp.WithDestructiveHintAnnotation(false),
				mcp.WithIdempotentHintAnnotation(true),
				mcp.WithOpenWorldHintAnnotation(false),
				mcp.WithString("task", mcp.Required(), mcp.Description("PROJ-123 | task-<hex> | #42 | change:<sdd_change> | slug")),
				mcp.WithString("category", mcp.Enum(evidenceCategoryEnum...),
					mcp.Description("Scan one vault folder instead of all eleven")),
				mcp.WithBoolean("dry_run", mcp.DefaultBool(true),
					mcp.Description("Report the plan without writing. Default true: a scan registers files nobody listed by hand")),
				mcp.WithNumber("max_bytes", mcp.Min(1),
					mcp.Description("Skip files larger than this instead of hashing them")),
			),
			queuedWriteHandler(writeQueue, handleEvidenceScan(s)),
		)
	}

	if shouldRegister("mem_benchmark_add", allowlist) {
		srv.AddTool(
			mcp.NewTool("mem_benchmark_add",
				mcp.WithDescription("Record one measurement against a task. Marking it as the baseline demotes the previous baseline of the same metric in the same transaction."),
				mcp.WithDeferLoading(true),
				mcp.WithTitleAnnotation("Add Benchmark"),
				mcp.WithReadOnlyHintAnnotation(false),
				mcp.WithDestructiveHintAnnotation(false),
				mcp.WithIdempotentHintAnnotation(true),
				mcp.WithOpenWorldHintAnnotation(false),
				mcp.WithString("task", mcp.Required()),
				mcp.WithString("name", mcp.Required(), mcp.Description("The run this measurement came from")),
				mcp.WithString("metric", mcp.Required(), mcp.Description("Dotted metric name, e.g. lookup.p95")),
				mcp.WithString("unit", mcp.Required(), mcp.Enum(benchmarkUnitEnum...)),
				mcp.WithNumber("value", mcp.Required()),
				mcp.WithString("direction", mcp.Enum(benchmarkDirectionEnum...),
					mcp.Description("Which way the metric improves; defaults to what the unit implies")),
				mcp.WithBoolean("baseline", mcp.DefaultBool(false), mcp.Description("Make this the metric's baseline")),
				mcp.WithString("run_path", mcp.Description("Vault-relative path of the run file")),
				mcp.WithString("sha256", mcp.Pattern(`^[0-9a-f]{64}$`)),
				mcp.WithString("config_stamp", mcp.MaxLength(300)),
				mcp.WithString("captured_at", mcp.Description("ISO-8601; defaults to now")),
				mcp.WithString("notes", mcp.MaxLength(500)),
			),
			queuedWriteHandler(writeQueue, handleBenchmarkAdd(s)),
		)
	}

	if shouldRegister("mem_benchmark_list", allowlist) {
		srv.AddTool(
			mcp.NewTool("mem_benchmark_list",
				mcp.WithDescription("List measurements newest first, each next to the baseline of its own metric and the percentage it moved."),
				mcp.WithDeferLoading(true),
				mcp.WithTitleAnnotation("List Benchmarks"),
				mcp.WithReadOnlyHintAnnotation(true),
				mcp.WithDestructiveHintAnnotation(false),
				mcp.WithIdempotentHintAnnotation(true),
				mcp.WithOpenWorldHintAnnotation(false),
				mcp.WithString("task", mcp.Description("Narrow to one task; omit to list the project")),
				mcp.WithString("project", mcp.Description("Slug; optional, resolved by precedence when omitted")),
				mcp.WithString("metric", mcp.Description("Narrow to one metric")),
				mcp.WithBoolean("include_children", mcp.DefaultBool(false),
					mcp.Description("Widen to the child tasks of task, or to the project subtree")),
				mcp.WithNumber("limit", mcp.Min(1), mcp.Max(200), mcp.DefaultNumber(50)),
				mcp.WithNumber("offset", mcp.Min(0), mcp.DefaultNumber(0)),
			),
			handleBenchmarkList(s, cfg),
		)
	}

	if shouldRegister("mem_benchmark_import", allowlist) {
		srv.AddTool(
			mcp.NewTool("mem_benchmark_import",
				mcp.WithDescription("Read one benchmark run file into a task's measurements. A run carrying the engram.benchmark.v1 marker is read directly; any other shape needs a pointer map, given here or found as benchmark_map.json beside the run."),
				mcp.WithDeferLoading(true),
				mcp.WithTitleAnnotation("Import Benchmarks"),
				mcp.WithReadOnlyHintAnnotation(false),
				mcp.WithDestructiveHintAnnotation(false),
				mcp.WithIdempotentHintAnnotation(true),
				mcp.WithOpenWorldHintAnnotation(false),
				mcp.WithString("task", mcp.Required()),
				mcp.WithString("path", mcp.Required(), mcp.Description("Path of the run file to read")),
				mcp.WithArray("map", mcp.MaxItems(50),
					mcp.Items(map[string]any{
						"type": "object",
						"properties": map[string]any{
							"metric":    map[string]any{"type": "string"},
							"unit":      map[string]any{"type": "string", "enum": benchmarkUnitEnum},
							"pointer":   map[string]any{"type": "string", "description": "RFC 6901 JSON pointer into the run"},
							"direction": map[string]any{"type": "string", "enum": benchmarkDirectionEnum},
						},
						"required":             []string{"metric", "unit", "pointer"},
						"additionalProperties": false,
					}),
					mcp.Description("How to read a run that is not engram.benchmark.v1"),
				),
				mcp.WithBoolean("baseline", mcp.DefaultBool(false), mcp.Description("Make every imported metric its own baseline")),
				mcp.WithBoolean("dry_run", mcp.DefaultBool(true), mcp.Description("Report what would be imported without writing")),
			),
			queuedWriteHandler(writeQueue, handleBenchmarkImport(s)),
		)
	}

	if shouldRegister("mem_vault_sync", allowlist) {
		srv.AddTool(
			mcp.NewTool("mem_vault_sync",
				mcp.WithDescription("Read a whole knowledge vault — <root>/<project>/<task>/ — into projects, tasks, evidence and benchmarks. Dry run by default; re-running an applied import changes nothing."),
				mcp.WithDeferLoading(true),
				mcp.WithTitleAnnotation("Sync Vault"),
				mcp.WithReadOnlyHintAnnotation(false),
				mcp.WithDestructiveHintAnnotation(false),
				mcp.WithIdempotentHintAnnotation(true),
				mcp.WithOpenWorldHintAnnotation(false),
				mcp.WithString("project", mcp.Description("Import only this project's folder; omit for the whole vault")),
				mcp.WithString("root", mcp.Description("Vault root; falls back to the card's knowledge hub and then ENGRAM_VAULT_ROOT")),
				mcp.WithBoolean("dry_run", mcp.DefaultBool(true),
					mcp.Description("Report the plan without writing. Default true: an import creates tasks nobody listed by hand")),
				mcp.WithBoolean("apply_states", mcp.DefaultBool(true),
					mcp.Description("Take task states from the vault README's task map")),
				mcp.WithBoolean("include_arch", mcp.DefaultBool(false),
					mcp.Description("Import _arquitectura as a spike; every other underscore folder stays out")),
			),
			queuedWriteHandler(writeQueue, handleVaultSync(s)),
		)
	}

	if shouldRegister("mem_workspace_search", allowlist) {
		srv.AddTool(
			mcp.NewTool("mem_workspace_search",
				mcp.WithDescription("Search observations, tasks, evidence, runbooks, project cards and benchmarks in one call, capped per kind. Use this when you do not yet know which kind of row holds the answer."),
				mcp.WithTitleAnnotation("Search Workspace"),
				mcp.WithReadOnlyHintAnnotation(true),
				mcp.WithDestructiveHintAnnotation(false),
				mcp.WithIdempotentHintAnnotation(true),
				mcp.WithOpenWorldHintAnnotation(false),
				mcp.WithString("query", mcp.Required(), mcp.MinLength(2),
					mcp.Description("Words to look for; the last one matches as a prefix")),
				mcp.WithString("project", mcp.Description("Scope to one project; omit to search everything")),
				mcp.WithBoolean("subtree", mcp.DefaultBool(false),
					mcp.Description("Widen project to the project and everything under it")),
				mcp.WithArray("kinds", mcp.Items(map[string]any{"type": "string", "enum": workspaceKindEnum}),
					mcp.Description("Ask only some of the six kinds; omit for all")),
				mcp.WithNumber("per_kind", mcp.Min(1), mcp.Max(25), mcp.DefaultNumber(5),
					mcp.Description("Cap each kind's hits; data.totals says how many there were")),
			),
			handleWorkspaceSearch(s),
		)
	}
}

// ─── mem_project_tree ────────────────────────────────────────────────────────

func handleProjectTree(s *store.Store) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		root := strings.TrimSpace(optString(req, "root"))
		depth := clampInt(intArg(req, "depth", maxProjectTreeDepth), 1, maxProjectTreeDepth, maxProjectTreeDepth)

		nodes, err := s.ProjectTree(root, optBoolDefault(req, "include_counts", false))
		if errors.Is(err, store.ErrNoProjectCard) {
			return projectToolError("unknown_project", fmt.Sprintf("project %q has no card", root), nil), nil
		}
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		// Depth is counted from the root the caller asked about, not from the
		// forest floor: "two levels under koi-garden" must mean the same thing
		// wherever koi-garden itself happens to sit.
		base := 0
		if len(nodes) > 0 {
			base = nodes[0].Depth
		}
		out := make([]map[string]any, 0, len(nodes))
		for _, node := range nodes {
			if node.Depth-base >= depth {
				continue
			}
			entry := map[string]any{
				"slug": node.Slug, "display_name": node.DisplayName, "kind": node.Kind,
				"parent": node.ParentSlug, "depth": node.Depth, "children": node.Children,
				"icon": node.Icon, "color": node.Color, "description": node.Description,
				"tags": node.Tags,
			}
			if node.Counts != nil {
				entry["counts"] = node.Counts
			}
			out = append(out, entry)
		}

		return respondProjectResult(namedProjectResult(root), map[string]any{
			"nodes": out, "root": root, "depth": depth,
		}), nil
	}
}

// ─── mem_evidence_scan ───────────────────────────────────────────────────────

func handleEvidenceScan(s *store.Store) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		taskRef := strings.TrimSpace(optString(req, "task"))
		if taskRef == "" {
			return projectToolError("missing_field", "task is required", nil), nil
		}
		category := optString(req, "category")
		if category != "" && !enumContains(evidenceCategoryEnum, category) {
			return projectToolError("invalid_enum", fmt.Sprintf("category %q is invalid", category),
				map[string]any{"allowed": evidenceCategoryEnum}), nil
		}

		opts := vault.Options{MaxBytes: int64(intArg(req, "max_bytes", 0))}
		report, err := workspace.ScanEvidence(s, taskRef, category, optBoolDefault(req, "dry_run", true), opts)
		if err != nil {
			if coded := workspaceToolError(err); coded != nil {
				return coded, nil
			}
			return mcp.NewToolResultError(err.Error()), nil
		}

		return respondProjectResult(namedProjectResult(report.Project), map[string]any{
			"task_sync_id": report.TaskSyncID, "root": report.Root, "task_dir": report.TaskDir,
			"dry_run": report.DryRun, "added": report.Added, "updated": report.Updated,
			"skipped": report.Skipped, "benchmark_candidates": report.BenchmarkCandidates,
			"total_bytes": report.TotalBytes,
		}), nil
	}
}

// ─── mem_benchmark_add ───────────────────────────────────────────────────────

func handleBenchmarkAdd(s *store.Store) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		taskRef := strings.TrimSpace(optString(req, "task"))
		name := strings.TrimSpace(optString(req, "name"))
		metric := strings.TrimSpace(optString(req, "metric"))
		unit := strings.TrimSpace(optString(req, "unit"))
		if taskRef == "" || name == "" || metric == "" || unit == "" {
			return projectToolError("missing_field", "task, name, metric, unit and value are required", nil), nil
		}
		value, ok := req.GetArguments()["value"].(float64)
		if !ok {
			return projectToolError("missing_field", "value is required and must be a number", nil), nil
		}
		if !enumContains(benchmarkUnitEnum, unit) {
			return projectToolError("invalid_enum", fmt.Sprintf("unit %q is invalid", unit),
				map[string]any{"allowed": benchmarkUnitEnum}), nil
		}
		direction := strings.TrimSpace(optString(req, "direction"))
		if direction != "" && !enumContains(benchmarkDirectionEnum, direction) {
			return projectToolError("invalid_enum", fmt.Sprintf("direction %q is invalid", direction),
				map[string]any{"allowed": benchmarkDirectionEnum}), nil
		}

		task, err := workspace.ResolveTask(s, taskRef)
		if err != nil {
			if coded := workspaceToolError(err); coded != nil {
				return coded, nil
			}
			return mcp.NewToolResultError(err.Error()), nil
		}

		result, err := s.AddBenchmark(store.AddBenchmarkParams{
			Task: task, Name: name, Metric: metric, Unit: unit, Direction: direction, Value: value,
			Baseline:    optBoolDefault(req, "baseline", false),
			RunPath:     optStringPtr(req, "run_path"),
			SHA256:      optStringPtr(req, "sha256"),
			ConfigStamp: optStringPtr(req, "config_stamp"),
			CapturedAt:  optString(req, "captured_at"),
			Notes:       optStringPtr(req, "notes"),
			Source:      "manual",
		})
		if errors.Is(err, store.ErrInvalidBenchmarkUnit) {
			return projectToolError("invalid_enum", err.Error(), map[string]any{"allowed": benchmarkUnitEnum}), nil
		}
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		// AddBenchmark is idempotent by (task, name, metric, captured_at) and
		// hands the existing row back. Reporting that as a success would tell a
		// caller it recorded a number it did not, so the duplicate is named —
		// with the row it collided with, which is the only useful next step.
		if !result.Created {
			return projectToolError("duplicate_benchmark",
				fmt.Sprintf("%s/%s at %s is already recorded for this task", name, metric, result.Benchmark.CapturedAt),
				map[string]any{"benchmark": result.Benchmark}), nil
		}

		return respondProjectResult(namedProjectResult(task.Project), map[string]any{
			"benchmark": result.Benchmark, "created": result.Created,
			"demoted_baseline": result.DemotedBaseline,
		}), nil
	}
}

// ─── mem_benchmark_list ──────────────────────────────────────────────────────

func handleBenchmarkList(s *store.Store, cfg MCPConfig) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		filter := store.BenchmarkListFilter{
			Metric:          optString(req, "metric"),
			IncludeChildren: optBoolDefault(req, "include_children", false),
			Limit:           clampInt(intArg(req, "limit", 50), 1, 200, 50),
			Offset:          intArg(req, "offset", 0),
		}

		project := ""
		if taskRef := strings.TrimSpace(optString(req, "task")); taskRef != "" {
			task, err := workspace.ResolveTask(s, taskRef)
			if err != nil {
				if coded := workspaceToolError(err); coded != nil {
					return coded, nil
				}
				return mcp.NewToolResultError(err.Error()), nil
			}
			filter.Task = task.SyncID
			project = task.Project
		} else {
			detRes, errResult := resolveProjectsToolReadProject(s, cfg, optString(req, "project"))
			if errResult != nil {
				return errResult, nil
			}
			filter.Project = detRes.Project
			project = detRes.Project
		}

		page, err := s.ListBenchmarks(filter)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return respondProjectResult(namedProjectResult(project), map[string]any{
			"items": page.Items, "total": page.Total, "limit": page.Limit, "offset": page.Offset,
		}), nil
	}
}

// ─── mem_benchmark_import ────────────────────────────────────────────────────

func handleBenchmarkImport(s *store.Store) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		taskRef := strings.TrimSpace(optString(req, "task"))
		path := strings.TrimSpace(optString(req, "path"))
		if taskRef == "" || path == "" {
			return projectToolError("missing_field", "task and path are required", nil), nil
		}

		mapping, errResult := pointerMapArg(req)
		if errResult != nil {
			return errResult, nil
		}

		report, err := workspace.ImportBenchmarks(s, taskRef, path, mapping,
			optBoolDefault(req, "baseline", false), optBoolDefault(req, "dry_run", true))
		if err != nil {
			if coded := workspaceToolError(err); coded != nil {
				return coded, nil
			}
			return mcp.NewToolResultError(err.Error()), nil
		}

		return respondProjectResult(namedProjectResult(report.Project), map[string]any{
			"task_sync_id": report.TaskSyncID, "format": report.Format, "name": report.Name,
			"run_path": report.RunPath, "dry_run": report.DryRun,
			"imported": report.Imported, "duplicates": report.Duplicates, "skipped": report.Skipped,
			"metrics": report.Metrics, "demoted_baselines": report.DemotedBaselines,
		}), nil
	}
}

// pointerMapArg reads the `map` argument into pointer maps, refusing an entry
// that names a unit no measurement can be compared in.
func pointerMapArg(req mcp.CallToolRequest) ([]vault.PointerMap, *mcp.CallToolResult) {
	raw, ok := req.GetArguments()["map"].([]any)
	if !ok {
		return nil, nil
	}
	out := make([]vault.PointerMap, 0, len(raw))
	for _, item := range raw {
		entry, ok := item.(map[string]any)
		if !ok {
			return nil, projectToolError("invalid_enum", "each map entry must be an object", nil)
		}
		pm := vault.PointerMap{}
		pm.Metric, _ = entry["metric"].(string)
		pm.Unit, _ = entry["unit"].(string)
		pm.Pointer, _ = entry["pointer"].(string)
		pm.Direction, _ = entry["direction"].(string)
		if strings.TrimSpace(pm.Metric) == "" || strings.TrimSpace(pm.Pointer) == "" {
			return nil, projectToolError("missing_field", "each map entry needs a metric and a pointer", nil)
		}
		if !enumContains(benchmarkUnitEnum, pm.Unit) {
			return nil, projectToolError("invalid_enum", fmt.Sprintf("unit %q is invalid", pm.Unit),
				map[string]any{"allowed": benchmarkUnitEnum})
		}
		if pm.Direction != "" && !enumContains(benchmarkDirectionEnum, pm.Direction) {
			return nil, projectToolError("invalid_enum", fmt.Sprintf("direction %q is invalid", pm.Direction),
				map[string]any{"allowed": benchmarkDirectionEnum})
		}
		out = append(out, pm)
	}
	return out, nil
}

// ─── mem_vault_sync ──────────────────────────────────────────────────────────

func handleVaultSync(s *store.Store) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		project := strings.TrimSpace(optString(req, "project"))
		plan, err := workspace.ImportVault(s, optString(req, "root"), project,
			!optBoolDefault(req, "dry_run", true),
			optBoolDefault(req, "apply_states", true),
			optBoolDefault(req, "include_arch", false),
			vault.Options{})
		if err != nil {
			if coded := workspaceToolError(err); coded != nil {
				return coded, nil
			}
			return mcp.NewToolResultError(err.Error()), nil
		}

		return respondProjectResult(namedProjectResult(project), map[string]any{
			"root": plan.Root, "dry_run": plan.DryRun, "projects": plan.Projects,
			"tasks": plan.Tasks, "evidence": plan.Evidence, "benchmarks": plan.Benchmarks,
			"warnings": plan.Warnings,
		}), nil
	}
}

// ─── mem_workspace_search ────────────────────────────────────────────────────

func handleWorkspaceSearch(s *store.Store) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		query := strings.TrimSpace(optString(req, "query"))
		kinds := optStringSlice(req, "kinds")
		for _, kind := range kinds {
			if !enumContains(workspaceKindEnum, kind) {
				return projectToolError("invalid_enum", fmt.Sprintf("kind %q is invalid", kind),
					map[string]any{"allowed": workspaceKindEnum}), nil
			}
		}

		project := strings.TrimSpace(optString(req, "project"))
		results, err := s.SearchWorkspace(store.SearchWorkspaceParams{
			Query:   query,
			Project: project,
			Subtree: optBoolDefault(req, "subtree", false),
			Kinds:   kinds,
			PerKind: clampInt(intArg(req, "per_kind", 5), 1, 25, 5),
		})
		switch {
		case errors.Is(err, store.ErrWorkspaceQueryTooShort):
			return projectToolError("query_too_short", err.Error(), nil), nil
		case errors.Is(err, store.ErrUnknownWorkspaceKind):
			return projectToolError("invalid_enum", err.Error(), map[string]any{"allowed": workspaceKindEnum}), nil
		case errors.Is(err, store.ErrNoProjectCard):
			return projectToolError("unknown_project", fmt.Sprintf("project %q has no card to take a subtree of", project), nil), nil
		case err != nil:
			return mcp.NewToolResultError(err.Error()), nil
		}

		return respondProjectResult(namedProjectResult(project), map[string]any{
			"hits": results.Hits, "totals": results.Totals, "query": query,
		}), nil
	}
}
