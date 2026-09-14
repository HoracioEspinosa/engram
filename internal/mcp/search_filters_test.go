package mcp

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/HoracioEspinosa/engram/internal/store"
	mcppkg "github.com/mark3labs/mcp-go/mcp"
)

func searchWith(t *testing.T, s *store.Store, args map[string]any) (map[string]any, *mcppkg.CallToolResult) {
	t.Helper()
	h := handleSearch(s, MCPConfig{}, NewSessionActivity(10*time.Minute))
	res, err := h(context.Background(), mcppkg.CallToolRequest{Params: mcppkg.CallToolParams{Arguments: args}})
	if err != nil {
		t.Fatalf("handleSearch: %v", err)
	}
	return envelopeOf(t, res), res
}

// TestSearchExposesTotalOffsetAndFilters pins mem_search's workspace surface:
// the page says how big the whole answer is, and the task and graph filters
// narrow it to what one piece of work touched.
func TestSearchExposesTotalOffsetAndFilters(t *testing.T) {
	s, task := seedSaveTask(t, "engram")
	for i := 0; i < 4; i++ {
		if _, err := s.AddObservation(store.AddObservationParams{
			SessionID: "s1", Type: "manual", Title: fmt.Sprintf("sync route note %d", i), Content: fmt.Sprintf("the sync route answered 403 on attempt %d", i), Project: "engram",
		}); err != nil {
			t.Fatalf("AddObservation %d: %v", i, err)
		}
	}
	linked, err := s.AddObservationLinked(
		store.AddObservationParams{SessionID: "s1", Type: "bugfix", Title: "sync route root cause", Content: "the sync route needed no principal", Project: "engram"},
		&store.ObservationLink{Task: &task, Role: "root_cause", GraphRef: "node:sync", GraphCommit: testGraphCommit},
	)
	if err != nil {
		t.Fatalf("AddObservationLinked: %v", err)
	}

	t.Run("data carries results, total and offset", func(t *testing.T) {
		envelope, res := searchWith(t, s, map[string]any{"query": "sync route", "project": "engram", "limit": float64(2)})
		if res.IsError {
			t.Fatalf("unexpected error: %#v", envelope)
		}
		data := saveData(t, envelope)
		if data["total"] != float64(5) {
			t.Fatalf("total = %#v, want 5", data["total"])
		}
		if data["offset"] != float64(0) {
			t.Fatalf("offset = %#v, want 0", data["offset"])
		}
		results, ok := data["results"].([]any)
		if !ok || len(results) != 2 {
			t.Fatalf("expected two structured results, got %#v", data["results"])
		}
	})

	t.Run("offset pages", func(t *testing.T) {
		envelope, _ := searchWith(t, s, map[string]any{"query": "sync route", "project": "engram", "limit": float64(2), "offset": float64(4)})
		data := saveData(t, envelope)
		if data["offset"] != float64(4) {
			t.Fatalf("offset = %#v, want 4", data["offset"])
		}
		results, _ := data["results"].([]any)
		if len(results) != 1 {
			t.Fatalf("expected the last result alone, got %d", len(results))
		}
	})

	t.Run("task narrows to one task", func(t *testing.T) {
		envelope, res := searchWith(t, s, map[string]any{"query": "sync route", "project": "engram", "task": "PROJ-123"})
		if res.IsError {
			t.Fatalf("unexpected error: %#v", envelope)
		}
		data := saveData(t, envelope)
		if data["total"] != float64(1) {
			t.Fatalf("total = %#v, want 1", data["total"])
		}
		results, _ := data["results"].([]any)
		first, _ := results[0].(map[string]any)
		if first["id"] != float64(linked.ObservationID) {
			t.Fatalf("expected the linked observation, got %#v", first["id"])
		}
	})

	t.Run("an unknown task is typed", func(t *testing.T) {
		envelope, res := searchWith(t, s, map[string]any{"query": "sync route", "project": "engram", "task": "PROJ-999"})
		if !res.IsError || envelope["code"] != "unknown_task" {
			t.Fatalf("expected a typed unknown_task error, got %#v", envelope)
		}
	})

	t.Run("graph_ref narrows to one node", func(t *testing.T) {
		envelope, _ := searchWith(t, s, map[string]any{"query": "sync route", "project": "engram", "graph_ref": "node:sync"})
		data := saveData(t, envelope)
		if data["total"] != float64(1) {
			t.Fatalf("total = %#v, want 1", data["total"])
		}
	})

	t.Run("since and until bound the range", func(t *testing.T) {
		envelope, _ := searchWith(t, s, map[string]any{"query": "sync route", "project": "engram", "until": "2000-01-01"})
		data := saveData(t, envelope)
		if data["total"] != float64(0) {
			t.Fatalf("an impossible range must match nothing, got %#v", data["total"])
		}
	})
}

// TestPinTakesAnExplicitState pins mem_pin's new argument: one tool can set
// either state, and the tools that always meant one keep meaning it.
func TestPinTakesAnExplicitState(t *testing.T) {
	s := newMCPTestStore(t)
	if err := s.EnrollProject("engram"); err != nil {
		t.Fatalf("enroll project: %v", err)
	}
	if err := s.CreateSession("s1", "engram", ""); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	id, err := s.AddObservation(store.AddObservationParams{SessionID: "s1", Type: "manual", Title: "t", Content: "c", Project: "engram"})
	if err != nil {
		t.Fatalf("AddObservation: %v", err)
	}

	pin := handlePin(s, true)
	if _, err := pin(context.Background(), mcppkg.CallToolRequest{Params: mcppkg.CallToolParams{Arguments: map[string]any{"id": float64(id)}}}); err != nil {
		t.Fatalf("pin: %v", err)
	}
	obs, _ := s.GetObservation(id)
	if !obs.Pinned {
		t.Fatal("mem_pin without an explicit state must still pin")
	}

	if _, err := pin(context.Background(), mcppkg.CallToolRequest{Params: mcppkg.CallToolParams{Arguments: map[string]any{"id": float64(id), "pinned": false}}}); err != nil {
		t.Fatalf("unpin through mem_pin: %v", err)
	}
	obs, _ = s.GetObservation(id)
	if obs.Pinned {
		t.Fatal("pinned=false must unpin through mem_pin")
	}
}

// TestCurrentProjectExposesTheResolutionInData pins that the alias resolution
// mem_current_project already reported is also readable from the structured
// half of the envelope.
func TestCurrentProjectExposesTheResolutionInData(t *testing.T) {
	s := newMCPTestStore(t)
	if err := s.EnrollProject("engram"); err != nil {
		t.Fatalf("enroll project: %v", err)
	}
	h := handleCurrentProject(s, MCPConfig{DefaultProject: "engram"})
	res, err := h(context.Background(), mcppkg.CallToolRequest{})
	if err != nil {
		t.Fatalf("handleCurrentProject: %v", err)
	}
	envelope := envelopeOf(t, res)
	data := saveData(t, envelope)
	resolved, ok := data["resolved"].(map[string]any)
	if !ok {
		t.Fatalf("expected data.resolved, got %#v", data["resolved"])
	}
	if envelope["resolved_slug"] != nil && resolved["slug"] != envelope["resolved_slug"] {
		t.Fatalf("data.resolved.slug must mirror resolved_slug, got %#v and %#v", resolved["slug"], envelope["resolved_slug"])
	}
}

// TestContextHonoursItsLimit pins mem_context's recovered limit: the schema
// advertises it and the handler now reads it, so a host with a small window
// gets the smaller render it asked for.
func TestContextHonoursItsLimit(t *testing.T) {
	s := newMCPTestStore(t)
	if err := s.EnrollProject("engram"); err != nil {
		t.Fatalf("enroll project: %v", err)
	}
	if err := s.CreateSession("s1", "engram", ""); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	for i := 0; i < 6; i++ {
		if _, err := s.AddObservation(store.AddObservationParams{
			SessionID: "s1", Type: "manual", Title: fmt.Sprintf("note %d", i), Content: fmt.Sprintf("body %d", i), Project: "engram",
		}); err != nil {
			t.Fatalf("AddObservation %d: %v", i, err)
		}
	}

	h := handleContext(s, MCPConfig{DefaultProject: "engram"}, NewSessionActivity(10*time.Minute))
	call := func(args map[string]any) string {
		t.Helper()
		res, err := h(context.Background(), mcppkg.CallToolRequest{Params: mcppkg.CallToolParams{Arguments: args}})
		if err != nil {
			t.Fatalf("handleContext: %v", err)
		}
		envelope := envelopeOf(t, res)
		text, _ := envelope["result"].(string)
		return text
	}

	capped := strings.Count(call(map[string]any{"project": "engram", "limit": float64(2)}), "- [manual]")
	full := strings.Count(call(map[string]any{"project": "engram"}), "- [manual]")
	if capped != 2 {
		t.Fatalf("expected 2 rendered observations, got %d", capped)
	}
	if full <= capped {
		t.Fatalf("the unlimited render must show more than the capped one, got %d and %d", full, capped)
	}
}
