package project

import (
	"strings"
	"testing"

	"github.com/HoracioEspinosa/engram/internal/store"
)

// TestContextPackCarriesBenchmarksAndHierarchy pins the two sections the pack
// grew: what the task measured, and where its project sits in the tree.
func TestContextPackCarriesBenchmarksAndHierarchy(t *testing.T) {
	s := newHierarchyPackTestStore(t)

	pack, rendered, err := BuildContextPack(s, "koi-garden-pond-01", "PROJ-1", DefaultContextPackOptions())
	if err != nil {
		t.Fatalf("BuildContextPack: %v", err)
	}

	if pack.Hierarchy == nil {
		t.Fatal("the pack must carry the project hierarchy")
	}
	if pack.Hierarchy["parent"] != "koi-garden" || pack.Hierarchy["depth"] != 1 {
		t.Fatalf("hierarchy = %#v", pack.Hierarchy)
	}
	if !strings.Contains(rendered, "**Jerarquía**") || !strings.Contains(rendered, "padre: koi-garden") {
		t.Fatalf("the rendered pack must show the hierarchy:\n%s", rendered)
	}

	if len(pack.Benchmarks) != 1 {
		t.Fatalf("expected one measurement, got %#v", pack.Benchmarks)
	}
	if pack.Benchmarks[0]["metric"] != "p95" || pack.Benchmarks[0]["value"] != 12.5 {
		t.Fatalf("benchmark = %#v", pack.Benchmarks[0])
	}
	if !strings.Contains(rendered, "**Mediciones (1)**") {
		t.Fatalf("the rendered pack must show the measurements:\n%s", rendered)
	}

	// A caller that names its sections still gets only those.
	opts := DefaultContextPackOptions()
	opts.Sections = []string{"header", "benchmarks"}
	narrow, narrowRendered, err := BuildContextPack(s, "koi-garden-pond-01", "PROJ-1", opts)
	if err != nil {
		t.Fatalf("BuildContextPack narrow: %v", err)
	}
	if narrow.Hierarchy != nil {
		t.Fatal("a section that was not asked for must not be rendered")
	}
	if !strings.Contains(narrowRendered, "**Mediciones") {
		t.Fatalf("benchmarks must survive an explicit section list:\n%s", narrowRendered)
	}
}

// TestSectionBudgetsAddUpToOne pins the property that makes the budget a
// budget: a pack that fills every section fills MaxChars exactly once.
func TestSectionBudgetsAddUpToOne(t *testing.T) {
	total := 0.0
	for _, sec := range canonicalSections {
		pct, ok := sectionBudgetPct[sec]
		if !ok {
			t.Fatalf("section %q has no budget", sec)
		}
		total += pct
	}
	if total < 0.999 || total > 1.001 {
		t.Fatalf("section budgets add up to %.3f, want 1", total)
	}
}

func newHierarchyPackTestStore(t *testing.T) *store.Store {
	t.Helper()
	cfg, err := store.DefaultConfig()
	if err != nil {
		t.Fatalf("DefaultConfig: %v", err)
	}
	cfg.DataDir = t.TempDir()
	s, err := store.New(cfg)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	for _, slug := range []string{"koi-garden", "koi-garden-pond-01"} {
		if _, _, err := s.UpsertProjectCard(store.UpsertProjectCardParams{Slug: slug}); err != nil {
			t.Fatalf("UpsertProjectCard %s: %v", slug, err)
		}
	}
	parent := "koi-garden"
	if err := s.SetProjectParent("koi-garden-pond-01", &parent); err != nil {
		t.Fatalf("SetProjectParent: %v", err)
	}

	key := "PROJ-1"
	title := "drain the pond"
	kind := "incident"
	task, err := s.UpsertTask(store.UpsertTaskParams{Project: "koi-garden-pond-01", JiraKey: &key, Title: &title, Kind: &kind})
	if err != nil {
		t.Fatalf("UpsertTask: %v", err)
	}
	if _, err := s.AddBenchmark(store.AddBenchmarkParams{
		Task: task.Task, Name: "drain", Metric: "p95", Unit: "ms", Value: 12.5, Baseline: true,
	}); err != nil {
		t.Fatalf("AddBenchmark: %v", err)
	}
	return s
}
