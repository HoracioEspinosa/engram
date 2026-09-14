package mcp

import (
	"testing"

	"github.com/HoracioEspinosa/engram/internal/store"
	mcppkg "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// callProjectEnvelope calls a projects tool and decodes its envelope, so a
// test can assert on the structured half rather than on a JSON string.
func callProjectEnvelope(t *testing.T, h server.ToolHandlerFunc, args map[string]any) (map[string]any, *mcppkg.CallToolResult) {
	t.Helper()
	res := callProjectTool(t, h, args)
	return envelopeOf(t, res), res
}

// TestProjectUpsertWritesHierarchyMetadataAndAliases pins the round trip of
// mem_project_upsert's new fields: what goes in comes back on the card, the
// parent is set through the hierarchy writer, and the aliases resolve.
func TestProjectUpsertWritesHierarchyMetadataAndAliases(t *testing.T) {
	s := newMCPTestStore(t)
	for _, slug := range []string{"koi-garden", "koi-garden-pond-01"} {
		if err := s.EnrollProject(slug); err != nil {
			t.Fatalf("enroll %s: %v", slug, err)
		}
	}
	upsert := handleProjectUpsert(s, MCPConfig{})

	if _, res := callProjectEnvelope(t, upsert, map[string]any{"project": "koi-garden", "kind": "umbrella"}); res.IsError {
		t.Fatal("seeding the parent card must succeed")
	}

	envelope, res := callProjectEnvelope(t, upsert, map[string]any{
		"project": "koi-garden-pond-01", "parent": "koi-garden",
		"kind": "instance", "description": "the first pond", "icon": "fish", "color": "accent",
		"tags": []any{"koi", "pond"}, "aliases": []any{"koi_garden_pond_01"},
	})
	if res.IsError {
		t.Fatalf("upsert failed: %#v", envelope)
	}
	data := saveData(t, envelope)
	card, ok := data["card"].(map[string]any)
	if !ok {
		t.Fatalf("expected a card, got %#v", data["card"])
	}
	if card["parent_slug"] != "koi-garden" || card["depth"] != float64(1) {
		t.Fatalf("expected the card under its parent, got parent=%#v depth=%#v", card["parent_slug"], card["depth"])
	}
	if card["kind"] != "instance" || card["icon"] != "fish" || card["color"] != "accent" {
		t.Fatalf("metadata did not round trip: %#v", card)
	}
	if card["tags"] != `["koi","pond"]` {
		t.Fatalf("tags did not round trip, got %#v", card["tags"])
	}
	added, _ := data["aliases_added"].([]any)
	if len(added) != 1 || added[0] != "koi_garden_pond_01" {
		t.Fatalf("expected the alias to be added, got %#v errors=%#v", data["aliases_added"], data["alias_errors"])
	}
	// An icon that is not a token is refused by name, not by a raw constraint
	// failure from the schema.
	badIcon, badRes := callProjectEnvelope(t, upsert, map[string]any{"project": "koi-garden-pond-01", "icon": "🐟"})
	if !badRes.IsError || badIcon["code"] != "invalid_icon" {
		t.Fatalf("expected a typed invalid_icon error, got %#v", badIcon)
	}

	resolution, err := s.ResolveProjectSlug("koi_garden_pond_01")
	if err != nil || resolution.Slug != "koi-garden-pond-01" {
		t.Fatalf("the alias must resolve to the project, got %+v err=%v", resolution, err)
	}
}

// TestProjectUpsertReturnsTheCardWhenTheParentIsRefused pins the two-step
// write: the card exists even when the parent is rejected, so the caller is
// told both facts at once instead of one of them.
func TestProjectUpsertReturnsTheCardWhenTheParentIsRefused(t *testing.T) {
	s := newMCPTestStore(t)
	if err := s.EnrollProject("koi-garden"); err != nil {
		t.Fatalf("enroll: %v", err)
	}
	upsert := handleProjectUpsert(s, MCPConfig{})

	envelope, res := callProjectEnvelope(t, upsert, map[string]any{"project": "koi-garden", "parent": "koi-garden"})
	if !res.IsError {
		t.Fatalf("a card cannot be its own parent: %#v", envelope)
	}
	if envelope["code"] != "project_cycle" {
		t.Fatalf("expected project_cycle, got %#v", envelope["code"])
	}
	if _, ok := envelope["card"]; !ok {
		t.Fatalf("the created card must travel with the refusal: %#v", envelope)
	}
}

// TestProjectCardReportsHierarchyInheritanceAndGraph pins the three sections
// mem_project_card grew: where the card sits, what it takes from above, and
// whether its graph still describes the working copy.
func TestProjectCardReportsHierarchyInheritanceAndGraph(t *testing.T) {
	s := newMCPTestStore(t)
	for _, slug := range []string{"koi-garden", "koi-garden-pond-01"} {
		if err := s.EnrollProject(slug); err != nil {
			t.Fatalf("enroll %s: %v", slug, err)
		}
	}
	owner := "koi-team"
	if _, _, err := s.UpsertProjectCard(store.UpsertProjectCardParams{Slug: "koi-garden", Owner: &owner}); err != nil {
		t.Fatalf("UpsertProjectCard parent: %v", err)
	}
	if _, _, err := s.UpsertProjectCard(store.UpsertProjectCardParams{Slug: "koi-garden-pond-01"}); err != nil {
		t.Fatalf("UpsertProjectCard child: %v", err)
	}
	parent := "koi-garden"
	if err := s.SetProjectParent("koi-garden-pond-01", &parent); err != nil {
		t.Fatalf("SetProjectParent: %v", err)
	}

	card := handleProjectCard(s, MCPConfig{})

	envelope, res := callProjectEnvelope(t, card, map[string]any{"project": "koi-garden"})
	if res.IsError {
		t.Fatalf("project card failed: %#v", envelope)
	}
	data := saveData(t, envelope)
	hierarchy, ok := data["hierarchy"].(map[string]any)
	if !ok {
		t.Fatalf("expected a hierarchy section, got %#v", data["hierarchy"])
	}
	children, _ := hierarchy["children"].([]any)
	if len(children) != 1 || children[0] != "koi-garden-pond-01" {
		t.Fatalf("expected the child listed, got %#v", hierarchy["children"])
	}

	childEnvelope, _ := callProjectEnvelope(t, card, map[string]any{"project": "koi-garden-pond-01"})
	childData := saveData(t, childEnvelope)
	inherited, ok := childData["inherited"].(map[string]any)
	if !ok {
		t.Fatalf("expected an inherited section, got %#v", childData["inherited"])
	}
	ownerField, ok := inherited["owner"].(map[string]any)
	if !ok || ownerField["value"] != "koi-team" || ownerField["from"] != "koi-garden" {
		t.Fatalf("owner must be inherited from the parent, got %#v", inherited["owner"])
	}
	graph, ok := childData["graph"].(map[string]any)
	if !ok {
		t.Fatalf("expected a graph section, got %#v", childData["graph"])
	}
	if graph["stale"] != true || graph["stale_reason"] != "no_graph_commit" {
		t.Fatalf("a card with no stamped graph must read as stale, got %#v", graph)
	}

	// include_graph_status=false drops the section rather than reporting an
	// answer nobody asked for.
	withoutGraph, _ := callProjectEnvelope(t, card, map[string]any{"project": "koi-garden-pond-01", "include_graph_status": false})
	if _, present := saveData(t, withoutGraph)["graph"]; present {
		t.Fatal("include_graph_status=false must omit data.graph")
	}
}
