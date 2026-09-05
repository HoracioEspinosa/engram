package chunkcodec

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Gentleman-Programming/engram/internal/store"
)

// chunkWith wraps one mutation in the chunk envelope CanonicalizeForProject
// expects, so each case exercises the real entry point rather than the
// unexported helper behind it.
func chunkWith(entity, entityKey, op, payload string) []byte {
	encodedPayload, _ := json.Marshal(payload)
	doc := `{"mutations":[{"entity":` + quote(entity) +
		`,"entity_key":` + quote(entityKey) +
		`,"op":` + quote(op) +
		`,"project":"wrong","payload":` + string(encodedPayload) + `}]}`
	return []byte(doc)
}

func quote(v string) string {
	encoded, _ := json.Marshal(v)
	return string(encoded)
}

func canonicalMutation(t *testing.T, raw []byte, project string) store.SyncMutation {
	t.Helper()
	normalized, err := CanonicalizeForProject(raw, project)
	if err != nil {
		t.Fatalf("canonicalize: %v", err)
	}
	var chunk struct {
		Mutations []store.SyncMutation `json:"mutations"`
	}
	if err := json.Unmarshal(normalized, &chunk); err != nil {
		t.Fatalf("decode canonicalized chunk: %v", err)
	}
	if len(chunk.Mutations) != 1 {
		t.Fatalf("expected exactly one mutation, got %d", len(chunk.Mutations))
	}
	return chunk.Mutations[0]
}

func payloadField(t *testing.T, mutation store.SyncMutation, key string) any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal([]byte(mutation.Payload), &body); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	return body[key]
}

const (
	validSHA        = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	validGraphSHA1  = "1111111111111111111111111111111111111111"
	cardPayloadJSON = `{"slug":"nextcloud","sync_id":"proj-1","display_name":"Nextcloud",` +
		`"default_branch":"master","jira_project":"PROJ","graph_path":"graphify-out/graph.json",` +
		`"created_at":"2026-01-01 09:00:00","updated_at":"2026-01-01 09:00:00","project":"wrong"}`
	taskPayloadJSON = `{"sync_id":"task-1","project":"wrong","jira_key":"PROJ-100","title":"t",` +
		`"kind":"bugfix","state":"open","created_at":"2026-01-01 10:00:00","updated_at":"2026-01-01 10:00:00"}`
	linkPayloadJSON = `{"task_sync_id":"task-1","observation_sync_id":"obs-1","role":"context",` +
		`"linked_at":"2026-01-04 11:00:00","project":"wrong"}`
)

func evidenceJSON(path, sha string) string {
	return `{"sync_id":"evd-1","project":"wrong","task_sync_id":"task-1","path":"` + path +
		`","sha256":"` + sha + `","kind":"png","proves":"it works",` +
		`"captured_at":"2026-01-04 10:00:00","created_at":"2026-01-04 10:00:00",` +
		`"occurred_at":"2026-01-04 10:00:00"}`
}

func TestCanonicalizeForProject_StampsProjectAndDerivesEntityKeys(t *testing.T) {
	cases := []struct {
		name    string
		entity  string
		op      string
		payload string
		wantKey string
	}{
		{"project card", store.SyncEntityProjectCard, store.SyncOpUpsert, cardPayloadJSON, "proj-1"},
		{"task", store.SyncEntityTask, store.SyncOpUpsert, taskPayloadJSON, "task-1"},
		{"evidence", store.SyncEntityEvidence, store.SyncOpUpsert, evidenceJSON("evidence/a.png", validSHA), "evd-1"},
		{"task link", store.SyncEntityTaskLink, store.SyncOpUpsert, linkPayloadJSON, "task-1|obs-1"},
		{"observation ref", store.SyncEntityObservationRef, store.SyncOpUpsert,
			`{"observation_sync_id":"obs-1","ref_kind":"knowledge","ref":"Runbooks/RB-003.md",` +
				`"created_at":"2026-01-05 10:00:00","project":"wrong"}`, "obs-1|knowledge|Runbooks/RB-003.md"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// entity_key is left empty on purpose: the canonicalizer must
			// derive it from the payload rather than trust the sender.
			mutation := canonicalMutation(t, chunkWith(tc.entity, "", tc.op, tc.payload), "nextcloud")
			if mutation.EntityKey != tc.wantKey {
				t.Fatalf("entity_key = %q, want %q", mutation.EntityKey, tc.wantKey)
			}
			if mutation.Project != "nextcloud" {
				t.Fatalf("mutation project = %q, want the chunk project", mutation.Project)
			}
			if got := payloadField(t, mutation, "project"); got != "nextcloud" {
				t.Fatalf("payload project = %v; a payload claiming another project must be overwritten", got)
			}
		})
	}
}

func TestCanonicalizeForProject_RejectsAnEntityKeyThatDisagreesWithThePayload(t *testing.T) {
	raw := chunkWith(store.SyncEntityTask, "task-someone-elses", store.SyncOpUpsert, taskPayloadJSON)
	if _, err := CanonicalizeForProject(raw, "nextcloud"); err == nil {
		t.Fatal("expected a mismatched entity_key to be rejected")
	}
}

func TestCanonicalizeForProject_RejectsUnusableProjectsPayloads(t *testing.T) {
	cases := []struct {
		name    string
		entity  string
		op      string
		payload string
		wantErr string
	}{
		{
			"evidence with an absolute path",
			store.SyncEntityEvidence, store.SyncOpUpsert,
			evidenceJSON("/Users/someone/evidence/a.png", validSHA),
			"must be relative",
		},
		{
			"evidence with a truncated digest",
			store.SyncEntityEvidence, store.SyncOpUpsert,
			evidenceJSON("evidence/a.png", "abc"),
			"sha256",
		},
		{
			"card with a graph summary and no commit",
			store.SyncEntityProjectCard, store.SyncOpUpsert,
			`{"slug":"nextcloud","sync_id":"proj-1","display_name":"Nextcloud","default_branch":"master",` +
				`"jira_project":"PROJ","graph_path":"graphify-out/graph.json","graph_summary":"{}",` +
				`"created_at":"2026-01-01 09:00:00","updated_at":"2026-01-01 09:00:00","project":"nextcloud"}`,
			"graph_summary requires graph_commit",
		},
		{
			"card with a commit and no build time",
			store.SyncEntityProjectCard, store.SyncOpUpsert,
			`{"slug":"nextcloud","sync_id":"proj-1","display_name":"Nextcloud","default_branch":"master",` +
				`"jira_project":"PROJ","graph_path":"graphify-out/graph.json","graph_commit":"` + validGraphSHA1 + `",` +
				`"created_at":"2026-01-01 09:00:00","updated_at":"2026-01-01 09:00:00","project":"nextcloud"}`,
			"must be set together",
		},
		{
			"task with neither jira_key nor sdd_change",
			store.SyncEntityTask, store.SyncOpUpsert,
			`{"sync_id":"task-1","project":"nextcloud","title":"t","kind":"bugfix","state":"open",` +
				`"created_at":"2026-01-01 10:00:00","updated_at":"2026-01-01 10:00:00"}`,
			"jira_key or sdd_change",
		},
		{
			"task link delete without a clock",
			store.SyncEntityTaskLink, store.SyncOpDelete, linkPayloadJSON,
			"deleted_at is required",
		},
		{
			"graph reference without its commit",
			store.SyncEntityObservationRef, store.SyncOpUpsert,
			`{"observation_sync_id":"obs-1","ref_kind":"graph","ref":"Uploader::put",` +
				`"created_at":"2026-01-05 10:00:00","project":"nextcloud"}`,
			"graph_commit is required",
		},
		{
			"observation ref delete",
			store.SyncEntityObservationRef, store.SyncOpDelete,
			`{"observation_sync_id":"obs-1","ref_kind":"knowledge","ref":"x","project":"nextcloud"}`,
			"unsupported mutation",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := CanonicalizeForProject(chunkWith(tc.entity, "", tc.op, tc.payload), "nextcloud")
			if err == nil {
				t.Fatalf("expected %q to be rejected", tc.name)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error %q does not mention %q", err, tc.wantErr)
			}
		})
	}
}

// TestCanonicalizeForProject_StillRejectsUnknownEntities pins the boundary of
// the new default branch: adding the five engram-projects entities must not
// turn the canonicalizer into something that accepts anything.
func TestCanonicalizeForProject_StillRejectsUnknownEntities(t *testing.T) {
	raw := chunkWith("runbook_index", "rb-1", store.SyncOpUpsert, `{"id":"RB-003","project":"nextcloud"}`)
	_, err := CanonicalizeForProject(raw, "nextcloud")
	if err == nil {
		t.Fatal("expected an unknown entity to be rejected")
	}
	if !strings.Contains(err.Error(), "unsupported mutation") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestCanonicalizeForProject_UpstreamEntitiesAreUntouched is the regression
// guard for the upstream half of the codec: the same three payloads the
// existing tests use must survive unchanged.
func TestCanonicalizeForProject_UpstreamEntitiesAreUntouched(t *testing.T) {
	raw := chunkWith(store.SyncEntitySession, "", store.SyncOpUpsert,
		`{"id":"sess-1","project":"wrong","directory":"/tmp/sess-1","started_at":"2026-04-10T12:00:00Z"}`)
	mutation := canonicalMutation(t, raw, "nextcloud")
	if mutation.EntityKey != "sess-1" || mutation.Project != "nextcloud" {
		t.Fatalf("upstream session canonicalization changed: %+v", mutation)
	}
	if got := payloadField(t, mutation, "directory"); got != "/tmp/sess-1" {
		t.Fatalf("session directory was altered: %v", got)
	}
}
