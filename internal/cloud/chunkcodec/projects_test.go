package chunkcodec

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/HoracioEspinosa/engram/internal/store"
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
			"task with no identity at all",
			store.SyncEntityTask, store.SyncOpUpsert,
			`{"sync_id":"task-1","project":"nextcloud","title":"t","kind":"bugfix","state":"open",` +
				`"created_at":"2026-01-01 10:00:00","updated_at":"2026-01-01 10:00:00"}`,
			"jira_key, sdd_change or slug",
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

// TestCanonicalizeForProject_CarriesTheWorkspaceEntities pins the two entities
// the workspace writes and the fields the hierarchy added.
//
// The payload structs here are a second, independent copy of the store's, so a
// column that the store replicates but this file never declared is dropped on
// the way out — silently, because encoding a struct cannot fail on a field it
// does not know. A card that arrives at the other end with its parent gone and
// its kind back at the default still compares equal to a replica that lost the
// same fields, which is why this is asserted on the payload rather than on a
// round trip.
func TestCanonicalizeForProject_CarriesTheWorkspaceEntities(t *testing.T) {
	cases := []struct {
		name    string
		entity  string
		payload string
		wantKey string
		fields  map[string]any
	}{
		{
			name:   "a card carries its hierarchy and its appearance",
			entity: store.SyncEntityProjectCard,
			payload: `{"slug":"koi-garden-pond-02","sync_id":"proj-2","display_name":"Pond 02",` +
				`"default_branch":"master","jira_project":"KOI","graph_path":"graphify-out/graph.json",` +
				`"created_at":"2026-01-01 09:00:00","updated_at":"2026-01-01 09:00:00","project":"wrong",` +
				`"parent_slug":"koi-garden","depth":1,"kind":"instance","description":"el segundo estanque",` +
				`"icon":"pond","color":"accent","tags":"[\"instance\",\"pond\"]"}`,
			wantKey: "proj-2",
			fields: map[string]any{
				"parent_slug": "koi-garden", "depth": float64(1), "kind": "instance",
				"description": "el segundo estanque", "icon": "pond", "color": "accent",
				"tags": `["instance","pond"]`,
			},
		},
		{
			name:   "a task carries the fields the vault gave it",
			entity: store.SyncEntityTask,
			payload: `{"sync_id":"task-2","project":"wrong","jira_key":"KOI-1099","title":"t",` +
				`"kind":"bugfix","state":"pending","created_at":"2026-01-01 10:00:00",` +
				`"updated_at":"2026-01-01 10:00:00","slug":"lookup-timeout","summary":"el lookup falla",` +
				`"pending_note":"falta validar","vault_path":"koi-garden/KOI-1099-lookup-timeout",` +
				`"parent_task_sync_id":"task-1"}`,
			wantKey: "task-2",
			fields: map[string]any{
				"slug": "lookup-timeout", "summary": "el lookup falla", "pending_note": "falta validar",
				"vault_path": "koi-garden/KOI-1099-lookup-timeout", "parent_task_sync_id": "task-1",
			},
		},
		{
			name:   "evidence carries its category and the clock of its location",
			entity: store.SyncEntityEvidence,
			payload: `{"sync_id":"evd-2","project":"wrong","task_sync_id":"task-1","path":"analysis/a.md",` +
				`"sha256":"` + validSHA + `","category":"analysis","kind":"md","proves":"it works",` +
				`"captured_at":"2026-01-04 10:00:00","created_at":"2026-01-04 10:00:00",` +
				`"occurred_at":"2026-01-04 10:00:00","location_set_at":"2026-01-04 10:00:00"}`,
			wantKey: "evd-2",
			fields:  map[string]any{"category": "analysis", "location_set_at": "2026-01-04 10:00:00"},
		},
		{
			name:   "an alias travels under its own name",
			entity: store.SyncEntityProjectAlias,
			payload: `{"alias":"koi_garden","sync_id":"alias-1","slug":"koi-garden","source":"manual",` +
				`"created_at":"2026-01-06 10:00:00","updated_at":"2026-01-06 10:00:00","project":"wrong"}`,
			wantKey: "koi_garden",
			fields:  map[string]any{"alias": "koi_garden", "slug": "koi-garden", "source": "manual"},
		},
		{
			name:   "a benchmark travels with the direction it is read against",
			entity: store.SyncEntityBenchmark,
			payload: `{"sync_id":"bench-1","project":"wrong","task_sync_id":"task-1","name":"lookup",` +
				`"metric":"lookup.p95","unit":"ms","direction":"lower","value":1512,"baseline":true,` +
				`"baseline_set_at":"2026-01-06 10:00:00","captured_at":"2026-01-06 10:00:00",` +
				`"source":"manual","created_at":"2026-01-06 10:00:00"}`,
			wantKey: "bench-1",
			fields: map[string]any{
				"metric": "lookup.p95", "unit": "ms", "direction": "lower",
				"value": float64(1512), "baseline": true, "source": "manual",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mutation := canonicalMutation(t, chunkWith(tc.entity, "", store.SyncOpUpsert, tc.payload), "koi-garden")
			if mutation.EntityKey != tc.wantKey {
				t.Fatalf("entity_key = %q, want %q", mutation.EntityKey, tc.wantKey)
			}
			if got := payloadField(t, mutation, "project"); got != "koi-garden" {
				t.Fatalf("payload project = %v, want the chunk project", got)
			}
			for key, want := range tc.fields {
				if got := payloadField(t, mutation, key); got != want {
					t.Errorf("payload %s = %#v, want %#v", key, got, want)
				}
			}
		})
	}
}

// TestCanonicalizeForProject_RejectsAnUnusableWorkspacePayload pins the refusals
// for the two new entities: an identity that is missing, and a measurement with
// no unit to compare it in.
func TestCanonicalizeForProject_RejectsAnUnusableWorkspacePayload(t *testing.T) {
	cases := []struct {
		name    string
		entity  string
		op      string
		payload string
		want    string
	}{
		{
			name: "an alias with no slug", entity: store.SyncEntityProjectAlias, op: store.SyncOpUpsert,
			payload: `{"alias":"koi_garden","sync_id":"alias-1","project":"wrong"}`,
			want:    "slug is required",
		},
		{
			name: "an alias pointing at itself", entity: store.SyncEntityProjectAlias, op: store.SyncOpUpsert,
			payload: `{"alias":"koi-garden","sync_id":"alias-1","slug":"koi-garden","project":"wrong"}`,
			want:    "cannot point at itself",
		},
		{
			name: "a benchmark with no metric", entity: store.SyncEntityBenchmark, op: store.SyncOpUpsert,
			payload: `{"sync_id":"bench-1","project":"wrong","task_sync_id":"task-1","name":"lookup",` +
				`"unit":"ms","direction":"lower","value":1,"captured_at":"2026-01-06 10:00:00","source":"manual"}`,
			want: "metric is required",
		},
		{
			name: "a benchmark with a direction nothing reads", entity: store.SyncEntityBenchmark, op: store.SyncOpUpsert,
			payload: `{"sync_id":"bench-1","project":"wrong","task_sync_id":"task-1","name":"lookup",` +
				`"metric":"lookup.p95","unit":"ms","direction":"sideways","value":1,` +
				`"captured_at":"2026-01-06 10:00:00","source":"manual"}`,
			want: "direction",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := CanonicalizeForProject(chunkWith(tc.entity, "", tc.op, tc.payload), "koi-garden")
			if err == nil {
				t.Fatal("expected the payload to be rejected")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want one mentioning %q", err, tc.want)
			}
		})
	}
}

// TestCanonicalizeForProject_AcceptsASlugOnlyTask holds the codec to the same
// identity rule the store's CHECK enforces — a task is identified by a Jira
// key, an SDD change or a slug. The vault importer writes tasks that carry only
// the third, and refusing one of those aborts the push of every other row in
// the project, not just the task.
func TestCanonicalizeForProject_AcceptsASlugOnlyTask(t *testing.T) {
	payload := `{"sync_id":"task-slug-1","project":"wrong","title":"mantenimiento del fork",` +
		`"kind":"spike","state":"open","slug":"mantenimiento-del-fork",` +
		`"created_at":"2026-01-01 10:00:00","updated_at":"2026-01-01 10:00:00"}`
	mutation := canonicalMutation(t, chunkWith(store.SyncEntityTask, "", store.SyncOpUpsert, payload), "nextcloud")
	if mutation.EntityKey != "task-slug-1" {
		t.Fatalf("entity_key = %q, want the task sync_id", mutation.EntityKey)
	}
	if got := payloadField(t, mutation, "slug"); got != "mantenimiento-del-fork" {
		t.Fatalf("slug did not survive canonicalization: %v", got)
	}
	if got := payloadField(t, mutation, "jira_key"); got != nil {
		t.Fatalf("an absent jira_key must stay absent, got %v", got)
	}
}

// TestCanonicalizeForProject_TaskIdentityErrorNamesTheRow covers the other
// half: a task carrying none of the three identities is still rejected, and the
// error names the entity and the row so an operator can find it in a chunk of
// hundreds.
func TestCanonicalizeForProject_TaskIdentityErrorNamesTheRow(t *testing.T) {
	payload := `{"sync_id":"task-anon","project":"nextcloud","title":"t","kind":"bugfix","state":"open",` +
		`"created_at":"2026-01-01 10:00:00","updated_at":"2026-01-01 10:00:00"}`
	_, err := CanonicalizeForProject(chunkWith(store.SyncEntityTask, "", store.SyncOpUpsert, payload), "nextcloud")
	if err == nil {
		t.Fatal("expected a task with no identity at all to be rejected")
	}
	for _, want := range []string{store.SyncEntityTask, "task-anon", "jira_key, sdd_change or slug"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not mention %q", err, want)
		}
	}
}

// TestCanonicalizeForProject_PayloadErrorsNameTheOffendingRow extends the same
// promise to the entities whose identity is not a sync_id.
func TestCanonicalizeForProject_PayloadErrorsNameTheOffendingRow(t *testing.T) {
	cases := []struct {
		name     string
		entity   string
		payload  string
		wantRow  string
		wantText string
	}{
		{
			"evidence", store.SyncEntityEvidence,
			evidenceJSON("/Users/someone/evidence/a.png", validSHA),
			"evd-1", "must be relative",
		},
		{
			"project alias", store.SyncEntityProjectAlias,
			`{"alias":"koi_garden","sync_id":"alias-1","source":"manual","project":"wrong",` +
				`"created_at":"2026-01-01 09:00:00","updated_at":"2026-01-01 09:00:00"}`,
			"koi_garden", "slug is required",
		},
		{
			"observation ref", store.SyncEntityObservationRef,
			`{"observation_sync_id":"obs-77","ref_kind":"graph","ref":"Uploader::put",` +
				`"created_at":"2026-01-05 10:00:00","project":"wrong"}`,
			"obs-77", "graph_commit is required",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := CanonicalizeForProject(chunkWith(tc.entity, "", store.SyncOpUpsert, tc.payload), "nextcloud")
			if err == nil {
				t.Fatalf("expected %q to be rejected", tc.name)
			}
			for _, want := range []string{tc.entity, tc.wantRow, tc.wantText} {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("error %q does not mention %q", err, want)
				}
			}
		})
	}
}
