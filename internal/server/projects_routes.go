// engram-projects HTTP API (RFC rfc-engram-projects.md §6): the 16 routes
// under /projects that expose the same domain the `projects` MCP tool profile
// exposes, for clients that speak HTTP instead of MCP (the TUI if it is ever
// decoupled from the store, hooks, and cd-knowledge-mcp v1.1).
//
// Conventions, all from RFC §6.1:
//   - Reads are open, mutations are wrapped in requireAuth, exactly as
//     DELETE, /export, /import and /projects/migrate already are. The server
//     only listens on loopback, which is what makes open reads acceptable.
//   - Errors carry {"error": message, "code": code, ...fields}; the codes are
//     the same vocabulary the MCP tools return, so a client can switch
//     transports without relearning them.
//   - Lists answer {items, total, limit, offset} plus an X-Total-Count
//     header. limit is clamped to [1, 200] with a default of 20.
//   - Every successful write calls notifyWrite so autosync wakes up.
//
// Two deliberate divergences from the RFC's route table, both because an HTTP
// server cannot see the caller's working directory the way an in-process MCP
// tool can:
//   - POST /projects/{slug}/graph/sync requires repo_dir. The tool defaults it
//     to the project path detected from the *client's* cwd; over HTTP that
//     path is unknowable, and defaulting to the server process's own cwd would
//     silently stamp a graph commit read from an unrelated checkout.
//   - The project is always the {slug} in the path. There is no cwd-based
//     resolution and no ENGRAM_PROJECT fallback.
package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	projectpkg "github.com/HoracioEspinosa/engram/internal/project"
	"github.com/HoracioEspinosa/engram/internal/runbooks"
	"github.com/HoracioEspinosa/engram/internal/store"
)

// ─── Limits ──────────────────────────────────────────────────────────────────

const (
	// projectsMaxBodyBytes caps ordinary JSON bodies (cards, tasks, links,
	// evidence). Every documented field is short; 64 KiB is generous.
	projectsMaxBodyBytes int64 = 64 << 10
	// runbookSyncMaxBodyBytes caps the runbook sync body. Its `entries` array
	// is separately capped at 1 MiB, so the envelope gets twice that.
	runbookSyncMaxBodyBytes int64 = 2 << 20
	// runbookSyncMaxEntries and runbookSyncMaxEntryBytes mirror the tool's
	// own caps (RFC §5.8) so both transports reject the same payloads.
	runbookSyncMaxEntries    = 500
	runbookSyncMaxEntryBytes = 1 << 20

	projectsDefaultLimit = 20
	projectsMaxLimit     = 200
)

// ─── Validation ──────────────────────────────────────────────────────────────
//
// These mirror the CHECK constraints in internal/store/projects_schema.go.
// Validating at the edge is what turns a bad enum into a 400 with a usable
// code instead of a 500 carrying a raw SQLite constraint message.

var (
	httpSlugPattern      = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)
	httpSHA256Pattern    = regexp.MustCompile(`^[0-9a-f]{64}$`)
	httpObsSyncIDPattern = regexp.MustCompile(`^obs-[0-9a-f]{16,32}$`)
	httpTaskSyncPattern  = regexp.MustCompile(`^task-[0-9a-f]{16}$`)
	httpJiraKeyPattern   = regexp.MustCompile(`^[A-Z][A-Z0-9]+-[0-9]+$`)
	httpSDDChangePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
)

// httpReservedSlugs are the path segments project_cards.slug forbids, so that
// PUT /projects/migrate can never shadow POST /projects/migrate's semantics.
var httpReservedSlugs = map[string]bool{"migrate": true, "current": true}

var (
	httpTaskKindEnum       = []string{"feature", "bugfix", "refactor", "incident", "migration", "spike"}
	httpTaskStateEnum      = []string{"open", "analysis", "in_progress", "review", "verified", "done", "blocked", "cancelled"}
	httpTaskListStateEnum  = append([]string{"active"}, httpTaskStateEnum...)
	httpJiraCategoryEnum   = []string{"new", "indeterminate", "done"}
	httpTaskLinkRoleEnum   = []string{"context", "decision", "root_cause", "evidence", "summary"}
	httpEvidenceKindEnum   = []string{"png", "gif", "mp4", "json", "log", "txt"}
	httpRunbookCategory    = []string{"auth", "database", "queue", "network", "performance", "data-integrity", "registration"}
	httpRunbookPattern     = []string{"missing-files", "auth-access", "file-save-failure", "sync-upload", "registration-subscription", "other"}
	httpRunbookStatusEnum  = []string{"draft", "verified", "outdated"}
	httpRunbookSourceEnum  = []string{"knowledge-mcp", "vault-fs"}
	httpMatchModeEnum      = []string{"all", "any"}
	httpContextFormatEnum  = []string{"markdown", "json"}
	httpContextSectionEnum = []string{"header", "card", "pointers", "pinned", "observations", "evidence", "runbooks", "refs", "footer"}
)

func httpEnumHas(values []string, v string) bool {
	for _, x := range values {
		if x == v {
			return true
		}
	}
	return false
}

// ─── Registration ────────────────────────────────────────────────────────────

// registerProjectRoutes wires the engram-projects routes onto the shared mux.
//
// Method-plus-path patterns keep these from colliding with the pre-existing
// POST /projects/migrate and GET /project/current: migrate is a POST literal
// and nothing here registers POST /projects/{slug}, so ServeMux never has to
// break a tie. PUT /projects/migrate does reach handleProjectUpsertHTTP, which
// rejects it as a reserved slug rather than creating a card named "migrate"
// that the table's own CHECK forbids.
func (s *Server) registerProjectRoutes() {
	s.mux.HandleFunc("GET /projects", s.handleProjectsListHTTP)
	s.mux.HandleFunc("GET /projects/{slug}", s.handleProjectCardHTTP)
	s.mux.HandleFunc("PUT /projects/{slug}", requireAuth(s.handleProjectUpsertHTTP))
	s.mux.HandleFunc("POST /projects/{slug}/graph/sync", requireAuth(s.handleProjectGraphSyncHTTP))

	s.mux.HandleFunc("GET /projects/{slug}/tasks", s.handleTaskListHTTP)
	s.mux.HandleFunc("POST /projects/{slug}/tasks", requireAuth(s.handleTaskUpsertHTTP))
	s.mux.HandleFunc("GET /projects/{slug}/tasks/{task}", s.handleTaskGetHTTP)
	s.mux.HandleFunc("GET /projects/{slug}/tasks/{task}/observations", s.handleTaskObservationsHTTP)
	s.mux.HandleFunc("POST /projects/{slug}/tasks/{task}/links", requireAuth(s.handleTaskLinkHTTP))
	s.mux.HandleFunc("GET /projects/{slug}/tasks/{task}/evidence", s.handleTaskEvidenceListHTTP)
	s.mux.HandleFunc("POST /projects/{slug}/tasks/{task}/evidence", requireAuth(s.handleEvidenceAddHTTP))
	s.mux.HandleFunc("GET /projects/{slug}/tasks/{task}/context", s.handleTaskContextHTTP)

	s.mux.HandleFunc("GET /projects/{slug}/evidence", s.handleProjectEvidenceListHTTP)

	s.mux.HandleFunc("GET /projects/{slug}/runbooks", s.handleRunbookListHTTP)
	s.mux.HandleFunc("POST /projects/{slug}/runbooks/sync", requireAuth(s.handleRunbookSyncHTTP))
	s.mux.HandleFunc("GET /projects/{slug}/runbooks/find", s.handleRunbookFindHTTP)
}

// ─── Shared helpers ──────────────────────────────────────────────────────────

// projectError writes the {"error","code",...} envelope every route in this
// file uses for failures.
func projectError(w http.ResponseWriter, status int, code, msg string, fields map[string]any) {
	payload := map[string]any{"error": msg, "code": code}
	for k, v := range fields {
		payload[k] = v
	}
	jsonResponse(w, status, payload)
}

// paginated writes a list response with the uniform envelope and the
// X-Total-Count header (RFC §6.1).
func paginated(w http.ResponseWriter, items any, total, limit, offset int, extra map[string]any) {
	w.Header().Set("X-Total-Count", strconv.Itoa(total))
	payload := map[string]any{"items": items, "total": total, "limit": limit, "offset": offset}
	for k, v := range extra {
		payload[k] = v
	}
	jsonResponse(w, http.StatusOK, payload)
}

// pageParams reads and clamps limit/offset.
func pageParams(r *http.Request) (limit, offset int) {
	limit = queryInt(r, "limit", projectsDefaultLimit)
	if limit < 1 {
		limit = 1
	}
	if limit > projectsMaxLimit {
		limit = projectsMaxLimit
	}
	offset = queryInt(r, "offset", 0)
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}

// slugParam validates the {slug} path segment. It writes a 400 and returns
// false when the segment could never name a card.
func slugParam(w http.ResponseWriter, r *http.Request) (string, bool) {
	slug := r.PathValue("slug")
	if !httpSlugPattern.MatchString(slug) || httpReservedSlugs[slug] {
		projectError(w, http.StatusBadRequest, "invalid_slug",
			fmt.Sprintf("invalid project slug %q", slug), nil)
		return "", false
	}
	return slug, true
}

// knownProject validates {slug} and asserts the project exists — either as a
// card or as a project that already owns observations. Sub-resources answer
// 404 for anything else instead of an empty list, so a typo in the slug is
// visible rather than silently returning nothing.
func (s *Server) knownProject(w http.ResponseWriter, r *http.Request) (string, bool) {
	slug, ok := slugParam(w, r)
	if !ok {
		return "", false
	}
	cardExists, err := s.store.ProjectCardExists(slug)
	if err != nil {
		projectError(w, http.StatusInternalServerError, "internal_error", err.Error(), nil)
		return "", false
	}
	if cardExists {
		return slug, true
	}
	obsExists, err := s.store.ProjectExists(slug)
	if err != nil {
		projectError(w, http.StatusInternalServerError, "internal_error", err.Error(), nil)
		return "", false
	}
	if obsExists {
		return slug, true
	}
	projectError(w, http.StatusNotFound, "unknown_project",
		fmt.Sprintf("project %q has no card and no observations", slug), nil)
	return "", false
}

// resolveTask resolves the {task} path segment inside project.
func (s *Server) resolveTask(w http.ResponseWriter, r *http.Request, project string) (store.Task, bool) {
	ref := r.PathValue("task")
	task, err := s.store.ResolveTaskRef(project, ref)
	if errors.Is(err, store.ErrUnknownTask) {
		projectError(w, http.StatusNotFound, "unknown_task",
			fmt.Sprintf("task %q not found in project %s", ref, project), nil)
		return store.Task{}, false
	}
	if err != nil {
		projectError(w, http.StatusInternalServerError, "internal_error", err.Error(), nil)
		return store.Task{}, false
	}
	return task, true
}

// decodeBody decodes a JSON request body under a size cap. An oversized body
// answers 413 payload_too_large; a malformed one answers 400 invalid_json;
// an absent one is treated as {} when optional is set.
func decodeBody(w http.ResponseWriter, r *http.Request, limit int64, optional bool, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	err := dec.Decode(dst)
	switch {
	case err == nil:
		return true
	case errors.Is(err, io.EOF) && optional:
		return true
	}

	var tooBig *http.MaxBytesError
	if errors.As(err, &tooBig) {
		projectError(w, http.StatusRequestEntityTooLarge, "payload_too_large",
			fmt.Sprintf("request body exceeds %d bytes", limit), map[string]any{"limit_bytes": limit})
		return false
	}
	if strings.Contains(err.Error(), "unknown field") {
		projectError(w, http.StatusBadRequest, "invalid_field", err.Error(), nil)
		return false
	}
	projectError(w, http.StatusBadRequest, "invalid_json", err.Error(), nil)
	return false
}

// trimPtr normalizes an optional string body field: an absent or blank value
// becomes nil, which every store upsert reads as "leave untouched".
func trimPtr(v *string) *string {
	if v == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*v)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

// storeFailure maps a store error that no route-specific branch claimed onto
// a 500. It exists so every handler's fallthrough looks the same.
func storeFailure(w http.ResponseWriter, err error) {
	projectError(w, http.StatusInternalServerError, "internal_error", err.Error(), nil)
}

// knowledgeRefFailure answers the knowledge_ref shape rule (RFC §9.1/§9.2)
// with the same codes the MCP tools return, and reports false for any other
// error so the caller falls through to its own mapping.
func knowledgeRefFailure(w http.ResponseWriter, err error) bool {
	switch {
	case errors.Is(err, store.ErrKnowledgeRefNotCurated):
		projectError(w, http.StatusUnprocessableEntity, "knowledge_ref_not_curated", err.Error(),
			map[string]any{"hint": "point knowledge_ref at a curated document; 90 - Engram/ holds engram's own export"})
	case errors.Is(err, store.ErrKnowledgeRefAbsolute):
		projectError(w, http.StatusUnprocessableEntity, "absolute_path_rejected", err.Error(),
			map[string]any{"hint": "use the vault-relative form the knowledge tools return, e.g. Services/Nextcloud/Architecture.md"})
	case errors.Is(err, store.ErrKnowledgeRefInvalid):
		projectError(w, http.StatusUnprocessableEntity, "invalid_knowledge_ref", err.Error(),
			map[string]any{"hint": "a knowledge_ref is a vault-relative .md path with an optional #Anchor"})
	default:
		return false
	}
	return true
}

// ─── Cards ───────────────────────────────────────────────────────────────────

// handleProjectsListHTTP answers GET /projects.
func (s *Server) handleProjectsListHTTP(w http.ResponseWriter, r *http.Request) {
	includeCounts := queryBool(r, "include_counts", false)
	includeGraphSummary := queryBool(r, "include_graph_summary", false)

	items, total, err := s.store.ListProjectCards(includeCounts)
	if err != nil {
		storeFailure(w, err)
		return
	}
	if !includeGraphSummary {
		// The summary is a multi-kilobyte JSON blob per card; the selector
		// that consumes this list never renders it.
		for i := range items {
			items[i].GraphSummary = nil
		}
	}

	w.Header().Set("X-Total-Count", strconv.Itoa(total))
	jsonResponse(w, http.StatusOK, map[string]any{"items": items, "total": total})
}

// handleProjectCardHTTP answers GET /projects/{slug} with the same payload
// mem_project_card returns: card, counts and cloud-sync status.
func (s *Server) handleProjectCardHTTP(w http.ResponseWriter, r *http.Request) {
	slug, ok := slugParam(w, r)
	if !ok {
		return
	}

	card, err := s.store.GetProjectCard(slug)
	if errors.Is(err, store.ErrNoProjectCard) {
		projectError(w, http.StatusNotFound, "no_card",
			fmt.Sprintf("no project card for %s", slug),
			map[string]any{"hint": "PUT /projects/" + slug})
		return
	}
	if err != nil {
		storeFailure(w, err)
		return
	}
	if !queryBool(r, "include_graph_summary", false) {
		card.GraphSummary = nil
	}

	payload := map[string]any{"card": card}
	if queryBool(r, "include_counts", true) {
		counts, err := s.store.ProjectCardCounts(slug)
		if err != nil {
			storeFailure(w, err)
			return
		}
		payload["counts"] = counts
	}
	sync, err := s.store.ProjectSyncSummary(slug)
	if err != nil {
		storeFailure(w, err)
		return
	}
	payload["sync"] = sync

	jsonResponse(w, http.StatusOK, payload)
}

// projectUpsertBody is PUT /projects/{slug}'s body: mem_project_upsert's
// fields minus `project`, which the path already carries.
type projectUpsertBody struct {
	DisplayName      *string `json:"display_name"`
	RepoURL          *string `json:"repo_url"`
	DefaultBranch    *string `json:"default_branch"`
	JiraProject      *string `json:"jira_project"`
	JiraComponent    *string `json:"jira_component"`
	KnowledgeHubPath *string `json:"knowledge_hub_path"`
	Owner            *string `json:"owner"`
	GraphPath        *string `json:"graph_path"`
}

// handleProjectUpsertHTTP answers PUT /projects/{slug}: 201 on create, 200 on
// update, idempotent either way.
func (s *Server) handleProjectUpsertHTTP(w http.ResponseWriter, r *http.Request) {
	slug, ok := slugParam(w, r)
	if !ok {
		return
	}
	var body projectUpsertBody
	if !decodeBody(w, r, projectsMaxBodyBytes, true, &body) {
		return
	}

	card, created, err := s.store.UpsertProjectCard(store.UpsertProjectCardParams{
		Slug:             slug,
		DisplayName:      trimPtr(body.DisplayName),
		RepoURL:          trimPtr(body.RepoURL),
		DefaultBranch:    trimPtr(body.DefaultBranch),
		JiraProject:      trimPtr(body.JiraProject),
		JiraComponent:    trimPtr(body.JiraComponent),
		KnowledgeHubPath: trimPtr(body.KnowledgeHubPath),
		Owner:            trimPtr(body.Owner),
		GraphPath:        trimPtr(body.GraphPath),
	})
	if err != nil {
		storeFailure(w, err)
		return
	}
	s.notifyWrite()

	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	jsonResponse(w, status, map[string]any{"card": card, "created": created})
}

// graphSyncBody is POST /projects/{slug}/graph/sync's body.
type graphSyncBody struct {
	RepoDir string `json:"repo_dir"`
}

// handleProjectGraphSyncHTTP answers POST /projects/{slug}/graph/sync: it
// reads graphify's output from repo_dir and stamps graph_commit,
// graph_built_at and graph_summary on the card.
func (s *Server) handleProjectGraphSyncHTTP(w http.ResponseWriter, r *http.Request) {
	slug, ok := slugParam(w, r)
	if !ok {
		return
	}
	var body graphSyncBody
	if !decodeBody(w, r, projectsMaxBodyBytes, true, &body) {
		return
	}
	repoDir := strings.TrimSpace(body.RepoDir)
	if repoDir == "" {
		projectError(w, http.StatusBadRequest, "missing_field",
			"repo_dir is required: the server cannot infer the caller's checkout", nil)
		return
	}
	if !strings.HasPrefix(repoDir, "/") {
		projectError(w, http.StatusBadRequest, "invalid_path",
			fmt.Sprintf("repo_dir %q must be an absolute path", repoDir), nil)
		return
	}

	// The card must already exist: StampProjectGraph is a plain UPDATE, so a
	// missing row would make the sync report success while persisting nothing.
	card, err := s.store.GetProjectCard(slug)
	if errors.Is(err, store.ErrNoProjectCard) {
		projectError(w, http.StatusNotFound, "no_card",
			fmt.Sprintf("no project card for %s", slug),
			map[string]any{"hint": "PUT /projects/" + slug + " first"})
		return
	}
	if err != nil {
		storeFailure(w, err)
		return
	}

	graph, err := projectpkg.SyncGraph(s.store, slug, repoDir, card.GraphPath)
	switch {
	case errors.Is(err, store.ErrGraphNotFound):
		projectError(w, http.StatusNotFound, "graph_not_found", "graph.json not found",
			map[string]any{"repo_dir": repoDir, "graph_path": card.GraphPath})
		return
	case errors.Is(err, store.ErrGraphMissingCommit):
		projectError(w, http.StatusUnprocessableEntity, "graph_missing_commit",
			"graph.json has no built_at_commit; no graph fact was persisted", nil)
		return
	case err != nil:
		storeFailure(w, err)
		return
	}
	s.notifyWrite()

	updated, err := s.store.GetProjectCard(slug)
	if err != nil {
		storeFailure(w, err)
		return
	}
	updated.GraphSummary = nil
	jsonResponse(w, http.StatusOK, map[string]any{"graph": graph, "card": updated})
}

// ─── Tasks ───────────────────────────────────────────────────────────────────

// handleTaskListHTTP answers GET /projects/{slug}/tasks. This is the route
// the acceptance criterion of the roadmap names.
func (s *Server) handleTaskListHTTP(w http.ResponseWriter, r *http.Request) {
	slug, ok := s.knownProject(w, r)
	if !ok {
		return
	}

	state := strings.TrimSpace(r.URL.Query().Get("state"))
	if state != "" && !httpEnumHas(httpTaskListStateEnum, state) {
		projectError(w, http.StatusBadRequest, "invalid_enum", fmt.Sprintf("state %q is invalid", state),
			map[string]any{"allowed": httpTaskListStateEnum})
		return
	}
	kind := strings.TrimSpace(r.URL.Query().Get("kind"))
	if kind != "" && !httpEnumHas(httpTaskKindEnum, kind) {
		projectError(w, http.StatusBadRequest, "invalid_enum", fmt.Sprintf("kind %q is invalid", kind),
			map[string]any{"allowed": httpTaskKindEnum})
		return
	}

	limit, offset := pageParams(r)
	staleAfter := queryInt(r, "stale_after_hours", 24)
	if staleAfter < 1 {
		staleAfter = 24
	}

	// The route table spells the full-text parameter `q`; the MCP tool calls
	// it `query`. Both are accepted so a client can use either name.
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if query == "" {
		query = strings.TrimSpace(r.URL.Query().Get("query"))
	}

	items, total, err := s.store.ListTasks(slug, store.TaskListFilter{
		State:           state,
		Kind:            kind,
		JiraKey:         strings.TrimSpace(r.URL.Query().Get("jira_key")),
		Query:           query,
		Limit:           limit,
		Offset:          offset,
		StaleAfterHours: staleAfter,
	})
	if err != nil {
		storeFailure(w, err)
		return
	}
	if items == nil {
		items = []store.TaskListItem{}
	}
	paginated(w, items, total, limit, offset, nil)
}

// taskUpsertBody is POST /projects/{slug}/tasks's body: mem_task_upsert's
// fields minus `project`.
type taskUpsertBody struct {
	SyncID             *string `json:"sync_id"`
	JiraKey            *string `json:"jira_key"`
	SDDChange          *string `json:"sdd_change"`
	Title              *string `json:"title"`
	Kind               *string `json:"kind"`
	State              *string `json:"state"`
	JiraStatus         *string `json:"jira_status"`
	JiraStatusCategory *string `json:"jira_status_category"`
	Branch             *string `json:"branch"`
	PRUrl              *string `json:"pr_url"`
	KnowledgeRef       *string `json:"knowledge_ref"`
	Assignee           *string `json:"assignee"`
}

// handleTaskUpsertHTTP answers POST /projects/{slug}/tasks.
func (s *Server) handleTaskUpsertHTTP(w http.ResponseWriter, r *http.Request) {
	slug, ok := slugParam(w, r)
	if !ok {
		return
	}
	var body taskUpsertBody
	if !decodeBody(w, r, projectsMaxBodyBytes, false, &body) {
		return
	}

	syncID, jiraKey, sddChange := trimPtr(body.SyncID), trimPtr(body.JiraKey), trimPtr(body.SDDChange)
	if syncID == nil && jiraKey == nil && sddChange == nil {
		projectError(w, http.StatusBadRequest, "missing_field",
			"one of sync_id, jira_key, or sdd_change is required", nil)
		return
	}
	for _, check := range []struct {
		value   *string
		pattern *regexp.Regexp
		field   string
		shape   string
	}{
		{syncID, httpTaskSyncPattern, "sync_id", "task-<16 hex>"},
		{jiraKey, httpJiraKeyPattern, "jira_key", "PROJ-123"},
		{sddChange, httpSDDChangePattern, "sdd_change", "lowercase-slug"},
	} {
		if check.value != nil && !check.pattern.MatchString(*check.value) {
			projectError(w, http.StatusBadRequest, "invalid_enum",
				fmt.Sprintf("%s %q does not match %s", check.field, *check.value, check.shape), nil)
			return
		}
	}
	for _, check := range []struct {
		value *string
		enum  []string
		field string
	}{
		{trimPtr(body.Kind), httpTaskKindEnum, "kind"},
		{trimPtr(body.State), httpTaskStateEnum, "state"},
		{trimPtr(body.JiraStatusCategory), httpJiraCategoryEnum, "jira_status_category"},
	} {
		if check.value != nil && !httpEnumHas(check.enum, *check.value) {
			projectError(w, http.StatusBadRequest, "invalid_enum",
				fmt.Sprintf("%s %q is invalid", check.field, *check.value),
				map[string]any{"allowed": check.enum})
			return
		}
	}

	result, err := s.store.UpsertTask(store.UpsertTaskParams{
		Project:            slug,
		SyncID:             syncID,
		JiraKey:            jiraKey,
		SDDChange:          sddChange,
		Title:              trimPtr(body.Title),
		Kind:               trimPtr(body.Kind),
		State:              trimPtr(body.State),
		JiraStatus:         trimPtr(body.JiraStatus),
		JiraStatusCategory: trimPtr(body.JiraStatusCategory),
		Branch:             trimPtr(body.Branch),
		PRUrl:              trimPtr(body.PRUrl),
		KnowledgeRef:       trimPtr(body.KnowledgeRef),
		Assignee:           trimPtr(body.Assignee),
	})
	if err != nil {
		if knowledgeRefFailure(w, err) {
			return
		}
		var missing *store.MissingFieldError
		if errors.As(err, &missing) {
			projectError(w, http.StatusBadRequest, "missing_field", missing.Error(),
				map[string]any{"field": missing.Field})
			return
		}
		var conflict *store.TaskKeyConflictError
		if errors.As(err, &conflict) {
			projectError(w, http.StatusConflict, "task_key_conflict", conflict.Error(),
				map[string]any{"existing_project": conflict.ExistingProject})
			return
		}
		storeFailure(w, err)
		return
	}
	s.notifyWrite()

	status := http.StatusOK
	if result.Created {
		status = http.StatusCreated
	}
	jsonResponse(w, status, map[string]any{
		"task": result.Task, "created": result.Created, "card_created": result.CardCreated,
	})
}

// handleTaskGetHTTP answers GET /projects/{slug}/tasks/{task}.
func (s *Server) handleTaskGetHTTP(w http.ResponseWriter, r *http.Request) {
	slug, ok := s.knownProject(w, r)
	if !ok {
		return
	}
	task, ok := s.resolveTask(w, r, slug)
	if !ok {
		return
	}
	counts, err := s.store.TaskCounts(task.ID)
	if err != nil {
		storeFailure(w, err)
		return
	}
	staleAfter := queryInt(r, "stale_after_hours", 24)
	jsonResponse(w, http.StatusOK, map[string]any{
		"task":        task,
		"counts":      counts,
		"state_stale": store.TaskStateStale(task.StateSyncedAt, staleAfter),
	})
}

// handleTaskObservationsHTTP answers GET
// /projects/{slug}/tasks/{task}/observations. The link table is small per
// task, so the role filter and the page window are applied in memory over the
// store's role-ordered result rather than pushed into SQL.
func (s *Server) handleTaskObservationsHTTP(w http.ResponseWriter, r *http.Request) {
	slug, ok := s.knownProject(w, r)
	if !ok {
		return
	}
	task, ok := s.resolveTask(w, r, slug)
	if !ok {
		return
	}
	role := strings.TrimSpace(r.URL.Query().Get("role"))
	if role != "" && !httpEnumHas(httpTaskLinkRoleEnum, role) {
		projectError(w, http.StatusBadRequest, "invalid_enum", fmt.Sprintf("role %q is invalid", role),
			map[string]any{"allowed": httpTaskLinkRoleEnum})
		return
	}

	details, err := s.store.TaskObservationsForTask(task.ID)
	if err != nil {
		storeFailure(w, err)
		return
	}
	filtered := make([]store.TaskObservationDetail, 0, len(details))
	for _, d := range details {
		if role == "" || d.Role == role {
			filtered = append(filtered, d)
		}
	}

	limit, offset := pageParams(r)
	total := len(filtered)
	start := offset
	if start > total {
		start = total
	}
	end := start + limit
	if end > total {
		end = total
	}
	paginated(w, filtered[start:end], total, limit, offset, nil)
}

// taskLinkBody is POST /projects/{slug}/tasks/{task}/links's body:
// mem_task_link's fields minus `task`.
type taskLinkBody struct {
	ObservationID     *int64  `json:"observation_id"`
	ObservationSyncID *string `json:"observation_sync_id"`
	Role              *string `json:"role"`
	KnowledgeRef      *string `json:"knowledge_ref"`
	GraphRef          *string `json:"graph_ref"`
	GraphCommit       *string `json:"graph_commit"`
	RunbookID         *string `json:"runbook_id"`
	JiraRef           *string `json:"jira_ref"`
}

// handleTaskLinkHTTP answers POST /projects/{slug}/tasks/{task}/links.
func (s *Server) handleTaskLinkHTTP(w http.ResponseWriter, r *http.Request) {
	slug, ok := s.knownProject(w, r)
	if !ok {
		return
	}
	task, ok := s.resolveTask(w, r, slug)
	if !ok {
		return
	}
	var body taskLinkBody
	if !decodeBody(w, r, projectsMaxBodyBytes, false, &body) {
		return
	}

	obsSyncID := trimPtr(body.ObservationSyncID)
	if body.ObservationID == nil && obsSyncID == nil {
		projectError(w, http.StatusBadRequest, "missing_field",
			"one of observation_id or observation_sync_id is required", nil)
		return
	}
	if obsSyncID != nil && !httpObsSyncIDPattern.MatchString(*obsSyncID) {
		projectError(w, http.StatusBadRequest, "invalid_enum",
			fmt.Sprintf("observation_sync_id %q is invalid", *obsSyncID), nil)
		return
	}
	role := trimPtr(body.Role)
	if role != nil && !httpEnumHas(httpTaskLinkRoleEnum, *role) {
		projectError(w, http.StatusBadRequest, "invalid_enum", fmt.Sprintf("role %q is invalid", *role),
			map[string]any{"allowed": httpTaskLinkRoleEnum})
		return
	}

	obsID := int64(0)
	if body.ObservationID != nil {
		obsID = *body.ObservationID
	} else {
		obs, err := s.store.GetObservationBySyncID(*obsSyncID)
		if err != nil || obs == nil {
			projectError(w, http.StatusNotFound, "unknown_observation",
				fmt.Sprintf("observation %q not found", *obsSyncID), nil)
			return
		}
		obsID = obs.ID
	}

	roleValue := ""
	if role != nil {
		roleValue = *role
	}
	result, err := s.store.LinkTaskObservation(store.LinkTaskObservationParams{
		Task:          task,
		ObservationID: obsID,
		Role:          roleValue,
		KnowledgeRef:  trimPtr(body.KnowledgeRef),
		GraphRef:      trimPtr(body.GraphRef),
		GraphCommit:   trimPtr(body.GraphCommit),
		RunbookID:     trimPtr(body.RunbookID),
		JiraRef:       trimPtr(body.JiraRef),
	})
	switch {
	case errors.Is(err, store.ErrUnknownObservation):
		projectError(w, http.StatusNotFound, "unknown_observation", "observation not found", nil)
		return
	case errors.Is(err, store.ErrCrossProjectLink):
		projectError(w, http.StatusConflict, "cross_project_link",
			"observation and task belong to different projects", nil)
		return
	case errors.Is(err, store.ErrGraphCommitRequired):
		projectError(w, http.StatusUnprocessableEntity, "graph_commit_required",
			"graph_ref requires graph_commit", nil)
		return
	case err != nil:
		if knowledgeRefFailure(w, err) {
			return
		}
		storeFailure(w, err)
		return
	}
	s.notifyWrite()

	jsonResponse(w, http.StatusCreated, map[string]any{
		"linked":              result.Linked,
		"task_sync_id":        result.TaskSyncID,
		"observation_sync_id": result.ObservationSyncID,
		"role":                result.Role,
		"refs_added":          result.RefsAdded,
		"refs":                result.Refs,
	})
}

// ─── Evidence ────────────────────────────────────────────────────────────────

// evidenceAddBody is POST /projects/{slug}/tasks/{task}/evidence's body:
// mem_evidence_add's fields minus `task`.
type evidenceAddBody struct {
	Path                  string  `json:"path"`
	SHA256                string  `json:"sha256"`
	Kind                  string  `json:"kind"`
	Proves                string  `json:"proves"`
	ConfigStamp           *string `json:"config_stamp"`
	CapturedAt            *string `json:"captured_at"`
	SizeBytes             *int64  `json:"size_bytes"`
	ManifestPath          *string `json:"manifest_path"`
	AttachedJira          *bool   `json:"attached_jira"`
	AttachedConfluenceURL *string `json:"attached_confluence_url"`
}

// handleEvidenceAddHTTP answers POST /projects/{slug}/tasks/{task}/evidence.
// It registers an already-captured file; engram never reads the file itself.
func (s *Server) handleEvidenceAddHTTP(w http.ResponseWriter, r *http.Request) {
	slug, ok := s.knownProject(w, r)
	if !ok {
		return
	}
	task, ok := s.resolveTask(w, r, slug)
	if !ok {
		return
	}
	var body evidenceAddBody
	if !decodeBody(w, r, projectsMaxBodyBytes, false, &body) {
		return
	}

	path := strings.TrimSpace(body.Path)
	sha := strings.TrimSpace(body.SHA256)
	kind := strings.TrimSpace(body.Kind)
	proves := strings.TrimSpace(body.Proves)
	if path == "" || sha == "" || kind == "" || proves == "" {
		projectError(w, http.StatusBadRequest, "missing_field",
			"path, sha256, kind, and proves are required", nil)
		return
	}
	if !httpSHA256Pattern.MatchString(sha) {
		projectError(w, http.StatusBadRequest, "invalid_sha256",
			"sha256 must be 64 lowercase hex characters", nil)
		return
	}
	if !httpEnumHas(httpEvidenceKindEnum, kind) {
		projectError(w, http.StatusBadRequest, "invalid_enum", fmt.Sprintf("kind %q is invalid", kind),
			map[string]any{"allowed": httpEvidenceKindEnum})
		return
	}
	// An evidence path is a name inside the evidence directory, so it is checked
	// for what it RESOLVES to, not for how it starts. Rejecting a leading "/" or
	// "~" stops the obvious absolute path and lets "../../etc/passwd" straight
	// through: filepath.Clean is what collapses the traversal so it can be seen.
	// The same reasoning as the staging guard, which resolves symlinks before
	// deciding whether a directory sits inside the work tree.
	if strings.HasPrefix(path, "/") || strings.HasPrefix(path, "~") {
		projectError(w, http.StatusBadRequest, "absolute_path_rejected",
			fmt.Sprintf("path %q must be relative to the evidence directory", path), nil)
		return
	}
	if cleaned := filepath.Clean(path); cleaned == ".." ||
		strings.HasPrefix(cleaned, "../") || filepath.IsAbs(cleaned) {
		projectError(w, http.StatusBadRequest, "path_escapes_evidence_dir",
			fmt.Sprintf("path %q escapes the evidence directory", path), nil)
		return
	}

	attachedJira := false
	if body.AttachedJira != nil {
		attachedJira = *body.AttachedJira
	}
	evidence, duplicate, limits, err := s.store.AddEvidence(store.AddEvidenceParams{
		Task:                  task,
		Path:                  path,
		SHA256:                sha,
		Kind:                  kind,
		Proves:                proves,
		ConfigStamp:           trimPtr(body.ConfigStamp),
		CapturedAt:            trimPtr(body.CapturedAt),
		SizeBytes:             body.SizeBytes,
		ManifestPath:          trimPtr(body.ManifestPath),
		AttachedJira:          attachedJira,
		AttachedConfluenceURL: trimPtr(body.AttachedConfluenceURL),
	})
	if err != nil {
		storeFailure(w, err)
		return
	}
	s.notifyWrite()

	jsonResponse(w, http.StatusCreated, map[string]any{
		"evidence": evidence, "duplicate": duplicate, "limits": limits,
	})
}

// handleTaskEvidenceListHTTP answers GET
// /projects/{slug}/tasks/{task}/evidence.
func (s *Server) handleTaskEvidenceListHTTP(w http.ResponseWriter, r *http.Request) {
	slug, ok := s.knownProject(w, r)
	if !ok {
		return
	}
	task, ok := s.resolveTask(w, r, slug)
	if !ok {
		return
	}
	s.writeEvidencePage(w, r, slug, task.SyncID)
}

// handleProjectEvidenceListHTTP answers GET /projects/{slug}/evidence.
func (s *Server) handleProjectEvidenceListHTTP(w http.ResponseWriter, r *http.Request) {
	slug, ok := s.knownProject(w, r)
	if !ok {
		return
	}
	s.writeEvidencePage(w, r, slug, "")
}

// writeEvidencePage is the shared body of both evidence listings; taskSyncID
// scopes it to one task when non-empty.
func (s *Server) writeEvidencePage(w http.ResponseWriter, r *http.Request, slug, taskSyncID string) {
	kind := strings.TrimSpace(r.URL.Query().Get("kind"))
	if kind != "" && !httpEnumHas(httpEvidenceKindEnum, kind) {
		projectError(w, http.StatusBadRequest, "invalid_enum", fmt.Sprintf("kind %q is invalid", kind),
			map[string]any{"allowed": httpEvidenceKindEnum})
		return
	}

	filter := store.EvidenceListFilter{TaskSyncID: taskSyncID, Kind: kind}
	if raw := r.URL.Query().Get("attached_jira"); raw != "" {
		parsed, err := strconv.ParseBool(raw)
		if err != nil {
			projectError(w, http.StatusBadRequest, "invalid_enum",
				fmt.Sprintf("attached_jira %q is not a boolean", raw), nil)
			return
		}
		filter.AttachedJira = &parsed
	}
	filter.Limit, filter.Offset = pageParams(r)

	items, total, totalBytes, err := s.store.ListEvidence(slug, filter)
	if err != nil {
		storeFailure(w, err)
		return
	}
	if items == nil {
		items = []store.EvidenceListItem{}
	}
	paginated(w, items, total, filter.Limit, filter.Offset, map[string]any{"total_bytes": totalBytes})
}

// ─── Runbooks ────────────────────────────────────────────────────────────────

// handleRunbookListHTTP answers GET /projects/{slug}/runbooks.
func (s *Server) handleRunbookListHTTP(w http.ResponseWriter, r *http.Request) {
	slug, ok := s.knownProject(w, r)
	if !ok {
		return
	}

	filter := store.RunbookListFilter{
		Category: strings.TrimSpace(r.URL.Query().Get("category")),
		Pattern:  strings.TrimSpace(r.URL.Query().Get("pattern")),
		Status:   strings.TrimSpace(r.URL.Query().Get("status")),
	}
	for _, check := range []struct {
		value string
		enum  []string
		field string
	}{
		{filter.Category, httpRunbookCategory, "category"},
		{filter.Pattern, httpRunbookPattern, "pattern"},
		{filter.Status, httpRunbookStatusEnum, "status"},
	} {
		if check.value != "" && !httpEnumHas(check.enum, check.value) {
			projectError(w, http.StatusBadRequest, "invalid_enum",
				fmt.Sprintf("%s %q is invalid", check.field, check.value),
				map[string]any{"allowed": check.enum})
			return
		}
	}
	if raw := r.URL.Query().Get("stale"); raw != "" {
		parsed, err := strconv.ParseBool(raw)
		if err != nil {
			projectError(w, http.StatusBadRequest, "invalid_enum",
				fmt.Sprintf("stale %q is not a boolean", raw), nil)
			return
		}
		filter.Stale = &parsed
	}
	filter.Limit, filter.Offset = pageParams(r)

	items, total, err := s.store.ListRunbookIndex(slug, filter)
	if err != nil {
		storeFailure(w, err)
		return
	}
	paginated(w, items, total, filter.Limit, filter.Offset, nil)
}

// runbookSyncBody is POST /projects/{slug}/runbooks/sync's body. Exactly one
// source is used: `entries` when the caller already read the vault through
// cd-knowledge-mcp, `vault_dir` when it wants engram to read a local
// checkout itself.
type runbookSyncBody struct {
	Source       string                 `json:"source"`
	VaultDir     string                 `json:"vault_dir"`
	PruneMissing bool                   `json:"prune_missing"`
	Entries      []runbookSyncEntryBody `json:"entries"`
}

type runbookSyncEntryBody struct {
	ID              string   `json:"id"`
	VaultPath       string   `json:"vault_path"`
	Title           string   `json:"title"`
	Service         string   `json:"service"`
	Category        string   `json:"category"`
	Pattern         string   `json:"pattern"`
	Severity        string   `json:"severity"`
	Status          string   `json:"status"`
	Symptoms        []string `json:"symptoms"`
	Tags            []string `json:"tags"`
	Owner           string   `json:"owner"`
	AutomationLevel string   `json:"automation_level"`
	LastUpdated     string   `json:"last_updated"`
	LastVerified    string   `json:"last_verified"`
	NeedsReview     *bool    `json:"needs_review"`
	AgeDays         *int     `json:"age_days"`
}

// handleRunbookSyncHTTP answers POST /projects/{slug}/runbooks/sync.
//
// {slug} scopes prune_missing, which is what store.SyncRunbookIndex's
// `Project` field means. It does not filter the entries: every entry carries
// its own `service` and is indexed under it, so one pass over a vault
// checkout indexes every service while only the addressed project can lose
// rows to a prune.
func (s *Server) handleRunbookSyncHTTP(w http.ResponseWriter, r *http.Request) {
	slug, ok := s.knownProject(w, r)
	if !ok {
		return
	}
	var body runbookSyncBody
	if !decodeBody(w, r, runbookSyncMaxBodyBytes, false, &body) {
		return
	}

	source := strings.TrimSpace(body.Source)
	if source == "" {
		source = "knowledge-mcp"
	}
	if !httpEnumHas(httpRunbookSourceEnum, source) {
		projectError(w, http.StatusBadRequest, "invalid_enum", fmt.Sprintf("source %q is invalid", source),
			map[string]any{"allowed": httpRunbookSourceEnum})
		return
	}

	var entries []store.RunbookIndexEntryInput
	var preSkipped []store.RunbookSkipped
	scanned := 0
	// structuralOnly marks the knowledge-mcp path, where a body whose every
	// entry is malformed is a client error. A vault scan that happens to
	// contain only templates is not: it is a correct, empty result, and the
	// weekly LaunchAgent must not read it as a failure.
	structuralOnly := false

	if source == "vault-fs" {
		vaultDir := strings.TrimSpace(body.VaultDir)
		if vaultDir == "" {
			projectError(w, http.StatusBadRequest, "missing_field",
				`vault_dir is required when source is "vault-fs"`, nil)
			return
		}
		if len(body.Entries) > 0 {
			projectError(w, http.StatusBadRequest, "invalid_field",
				`entries must be omitted when source is "vault-fs"`, nil)
			return
		}
		result, err := runbooks.ScanVault(vaultDir, time.Now())
		if errors.Is(err, runbooks.ErrVaultDirNotFound) {
			projectError(w, http.StatusNotFound, "vault_not_found", err.Error(),
				map[string]any{"vault_dir": vaultDir})
			return
		}
		if err != nil {
			projectError(w, http.StatusBadRequest, "vault_scan_failed", err.Error(), nil)
			return
		}
		entries, preSkipped, scanned = result.Entries, result.Skipped, result.Scanned
	} else {
		if len(body.Entries) == 0 {
			projectError(w, http.StatusBadRequest, "entries_rejected",
				"entries must be a non-empty array", nil)
			return
		}
		entries, preSkipped = convertRunbookEntries(body.Entries)
		scanned = len(body.Entries)
		structuralOnly = true
	}

	if len(entries) > runbookSyncMaxEntries {
		projectError(w, http.StatusRequestEntityTooLarge, "payload_too_large",
			fmt.Sprintf("entries exceeds %d items", runbookSyncMaxEntries),
			map[string]any{"entries": len(entries)})
		return
	}
	if raw, err := json.Marshal(entries); err == nil && len(raw) > runbookSyncMaxEntryBytes {
		projectError(w, http.StatusRequestEntityTooLarge, "payload_too_large",
			fmt.Sprintf("entries payload exceeds %d bytes", runbookSyncMaxEntryBytes),
			map[string]any{"entries_bytes": len(raw)})
		return
	}

	result, err := runbooks.SyncIndex(s.store, store.RunbookIndexSyncParams{
		Project:      slug,
		Source:       source,
		PruneMissing: body.PruneMissing,
		Entries:      entries,
	})
	if err != nil {
		storeFailure(w, err)
		return
	}
	result.Skipped = append(preSkipped, result.Skipped...)
	if result.Skipped == nil {
		result.Skipped = []store.RunbookSkipped{}
	}

	// A body whose every entry was malformed is a client error. Entries the
	// store filtered on a business rule — template, invalid_status,
	// missing_service — are an expected outcome and still answer 200 with
	// the filtered rows reported in `skipped`, the same way the MCP tool
	// behaves for the same payload.
	if structuralOnly && scanned > 0 && len(preSkipped) == scanned {
		projectError(w, http.StatusBadRequest, "entries_rejected", "all entries were rejected",
			map[string]any{"skipped": result.Skipped, "scanned": scanned})
		return
	}
	s.notifyWrite()

	jsonResponse(w, http.StatusOK, map[string]any{
		"scanned":         scanned,
		"upserted":        result.Upserted,
		"unchanged":       result.Unchanged,
		"pruned":          result.Pruned,
		"skipped":         result.Skipped,
		"stale_count":     result.StaleCount,
		"exec_recomputed": result.ExecRecomputed,
	})
}

// convertRunbookEntries maps the JSON body's entries onto the store input,
// rejecting the ones missing a required field before they reach the store.
func convertRunbookEntries(in []runbookSyncEntryBody) ([]store.RunbookIndexEntryInput, []store.RunbookSkipped) {
	var entries []store.RunbookIndexEntryInput
	var skipped []store.RunbookSkipped
	for _, e := range in {
		if e.ID == "" || e.VaultPath == "" || e.Title == "" || e.Service == "" ||
			e.Category == "" || e.Status == "" {
			skipped = append(skipped, store.RunbookSkipped{
				ID: e.ID, VaultPath: e.VaultPath, Reason: "missing_field",
			})
			continue
		}
		entries = append(entries, store.RunbookIndexEntryInput{
			ID:              e.ID,
			VaultPath:       e.VaultPath,
			Title:           e.Title,
			Service:         e.Service,
			Category:        e.Category,
			Pattern:         e.Pattern,
			Severity:        e.Severity,
			Status:          e.Status,
			Symptoms:        e.Symptoms,
			Tags:            e.Tags,
			Owner:           e.Owner,
			AutomationLevel: e.AutomationLevel,
			LastUpdated:     e.LastUpdated,
			LastVerified:    e.LastVerified,
			NeedsReview:     e.NeedsReview,
			AgeDays:         e.AgeDays,
		})
	}
	return entries, skipped
}

// handleRunbookFindHTTP answers GET /projects/{slug}/runbooks/find.
func (s *Server) handleRunbookFindHTTP(w http.ResponseWriter, r *http.Request) {
	slug, ok := s.knownProject(w, r)
	if !ok {
		return
	}

	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if query == "" {
		query = strings.TrimSpace(r.URL.Query().Get("query"))
	}
	if len(query) < 3 {
		projectError(w, http.StatusBadRequest, "missing_field",
			"q is required and must be at least 3 characters", nil)
		return
	}

	category := strings.TrimSpace(r.URL.Query().Get("category"))
	pattern := strings.TrimSpace(r.URL.Query().Get("pattern"))
	matchMode := strings.TrimSpace(r.URL.Query().Get("match_mode"))
	if matchMode == "" {
		matchMode = "any"
	}
	for _, check := range []struct {
		value string
		enum  []string
		field string
	}{
		{category, httpRunbookCategory, "category"},
		{pattern, httpRunbookPattern, "pattern"},
		{matchMode, httpMatchModeEnum, "match_mode"},
	} {
		if check.value != "" && !httpEnumHas(check.enum, check.value) {
			projectError(w, http.StatusBadRequest, "invalid_enum",
				fmt.Sprintf("%s %q is invalid", check.field, check.value),
				map[string]any{"allowed": check.enum})
			return
		}
	}

	limit := queryInt(r, "limit", 5)
	if limit < 1 {
		limit = 1
	}
	if limit > 20 {
		limit = 20
	}

	items, total, err := s.store.FindRunbooks(store.RunbookFindParams{
		Query:        query,
		Project:      slug,
		Category:     category,
		Pattern:      pattern,
		IncludeStale: queryBool(r, "include_stale", true),
		MatchMode:    matchMode,
		Limit:        limit,
	})
	if err != nil {
		storeFailure(w, err)
		return
	}
	if items == nil {
		items = []store.RunbookFindItem{}
	}
	w.Header().Set("X-Total-Count", strconv.Itoa(total))
	jsonResponse(w, http.StatusOK, map[string]any{"items": items, "total": total, "limit": limit})
}

// ─── Context pack ────────────────────────────────────────────────────────────

// handleTaskContextHTTP answers GET /projects/{slug}/tasks/{task}/context.
// format=markdown (the default) returns text/markdown so a client can pipe
// the pack straight into a prompt; format=json returns the structured pack.
func (s *Server) handleTaskContextHTTP(w http.ResponseWriter, r *http.Request) {
	slug, ok := s.knownProject(w, r)
	if !ok {
		return
	}

	format := strings.TrimSpace(r.URL.Query().Get("format"))
	if format == "" {
		format = "markdown"
	}
	if !httpEnumHas(httpContextFormatEnum, format) {
		projectError(w, http.StatusBadRequest, "invalid_enum", fmt.Sprintf("format %q is invalid", format),
			map[string]any{"allowed": httpContextFormatEnum})
		return
	}

	var sections []string
	if raw := strings.TrimSpace(r.URL.Query().Get("sections")); raw != "" {
		for _, sec := range strings.Split(raw, ",") {
			sec = strings.TrimSpace(sec)
			if sec == "" {
				continue
			}
			if !httpEnumHas(httpContextSectionEnum, sec) {
				projectError(w, http.StatusBadRequest, "invalid_enum",
					fmt.Sprintf("section %q is invalid", sec),
					map[string]any{"allowed": httpContextSectionEnum})
				return
			}
			sections = append(sections, sec)
		}
	}

	opts := projectpkg.DefaultContextPackOptions()
	opts.MaxChars = clampRange(queryInt(r, "max_chars", opts.MaxChars), 2000, 40000)
	opts.ObservationsLimit = clampRange(queryInt(r, "observations_limit", opts.ObservationsLimit), 1, 30)
	opts.ObservationChars = clampRange(queryInt(r, "observation_chars", opts.ObservationChars), 200, 4000)
	opts.IncludeRunbooks = queryBool(r, "include_runbooks", true)
	opts.Sections = sections
	opts.Format = format

	pack, rendered, err := projectpkg.BuildContextPack(s.store, slug, r.PathValue("task"), opts)
	if errors.Is(err, store.ErrUnknownTask) {
		projectError(w, http.StatusNotFound, "unknown_task",
			fmt.Sprintf("task %q not found in project %s", r.PathValue("task"), slug), nil)
		return
	}
	if err != nil {
		storeFailure(w, err)
		return
	}

	if format == "json" {
		jsonResponse(w, http.StatusOK, pack)
		return
	}
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, rendered)
}

// clampRange bounds v to [minVal, maxVal].
func clampRange(v, minVal, maxVal int) int {
	if v < minVal {
		return minVal
	}
	if v > maxVal {
		return maxVal
	}
	return v
}
