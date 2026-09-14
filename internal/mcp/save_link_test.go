package mcp

import (
	"context"
	"testing"
	"time"

	"github.com/HoracioEspinosa/engram/internal/store"
	mcppkg "github.com/mark3labs/mcp-go/mcp"
)

const testGraphCommit = "0123456789abcdef0123456789abcdef01234567"

// saveWith drives handleSave with the given arguments and returns the decoded
// envelope plus the raw result, so a test can assert on both the envelope and
// the error flag.
func saveWith(t *testing.T, s *store.Store, args map[string]any) (map[string]any, *mcppkg.CallToolResult) {
	t.Helper()
	h := handleSave(s, MCPConfig{}, NewSessionActivity(10*time.Minute))
	res, err := h(context.Background(), mcppkg.CallToolRequest{Params: mcppkg.CallToolParams{Arguments: args}})
	if err != nil {
		t.Fatalf("handleSave: %v", err)
	}
	return envelopeOf(t, res), res
}

func saveData(t *testing.T, envelope map[string]any) map[string]any {
	t.Helper()
	data, ok := envelope["data"].(map[string]any)
	if !ok {
		t.Fatalf("expected a data object, got %#v", envelope["data"])
	}
	return data
}

func seedSaveTask(t *testing.T, project string) (*store.Store, store.Task) {
	t.Helper()
	s := newMCPTestStore(t)
	if err := s.EnrollProject(project); err != nil {
		t.Fatalf("enroll project: %v", err)
	}
	key := "PROJ-123"
	title := "harden the sync route"
	kind := "incident"
	r, err := s.UpsertTask(store.UpsertTaskParams{Project: project, JiraKey: &key, Title: &title, Kind: &kind})
	if err != nil {
		t.Fatalf("UpsertTask: %v", err)
	}
	return s, r.Task
}

// TestSaveLinksTaskAtomically pins that mem_save can write the observation and
// its task link as one call, and that a link it cannot make leaves no
// observation behind.
func TestSaveLinksTaskAtomically(t *testing.T) {
	t.Run("links the resolved task", func(t *testing.T) {
		s, task := seedSaveTask(t, "engram")

		envelope, res := saveWith(t, s, map[string]any{
			"title": "root cause", "content": "the authorizer needed a principal",
			"type": "bugfix", "project": "engram",
			"task": "PROJ-123", "role": "root_cause",
		})
		if res.IsError {
			t.Fatalf("expected a successful save, got %#v", envelope)
		}
		data := saveData(t, envelope)
		if data["linked_task"] != task.SyncID {
			t.Fatalf("linked_task = %#v, want %q", data["linked_task"], task.SyncID)
		}
		if data["role"] != "root_cause" {
			t.Fatalf("role = %#v, want root_cause", data["role"])
		}
		counts, err := s.TaskCounts(task.ID)
		if err != nil {
			t.Fatalf("TaskCounts: %v", err)
		}
		if counts.Observations != 1 {
			t.Fatalf("expected the link to be persisted, got %d observations", counts.Observations)
		}
	})

	t.Run("an unknown task saves nothing", func(t *testing.T) {
		s, _ := seedSaveTask(t, "engram")

		envelope, res := saveWith(t, s, map[string]any{
			"title": "orphan", "content": "c", "project": "engram", "task": "PROJ-999",
		})
		if !res.IsError || envelope["code"] != "unknown_task" {
			t.Fatalf("expected a typed unknown_task error, got %#v", envelope)
		}
		obs, err := s.RecentObservations("engram", "project", 10)
		if err != nil {
			t.Fatalf("RecentObservations: %v", err)
		}
		if len(obs) != 0 {
			t.Fatalf("a rejected link must save no observation, got %d", len(obs))
		}
	})

	t.Run("an invalid role is refused", func(t *testing.T) {
		s, _ := seedSaveTask(t, "engram")

		envelope, res := saveWith(t, s, map[string]any{
			"title": "bad role", "content": "c", "project": "engram",
			"task": "PROJ-123", "role": "blame",
		})
		if !res.IsError || envelope["code"] != "invalid_enum" {
			t.Fatalf("expected a typed invalid_enum error, got %#v", envelope)
		}
	})
}

// TestSaveGraphRefFallsBackToCardCommit pins where a graph reference gets its
// commit from, and says so in the envelope: the caller's argument wins, the
// project card is the fallback.
func TestSaveGraphRefFallsBackToCardCommit(t *testing.T) {
	t.Run("the argument wins", func(t *testing.T) {
		s, _ := seedSaveTask(t, "engram")

		envelope, res := saveWith(t, s, map[string]any{
			"title": "auth node", "content": "c", "project": "engram",
			"graph_ref": "node:auth", "graph_commit": testGraphCommit,
		})
		if res.IsError {
			t.Fatalf("expected a successful save, got %#v", envelope)
		}
		data := saveData(t, envelope)
		if data["graph_commit_source"] != "arg" || data["refs_added"] != float64(1) {
			t.Fatalf("expected the argument to be used, got %#v", data)
		}
	})

	t.Run("the card is the fallback", func(t *testing.T) {
		s, _ := seedSaveTask(t, "engram")
		if _, _, err := s.UpsertProjectCard(store.UpsertProjectCardParams{Slug: "engram"}); err != nil {
			t.Fatalf("UpsertProjectCard: %v", err)
		}
		if err := s.StampProjectGraph("engram", testGraphCommit, "2026-09-14T00:00:00Z", nil); err != nil {
			t.Fatalf("StampProjectGraph: %v", err)
		}

		envelope, res := saveWith(t, s, map[string]any{
			"title": "store node", "content": "c", "project": "engram",
			"graph_ref": "node:store",
		})
		if res.IsError {
			t.Fatalf("expected a successful save, got %#v", envelope)
		}
		data := saveData(t, envelope)
		if data["graph_commit_source"] != "card" || data["refs_added"] != float64(1) {
			t.Fatalf("expected the card commit to be used, got %#v", data)
		}
	})
}

// TestSaveGraphRefWithoutCommitIsTyped pins the refusal: with no argument and
// no card commit there is nothing to stamp the reference with, and the caller
// is told so by code rather than by prose.
func TestSaveGraphRefWithoutCommitIsTyped(t *testing.T) {
	s, _ := seedSaveTask(t, "engram")

	envelope, res := saveWith(t, s, map[string]any{
		"title": "unstamped", "content": "c", "project": "engram",
		"graph_ref": "node:auth",
	})
	if !res.IsError || envelope["code"] != "graph_commit_required" {
		t.Fatalf("expected a typed graph_commit_required error, got %#v", envelope)
	}
	obs, err := s.RecentObservations("engram", "project", 10)
	if err != nil {
		t.Fatalf("RecentObservations: %v", err)
	}
	if len(obs) != 0 {
		t.Fatalf("a refused save must persist nothing, got %d", len(obs))
	}
}
