package mcp

import (
	"testing"

	"github.com/HoracioEspinosa/engram/internal/store"
)

// TestTaskUpsertWritesVaultFields pins the round trip of the fields a task
// grew for the vault: the short name, the prose, what is still missing, where
// its files live, and the task it hangs from.
func TestTaskUpsertWritesVaultFields(t *testing.T) {
	s := newMCPTestStore(t)
	if err := s.EnrollProject("koi-garden"); err != nil {
		t.Fatalf("enroll: %v", err)
	}
	upsert := handleTaskUpsert(s, MCPConfig{})

	parent, res := callProjectEnvelope(t, upsert, map[string]any{
		"project": "koi-garden", "jira_key": "PROJ-1", "title": "drain the pond", "kind": "incident",
		"slug": "drain-pond", "summary": "the pond has to be drained before the filter is replaced",
		"pending_note": "waiting for the pump", "vault_path": "Work/Koi/Drain",
	})
	if res.IsError {
		t.Fatalf("upsert failed: %#v", parent)
	}
	task, _ := saveData(t, parent)["task"].(map[string]any)
	if task["slug"] != "drain-pond" || task["vault_path"] != "Work/Koi/Drain" || task["pending_note"] != "waiting for the pump" {
		t.Fatalf("vault fields did not round trip: %#v", task)
	}

	child, res := callProjectEnvelope(t, upsert, map[string]any{
		"project": "koi-garden", "jira_key": "PROJ-2", "title": "order the pump", "kind": "feature",
		"parent_task": "PROJ-1", "state": "pending",
	})
	if res.IsError {
		t.Fatalf("child upsert failed: %#v", child)
	}
	childTask, _ := saveData(t, child)["task"].(map[string]any)
	if childTask["parent_task_sync_id"] != task["sync_id"] {
		t.Fatalf("expected the child under its parent, got %#v", childTask["parent_task_sync_id"])
	}
	if childTask["state"] != "pending" {
		t.Fatalf("the state enum must accept pending, got %#v", childTask["state"])
	}

	unknownParent, res := callProjectEnvelope(t, upsert, map[string]any{
		"project": "koi-garden", "jira_key": "PROJ-3", "title": "t", "kind": "feature", "parent_task": "PROJ-999",
	})
	if !res.IsError || unknownParent["code"] != "unknown_task" {
		t.Fatalf("expected a typed unknown_task error, got %#v", unknownParent)
	}
}

// TestTaskAndEvidenceListingsCoverTheSubtree pins include_children on both
// listings: the subtree is one question, and the answer says which projects it
// covered.
func TestTaskAndEvidenceListingsCoverTheSubtree(t *testing.T) {
	s := newMCPTestStore(t)
	for _, slug := range []string{"koi-garden", "koi-garden-pond-01"} {
		if err := s.EnrollProject(slug); err != nil {
			t.Fatalf("enroll %s: %v", slug, err)
		}
		if _, _, err := s.UpsertProjectCard(store.UpsertProjectCardParams{Slug: slug}); err != nil {
			t.Fatalf("UpsertProjectCard %s: %v", slug, err)
		}
	}
	parent := "koi-garden"
	if err := s.SetProjectParent("koi-garden-pond-01", &parent); err != nil {
		t.Fatalf("SetProjectParent: %v", err)
	}

	upsert := handleTaskUpsert(s, MCPConfig{})
	for i, slug := range []string{"koi-garden", "koi-garden-pond-01"} {
		key := []string{"PROJ-1", "PROJ-2"}[i]
		if _, res := callProjectEnvelope(t, upsert, map[string]any{
			"project": slug, "jira_key": key, "title": "drain the pond", "kind": "incident",
		}); res.IsError {
			t.Fatalf("seeding %s failed", slug)
		}
	}

	list := handleTaskList(s, MCPConfig{})
	own, _ := callProjectEnvelope(t, list, map[string]any{"project": "koi-garden"})
	if total := saveData(t, own)["total"]; total != float64(1) {
		t.Fatalf("the parent alone has one task, got %#v", total)
	}
	subtree, _ := callProjectEnvelope(t, list, map[string]any{"project": "koi-garden", "include_children": true})
	data := saveData(t, subtree)
	if data["total"] != float64(2) {
		t.Fatalf("the subtree has two tasks, got %#v", data["total"])
	}
	projects, _ := data["projects"].([]any)
	if len(projects) != 2 {
		t.Fatalf("expected the covered projects listed, got %#v", data["projects"])
	}

	// match_mode widens a two-token query the default would find nothing for.
	narrow, _ := callProjectEnvelope(t, list, map[string]any{"project": "koi-garden", "query": "drain filter"})
	if saveData(t, narrow)["total"] != float64(0) {
		t.Fatalf("the default match mode needs every token")
	}
	wide, _ := callProjectEnvelope(t, list, map[string]any{"project": "koi-garden", "query": "drain filter", "match_mode": "any"})
	if saveData(t, wide)["total"] != float64(1) {
		t.Fatalf("match_mode=any must find the task, got %#v", saveData(t, wide)["total"])
	}

	bad, res := callProjectEnvelope(t, list, map[string]any{"project": "koi-garden", "match_mode": "some"})
	if !res.IsError || bad["code"] != "invalid_enum" {
		t.Fatalf("expected a typed invalid_enum error, got %#v", bad)
	}
}

// TestEvidenceCarriesItsVaultCategory pins the category an evidence row takes
// from the vault folder it came from, and the wider kind vocabulary the vault
// actually holds.
func TestEvidenceCarriesItsVaultCategory(t *testing.T) {
	s := newMCPTestStore(t)
	if err := s.EnrollProject("koi-garden"); err != nil {
		t.Fatalf("enroll: %v", err)
	}
	if _, res := callProjectEnvelope(t, handleTaskUpsert(s, MCPConfig{}), map[string]any{
		"project": "koi-garden", "jira_key": "PROJ-1", "title": "drain the pond", "kind": "incident",
	}); res.IsError {
		t.Fatal("seeding the task failed")
	}

	add := handleEvidenceAdd(s, MCPConfig{DefaultProject: "koi-garden"})
	envelope, res := callProjectEnvelope(t, add, map[string]any{
		"task": "PROJ-1", "path": "PROJ-1/analysis/plan.md",
		"sha256": "1111111111111111111111111111111111111111111111111111111111111111",
		"kind":   "md", "category": "analysis", "proves": "the plan the fix followed",
	})
	if res.IsError {
		t.Fatalf("evidence add failed: %#v", envelope)
	}
	evidence, _ := saveData(t, envelope)["evidence"].(map[string]any)
	if evidence["category"] != "analysis" || evidence["kind"] != "md" {
		t.Fatalf("category and kind did not round trip: %#v", evidence)
	}

	bad, res := callProjectEnvelope(t, add, map[string]any{
		"task": "PROJ-1", "path": "PROJ-1/x.md",
		"sha256": "2222222222222222222222222222222222222222222222222222222222222222",
		"kind":   "md", "category": "screenshots", "proves": "x",
	})
	if !res.IsError || bad["code"] != "invalid_enum" {
		t.Fatalf("expected a typed invalid_enum error, got %#v", bad)
	}

	list := handleEvidenceList(s, MCPConfig{})
	filtered, _ := callProjectEnvelope(t, list, map[string]any{"project": "koi-garden", "category": "analysis"})
	if saveData(t, filtered)["total"] != float64(1) {
		t.Fatalf("the category filter must find the row, got %#v", saveData(t, filtered)["total"])
	}
	empty, _ := callProjectEnvelope(t, list, map[string]any{"project": "koi-garden", "category": "benchmarks"})
	if saveData(t, empty)["total"] != float64(0) {
		t.Fatalf("a category with no rows must return none, got %#v", saveData(t, empty)["total"])
	}
}
