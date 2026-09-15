package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	mcppkg "github.com/mark3labs/mcp-go/mcp"

	"github.com/HoracioEspinosa/engram/internal/store"
)

// TestUnknownProjectResolvesThroughAlias pins the behaviour a configuration
// nobody wants to edit depends on: a tool that still names the project the way
// it was set up years ago writes into the project that name points at, instead
// of being refused as unknown or quietly founding a second one beside it.
func TestUnknownProjectResolvesThroughAlias(t *testing.T) {
	s := newMCPTestStore(t)
	if _, _, err := s.UpsertProjectCard(store.UpsertProjectCardParams{Slug: "engram"}); err != nil {
		t.Fatalf("UpsertProjectCard: %v", err)
	}
	if err := s.UpsertProjectAlias("ai-engram", "engram", "manual"); err != nil {
		t.Fatalf("UpsertProjectAlias: %v", err)
	}

	h := handleSave(s, MCPConfig{}, NewSessionActivity(10*time.Minute))
	res, err := h(context.Background(), mcppkg.CallToolRequest{Params: mcppkg.CallToolParams{
		Arguments: map[string]any{
			"title":   "Alias resolution",
			"content": "A save under the old name lands on the project it means",
			"type":    "discovery",
			"project": "ai-engram",
		},
	}})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if res.IsError {
		t.Fatalf("a save under an alias must succeed: %s", callResultText(t, res))
	}
	if text := callResultText(t, res); !strings.Contains(text, `"project":"engram"`) {
		t.Fatalf("the envelope must report the resolved project, got %q", text)
	}

	stored, err := s.RecentObservations("engram", "project", 5)
	if err != nil {
		t.Fatalf("RecentObservations: %v", err)
	}
	if len(stored) != 1 {
		t.Fatalf("observations under engram: %d, want 1", len(stored))
	}
	orphans, err := s.RecentObservations("ai-engram", "project", 5)
	if err != nil {
		t.Fatalf("RecentObservations(ai-engram): %v", err)
	}
	if len(orphans) != 0 {
		t.Fatalf("nothing may be written under the alias, got %d", len(orphans))
	}
}

// TestCurrentProjectReportsAliasResolution pins that the discovery tool says
// where a name leads before anything is written under it.
func TestCurrentProjectReportsAliasResolution(t *testing.T) {
	s := newMCPTestStore(t)
	if _, _, err := s.UpsertProjectCard(store.UpsertProjectCardParams{Slug: "engram"}); err != nil {
		t.Fatalf("UpsertProjectCard: %v", err)
	}
	if err := s.UpsertProjectAlias("ai-engram", "engram", "manual"); err != nil {
		t.Fatalf("UpsertProjectAlias: %v", err)
	}

	h := handleCurrentProject(s, MCPConfig{DefaultProject: "ai-engram"})
	res, err := h(context.Background(), mcppkg.CallToolRequest{})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	var envelope map[string]any
	if err := json.Unmarshal([]byte(callResultText(t, res)), &envelope); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if envelope["project"] != "ai-engram" {
		t.Fatalf("project %v, want the detected name verbatim", envelope["project"])
	}
	if envelope["resolved_slug"] != "engram" {
		t.Fatalf("resolved_slug %v, want engram", envelope["resolved_slug"])
	}
	if envelope["resolved_via"] != store.ProjectResolvedViaAlias {
		t.Fatalf("resolved_via %v, want %q", envelope["resolved_via"], store.ProjectResolvedViaAlias)
	}
	if envelope["resolved_alias_source"] != "manual" {
		t.Fatalf("resolved_alias_source %v, want manual", envelope["resolved_alias_source"])
	}
}
