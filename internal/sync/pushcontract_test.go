package sync

import (
	"strings"
	"testing"

	"github.com/HoracioEspinosa/engram/internal/store"
)

// TestValidateChunkRowsNamesTheFirstMissingFieldPerEntity pins the wording the
// push response carries. The exporter and the upgrade doctor read the same
// rules through the same functions, so a change here is a change to what a
// client is told and to what the doctor predicts.
func TestValidateChunkRowsNamesTheFirstMissingFieldPerEntity(t *testing.T) {
	cases := []struct {
		name  string
		chunk ChunkData
		want  string
	}{
		{
			name:  "session without an id",
			chunk: ChunkData{Sessions: []store.Session{{Project: "proj-a"}}},
			want:  "sessions[0].id is required",
		},
		{
			name:  "session without a directory is accepted",
			chunk: ChunkData{Sessions: []store.Session{{ID: "manual-save-proj-a", Project: "proj-a"}}},
		},
		{
			name: "observation without a title",
			chunk: ChunkData{Observations: []store.Observation{{
				SyncID: "obs-1", SessionID: "sess-a", Type: "note", Content: "c", Scope: "project",
			}}},
			want: "observations[0].title is required",
		},
		{
			name: "observation without a scope",
			chunk: ChunkData{Observations: []store.Observation{{
				SyncID: "obs-1", SessionID: "sess-a", Type: "note", Title: "t", Content: "c",
			}}},
			want: "observations[0].scope is required",
		},
		{
			name: "whitespace does not satisfy a required field",
			chunk: ChunkData{Observations: []store.Observation{{
				SyncID: "obs-1", SessionID: "sess-a", Type: "note", Title: "   ", Content: "c", Scope: "project",
			}}},
			want: "observations[0].title is required",
		},
		{
			name: "prompt without content",
			chunk: ChunkData{Prompts: []store.Prompt{{
				SyncID: "prompt-1", SessionID: "sess-a",
			}}},
			want: "prompts[0].content is required",
		},
		{
			name: "a complete chunk is accepted",
			chunk: ChunkData{
				Sessions: []store.Session{{ID: "sess-a", Project: "proj-a"}},
				Observations: []store.Observation{{
					SyncID: "obs-1", SessionID: "sess-a", Type: "note", Title: "t", Content: "c", Scope: "project",
				}},
				Prompts: []store.Prompt{{SyncID: "prompt-1", SessionID: "sess-a", Content: "c"}},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateChunkRows(tc.chunk)
			if tc.want == "" {
				if err != nil {
					t.Fatalf("expected the chunk to be accepted, got %v", err)
				}
				return
			}
			if err == nil || err.Error() != tc.want {
				t.Fatalf("expected %q, got %v", tc.want, err)
			}
		})
	}
}

// TestChunkRowRejectionsNamesEveryRowAndItsIdentity pins the report side: the
// operator has to complete or remove the rows by hand, so one rejection at a
// time and an index inside a chunk they cannot see would be useless.
func TestChunkRowRejectionsNamesEveryRowAndItsIdentity(t *testing.T) {
	rejections := ChunkRowRejections(ChunkData{
		Observations: []store.Observation{
			{SyncID: "obs-ok", SessionID: "sess-a", Type: "note", Title: "t", Content: "c", Scope: "project"},
			{SyncID: "obs-titleless", SessionID: "sess-a", Type: "note", Content: "c", Scope: "project"},
			{ID: 42, SessionID: "sess-a", Type: "note", Title: "t", Content: "c", Scope: "project"},
		},
	})

	if len(rejections) != 2 {
		t.Fatalf("expected the two unacceptable rows, got %d: %v", len(rejections), rejections)
	}
	if got := rejections[0].String(); got != "observation obs-titleless: title is required" {
		t.Fatalf("expected the row identity and reason, got %q", got)
	}
	// A row with no sync_id has no identity to print, so the local id is what
	// lets the operator find it.
	if got := rejections[1].String(); !strings.Contains(got, "id=42") || !strings.Contains(got, "sync_id is required") {
		t.Fatalf("expected the local id to stand in for a missing sync_id, got %q", got)
	}
}
