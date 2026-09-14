package mcp

import (
	"encoding/json"
	"testing"

	projectpkg "github.com/HoracioEspinosa/engram/internal/project"
	"github.com/mark3labs/mcp-go/mcp"
)

// envelopeOf decodes the single text payload every tool envelope is returned
// as, so a test can assert on fields rather than on a JSON string.
func envelopeOf(t *testing.T, res *mcp.CallToolResult) map[string]any {
	t.Helper()
	if res == nil {
		t.Fatal("expected a tool result, got nil")
	}
	if len(res.Content) != 1 {
		t.Fatalf("expected exactly one content block, got %d", len(res.Content))
	}
	text, ok := res.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatalf("expected a text content block, got %T", res.Content[0])
	}
	var envelope map[string]any
	if err := json.Unmarshal([]byte(text.Text), &envelope); err != nil {
		t.Fatalf("decode envelope %q: %v", text.Text, err)
	}
	return envelope
}

// TestEnvelopeKeepsResultAndAddsData pins the additive contract: every tool
// envelope still carries the exact `result` it carried before, and gains a
// structured `data` object beside it.
func TestEnvelopeKeepsResultAndAddsData(t *testing.T) {
	res := projectpkg.DetectionResult{Project: "engram", Source: projectpkg.SourceExplicitOverride, Path: "/tmp/engram"}

	t.Run("respondWithProject", func(t *testing.T) {
		envelope := envelopeOf(t, respondWithProject(res, "Saved observation #7", map[string]any{"id": 7}))
		if got := envelope["result"]; got != "Saved observation #7" {
			t.Fatalf("result must be unchanged, got %#v", got)
		}
		if envelope["project"] != "engram" || envelope["project_path"] != "/tmp/engram" {
			t.Fatalf("project envelope fields must be unchanged, got %#v", envelope)
		}
		data, ok := envelope["data"].(map[string]any)
		if !ok {
			t.Fatalf("expected a structured data object, got %#v", envelope["data"])
		}
		if data["text"] != "Saved observation #7" {
			t.Fatalf("data must carry the human-readable text, got %#v", data["text"])
		}
		if data["id"] != float64(7) {
			t.Fatalf("data must carry the extra fields, got %#v", data["id"])
		}
		if envelope["id"] != float64(7) {
			t.Fatalf("extra fields must still sit at the top level, got %#v", envelope["id"])
		}
	})

	t.Run("respondWithProjectData", func(t *testing.T) {
		envelope := envelopeOf(t, respondWithProjectData(res, "2 results", nil, map[string]any{
			"results": []any{"a", "b"},
			"total":   2,
		}))
		data, ok := envelope["data"].(map[string]any)
		if !ok {
			t.Fatalf("expected a structured data object, got %#v", envelope["data"])
		}
		if data["total"] != float64(2) {
			t.Fatalf("explicit data must win, got %#v", data["total"])
		}
		if _, dup := envelope["total"]; dup {
			t.Fatalf("explicit data must not be copied to the top level, got %#v", envelope)
		}
		if envelope["result"] != "2 results" {
			t.Fatalf("result must be unchanged, got %#v", envelope["result"])
		}
	})

	t.Run("respondProjectResult", func(t *testing.T) {
		card := map[string]any{"slug": "engram", "kind": "service"}
		envelope := envelopeOf(t, respondProjectResult(res, card))
		resultJSON, _ := json.Marshal(envelope["result"])
		dataJSON, _ := json.Marshal(envelope["data"])
		if string(resultJSON) != string(dataJSON) {
			t.Fatalf("data must mirror result byte for byte, result=%s data=%s", resultJSON, dataJSON)
		}
	})
}

// TestToolErrorCarriesBothCodes pins the single error helper: the two error
// vocabularies this package grew — {"error","code"} for the projects tools and
// {"error_code","message"} for the rest — are both emitted, so no caller that
// reads either one loses its field.
func TestToolErrorCarriesBothCodes(t *testing.T) {
	for name, res := range map[string]*mcp.CallToolResult{
		"toolError":        toolError("graph_commit_required", "graph_commit is required", map[string]any{"hint": "pass graph_commit"}),
		"projectToolError": projectToolError("graph_commit_required", "graph_commit is required", map[string]any{"hint": "pass graph_commit"}),
		"errorWithMeta":    errorWithMeta("unknown_project", "Project \"nope\" is unknown", []string{"engram"}),
	} {
		t.Run(name, func(t *testing.T) {
			if !res.IsError {
				t.Fatal("a tool error must set IsError")
			}
			envelope := envelopeOf(t, res)
			for _, key := range []string{"error", "error_code", "code", "message"} {
				if _, ok := envelope[key]; !ok {
					t.Fatalf("missing %q in %#v", key, envelope)
				}
			}
			if envelope["error"] != envelope["message"] {
				t.Fatalf("error and message must carry the same text, got %#v and %#v", envelope["error"], envelope["message"])
			}
			if envelope["code"] != envelope["error_code"] {
				t.Fatalf("code and error_code must carry the same value, got %#v and %#v", envelope["code"], envelope["error_code"])
			}
		})
	}
}
