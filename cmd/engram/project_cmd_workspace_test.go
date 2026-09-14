package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/HoracioEspinosa/engram/internal/store"
	"github.com/HoracioEspinosa/engram/internal/workspace"
)

// seedWorkspaceVault builds the smallest knowledge tree the workspace
// subcommands need: one project, one ticketed task with evidence and a
// benchmark run, and the root README's task map that carries the states.
func seedWorkspaceVault(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	write := func(rel, content string) {
		t.Helper()
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}

	write("README.md", `# Vault

## Mapa de tareas

| Tarea | Qué es | Estado | Archivos |
| --- | --- | --- | --- |
| [Timeout de lookup](./koi-garden/KOI-1099-lookup-timeout/README.md) | Mitigar el timeout | Con pendientes | 2 |
`)
	write("koi-garden/README.md", "# koi-garden\n\nPanel de un estanque.\n")
	write("koi-garden/KOI-1099-lookup-timeout/README.md",
		"# KOI-1099 — Timeout de lookup\n\n**Estado:** Con pendientes\n\nEl lookup falla con timeout.\n")
	write("koi-garden/KOI-1099-lookup-timeout/analysis/hipotesis.md", "# Hipótesis\n\nSin timeout explícito.\n")
	write("koi-garden/KOI-1099-lookup-timeout/evidences/01-timeout/captura.png", "\x89PNG timeout")
	write("koi-garden/KOI-1099-lookup-timeout/benchmarks/baseline-run1.json", `{
  "engram_benchmark": "v1",
  "task": "KOI-1099",
  "name": "lookup-timeout",
  "captured_at": "2026-08-20T10:00:00Z",
  "baseline": true,
  "metrics": [ { "metric": "lookup.p95", "unit": "ms", "value": 1512 } ]
}`)
	return root
}

// seedWorkspaceObservation records one memory under a project. The session is
// created first because an observation is a row of one, and the foreign key
// says so.
func seedWorkspaceObservation(t *testing.T, cfg store.Config, project, title, content string) {
	t.Helper()
	s := openTestStore(t, cfg)
	sessionID := "sess-" + project
	if err := s.CreateSession(sessionID, project, t.TempDir()); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if _, err := s.AddObservation(store.AddObservationParams{
		SessionID: sessionID, Type: "discovery", Title: title, Content: content, Project: project,
	}); err != nil {
		t.Fatalf("AddObservation: %v", err)
	}
}

// TestProjectTreeJSONOutput pins the tree's envelope: the same
// {project, project_source, project_path, result} shape every other
// subcommand prints, with the nodes in preorder and each one carrying its
// depth.
func TestProjectTreeJSONOutput(t *testing.T) {
	cfg := testConfig(t)
	stubExit(t)
	stubDetection(t, "koi-garden", t.TempDir())
	seedCard(t, cfg, "koi-garden")
	seedCard(t, cfg, "koi-garden-pond-01")

	runProject(t, cfg, "set-parent", "koi-garden-pond-01", "--to", "koi-garden")

	stdout, _ := runProject(t, cfg, "tree", "koi-garden", "--counts", "--json")
	envelope := decodeEnvelope(t, stdout)
	if envelope["project"] != "koi-garden" {
		t.Fatalf("envelope project = %v, want koi-garden", envelope["project"])
	}
	result, ok := envelope["result"].(map[string]any)
	if !ok {
		t.Fatalf("envelope has no result object: %s", stdout)
	}
	nodes, ok := result["nodes"].([]any)
	if !ok || len(nodes) != 2 {
		t.Fatalf("result.nodes = %v, want the root and its instance", result["nodes"])
	}
	first := nodes[0].(map[string]any)
	second := nodes[1].(map[string]any)
	if first["slug"] != "koi-garden" || second["slug"] != "koi-garden-pond-01" {
		t.Fatalf("nodes are not in preorder: %v then %v", first["slug"], second["slug"])
	}
	if second["depth"].(float64) != 1 {
		t.Errorf("the child sits at depth %v, want 1", second["depth"])
	}
	if _, ok := second["counts"]; !ok {
		t.Error("--counts did not include the counters")
	}
}

// TestSetParentRejectsCycleWithTypedCode pins that a move closing a cycle is
// refused under a code a script can branch on, not as prose.
func TestSetParentRejectsCycleWithTypedCode(t *testing.T) {
	cfg := testConfig(t)
	exited := stubExit(t)
	stubDetection(t, "koi-garden", t.TempDir())
	seedCard(t, cfg, "koi-garden")
	seedCard(t, cfg, "koi-garden-pond-01")

	runProject(t, cfg, "set-parent", "koi-garden-pond-01", "--to", "koi-garden")
	stdout, _ := runProject(t, cfg, "set-parent", "koi-garden", "--to", "koi-garden-pond-01", "--json")

	if code := decodeErrorCode(t, stdout); code != "project_cycle" {
		t.Fatalf("code = %q, want project_cycle", code)
	}
	if !*exited {
		t.Error("a refused move must exit non-zero")
	}
}

// TestAliasAddRejectsOwnerWithHint pins that a name holding memories of its
// own cannot be folded into an alias, and that the refusal names the command
// that would do the job properly.
func TestAliasAddRejectsOwnerWithHint(t *testing.T) {
	cfg := testConfig(t)
	stubExit(t)
	stubDetection(t, "koi-garden", t.TempDir())
	seedCard(t, cfg, "koi-garden")

	// The alias-to-be is a project in its own right: it has a card and it
	// holds memories. That is exactly the case an alias must not swallow.
	seedCard(t, cfg, "koi-pond")
	seedWorkspaceObservation(t, cfg, "koi-pond", "una memoria", "vive bajo koi-pond")

	stdout, _ := runProject(t, cfg, "alias", "add", "koi-pond", "--to", "koi-garden", "--json")
	envelope := decodeEnvelope(t, stdout)
	if envelope["code"] != "alias_owns_rows" {
		t.Fatalf("code = %v, want alias_owns_rows", envelope["code"])
	}
	hint, _ := envelope["hint"].(string)
	if !strings.Contains(hint, "engram projects merge") {
		t.Fatalf("hint = %q, want it to point at the merge command", hint)
	}
}

// TestAliasAddThenListResolvesTheName pins the happy path: a declared alias
// shows up in the listing and resolves to its project.
func TestAliasAddThenListResolvesTheName(t *testing.T) {
	cfg := testConfig(t)
	stubExit(t)
	stubDetection(t, "koi-garden", t.TempDir())
	seedCard(t, cfg, "koi-garden")

	stdout, _ := runProject(t, cfg, "alias", "add", "koi_garden", "--to", "koi-garden", "--json")
	envelope := decodeEnvelope(t, stdout)
	result := envelope["result"].(map[string]any)
	resolved := result["resolved"].(map[string]any)
	if resolved["slug"] != "koi-garden" || resolved["via"] != store.ProjectResolvedViaAlias {
		t.Fatalf("resolution = %v, want koi-garden via alias", resolved)
	}

	stdout, _ = runProject(t, cfg, "alias", "list", "koi-garden", "--json")
	aliases := decodeEnvelope(t, stdout)["result"].(map[string]any)["aliases"].([]any)
	if len(aliases) != 1 {
		t.Fatalf("listed %v, want the one alias", aliases)
	}
}

// TestBenchAddListShowsDelta pins that two measurements of one metric are
// listed against their baseline, with the change rendered as a percentage.
func TestBenchAddListShowsDelta(t *testing.T) {
	cfg := testConfig(t)
	stubExit(t)
	stubDetection(t, "koi-garden", t.TempDir())
	seedCard(t, cfg, "koi-garden")
	seedTask(t, cfg, "koi-garden", "KOI-1099", "Timeout de lookup", "incident")

	runProject(t, cfg, "bench", "add", "KOI-1099", "--name", "lookup", "--metric", "lookup.p95",
		"--unit", "ms", "--value", "1512", "--baseline", "--captured-at", "2026-08-20T10:00:00Z")
	runProject(t, cfg, "bench", "add", "KOI-1099", "--name", "lookup", "--metric", "lookup.p95",
		"--unit", "ms", "--value", "756", "--captured-at", "2026-08-21T10:00:00Z")

	stdout, _ := runProject(t, cfg, "bench", "list", "KOI-1099", "--json")
	page := decodeEnvelope(t, stdout)["result"].(map[string]any)
	if page["total"].(float64) != 2 {
		t.Fatalf("total = %v, want 2", page["total"])
	}
	items := page["items"].([]any)
	newest := items[0].(map[string]any)
	if newest["value"].(float64) != 756 {
		t.Fatalf("newest measurement = %v, want 756", newest["value"])
	}
	if newest["baseline_value"].(float64) != 1512 {
		t.Fatalf("baseline = %v, want 1512", newest["baseline_value"])
	}
	if delta := newest["delta_pct"].(float64); delta >= 0 {
		t.Fatalf("delta = %v, want a drop against the baseline", delta)
	}

	text, _ := runProject(t, cfg, "bench", "list", "KOI-1099")
	if !strings.Contains(text, "better") {
		t.Fatalf("the text rendering does not say the metric improved:\n%s", text)
	}
}

// TestEvidenceScanDryRunReportsPlan pins that a scan is a plan until asked
// otherwise: it reports what it found and writes nothing.
func TestEvidenceScanDryRunReportsPlan(t *testing.T) {
	cfg := testConfig(t)
	stubExit(t)
	root := seedWorkspaceVault(t)
	t.Setenv(workspace.VaultRootEnv, root)
	stubDetection(t, "koi-garden", t.TempDir())
	seedCard(t, cfg, "koi-garden")
	task := seedTask(t, cfg, "koi-garden", "KOI-1099", "Timeout de lookup", "incident")

	stdout, _ := runProject(t, cfg, "evidence", "scan", "KOI-1099", "--json")
	report := decodeEnvelope(t, stdout)["result"].(map[string]any)
	if !report["dry_run"].(bool) {
		t.Fatal("the report does not declare itself a dry run")
	}
	if report["added"].(float64) != 3 {
		t.Fatalf("planned %v additions, want 3", report["added"])
	}
	candidates := report["benchmark_candidates"].([]any)
	if len(candidates) != 1 {
		t.Fatalf("benchmark candidates = %v, want the one run", candidates)
	}

	s := openTestStore(t, cfg)
	_, total, _, err := s.ListEvidence("koi-garden", store.EvidenceListFilter{TaskSyncID: task.SyncID})
	if err != nil {
		t.Fatalf("ListEvidence: %v", err)
	}
	if total != 0 {
		t.Fatalf("the dry run wrote %d evidence rows", total)
	}

	runProject(t, cfg, "evidence", "scan", "KOI-1099", "--apply", "--json")
	_, total, _, err = s.ListEvidence("koi-garden", store.EvidenceListFilter{TaskSyncID: task.SyncID})
	if err != nil {
		t.Fatalf("ListEvidence: %v", err)
	}
	if total != 3 {
		t.Fatalf("--apply registered %d evidence rows, want 3", total)
	}
}

// TestImportVaultDryRunWritesNothing pins the same rule for the whole-vault
// import, including that the plan already knows the task's state.
func TestImportVaultDryRunWritesNothing(t *testing.T) {
	cfg := testConfig(t)
	stubExit(t)
	root := seedWorkspaceVault(t)
	stubDetection(t, "koi-garden", t.TempDir())

	stdout, _ := runProject(t, cfg, "import-vault", root, "--json")
	plan := decodeEnvelope(t, stdout)["result"].(map[string]any)
	if !plan["dry_run"].(bool) {
		t.Fatal("the plan does not declare itself a dry run")
	}
	tasks := plan["tasks"].([]any)
	if len(tasks) != 1 {
		t.Fatalf("planned %v, want the one task", tasks)
	}
	first := tasks[0].(map[string]any)
	if first["action"] != "create" || first["state"] != "pending" {
		t.Fatalf("task action/state = %v/%v, want create/pending", first["action"], first["state"])
	}

	s := openTestStore(t, cfg)
	page, err := s.ListTasksPage("koi-garden", store.TaskListFilter{})
	if err != nil {
		t.Fatalf("ListTasksPage: %v", err)
	}
	if page.Total != 0 {
		t.Fatalf("the dry run wrote %d tasks", page.Total)
	}
}

// TestImportVaultApplyRegistersEverything pins that --apply writes the tasks,
// the evidence and the measurements in one pass.
func TestImportVaultApplyRegistersEverything(t *testing.T) {
	cfg := testConfig(t)
	stubExit(t)
	root := seedWorkspaceVault(t)
	stubDetection(t, "koi-garden", t.TempDir())

	stdout, _ := runProject(t, cfg, "import-vault", root, "--apply", "--json")
	plan := decodeEnvelope(t, stdout)["result"].(map[string]any)
	if plan["dry_run"].(bool) {
		t.Fatal("--apply still reported a dry run")
	}

	s := openTestStore(t, cfg)
	page, err := s.ListTasksPage("koi-garden", store.TaskListFilter{States: []string{"pending"}})
	if err != nil {
		t.Fatalf("ListTasksPage: %v", err)
	}
	if page.Total != 1 {
		t.Fatalf("store holds %d pending tasks, want 1", page.Total)
	}
	benchmarks, err := s.ListBenchmarks(store.BenchmarkListFilter{Project: "koi-garden"})
	if err != nil {
		t.Fatalf("ListBenchmarks: %v", err)
	}
	if benchmarks.Total != 1 {
		t.Fatalf("store holds %d measurements, want 1", benchmarks.Total)
	}
}

// TestProjectSearchReturnsKinds pins that one query reaches across the
// workspace and reports, per kind, how many there were before the cap.
func TestProjectSearchReturnsKinds(t *testing.T) {
	cfg := testConfig(t)
	stubExit(t)
	stubDetection(t, "koi-garden", t.TempDir())
	seedCard(t, cfg, "koi-garden")
	seedTask(t, cfg, "koi-garden", "KOI-1099", "Timeout de lookup en el estanque", "incident")

	seedWorkspaceObservation(t, cfg, "koi-garden",
		"El lookup agota su timeout", "El cliente no fija un timeout explícito")

	stdout, _ := runProject(t, cfg, "search", "timeout", "--project", "koi-garden", "--json")
	results := decodeEnvelope(t, stdout)["result"].(map[string]any)
	hits := results["hits"].([]any)
	if len(hits) < 2 {
		t.Fatalf("hits = %v, want at least the task and the observation", hits)
	}
	kinds := map[string]bool{}
	for _, hit := range hits {
		kinds[hit.(map[string]any)["kind"].(string)] = true
	}
	if !kinds[store.WorkspaceKindTask] || !kinds[store.WorkspaceKindObservation] {
		t.Fatalf("kinds found = %v, want a task and an observation", kinds)
	}
	if _, ok := results["totals"].(map[string]any); !ok {
		t.Fatal("the result carries no per-kind totals")
	}
}

// TestUpsertAcceptsHierarchyFlags pins that the card's own columns and its
// place in the tree can be set in one call, and that the alias declared
// alongside them resolves afterwards.
func TestUpsertAcceptsHierarchyFlags(t *testing.T) {
	cfg := testConfig(t)
	stubExit(t)
	stubDetection(t, "koi-garden", t.TempDir())
	seedCard(t, cfg, "koi-garden")
	seedCard(t, cfg, "koi-garden-pond-01")

	stdout, stderr := runProject(t, cfg, "koi-garden-pond-01", "upsert",
		"--parent", "koi-garden", "--kind", "instance",
		"--description", "Instancia de trabajo", "--icon", "pond", "--color", "primary",
		"--tag", "instancia", "--tag", "pond", "--alias", "pond01", "--json")
	if stderr != "" {
		t.Fatalf("upsert wrote to stderr: %s", stderr)
	}

	card := decodeEnvelope(t, stdout)["result"].(map[string]any)["card"].(map[string]any)
	if card["parent_slug"] != "koi-garden" {
		t.Fatalf("parent_slug = %v, want koi-garden", card["parent_slug"])
	}
	if card["depth"].(float64) != 1 {
		t.Fatalf("depth = %v, want 1", card["depth"])
	}
	if card["kind"] != "instance" {
		t.Fatalf("kind = %v, want instance", card["kind"])
	}
	if card["tags"] != `["instancia","pond"]` {
		t.Fatalf("tags = %v, want a JSON array of the two tags", card["tags"])
	}

	s := openTestStore(t, cfg)
	resolution, err := s.ResolveProjectSlug("pond01")
	if err != nil {
		t.Fatalf("ResolveProjectSlug: %v", err)
	}
	if resolution.Slug != "koi-garden-pond-01" {
		t.Fatalf("pond01 resolves to %q, want koi-garden-pond-01", resolution.Slug)
	}
}

// TestTreeSuggestNeverApplies pins the rule the suggestion exists under: it
// proposes, and the tree is untouched until somebody applies it.
func TestTreeSuggestNeverApplies(t *testing.T) {
	cfg := testConfig(t)
	stubExit(t)
	stubDetection(t, "nextcloud-00", t.TempDir())
	for _, slug := range []string{"nextcloud-00", "nextcloud-01", "nextcloud-02"} {
		seedCard(t, cfg, slug)
	}

	stdout, _ := runProject(t, cfg, "tree", "suggest", "--json")
	suggestions := decodeEnvelope(t, stdout)["result"].(map[string]any)["suggestions"].([]any)
	if len(suggestions) != 1 {
		t.Fatalf("suggestions = %v, want the one family", suggestions)
	}

	s := openTestStore(t, cfg)
	for _, slug := range []string{"nextcloud-00", "nextcloud-01", "nextcloud-02"} {
		card, err := s.GetProjectCard(slug)
		if err != nil {
			t.Fatalf("GetProjectCard(%s): %v", slug, err)
		}
		if card.ParentSlug != nil {
			t.Fatalf("%s was reparented by a suggestion", slug)
		}
	}

	runProject(t, cfg, "tree", "apply", "--from-suggest", "--yes", "--json")
	card, err := s.GetProjectCard("nextcloud-01")
	if err != nil {
		t.Fatalf("GetProjectCard: %v", err)
	}
	if card.ParentSlug == nil || *card.ParentSlug != "nextcloud" {
		t.Fatalf("after the apply nextcloud-01 sits under %v, want nextcloud", card.ParentSlug)
	}
}

// TestTreeSuggestReportsSeparatorPairs pins the second half of the envelope:
// the projects that are one project written more than one way. The tree cannot
// join them back, so naming them is all the command does about it.
func TestTreeSuggestReportsSeparatorPairs(t *testing.T) {
	cfg := testConfig(t)
	stubExit(t)
	stubDetection(t, "koi-garden", t.TempDir())
	for _, slug := range []string{"koi-garden", "koi_garden"} {
		seedCard(t, cfg, slug)
	}

	stdout, _ := runProject(t, cfg, "tree", "suggest", "--json")
	result := decodeEnvelope(t, stdout)["result"].(map[string]any)
	pairs, ok := result["separator_pairs"].([]any)
	if !ok || len(pairs) != 1 {
		t.Fatalf("separator_pairs = %v, want the one family spelled two ways", result["separator_pairs"])
	}
	pair := pairs[0].(map[string]any)
	if pair["folded"] != "koi-garden" {
		t.Fatalf("folded = %v, want koi-garden", pair["folded"])
	}
	if names := pair["names"].([]any); len(names) != 2 {
		t.Fatalf("names = %v, want both spellings", names)
	}
}

// TestTreeDoctorReportsAConsistentTree pins the doctor's quiet answer, which
// is the one it gives most of the time.
func TestTreeDoctorReportsAConsistentTree(t *testing.T) {
	cfg := testConfig(t)
	stubExit(t)
	stubDetection(t, "koi-garden", t.TempDir())
	seedCard(t, cfg, "koi-garden")

	stdout, _ := runProject(t, cfg, "tree", "doctor", "--json")
	faults := decodeEnvelope(t, stdout)["result"].(map[string]any)["faults"].([]any)
	if len(faults) != 0 {
		t.Fatalf("faults = %v, want none", faults)
	}
}

// TestGraphCheckStampsTheCard pins that the check records its verdict where
// the card and the TUI read it from, rather than only printing it.
func TestGraphCheckStampsTheCard(t *testing.T) {
	cfg := testConfig(t)
	stubExit(t)
	stubDetection(t, "koi-garden", t.TempDir())
	seedCard(t, cfg, "koi-garden")

	stdout, _ := runProject(t, cfg, "koi-garden", "graph", "check", "--repo-dir", t.TempDir(), "--json")
	graph := decodeEnvelope(t, stdout)["result"].(map[string]any)["graph"].(map[string]any)
	if graph["reason"] != "no_graph" {
		t.Fatalf("reason = %v, want no_graph", graph["reason"])
	}

	s := openTestStore(t, cfg)
	card, err := s.GetProjectCard("koi-garden")
	if err != nil {
		t.Fatalf("GetProjectCard: %v", err)
	}
	if card.GraphStaleReason == nil || *card.GraphStaleReason != "no_graph" {
		t.Fatalf("card records %v, want the verdict stamped", card.GraphStaleReason)
	}
}

// TestBenchImportDryRunWritesNothing pins that reading a run file into
// measurements is a plan until --apply.
func TestBenchImportDryRunWritesNothing(t *testing.T) {
	cfg := testConfig(t)
	stubExit(t)
	root := seedWorkspaceVault(t)
	t.Setenv(workspace.VaultRootEnv, root)
	stubDetection(t, "koi-garden", t.TempDir())
	seedCard(t, cfg, "koi-garden")
	seedTask(t, cfg, "koi-garden", "KOI-1099", "Timeout de lookup", "incident")
	run := filepath.Join(root, "koi-garden", "KOI-1099-lookup-timeout", "benchmarks", "baseline-run1.json")

	stdout, _ := runProject(t, cfg, "bench", "import", "KOI-1099", run, "--json")
	report := decodeEnvelope(t, stdout)["result"].(map[string]any)
	if report["imported"].(float64) != 1 {
		t.Fatalf("planned %v measurements, want 1", report["imported"])
	}

	s := openTestStore(t, cfg)
	page, err := s.ListBenchmarks(store.BenchmarkListFilter{Project: "koi-garden"})
	if err != nil {
		t.Fatalf("ListBenchmarks: %v", err)
	}
	if page.Total != 0 {
		t.Fatalf("the dry run wrote %d measurements", page.Total)
	}

	runProject(t, cfg, "bench", "import", "KOI-1099", run, "--apply", "--json")
	page, err = s.ListBenchmarks(store.BenchmarkListFilter{Project: "koi-garden"})
	if err != nil {
		t.Fatalf("ListBenchmarks: %v", err)
	}
	if page.Total != 1 {
		t.Fatalf("--apply recorded %d measurements, want 1", page.Total)
	}
}

// TestEvidenceScanUnknownTaskCarriesItsCode pins that the workspace failures
// reach the JSON envelope under the code they were declared with.
func TestEvidenceScanUnknownTaskCarriesItsCode(t *testing.T) {
	cfg := testConfig(t)
	stubExit(t)
	root := seedWorkspaceVault(t)
	t.Setenv(workspace.VaultRootEnv, root)
	stubDetection(t, "koi-garden", t.TempDir())
	seedCard(t, cfg, "koi-garden")

	stdout, _ := runProject(t, cfg, "evidence", "scan", "KOI-4242", "--json")
	if code := decodeErrorCode(t, stdout); code != "unknown_task" {
		t.Fatalf("code = %q, want unknown_task", code)
	}
}
