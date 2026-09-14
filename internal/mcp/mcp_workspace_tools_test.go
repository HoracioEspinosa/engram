package mcp

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/HoracioEspinosa/engram/internal/store"
)

// ─── Fixtures ────────────────────────────────────────────────────────────────

// writeVaultFile creates path and every directory above it.
func writeVaultFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// newToolVault builds the smallest knowledge tree these tools can be asked
// about — one project, one ticket, one piece of evidence and one benchmark run
// — and points ENGRAM_VAULT_ROOT at it, which is how a tool with no root
// argument finds the vault.
func newToolVault(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	writeVaultFile(t, filepath.Join(root, "README.md"), `# Vault

## Mapa de tareas

| Tarea | Qué es | Estado | Archivos |
| --- | --- | --- | --- |
| [Timeout de lookup](./koi-garden/KOI-1099-lookup-timeout/README.md) | Mitigar el timeout | Con pendientes | 2 |
`)
	writeVaultFile(t, filepath.Join(root, "koi-garden", "README.md"), "# koi-garden\n\nUn estanque.\n")

	task := filepath.Join(root, "koi-garden", "KOI-1099-lookup-timeout")
	writeVaultFile(t, filepath.Join(task, "README.md"),
		"# KOI-1099 — Timeout de lookup\n\n**Estado:** Con pendientes\n\nEl lookup falla con timeout.\n")
	writeVaultFile(t, filepath.Join(task, "analysis", "hipotesis.md"), "# Hipótesis\n\nEl cliente no fija timeout.\n")
	writeVaultFile(t, filepath.Join(task, "benchmarks", "baseline-run1.json"), `{
  "engram_benchmark": "v1",
  "task": "KOI-1099",
  "name": "lookup-timeout",
  "captured_at": "2026-08-20T10:00:00Z",
  "baseline": true,
  "metrics": [ { "metric": "lookup.p95", "unit": "ms", "value": 1512 } ]
}`)

	t.Setenv("ENGRAM_VAULT_ROOT", root)
	return root
}

// seedWorkspaceStore gives the tools a project, a card and a task to act on.
func seedWorkspaceStore(t *testing.T) *store.Store {
	t.Helper()
	s := newMCPTestStore(t)
	for _, slug := range []string{"koi-garden", "koi-garden-pond-01", "koi-garden-pond-02"} {
		if err := s.EnrollProject(slug); err != nil {
			t.Fatalf("enroll %s: %v", slug, err)
		}
		if _, _, err := s.UpsertProjectCard(store.UpsertProjectCardParams{Slug: slug}); err != nil {
			t.Fatalf("UpsertProjectCard(%s): %v", slug, err)
		}
	}
	parent := "koi-garden"
	for _, slug := range []string{"koi-garden-pond-01", "koi-garden-pond-02"} {
		if err := s.SetProjectParent(slug, &parent); err != nil {
			t.Fatalf("SetProjectParent(%s): %v", slug, err)
		}
	}

	jira, slug, title, kind, vaultPath := "KOI-1099", "lookup-timeout", "Timeout de lookup", "bugfix", "koi-garden/KOI-1099-lookup-timeout"
	if _, err := s.UpsertTask(store.UpsertTaskParams{
		Project: "koi-garden", JiraKey: &jira, Slug: &slug, Title: &title, Kind: &kind, VaultPath: &vaultPath,
	}); err != nil {
		t.Fatalf("UpsertTask: %v", err)
	}
	return s
}

// ─── Profile contract ────────────────────────────────────────────────────────

// TestWorkspaceProfileDoesNotChangeProjectsProfile pins the promise the new
// profile was added under: nothing an existing caller loads changes. The
// overlap between the two profiles is deliberate, so it is asserted from the
// projects side, where a leak would break a caller that never asked for the
// workspace.
func TestWorkspaceProfileDoesNotChangeProjectsProfile(t *testing.T) {
	projects := ResolveTools("projects")
	expected := []string{
		"mem_project_card", "mem_project_upsert", "mem_task_upsert", "mem_task_list",
		"mem_task_link", "mem_evidence_add", "mem_evidence_list", "mem_runbook_index_sync",
		"mem_runbook_find", "mem_context_pack",
	}
	if len(projects) != len(expected) {
		t.Fatalf("projects profile holds %d tools, want %d: %v", len(projects), len(expected), projects)
	}
	for _, name := range expected {
		if !projects[name] {
			t.Errorf("projects profile lost %q", name)
		}
	}

	// The recommended set is the three profiles together, and their overlap is
	// counted once.
	recommended := ResolveTools("agent,projects,workspace")
	if len(recommended) != 35 {
		t.Fatalf("agent,projects,workspace resolves to %d tools, want 35", len(recommended))
	}
}

// TestWorkspaceProfileRegistersSevenTools pins what the profile adds to the
// registry and that asking for it alone still brings the three project tools a
// workspace session cannot work without.
func TestWorkspaceProfileRegistersSevenTools(t *testing.T) {
	added := []string{
		"mem_project_tree", "mem_evidence_scan", "mem_benchmark_add", "mem_benchmark_list",
		"mem_benchmark_import", "mem_vault_sync", "mem_workspace_search",
	}
	borrowed := []string{"mem_project_card", "mem_task_list", "mem_context_pack"}

	profile := ResolveTools("workspace")
	if len(profile) != len(added)+len(borrowed) {
		t.Fatalf("workspace profile holds %d tools, want %d: %v", len(profile), len(added)+len(borrowed), profile)
	}
	for _, name := range append(append([]string{}, added...), borrowed...) {
		if !profile[name] {
			t.Errorf("workspace profile is missing %q", name)
		}
	}

	s := newMCPTestStore(t)
	tools := NewServerWithTools(s, profile).ListTools()
	if len(tools) != len(profile) {
		t.Fatalf("registered %d tools for the workspace profile, want %d", len(tools), len(profile))
	}
	for _, name := range added {
		if tools[name] == nil {
			t.Errorf("tool %q was not registered", name)
		}
	}
	// Every workspace tool but the search is deferred: they are the deliberate
	// steps of a reorganisation, asked for by name.
	if tools["mem_workspace_search"] == nil {
		t.Fatal("mem_workspace_search was not registered")
	}
}

// ─── mem_project_tree ────────────────────────────────────────────────────────

func TestProjectTreeWalksTheSubtree(t *testing.T) {
	s := seedWorkspaceStore(t)
	h := handleProjectTree(s)

	envelope, res := callProjectEnvelope(t, h, map[string]any{"root": "koi-garden", "include_counts": true})
	if res.IsError {
		t.Fatalf("mem_project_tree failed: %#v", envelope)
	}
	nodes := saveData(t, envelope)["nodes"].([]any)
	if len(nodes) != 3 {
		t.Fatalf("nodes = %d, want the umbrella and its two ponds", len(nodes))
	}
	first := nodes[0].(map[string]any)
	if first["slug"] != "koi-garden" || first["children"].(float64) != 2 {
		t.Fatalf("first node = %#v, want koi-garden with two children", first)
	}
	if _, ok := first["counts"].(map[string]any); !ok {
		t.Fatalf("include_counts did not add counts: %#v", first)
	}

	// depth counts from the root the caller asked about.
	shallow, res := callProjectEnvelope(t, h, map[string]any{"root": "koi-garden", "depth": float64(1)})
	if res.IsError {
		t.Fatalf("mem_project_tree(depth=1) failed: %#v", shallow)
	}
	if got := len(saveData(t, shallow)["nodes"].([]any)); got != 1 {
		t.Fatalf("depth=1 returned %d nodes, want only the root", got)
	}
}

func TestProjectTreeUnknownRootIsTyped(t *testing.T) {
	s := seedWorkspaceStore(t)
	envelope, res := callProjectEnvelope(t, handleProjectTree(s), map[string]any{"root": "no-such-project"})
	if !res.IsError || envelope["code"] != "unknown_project" {
		t.Fatalf("expected a typed unknown_project error, got %#v", envelope)
	}
}

// ─── mem_evidence_scan ───────────────────────────────────────────────────────

func TestEvidenceScanDryRunsByDefault(t *testing.T) {
	newToolVault(t)
	s := seedWorkspaceStore(t)

	envelope, res := callProjectEnvelope(t, handleEvidenceScan(s), map[string]any{"task": "KOI-1099"})
	if res.IsError {
		t.Fatalf("mem_evidence_scan failed: %#v", envelope)
	}
	data := saveData(t, envelope)
	if data["dry_run"] != true {
		t.Fatalf("dry_run = %#v, want true by default", data["dry_run"])
	}
	if data["added"].(float64) < 1 {
		t.Fatalf("added = %#v, want the files under the task folder", data["added"])
	}
	if len(data["benchmark_candidates"].([]any)) != 1 {
		t.Fatalf("benchmark_candidates = %#v, want the one run", data["benchmark_candidates"])
	}

	// A dry run wrote nothing.
	page, _, err := s.ListEvidencePage("koi-garden", store.EvidenceListFilter{Limit: 50})
	if err != nil {
		t.Fatalf("ListEvidencePage: %v", err)
	}
	if page.Total != 0 {
		t.Fatalf("a dry run registered %d evidence rows", page.Total)
	}
}

func TestEvidenceScanUnknownTaskIsTyped(t *testing.T) {
	newToolVault(t)
	s := seedWorkspaceStore(t)

	envelope, res := callProjectEnvelope(t, handleEvidenceScan(s), map[string]any{"task": "KOI-9999"})
	if !res.IsError || envelope["code"] != "unknown_task" {
		t.Fatalf("expected a typed unknown_task error, got %#v", envelope)
	}
}

// ─── mem_benchmark_add / mem_benchmark_list ──────────────────────────────────

func TestBenchmarkAddRecordsABaseline(t *testing.T) {
	s := seedWorkspaceStore(t)
	h := handleBenchmarkAdd(s)

	envelope, res := callProjectEnvelope(t, h, map[string]any{
		"task": "KOI-1099", "name": "lookup", "metric": "lookup.p95", "unit": "ms",
		"value": float64(1512), "baseline": true, "captured_at": "2026-08-20 10:00:00",
	})
	if res.IsError {
		t.Fatalf("mem_benchmark_add failed: %#v", envelope)
	}
	data := saveData(t, envelope)
	if data["created"] != true {
		t.Fatalf("created = %#v, want true", data["created"])
	}
	benchmark := data["benchmark"].(map[string]any)
	if benchmark["direction"] != "lower" || benchmark["baseline"] != true {
		t.Fatalf("benchmark = %#v, want a lower-is-better baseline", benchmark)
	}

	// The same measurement twice is one number, and saying so is the point.
	dup, res := callProjectEnvelope(t, h, map[string]any{
		"task": "KOI-1099", "name": "lookup", "metric": "lookup.p95", "unit": "ms",
		"value": float64(1512), "captured_at": "2026-08-20 10:00:00",
	})
	if !res.IsError || dup["code"] != "duplicate_benchmark" {
		t.Fatalf("expected a typed duplicate_benchmark error, got %#v", dup)
	}

	listed, res := callProjectEnvelope(t, handleBenchmarkList(s, MCPConfig{}), map[string]any{"task": "KOI-1099"})
	if res.IsError {
		t.Fatalf("mem_benchmark_list failed: %#v", listed)
	}
	items := saveData(t, listed)["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("items = %#v, want the one measurement", items)
	}
}

func TestBenchmarkAddRejectsAUnitNothingCompares(t *testing.T) {
	s := seedWorkspaceStore(t)
	envelope, res := callProjectEnvelope(t, handleBenchmarkAdd(s), map[string]any{
		"task": "KOI-1099", "name": "lookup", "metric": "lookup.p95", "unit": "furlongs", "value": float64(1),
	})
	if !res.IsError || envelope["code"] != "invalid_enum" {
		t.Fatalf("expected a typed invalid_enum error, got %#v", envelope)
	}
}

func TestBenchmarkListUnknownTaskIsTyped(t *testing.T) {
	s := seedWorkspaceStore(t)
	envelope, res := callProjectEnvelope(t, handleBenchmarkList(s, MCPConfig{}), map[string]any{"task": "KOI-9999"})
	if !res.IsError || envelope["code"] != "unknown_task" {
		t.Fatalf("expected a typed unknown_task error, got %#v", envelope)
	}
}

// ─── mem_benchmark_import ────────────────────────────────────────────────────

func TestBenchmarkImportReadsAV1Run(t *testing.T) {
	root := newToolVault(t)
	s := seedWorkspaceStore(t)
	run := filepath.Join(root, "koi-garden", "KOI-1099-lookup-timeout", "benchmarks", "baseline-run1.json")

	envelope, res := callProjectEnvelope(t, handleBenchmarkImport(s), map[string]any{
		"task": "KOI-1099", "path": run,
	})
	if res.IsError {
		t.Fatalf("mem_benchmark_import failed: %#v", envelope)
	}
	data := saveData(t, envelope)
	if data["dry_run"] != true {
		t.Fatalf("dry_run = %#v, want true by default", data["dry_run"])
	}
	if data["format"] != "engram.benchmark.v1" {
		t.Fatalf("format = %#v, want the v1 marker", data["format"])
	}
	if data["imported"].(float64) != 1 {
		t.Fatalf("imported = %#v, want the one metric", data["imported"])
	}
}

func TestBenchmarkImportMissingRunIsTyped(t *testing.T) {
	newToolVault(t)
	s := seedWorkspaceStore(t)

	envelope, res := callProjectEnvelope(t, handleBenchmarkImport(s), map[string]any{
		"task": "KOI-1099", "path": filepath.Join(t.TempDir(), "nowhere.json"),
	})
	if !res.IsError || envelope["code"] != "run_not_found" {
		t.Fatalf("expected a typed run_not_found error, got %#v", envelope)
	}
}

func TestBenchmarkImportRejectsAPointerMapWithNoUnit(t *testing.T) {
	root := newToolVault(t)
	s := seedWorkspaceStore(t)
	run := filepath.Join(root, "koi-garden", "KOI-1099-lookup-timeout", "benchmarks", "baseline-run1.json")

	envelope, res := callProjectEnvelope(t, handleBenchmarkImport(s), map[string]any{
		"task": "KOI-1099", "path": run,
		"map": []any{map[string]any{"metric": "warm.resolve", "pointer": "/metrics/0/value"}},
	})
	if !res.IsError || envelope["code"] != "invalid_enum" {
		t.Fatalf("expected a typed invalid_enum error, got %#v", envelope)
	}
}

// ─── mem_vault_sync ──────────────────────────────────────────────────────────

func TestVaultSyncDryRunsByDefault(t *testing.T) {
	newToolVault(t)
	s := seedWorkspaceStore(t)

	envelope, res := callProjectEnvelope(t, handleVaultSync(s), map[string]any{})
	if res.IsError {
		t.Fatalf("mem_vault_sync failed: %#v", envelope)
	}
	data := saveData(t, envelope)
	if data["dry_run"] != true {
		t.Fatalf("dry_run = %#v, want true by default", data["dry_run"])
	}
	tasks := data["tasks"].([]any)
	if len(tasks) != 1 {
		t.Fatalf("tasks = %#v, want the one ticket in the vault", tasks)
	}
	task := tasks[0].(map[string]any)
	if task["jira_key"] != "KOI-1099" || task["state"] != "pending" {
		t.Fatalf("task = %#v, want KOI-1099 reported as pending", task)
	}
}

func TestVaultSyncUnresolvedRootIsTyped(t *testing.T) {
	s := seedWorkspaceStore(t)
	t.Setenv("ENGRAM_VAULT_ROOT", "")

	envelope, res := callProjectEnvelope(t, handleVaultSync(s), map[string]any{})
	if !res.IsError || envelope["code"] != "vault_root_unresolved" {
		t.Fatalf("expected a typed vault_root_unresolved error, got %#v", envelope)
	}
}

// ─── mem_workspace_search ────────────────────────────────────────────────────

func TestWorkspaceSearchAnswersAcrossKinds(t *testing.T) {
	s := seedWorkspaceStore(t)

	envelope, res := callProjectEnvelope(t, handleWorkspaceSearch(s), map[string]any{
		"query": "lookup", "project": "koi-garden",
	})
	if res.IsError {
		t.Fatalf("mem_workspace_search failed: %#v", envelope)
	}
	data := saveData(t, envelope)
	hits := data["hits"].([]any)
	if len(hits) == 0 {
		t.Fatalf("hits = %#v, want the task", hits)
	}
	if _, ok := data["totals"].(map[string]any); !ok {
		t.Fatalf("totals = %#v, want a per-kind count", data["totals"])
	}
}

func TestWorkspaceSearchRejectsAnUnknownKind(t *testing.T) {
	s := seedWorkspaceStore(t)
	envelope, res := callProjectEnvelope(t, handleWorkspaceSearch(s), map[string]any{
		"query": "lookup", "kinds": []any{"sonnet"},
	})
	if !res.IsError || envelope["code"] != "invalid_enum" {
		t.Fatalf("expected a typed invalid_enum error, got %#v", envelope)
	}
}

func TestWorkspaceSearchRejectsAOneCharacterQuery(t *testing.T) {
	s := seedWorkspaceStore(t)
	envelope, res := callProjectEnvelope(t, handleWorkspaceSearch(s), map[string]any{"query": "l"})
	if !res.IsError || envelope["code"] != "query_too_short" {
		t.Fatalf("expected a typed query_too_short error, got %#v", envelope)
	}
}
