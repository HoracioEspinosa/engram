package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/HoracioEspinosa/engram/internal/store"
)

// Contract tests for the engram-projects HTTP API (RFC §6). Every test runs
// against a throwaway store in t.TempDir(); none of them ever opens
// ~/.engram/engram.db.

// ─── Harness ─────────────────────────────────────────────────────────────────

type projectsAPI struct {
	t       *testing.T
	handler http.Handler
	store   *store.Store
}

func newProjectsAPI(t *testing.T) *projectsAPI {
	t.Helper()
	// A leftover token from another test would turn every mutation into a 401.
	os.Unsetenv("ENGRAM_HTTP_TOKEN")
	st := newServerTestStore(t)
	return &projectsAPI{t: t, handler: New(st, 0).Handler(), store: st}
}

// do issues a request and returns the recorder. An empty body sends no body
// at all, which is what a real client does for GET.
func (a *projectsAPI) do(method, path, body string) *httptest.ResponseRecorder {
	a.t.Helper()
	var reader io.Reader = http.NoBody
	if body != "" {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	a.handler.ServeHTTP(rec, req)
	return rec
}

// expect asserts the status code and decodes the JSON body.
func (a *projectsAPI) expect(rec *httptest.ResponseRecorder, want int) map[string]any {
	a.t.Helper()
	if rec.Code != want {
		a.t.Fatalf("status = %d, want %d; body = %s", rec.Code, want, rec.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		a.t.Fatalf("decode body %q: %v", rec.Body.String(), err)
	}
	return payload
}

// expectCode asserts both the HTTP status and the error `code` field.
func (a *projectsAPI) expectCode(rec *httptest.ResponseRecorder, status int, code string) map[string]any {
	a.t.Helper()
	payload := a.expect(rec, status)
	if got, _ := payload["code"].(string); got != code {
		a.t.Fatalf("code = %q, want %q; body = %s", got, code, rec.Body.String())
	}
	return payload
}

// seedCard creates a project card through the API itself.
func (a *projectsAPI) seedCard(slug string) {
	a.t.Helper()
	a.expect(a.do(http.MethodPut, "/projects/"+slug, `{"display_name":"`+slug+`"}`), http.StatusCreated)
}

// seedTask creates a task through the API itself and returns its sync_id.
func (a *projectsAPI) seedTask(slug, jiraKey, title string) string {
	a.t.Helper()
	body := fmt.Sprintf(`{"jira_key":%q,"title":%q,"kind":"incident"}`, jiraKey, title)
	payload := a.expect(a.do(http.MethodPost, "/projects/"+slug+"/tasks", body), http.StatusCreated)
	task, _ := payload["task"].(map[string]any)
	syncID, _ := task["sync_id"].(string)
	if syncID == "" {
		a.t.Fatalf("seeded task has no sync_id: %v", payload)
	}
	return syncID
}

// seedObservation stores an observation in project, creating the session row
// its foreign key needs.
func (a *projectsAPI) seedObservation(project, obsType, title string) int64 {
	a.t.Helper()
	sessionID := "sess-" + project + "-" + title
	if err := a.store.CreateSession(sessionID, project, a.t.TempDir()); err != nil {
		a.t.Fatalf("CreateSession: %v", err)
	}
	id, err := a.store.AddObservation(store.AddObservationParams{
		SessionID: sessionID, Type: obsType, Title: title, Content: "seeded by the contract tests",
		Project: project,
	})
	if err != nil {
		a.t.Fatalf("AddObservation: %v", err)
	}
	return id
}

// ─── Acceptance criterion ────────────────────────────────────────────────────

// TestProjectsRoutes_TaskListIsServed is the roadmap's acceptance criterion
// for T-04.03: `engram serve` answers GET /projects/{slug}/tasks.
func TestProjectsRoutes_TaskListIsServed(t *testing.T) {
	a := newProjectsAPI(t)
	a.seedCard("nextcloud")
	a.seedTask("nextcloud", "CDBS-10336", "Previews return 503 on object store files")

	rec := a.do(http.MethodGet, "/projects/nextcloud/tasks", "")
	payload := a.expect(rec, http.StatusOK)

	if got := rec.Header().Get("X-Total-Count"); got != "1" {
		t.Fatalf("X-Total-Count = %q, want %q", got, "1")
	}
	items, _ := payload["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("items = %d, want 1; body = %s", len(items), rec.Body.String())
	}
	first, _ := items[0].(map[string]any)
	if first["jira_key"] != "CDBS-10336" {
		t.Fatalf("unexpected task: %v", first)
	}
	if _, ok := first["state_stale"]; !ok {
		t.Fatalf("expected state_stale on every row, got %v", first)
	}
	for _, key := range []string{"total", "limit", "offset"} {
		if _, ok := payload[key]; !ok {
			t.Fatalf("missing %q in list envelope: %s", key, rec.Body.String())
		}
	}
}

// TestProjectsRoutes_TaskListUnknownProject checks that a slug backed by
// neither a card nor observations is a 404, not an empty page.
func TestProjectsRoutes_TaskListUnknownProject(t *testing.T) {
	a := newProjectsAPI(t)
	a.expectCode(a.do(http.MethodGet, "/projects/ghost/tasks", ""), http.StatusNotFound, "unknown_project")
}

// TestProjectsRoutes_TaskListProjectWithOnlyObservations checks the other
// side of that rule: a project that owns observations but no card answers an
// empty page instead of 404.
func TestProjectsRoutes_TaskListProjectWithOnlyObservations(t *testing.T) {
	a := newProjectsAPI(t)
	a.seedObservation("middleware", "manual", "only-observation")
	payload := a.expect(a.do(http.MethodGet, "/projects/middleware/tasks", ""), http.StatusOK)
	if total, _ := payload["total"].(float64); total != 0 {
		t.Fatalf("total = %v, want 0", payload["total"])
	}
}

// ─── Cards ───────────────────────────────────────────────────────────────────

func TestProjectsRoutes_CardLifecycle(t *testing.T) {
	a := newProjectsAPI(t)

	created := a.expect(a.do(http.MethodPut, "/projects/nextcloud",
		`{"display_name":"Nextcloud server","repo_url":"https://example/repo","owner":"someone@example.com"}`),
		http.StatusCreated)
	if created["created"] != true {
		t.Fatalf("expected created=true, got %v", created)
	}

	// A second PUT is an update, not a create, and leaves omitted fields alone.
	updated := a.expect(a.do(http.MethodPut, "/projects/nextcloud", `{"jira_component":"previews"}`), http.StatusOK)
	if updated["created"] != false {
		t.Fatalf("expected created=false on update, got %v", updated)
	}
	card, _ := updated["card"].(map[string]any)
	if card["display_name"] != "Nextcloud server" {
		t.Fatalf("update overwrote display_name: %v", card)
	}

	got := a.expect(a.do(http.MethodGet, "/projects/nextcloud", ""), http.StatusOK)
	for _, key := range []string{"card", "counts", "sync"} {
		if _, ok := got[key]; !ok {
			t.Fatalf("GET card missing %q: %v", key, got)
		}
	}
}

func TestProjectsRoutes_CardNotFoundAndInvalidSlug(t *testing.T) {
	a := newProjectsAPI(t)
	a.expectCode(a.do(http.MethodGet, "/projects/ghost", ""), http.StatusNotFound, "no_card")
	a.expectCode(a.do(http.MethodGet, "/projects/Not_A_Slug", ""), http.StatusBadRequest, "invalid_slug")
}

// TestProjectsRoutes_MigrateStillWinsOverCardUpsert pins RFC §6.1's routing
// rule: the pre-existing POST /projects/migrate keeps its handler, and the
// reserved slug can never be created through PUT /projects/{slug}.
func TestProjectsRoutes_MigrateStillWinsOverCardUpsert(t *testing.T) {
	a := newProjectsAPI(t)

	migrate := a.expect(a.do(http.MethodPost, "/projects/migrate",
		`{"old_project":"alpha","new_project":"beta"}`), http.StatusOK)
	if migrate["status"] != "skipped" {
		t.Fatalf("POST /projects/migrate did not reach the migration handler: %v", migrate)
	}

	a.expectCode(a.do(http.MethodPut, "/projects/migrate", `{}`), http.StatusBadRequest, "invalid_slug")
	a.expectCode(a.do(http.MethodPut, "/projects/current", `{}`), http.StatusBadRequest, "invalid_slug")

	// And the card never came into existence.
	if exists, err := a.store.ProjectCardExists("migrate"); err != nil || exists {
		t.Fatalf("reserved slug leaked into project_cards (exists=%v, err=%v)", exists, err)
	}
}

func TestProjectsRoutes_ProjectsListWithCounts(t *testing.T) {
	a := newProjectsAPI(t)
	a.seedCard("nextcloud")
	a.seedCard("middleware")
	a.seedTask("nextcloud", "CDBS-1", "one")

	rec := a.do(http.MethodGet, "/projects?include_counts=true", "")
	payload := a.expect(rec, http.StatusOK)
	if rec.Header().Get("X-Total-Count") != "2" {
		t.Fatalf("X-Total-Count = %q, want 2", rec.Header().Get("X-Total-Count"))
	}
	items, _ := payload["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("items = %d, want 2", len(items))
	}
	for _, raw := range items {
		item, _ := raw.(map[string]any)
		counts, ok := item["counts"].(map[string]any)
		if !ok {
			t.Fatalf("include_counts=true did not attach counts: %v", item)
		}
		if item["slug"] == "nextcloud" {
			if active, _ := counts["tasks_active"].(float64); active != 1 {
				t.Fatalf("nextcloud tasks_active = %v, want 1", counts["tasks_active"])
			}
		}
	}

	// Without the flag the counters are omitted entirely.
	plain := a.expect(a.do(http.MethodGet, "/projects", ""), http.StatusOK)
	first, _ := plain["items"].([]any)[0].(map[string]any)
	if _, present := first["counts"]; present {
		t.Fatalf("counts must be omitted unless include_counts=true: %v", first)
	}
}

// ─── Tasks ───────────────────────────────────────────────────────────────────

func TestProjectsRoutes_TaskUpsertConflict409(t *testing.T) {
	a := newProjectsAPI(t)
	a.seedCard("nextcloud")
	a.seedCard("middleware")
	a.seedTask("nextcloud", "CDBS-10336", "owned by nextcloud")

	rec := a.do(http.MethodPost, "/projects/middleware/tasks",
		`{"jira_key":"CDBS-10336","title":"stolen","kind":"bugfix"}`)
	payload := a.expectCode(rec, http.StatusConflict, "task_key_conflict")
	if payload["existing_project"] != "nextcloud" {
		t.Fatalf("expected existing_project=nextcloud, got %v", payload)
	}
}

func TestProjectsRoutes_TaskUpsertValidation(t *testing.T) {
	a := newProjectsAPI(t)
	a.seedCard("nextcloud")

	a.expectCode(a.do(http.MethodPost, "/projects/nextcloud/tasks", `{"title":"no key","kind":"bugfix"}`),
		http.StatusBadRequest, "missing_field")
	a.expectCode(a.do(http.MethodPost, "/projects/nextcloud/tasks",
		`{"jira_key":"cdbs-1","title":"bad key","kind":"bugfix"}`), http.StatusBadRequest, "invalid_enum")
	a.expectCode(a.do(http.MethodPost, "/projects/nextcloud/tasks",
		`{"jira_key":"CDBS-2","title":"bad kind","kind":"epic"}`), http.StatusBadRequest, "invalid_enum")
	a.expectCode(a.do(http.MethodPost, "/projects/nextcloud/tasks",
		`{"jira_key":"CDBS-3","kind":"bugfix"}`), http.StatusBadRequest, "missing_field")
	a.expectCode(a.do(http.MethodPost, "/projects/nextcloud/tasks",
		`{"jira_key":"CDBS-4","title":"t","kind":"bugfix","project":"elsewhere"}`),
		http.StatusBadRequest, "invalid_field")
}

func TestProjectsRoutes_TaskDetailAndObservations(t *testing.T) {
	a := newProjectsAPI(t)
	a.seedCard("nextcloud")
	a.seedTask("nextcloud", "CDBS-10336", "previews")

	obsID := a.seedObservation("nextcloud", "discovery", "root cause")

	linked := a.expect(a.do(http.MethodPost, "/projects/nextcloud/tasks/CDBS-10336/links",
		fmt.Sprintf(`{"observation_id":%d,"role":"root_cause","knowledge_ref":"Services/Nextcloud/Previews.md"}`, obsID)),
		http.StatusCreated)
	if linked["linked"] != true {
		t.Fatalf("expected linked=true, got %v", linked)
	}
	if refs, _ := linked["refs_added"].(float64); refs != 1 {
		t.Fatalf("refs_added = %v, want 1", linked["refs_added"])
	}

	detail := a.expect(a.do(http.MethodGet, "/projects/nextcloud/tasks/CDBS-10336", ""), http.StatusOK)
	counts, _ := detail["counts"].(map[string]any)
	if obs, _ := counts["observations"].(float64); obs != 1 {
		t.Fatalf("task counts.observations = %v, want 1", counts["observations"])
	}
	if _, ok := detail["state_stale"]; !ok {
		t.Fatalf("task detail missing state_stale: %v", detail)
	}

	// The task reference forms are interchangeable.
	a.expect(a.do(http.MethodGet, "/projects/nextcloud/tasks/%23"+"1", ""), http.StatusOK)

	list := a.expect(a.do(http.MethodGet,
		"/projects/nextcloud/tasks/CDBS-10336/observations?role=root_cause", ""), http.StatusOK)
	if total, _ := list["total"].(float64); total != 1 {
		t.Fatalf("observations total = %v, want 1", list["total"])
	}
	empty := a.expect(a.do(http.MethodGet,
		"/projects/nextcloud/tasks/CDBS-10336/observations?role=decision", ""), http.StatusOK)
	if total, _ := empty["total"].(float64); total != 0 {
		t.Fatalf("role filter ignored: %v", empty)
	}
	a.expectCode(a.do(http.MethodGet,
		"/projects/nextcloud/tasks/CDBS-10336/observations?role=nonsense", ""),
		http.StatusBadRequest, "invalid_enum")
}

func TestProjectsRoutes_LinkGraphCommitRequired422(t *testing.T) {
	a := newProjectsAPI(t)
	a.seedCard("nextcloud")
	a.seedTask("nextcloud", "CDBS-10336", "previews")
	obsID := a.seedObservation("nextcloud", "discovery", "graph fact")

	rec := a.do(http.MethodPost, "/projects/nextcloud/tasks/CDBS-10336/links",
		fmt.Sprintf(`{"observation_id":%d,"role":"root_cause","graph_ref":"OC\\Files\\Storage\\Wrapper"}`, obsID))
	a.expectCode(rec, http.StatusUnprocessableEntity, "graph_commit_required")

	// A 422 must leave nothing behind: otherwise the corrected retry would
	// hit INSERT OR IGNORE and keep the default role forever.
	detail := a.expect(a.do(http.MethodGet, "/projects/nextcloud/tasks/CDBS-10336", ""), http.StatusOK)
	counts, _ := detail["counts"].(map[string]any)
	if obs, _ := counts["observations"].(float64); obs != 0 {
		t.Fatalf("the rejected link was persisted anyway: counts = %v", counts)
	}

	linked := a.expect(a.do(http.MethodPost, "/projects/nextcloud/tasks/CDBS-10336/links",
		fmt.Sprintf(`{"observation_id":%d,"role":"root_cause","graph_ref":"OC\\Files\\Storage\\Wrapper",`+
			`"graph_commit":"7a79ef43a9570000000000000000000000000000"}`, obsID)), http.StatusCreated)
	if linked["role"] != "root_cause" {
		t.Fatalf("retry stored role %v, want root_cause", linked["role"])
	}
}

func TestProjectsRoutes_LinkCrossProject409(t *testing.T) {
	a := newProjectsAPI(t)
	a.seedCard("nextcloud")
	a.seedTask("nextcloud", "CDBS-10336", "previews")
	obsID := a.seedObservation("middleware", "discovery", "other project")

	rec := a.do(http.MethodPost, "/projects/nextcloud/tasks/CDBS-10336/links",
		fmt.Sprintf(`{"observation_id":%d}`, obsID))
	a.expectCode(rec, http.StatusConflict, "cross_project_link")
}

func TestProjectsRoutes_LinkUnknownTaskAndObservation(t *testing.T) {
	a := newProjectsAPI(t)
	a.seedCard("nextcloud")
	a.expectCode(a.do(http.MethodPost, "/projects/nextcloud/tasks/CDBS-99999/links",
		`{"observation_id":1}`), http.StatusNotFound, "unknown_task")

	a.seedTask("nextcloud", "CDBS-10336", "previews")
	a.expectCode(a.do(http.MethodPost, "/projects/nextcloud/tasks/CDBS-10336/links",
		`{"observation_sync_id":"obs-00000000000000ff"}`), http.StatusNotFound, "unknown_observation")
	a.expectCode(a.do(http.MethodPost, "/projects/nextcloud/tasks/CDBS-10336/links", `{}`),
		http.StatusBadRequest, "missing_field")
}

// ─── Evidence ────────────────────────────────────────────────────────────────

const testEvidenceSHA = "9f2b1c0a7e4d5b6c8a1f3e2d4c5b6a7f8e9d0c1b2a3f4e5d6c7b8a9f0e1d2c3b"

func TestProjectsRoutes_EvidenceRoundTrip(t *testing.T) {
	a := newProjectsAPI(t)
	a.seedCard("nextcloud")
	a.seedTask("nextcloud", "CDBS-10336", "previews")

	body := fmt.Sprintf(`{"path":"clarodrive/CDBS-10336/01-preview-200.png","sha256":%q,`+
		`"kind":"png","proves":"preview returns 200 after the fix","size_bytes":412337,"attached_jira":true}`,
		testEvidenceSHA)

	created := a.expect(a.do(http.MethodPost, "/projects/nextcloud/tasks/CDBS-10336/evidence", body),
		http.StatusCreated)
	if created["duplicate"] != false {
		t.Fatalf("expected duplicate=false on first insert: %v", created)
	}
	if _, ok := created["limits"]; !ok {
		t.Fatalf("expected the D-06 limits report: %v", created)
	}

	// Idempotent by (task, sha256).
	again := a.expect(a.do(http.MethodPost, "/projects/nextcloud/tasks/CDBS-10336/evidence", body),
		http.StatusCreated)
	if again["duplicate"] != true {
		t.Fatalf("expected duplicate=true on re-register: %v", again)
	}

	perTask := a.expect(a.do(http.MethodGet, "/projects/nextcloud/tasks/CDBS-10336/evidence", ""), http.StatusOK)
	if total, _ := perTask["total"].(float64); total != 1 {
		t.Fatalf("per-task evidence total = %v, want 1", perTask["total"])
	}

	rec := a.do(http.MethodGet, "/projects/nextcloud/evidence?attached_jira=true", "")
	perProject := a.expect(rec, http.StatusOK)
	if bytes, _ := perProject["total_bytes"].(float64); bytes != 412337 {
		t.Fatalf("total_bytes = %v, want 412337", perProject["total_bytes"])
	}
	if rec.Header().Get("X-Total-Count") != "1" {
		t.Fatalf("X-Total-Count = %q, want 1", rec.Header().Get("X-Total-Count"))
	}

	filtered := a.expect(a.do(http.MethodGet, "/projects/nextcloud/evidence?attached_jira=false", ""), http.StatusOK)
	if total, _ := filtered["total"].(float64); total != 0 {
		t.Fatalf("attached_jira=false should be empty, got %v", filtered)
	}
}

func TestProjectsRoutes_EvidenceValidation(t *testing.T) {
	a := newProjectsAPI(t)
	a.seedCard("nextcloud")
	a.seedTask("nextcloud", "CDBS-10336", "previews")
	base := "/projects/nextcloud/tasks/CDBS-10336/evidence"

	a.expectCode(a.do(http.MethodPost, base, `{"path":"a.png","kind":"png","proves":"x"}`),
		http.StatusBadRequest, "missing_field")
	a.expectCode(a.do(http.MethodPost, base,
		`{"path":"a.png","sha256":"NOTHEX","kind":"png","proves":"x"}`),
		http.StatusBadRequest, "invalid_sha256")
	a.expectCode(a.do(http.MethodPost, base,
		fmt.Sprintf(`{"path":"a.png","sha256":%q,"kind":"pdf","proves":"x"}`, testEvidenceSHA)),
		http.StatusBadRequest, "invalid_enum")
	a.expectCode(a.do(http.MethodPost, base,
		fmt.Sprintf(`{"path":"/abs/a.png","sha256":%q,"kind":"png","proves":"x"}`, testEvidenceSHA)),
		http.StatusBadRequest, "absolute_path_rejected")
}

// ─── Runbooks ────────────────────────────────────────────────────────────────

const knowledgeMCPEntries = `{"source":"knowledge-mcp","entries":[
  {"id":"RB-003","vault_path":"Runbooks/Performance/RB-003 Preview Endpoint Slow Or Failing.md",
   "title":"Preview endpoint slow or failing","service":"nextcloud","category":"performance",
   "pattern":"missing-files","severity":"P2","status":"verified",
   "symptoms":["GET /index.php/core/preview returns 503","previews time out on object store files"],
   "needs_review":true,"age_days":105},
  {"id":"RB-000","vault_path":"Runbooks/Templates/Auth Issue Template.md","title":"Auth template",
   "service":"middleware","category":"auth","status":"draft","tags":["template"]}
]}`

func TestProjectsRoutes_RunbookSyncListAndFind(t *testing.T) {
	a := newProjectsAPI(t)
	a.seedCard("nextcloud")

	synced := a.expect(a.do(http.MethodPost, "/projects/nextcloud/runbooks/sync", knowledgeMCPEntries),
		http.StatusOK)
	if up, _ := synced["upserted"].(float64); up != 1 {
		t.Fatalf("upserted = %v, want 1; body = %v", synced["upserted"], synced)
	}
	skipped, _ := synced["skipped"].([]any)
	if len(skipped) != 1 {
		t.Fatalf("expected the template to be skipped, got %v", synced["skipped"])
	}
	if reason, _ := skipped[0].(map[string]any)["reason"].(string); reason != "template" {
		t.Fatalf("skip reason = %q, want template", reason)
	}
	if stale, _ := synced["stale_count"].(float64); stale != 1 {
		t.Fatalf("stale_count = %v, want 1", synced["stale_count"])
	}

	rec := a.do(http.MethodGet, "/projects/nextcloud/runbooks?stale=true", "")
	list := a.expect(rec, http.StatusOK)
	if total, _ := list["total"].(float64); total != 1 {
		t.Fatalf("stale runbooks = %v, want 1", list["total"])
	}
	if rec.Header().Get("X-Total-Count") != "1" {
		t.Fatalf("X-Total-Count = %q, want 1", rec.Header().Get("X-Total-Count"))
	}
	items, _ := list["items"].([]any)
	first, _ := items[0].(map[string]any)
	symptoms, _ := first["symptoms"].([]any)
	if len(symptoms) != 2 {
		t.Fatalf("symptoms did not round-trip: %v", first)
	}

	if fresh := a.expect(a.do(http.MethodGet, "/projects/nextcloud/runbooks?stale=false", ""), http.StatusOK); fresh["total"].(float64) != 0 {
		t.Fatalf("stale=false should be empty, got %v", fresh)
	}
	a.expectCode(a.do(http.MethodGet, "/projects/nextcloud/runbooks?category=nonsense", ""),
		http.StatusBadRequest, "invalid_enum")

	found := a.expect(a.do(http.MethodGet, "/projects/nextcloud/runbooks/find?q=preview+503+object+store", ""),
		http.StatusOK)
	foundItems, _ := found["items"].([]any)
	if len(foundItems) != 1 {
		t.Fatalf("find returned %d items, want 1: %v", len(foundItems), found)
	}
	if id, _ := foundItems[0].(map[string]any)["id"].(string); id != "RB-003" {
		t.Fatalf("find returned %v, want RB-003", foundItems[0])
	}
	a.expectCode(a.do(http.MethodGet, "/projects/nextcloud/runbooks/find?q=ab", ""),
		http.StatusBadRequest, "missing_field")
}

func TestProjectsRoutes_RunbookSyncFromVaultCheckout(t *testing.T) {
	a := newProjectsAPI(t)
	a.seedCard("nextcloud")
	vault := writeTestVault(t)

	body := fmt.Sprintf(`{"source":"vault-fs","vault_dir":%q}`, vault)
	synced := a.expect(a.do(http.MethodPost, "/projects/nextcloud/runbooks/sync", body), http.StatusOK)

	if scanned, _ := synced["scanned"].(float64); scanned != 4 {
		t.Fatalf("scanned = %v, want 4; body = %v", synced["scanned"], synced)
	}
	if up, _ := synced["upserted"].(float64); up != 1 {
		t.Fatalf("upserted = %v, want 1; body = %v", synced["upserted"], synced)
	}
	reasons := map[string]bool{}
	for _, raw := range synced["skipped"].([]any) {
		reason, _ := raw.(map[string]any)["reason"].(string)
		reasons[reason] = true
	}
	for _, want := range []string{"template", "not_runbook", "unknown_service"} {
		if !reasons[want] {
			t.Fatalf("missing skip reason %q in %v", want, synced["skipped"])
		}
	}

	list := a.expect(a.do(http.MethodGet, "/projects/nextcloud/runbooks", ""), http.StatusOK)
	if total, _ := list["total"].(float64); total != 1 {
		t.Fatalf("indexed runbooks = %v, want 1", list["total"])
	}
}

// TestProjectsRoutes_RunbookSyncFilteringIsNotAnError pins the split between
// a malformed body (400) and a correct payload the store filtered out (200).
// The weekly vault sync depends on it: a checkout holding only templates is
// an empty result, not a failed run.
func TestProjectsRoutes_RunbookSyncFilteringIsNotAnError(t *testing.T) {
	a := newProjectsAPI(t)
	a.seedCard("nextcloud")

	onlyTemplates := `{"source":"knowledge-mcp","entries":[
	  {"id":"RB-000","vault_path":"Runbooks/Templates/Auth Issue Template.md","title":"Auth template",
	   "service":"middleware","category":"auth","status":"draft","tags":["template"]}
	]}`
	filtered := a.expect(a.do(http.MethodPost, "/projects/nextcloud/runbooks/sync", onlyTemplates), http.StatusOK)
	if up, _ := filtered["upserted"].(float64); up != 0 {
		t.Fatalf("upserted = %v, want 0", filtered["upserted"])
	}
	if skipped, _ := filtered["skipped"].([]any); len(skipped) != 1 {
		t.Fatalf("expected the template in skipped, got %v", filtered["skipped"])
	}

	// A vault whose Runbooks/ folder holds nothing indexable is also a 200.
	emptyVault := t.TempDir()
	if err := os.MkdirAll(filepath.Join(emptyVault, "Runbooks"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	empty := a.expect(a.do(http.MethodPost, "/projects/nextcloud/runbooks/sync",
		fmt.Sprintf(`{"source":"vault-fs","vault_dir":%q}`, emptyVault)), http.StatusOK)
	if scanned, _ := empty["scanned"].(float64); scanned != 0 {
		t.Fatalf("scanned = %v, want 0", empty["scanned"])
	}

	// A body whose entries are all structurally malformed is still a 400.
	malformed := `{"source":"knowledge-mcp","entries":[{"id":"RB-001"},{"title":"no id"}]}`
	a.expectCode(a.do(http.MethodPost, "/projects/nextcloud/runbooks/sync", malformed),
		http.StatusBadRequest, "entries_rejected")
}

func TestProjectsRoutes_RunbookSyncSourceValidation(t *testing.T) {
	a := newProjectsAPI(t)
	a.seedCard("nextcloud")

	a.expectCode(a.do(http.MethodPost, "/projects/nextcloud/runbooks/sync", `{"source":"carrier-pigeon"}`),
		http.StatusBadRequest, "invalid_enum")
	a.expectCode(a.do(http.MethodPost, "/projects/nextcloud/runbooks/sync", `{"source":"vault-fs"}`),
		http.StatusBadRequest, "missing_field")
	a.expectCode(a.do(http.MethodPost, "/projects/nextcloud/runbooks/sync",
		`{"source":"vault-fs","vault_dir":"relative/path"}`), http.StatusBadRequest, "vault_scan_failed")
	a.expectCode(a.do(http.MethodPost, "/projects/nextcloud/runbooks/sync",
		fmt.Sprintf(`{"source":"vault-fs","vault_dir":%q}`, t.TempDir())),
		http.StatusNotFound, "vault_not_found")
	a.expectCode(a.do(http.MethodPost, "/projects/nextcloud/runbooks/sync", `{"entries":[]}`),
		http.StatusBadRequest, "entries_rejected")
}

// ─── Context pack ────────────────────────────────────────────────────────────

func TestProjectsRoutes_ContextPackFormats(t *testing.T) {
	a := newProjectsAPI(t)
	a.seedCard("nextcloud")
	a.seedTask("nextcloud", "CDBS-10336", "previews return 503")

	rec := a.do(http.MethodGet, "/projects/nextcloud/tasks/CDBS-10336/context?max_chars=6000", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/markdown") {
		t.Fatalf("Content-Type = %q, want text/markdown", ct)
	}
	if !strings.Contains(rec.Body.String(), "CDBS-10336") {
		t.Fatalf("markdown pack does not mention the task: %s", rec.Body.String())
	}

	asJSON := a.expect(a.do(http.MethodGet,
		"/projects/nextcloud/tasks/CDBS-10336/context?format=json", ""), http.StatusOK)
	if _, ok := asJSON["task"]; !ok {
		t.Fatalf("json pack has no task section: %v", asJSON)
	}

	a.expectCode(a.do(http.MethodGet,
		"/projects/nextcloud/tasks/CDBS-10336/context?format=yaml", ""), http.StatusBadRequest, "invalid_enum")
	a.expectCode(a.do(http.MethodGet,
		"/projects/nextcloud/tasks/CDBS-10336/context?sections=header,nonsense", ""),
		http.StatusBadRequest, "invalid_enum")
	a.expectCode(a.do(http.MethodGet,
		"/projects/nextcloud/tasks/CDBS-99999/context", ""), http.StatusNotFound, "unknown_task")
}

// ─── Graph sync ──────────────────────────────────────────────────────────────

func TestProjectsRoutes_GraphSync(t *testing.T) {
	a := newProjectsAPI(t)
	a.seedCard("nextcloud")

	a.expectCode(a.do(http.MethodPost, "/projects/nextcloud/graph/sync", `{}`),
		http.StatusBadRequest, "missing_field")
	a.expectCode(a.do(http.MethodPost, "/projects/nextcloud/graph/sync", `{"repo_dir":"relative"}`),
		http.StatusBadRequest, "invalid_path")
	a.expectCode(a.do(http.MethodPost, "/projects/nextcloud/graph/sync",
		fmt.Sprintf(`{"repo_dir":%q}`, t.TempDir())), http.StatusNotFound, "graph_not_found")

	// A graph without built_at_commit must persist nothing at all.
	noCommit := writeTestGraph(t, "")
	a.expectCode(a.do(http.MethodPost, "/projects/nextcloud/graph/sync",
		fmt.Sprintf(`{"repo_dir":%q}`, noCommit)), http.StatusUnprocessableEntity, "graph_missing_commit")
	card, err := a.store.GetProjectCard("nextcloud")
	if err != nil {
		t.Fatalf("GetProjectCard: %v", err)
	}
	if card.GraphCommit != nil {
		t.Fatalf("a commit-less graph must not stamp the card, got %v", *card.GraphCommit)
	}

	const commit = "0123456789012345678901234567890123456789"
	good := writeTestGraph(t, commit)
	payload := a.expect(a.do(http.MethodPost, "/projects/nextcloud/graph/sync",
		fmt.Sprintf(`{"repo_dir":%q}`, good)), http.StatusOK)
	graph, _ := payload["graph"].(map[string]any)
	if graph["graph_commit"] != commit {
		t.Fatalf("graph_commit = %v, want %s", graph["graph_commit"], commit)
	}
	stamped, err := a.store.GetProjectCard("nextcloud")
	if err != nil {
		t.Fatalf("GetProjectCard: %v", err)
	}
	if stamped.GraphCommit == nil || *stamped.GraphCommit != commit {
		t.Fatalf("card was not stamped: %+v", stamped.GraphCommit)
	}
}

func TestProjectsRoutes_GraphSyncWithoutCard(t *testing.T) {
	a := newProjectsAPI(t)
	a.seedObservation("nextcloud", "manual", "no card yet")
	a.expectCode(a.do(http.MethodPost, "/projects/nextcloud/graph/sync",
		fmt.Sprintf(`{"repo_dir":%q}`, t.TempDir())), http.StatusNotFound, "no_card")
}

// ─── Payload limits ──────────────────────────────────────────────────────────

func TestProjectsRoutes_PayloadTooLarge(t *testing.T) {
	a := newProjectsAPI(t)
	a.seedCard("nextcloud")

	// The padding is a literal, not projectsMaxBodyBytes+n: deriving it from
	// the constant would make the assertion follow the limit wherever it
	// moved instead of pinning the documented 64 KiB.
	const oversizedBytes = 128 * 1024
	oversized := fmt.Sprintf(`{"display_name":%q}`, strings.Repeat("x", oversizedBytes))
	payload := a.expectCode(a.do(http.MethodPut, "/projects/nextcloud", oversized),
		http.StatusRequestEntityTooLarge, "payload_too_large")
	if limit, _ := payload["limit_bytes"].(float64); int64(limit) != 64<<10 {
		t.Fatalf("limit_bytes = %v, want %d", payload["limit_bytes"], 64<<10)
	}

	var entries []string
	for i := 0; i < runbookSyncMaxEntries+1; i++ {
		entries = append(entries, fmt.Sprintf(
			`{"id":"RB-%03d","vault_path":"Runbooks/RB-%03d.md","title":"t%d","service":"nextcloud",`+
				`"category":"auth","status":"draft"}`, i%1000, i%1000, i))
	}
	body := `{"source":"knowledge-mcp","entries":[` + strings.Join(entries, ",") + `]}`
	a.expectCode(a.do(http.MethodPost, "/projects/nextcloud/runbooks/sync", body),
		http.StatusRequestEntityTooLarge, "payload_too_large")
}

func TestProjectsRoutes_RunbookEntriesByteCap(t *testing.T) {
	a := newProjectsAPI(t)
	a.seedCard("nextcloud")

	// Under the item cap but over the 1 MiB entries cap.
	padding := strings.Repeat("s", 4096)
	var entries []string
	for i := 0; i < 300; i++ {
		entries = append(entries, fmt.Sprintf(
			`{"id":"RB-%03d","vault_path":"Runbooks/RB-%03d.md","title":%q,"service":"nextcloud",`+
				`"category":"auth","status":"draft"}`, i, i, padding))
	}
	body := `{"source":"knowledge-mcp","entries":[` + strings.Join(entries, ",") + `]}`
	payload := a.expectCode(a.do(http.MethodPost, "/projects/nextcloud/runbooks/sync", body),
		http.StatusRequestEntityTooLarge, "payload_too_large")
	if _, ok := payload["entries_bytes"]; !ok {
		t.Fatalf("expected entries_bytes in the 413 body: %v", payload)
	}
}

// ─── Pagination ──────────────────────────────────────────────────────────────

func TestProjectsRoutes_PaginationClampAndWindow(t *testing.T) {
	a := newProjectsAPI(t)
	a.seedCard("nextcloud")
	for i := 1; i <= 5; i++ {
		a.seedTask("nextcloud", fmt.Sprintf("CDBS-%d", i), fmt.Sprintf("task %d", i))
	}

	page := a.expect(a.do(http.MethodGet, "/projects/nextcloud/tasks?limit=2&offset=2", ""), http.StatusOK)
	if total, _ := page["total"].(float64); total != 5 {
		t.Fatalf("total = %v, want 5", page["total"])
	}
	if items, _ := page["items"].([]any); len(items) != 2 {
		t.Fatalf("page size = %d, want 2", len(items))
	}
	if limit, _ := page["limit"].(float64); limit != 2 {
		t.Fatalf("limit echoed as %v, want 2", page["limit"])
	}

	clamped := a.expect(a.do(http.MethodGet, "/projects/nextcloud/tasks?limit=9999&offset=-4", ""), http.StatusOK)
	if limit, _ := clamped["limit"].(float64); limit != float64(projectsMaxLimit) {
		t.Fatalf("limit = %v, want %d", clamped["limit"], projectsMaxLimit)
	}
	if offset, _ := clamped["offset"].(float64); offset != 0 {
		t.Fatalf("offset = %v, want 0", clamped["offset"])
	}

	beyond := a.expect(a.do(http.MethodGet,
		"/projects/nextcloud/tasks/CDBS-1/observations?offset=50", ""), http.StatusOK)
	if items, _ := beyond["items"].([]any); len(items) != 0 {
		t.Fatalf("offset past the end must be empty, got %d items", len(items))
	}
}

// ─── Auth ────────────────────────────────────────────────────────────────────

// projectMutations lists every engram-projects route that must answer 401
// when ENGRAM_HTTP_TOKEN is set and no bearer token is presented.
var projectMutations = []authCase{
	{http.MethodPut, "/projects/nextcloud", `{}`},
	{http.MethodPost, "/projects/nextcloud/graph/sync", `{"repo_dir":"/tmp"}`},
	{http.MethodPost, "/projects/nextcloud/tasks", `{"jira_key":"CDBS-1","title":"t","kind":"bugfix"}`},
	{http.MethodPost, "/projects/nextcloud/tasks/CDBS-1/links", `{"observation_id":1}`},
	{http.MethodPost, "/projects/nextcloud/tasks/CDBS-1/evidence", `{}`},
	{http.MethodPost, "/projects/nextcloud/runbooks/sync", `{"entries":[]}`},
}

// projectReads lists the engram-projects routes that must stay open: the
// server binds to loopback only (RFC §6.1).
var projectReads = []authCase{
	{http.MethodGet, "/projects", ""},
	{http.MethodGet, "/projects/nextcloud", ""},
	{http.MethodGet, "/projects/nextcloud/tasks", ""},
	{http.MethodGet, "/projects/nextcloud/tasks/CDBS-1", ""},
	{http.MethodGet, "/projects/nextcloud/tasks/CDBS-1/observations", ""},
	{http.MethodGet, "/projects/nextcloud/tasks/CDBS-1/evidence", ""},
	{http.MethodGet, "/projects/nextcloud/tasks/CDBS-1/context", ""},
	{http.MethodGet, "/projects/nextcloud/evidence", ""},
	{http.MethodGet, "/projects/nextcloud/runbooks", ""},
	{http.MethodGet, "/projects/nextcloud/runbooks/find?q=preview", ""},
}

func TestProjectsRoutes_AuthGuardsMutationsOnly(t *testing.T) {
	a := newProjectsAPI(t)
	a.seedCard("nextcloud")
	a.seedTask("nextcloud", "CDBS-1", "seeded before the token is set")

	t.Setenv("ENGRAM_HTTP_TOKEN", "unit-test-token-not-a-real-secret")

	for _, tc := range projectMutations {
		t.Run("guarded "+tc.method+" "+tc.path, func(t *testing.T) {
			rec := a.do(tc.method, tc.path, tc.body)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401; body = %s", rec.Code, rec.Body.String())
			}
			if rec.Header().Get("WWW-Authenticate") == "" {
				t.Fatalf("401 without a WWW-Authenticate challenge")
			}
		})
	}

	for _, tc := range projectReads {
		t.Run("open "+tc.method+" "+tc.path, func(t *testing.T) {
			rec := a.do(tc.method, tc.path, tc.body)
			if rec.Code == http.StatusUnauthorized || rec.Code == http.StatusForbidden {
				t.Fatalf("read route is gated: status = %d", rec.Code)
			}
		})
	}
}

func TestProjectsRoutes_AuthAcceptsBearerToken(t *testing.T) {
	a := newProjectsAPI(t)
	const token = "unit-test-token-not-a-real-secret"
	t.Setenv("ENGRAM_HTTP_TOKEN", token)

	req := httptest.NewRequest(http.MethodPut, "/projects/nextcloud", strings.NewReader(`{"owner":"someone"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	a.handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body = %s", rec.Code, rec.Body.String())
	}
}

// ─── Write notification ──────────────────────────────────────────────────────

// TestProjectsRoutes_WritesNotifyAutosync pins RFC §6.1's "every successful
// write calls notifyWrite" rule: without it, a task created over HTTP would
// sit unsynced until some other write happened to wake the manager.
func TestProjectsRoutes_WritesNotifyAutosync(t *testing.T) {
	os.Unsetenv("ENGRAM_HTTP_TOKEN")
	st := newServerTestStore(t)
	srv := New(st, 0)
	writes := 0
	srv.SetOnWrite(func() { writes++ })
	a := &projectsAPI{t: t, handler: srv.Handler(), store: st}

	a.seedCard("nextcloud")
	a.seedTask("nextcloud", "CDBS-10336", "previews")
	if writes != 2 {
		t.Fatalf("onWrite fired %d times, want 2", writes)
	}

	// A rejected write must not wake autosync.
	a.expectCode(a.do(http.MethodPost, "/projects/nextcloud/tasks", `{"title":"no key","kind":"bugfix"}`),
		http.StatusBadRequest, "missing_field")
	if writes != 2 {
		t.Fatalf("onWrite fired on a rejected write: %d", writes)
	}
}

// ─── Fixtures ────────────────────────────────────────────────────────────────

// writeTestVault builds a miniature vault checkout: one indexable runbook,
// one template, one playbook and one runbook for a service outside the
// canonical list.
func writeTestVault(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	write := func(rel, content string) {
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", full, err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", full, err)
		}
	}

	write("Runbooks/Performance/RB-003 Preview Endpoint Slow Or Failing.md", `---
type: runbook
id: RB-003
title: "Preview endpoint slow or failing"
service: nextcloud
severity: P2
category: performance
status: verified
symptoms:
  - "GET /index.php/core/preview returns 503"
  - "previews time out on object store files"
tags:
  - type/runbook
  - service/nextcloud
last_updated: 2020-01-01
---

# RB-003
`)

	write("Runbooks/Templates/Auth Issue Template.md", `---
type: runbook
category: auth
status: open
tags: [type/runbook, template]
---

# RB-XXX
`)

	write("Runbooks/Telmex Account Diagnostic Playbook.md", `---
type: playbook
title: "Telmex account diagnostic"
service: middleware
---

# Playbook
`)

	write("Runbooks/Network/RB-009 Unknown Service.md", `---
type: runbook
id: RB-009
title: "Runbook for a service outside the canonical list"
service: some-service-that-does-not-exist
category: network
status: verified
last_updated: 2026-01-01
---

# RB-009
`)

	return root
}

// writeTestGraph builds a minimal graphify-out/graph.json inside a temp repo.
// An empty commit produces the malformed case RFC §8.3 step 2 rejects.
func writeTestGraph(t *testing.T, commit string) string {
	t.Helper()
	repo := t.TempDir()
	dir := filepath.Join(repo, "graphify-out")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}

	var b strings.Builder
	b.WriteString(`{"directed":false,"multigraph":false,"graph":{},"nodes":[`)
	for i := 0; i < 12; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(fmt.Sprintf(
			`{"id":"n%d","label":"Sym%d","file_type":"code","source_file":"pkg/file%d.go","community":%d}`,
			i, i, i, i%3))
	}
	b.WriteString(`],"links":[`)
	for i := 0; i < 20; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(fmt.Sprintf(
			`{"source":"n%d","target":"n%d","relation":"CALLS","confidence":"EXTRACTED"}`, i%12, (i*7+3)%12))
	}
	b.WriteString(`],"hyperedges":[],"built_at_commit":` + strconv.Quote(commit) + `}`)

	if err := os.WriteFile(filepath.Join(dir, "graph.json"), []byte(b.String()), 0o644); err != nil {
		t.Fatalf("write graph.json: %v", err)
	}
	return repo
}
