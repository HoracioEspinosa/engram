package data

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/HoracioEspinosa/engram/internal/project"
	"github.com/HoracioEspinosa/engram/internal/store"
)

// sqliteMemory adapts the engram SQLite store to MemoryReader.
//
// The TUI shares the single *store.Store that cmd/engram already opened; it
// does not open a connection of its own.
type sqliteMemory struct {
	store *store.Store
}

// NewMemoryReader wraps an engram store as the Memory tab's reader.
//
// A nil store yields a reader whose destructive call reports
// ErrStoreUnavailable; the queries assume a real store, exactly as the screens
// did before they were extracted.
func NewMemoryReader(s *store.Store) MemoryReader {
	return sqliteMemory{store: s}
}

// NewScopedMemoryReader wraps an engram store as the Memory tab's
// project-scoped, paged reader (ScopedMemoryReader) — the same sqliteMemory
// NewMemoryReader returns, under the interface that carries its newer
// methods (see ScopedMemoryReader's doc comment for why it is a separate
// interface rather than three more methods on MemoryReader).
//
// A nil store yields a reader whose queries report ErrStoreUnavailable.
func NewScopedMemoryReader(s *store.Store) ScopedMemoryReader {
	return sqliteMemory{store: s}
}

func (r sqliteMemory) Stats() (*store.Stats, error) {
	return r.store.Stats()
}

func (r sqliteMemory) Search(query string, opts store.SearchOptions) ([]store.SearchResult, error) {
	return r.store.Search(query, opts)
}

func (r sqliteMemory) RecentObservations(limit int) ([]store.Observation, error) {
	return r.store.AllObservations("", "", limit)
}

func (r sqliteMemory) Observation(id int64) (*store.Observation, error) {
	return r.store.GetObservation(id)
}

func (r sqliteMemory) Timeline(id int64, before, after int) (*store.TimelineResult, error) {
	return r.store.Timeline(id, before, after)
}

func (r sqliteMemory) RecentSessions(limit int) ([]store.SessionSummary, error) {
	return r.store.AllSessions("", limit)
}

func (r sqliteMemory) SessionObservations(sessionID string, limit int) ([]store.Observation, error) {
	return r.store.SessionObservations(sessionID, limit)
}

func (r sqliteMemory) DeleteSession(sessionID string) error {
	if r.store == nil {
		return ErrStoreUnavailable
	}
	return r.store.DeleteSession(sessionID)
}

// maxScopedFetch bounds every *Scoped method's fetch window (offset+limit,
// and per-slug when Subtree widens the scope to several projects). store.
// Search/AllObservations/AllSessions take no Offset and report no total of
// their own, so pagination and Total here are computed client-side over a
// capped fetch rather than an exact SQL COUNT(*) OVER() — see SearchScoped's
// doc comment on MemoryReader.
const maxScopedFetch = 200

// capScopedFetch turns a raw offset+limit into the bounded fetch size a
// *Scoped method asks the store for.
func capScopedFetch(n int) int {
	if n <= 0 {
		return maxScopedFetch
	}
	if n > maxScopedFetch {
		return maxScopedFetch
	}
	return n
}

// sliceWindow returns items[offset:offset+limit], clamped to items' bounds.
func sliceWindow[T any](items []T, offset, limit int) []T {
	if offset >= len(items) {
		return nil
	}
	end := offset + limit
	if end > len(items) {
		end = len(items)
	}
	return items[offset:end]
}

// scopeSlugs turns a ProjectScope into the slugs a *Scoped method fetches
// from: nil for "everything" (Project empty), the one project, or the
// project and its subtree.
func (r sqliteMemory) scopeSlugs(scope ProjectScope) ([]string, error) {
	proj := strings.TrimSpace(scope.Project)
	if proj == "" {
		return nil, nil
	}
	if !scope.Subtree {
		return []string{proj}, nil
	}
	return r.store.SubtreeSlugs(proj)
}

func (r sqliteMemory) SearchScoped(query string, scope ProjectScope, limit, offset int) (Page[store.SearchResult], error) {
	if r.store == nil {
		return Page[store.SearchResult]{}, ErrStoreUnavailable
	}
	if limit <= 0 {
		limit = 10
	}
	slugs, err := r.scopeSlugs(scope)
	if err != nil {
		return Page[store.SearchResult]{}, err
	}
	fetch := capScopedFetch(limit + offset)

	var merged []store.SearchResult
	if len(slugs) == 0 {
		if merged, err = r.store.Search(query, store.SearchOptions{Limit: fetch}); err != nil {
			return Page[store.SearchResult]{}, err
		}
	} else {
		seen := map[int64]bool{}
		for _, slug := range slugs {
			hits, err := r.store.Search(query, store.SearchOptions{Project: slug, Limit: fetch})
			if err != nil {
				return Page[store.SearchResult]{}, err
			}
			for _, h := range hits {
				if seen[h.ID] {
					continue
				}
				seen[h.ID] = true
				merged = append(merged, h)
			}
		}
		sort.SliceStable(merged, func(i, j int) bool { return merged[i].Rank > merged[j].Rank })
		if len(merged) > fetch {
			merged = merged[:fetch]
		}
	}
	return Page[store.SearchResult]{
		Items: sliceWindow(merged, offset, limit), Total: len(merged), Limit: limit, Offset: offset,
	}, nil
}

func (r sqliteMemory) RecentObservationsScoped(scope ProjectScope, limit, offset int) (Page[store.Observation], error) {
	if r.store == nil {
		return Page[store.Observation]{}, ErrStoreUnavailable
	}
	if limit <= 0 {
		limit = 20
	}
	slugs, err := r.scopeSlugs(scope)
	if err != nil {
		return Page[store.Observation]{}, err
	}
	fetch := capScopedFetch(limit + offset)

	var merged []store.Observation
	if len(slugs) == 0 {
		if merged, err = r.store.AllObservations("", "", fetch); err != nil {
			return Page[store.Observation]{}, err
		}
	} else {
		seen := map[int64]bool{}
		for _, slug := range slugs {
			obs, err := r.store.AllObservations(slug, "", fetch)
			if err != nil {
				return Page[store.Observation]{}, err
			}
			for _, o := range obs {
				if seen[o.ID] {
					continue
				}
				seen[o.ID] = true
				merged = append(merged, o)
			}
		}
		sort.SliceStable(merged, func(i, j int) bool { return merged[i].CreatedAt > merged[j].CreatedAt })
		if len(merged) > fetch {
			merged = merged[:fetch]
		}
	}
	return Page[store.Observation]{
		Items: sliceWindow(merged, offset, limit), Total: len(merged), Limit: limit, Offset: offset,
	}, nil
}

func (r sqliteMemory) RecentSessionsScoped(scope ProjectScope, limit, offset int) (Page[store.SessionSummary], error) {
	if r.store == nil {
		return Page[store.SessionSummary]{}, ErrStoreUnavailable
	}
	if limit <= 0 {
		limit = 20
	}
	slugs, err := r.scopeSlugs(scope)
	if err != nil {
		return Page[store.SessionSummary]{}, err
	}
	fetch := capScopedFetch(limit + offset)

	var merged []store.SessionSummary
	if len(slugs) == 0 {
		if merged, err = r.store.AllSessions("", fetch); err != nil {
			return Page[store.SessionSummary]{}, err
		}
	} else {
		seen := map[string]bool{}
		for _, slug := range slugs {
			sessions, err := r.store.AllSessions(slug, fetch)
			if err != nil {
				return Page[store.SessionSummary]{}, err
			}
			for _, s := range sessions {
				if seen[s.ID] {
					continue
				}
				seen[s.ID] = true
				merged = append(merged, s)
			}
		}
		sort.SliceStable(merged, func(i, j int) bool { return merged[i].StartedAt > merged[j].StartedAt })
		if len(merged) > fetch {
			merged = merged[:fetch]
		}
	}
	return Page[store.SessionSummary]{
		Items: sliceWindow(merged, offset, limit), Total: len(merged), Limit: limit, Offset: offset,
	}, nil
}

// sqliteProject adapts the engram SQLite store to ProjectReader.
//
// The TUI shares the single *store.Store that cmd/engram already opened; it
// does not open a connection of its own.
type sqliteProject struct {
	store *store.Store
}

// NewProjectReader wraps an engram store as the Selector and Dashboard screens'
// reader.
//
// A nil store yields a reader whose queries report ErrStoreUnavailable.
func NewProjectReader(s *store.Store) ProjectReader {
	return sqliteProject{store: s}
}

func (r sqliteProject) ListCards() ([]store.ProjectCardListItem, error) {
	if r.store == nil {
		return nil, ErrStoreUnavailable
	}
	cards, _, err := r.store.ListProjectCards(true)
	return cards, err
}

func (r sqliteProject) Card(slug string) (store.ProjectCard, error) {
	if r.store == nil {
		return store.ProjectCard{}, ErrStoreUnavailable
	}
	return r.store.GetProjectCard(slug)
}

func (r sqliteProject) Health(slug string) (ProjectHealth, error) {
	if r.store == nil {
		return ProjectHealth{}, ErrStoreUnavailable
	}
	counts, err := r.store.ProjectCardCounts(slug)
	if err != nil {
		return ProjectHealth{}, err
	}
	sync, err := r.store.ProjectSyncSummary(slug)
	if err != nil {
		return ProjectHealth{}, err
	}
	return ProjectHealth{ProjectCardCounts: counts, Sync: sync}, nil
}

func (r sqliteProject) RecentTasks(slug string, limit int) ([]store.TaskListItem, error) {
	if r.store == nil {
		return nil, ErrStoreUnavailable
	}
	tasks, _, err := r.store.ListTasks(slug, store.TaskListFilter{
		State:  "active",
		Limit:  limit,
		Offset: 0,
	})
	return tasks, err
}

func (r sqliteProject) StaleRunbooks(slug string, limit int) ([]store.RunbookIndexRow, error) {
	if r.store == nil {
		return nil, ErrStoreUnavailable
	}
	staleTrue := true
	runbooks, _, err := r.store.ListRunbookIndex(slug, store.RunbookListFilter{
		Stale:  &staleTrue,
		Limit:  limit,
		Offset: 0,
	})
	return runbooks, err
}

func (r sqliteProject) LatestEvidence(slug string, limit int) ([]store.EvidenceListItem, error) {
	if r.store == nil {
		return nil, ErrStoreUnavailable
	}
	evidence, _, _, err := r.store.ListEvidence(slug, store.EvidenceListFilter{
		TaskSyncID: "",
		Limit:      limit,
		Offset:     0,
	})
	return evidence, err
}

// benchmarksFromDeltas wraps every store.BenchmarkDelta as this package's
// own Benchmark, so its Delta/Improved helpers are available on every
// reader that returns one.
func benchmarksFromDeltas(items []store.BenchmarkDelta) []Benchmark {
	out := make([]Benchmark, len(items))
	for i, item := range items {
		out[i] = Benchmark{BenchmarkDelta: item}
	}
	return out
}

// sqliteTask adapts the engram SQLite store to TaskReader.
//
// The TUI shares the single *store.Store that cmd/engram already opened; it
// does not open a connection of its own.
type sqliteTask struct {
	store *store.Store
}

// NewTaskReader wraps an engram store as the Tasks tab's reader.
//
// A nil store yields a reader whose queries report ErrStoreUnavailable.
func NewTaskReader(s *store.Store) TaskReader {
	return sqliteTask{store: s}
}

// NewTaskPageReader wraps an engram store as the Tasks tab's paginated,
// vault-aware reader (TaskPageReader) — the same sqliteTask NewTaskReader
// returns, under the interface that carries its newer methods.
//
// A nil store yields a reader whose queries report ErrStoreUnavailable.
func NewTaskPageReader(s *store.Store) TaskPageReader {
	return sqliteTask{store: s}
}

func (r sqliteTask) ListTasks(taskProject string, f store.TaskListFilter) ([]store.TaskListItem, error) {
	if r.store == nil {
		return nil, ErrStoreUnavailable
	}
	items, _, err := r.store.ListTasks(taskProject, f)
	return items, err
}

func (r sqliteTask) ListTasksPage(taskProject string, f store.TaskListFilter) (Page[store.TaskListItem], error) {
	if r.store == nil {
		return Page[store.TaskListItem]{}, ErrStoreUnavailable
	}
	page, err := r.store.ListTasksPage(taskProject, f)
	if err != nil {
		return Page[store.TaskListItem]{}, err
	}
	return pageFrom(page), nil
}

func (r sqliteTask) Task(id int64) (TaskDetail, error) {
	if r.store == nil {
		return TaskDetail{}, ErrStoreUnavailable
	}
	t, err := r.store.GetTask(id)
	if err != nil {
		return TaskDetail{}, err
	}
	return r.taskDetail(t)
}

// taskSlugScanPageSize and taskSlugScanLimit bound TaskBySlug's scan (see its
// doc comment): a page size worth fetching in one round trip, and a total
// scanned-rows cap so a very large project cannot turn one lookup into an
// unbounded walk of its whole task list.
const (
	taskSlugScanPageSize = 100
	taskSlugScanLimit    = 2000
)

// TaskBySlug returns one task's aggregate detail by its project-scoped
// slug — the vault folder name a task with no Jira key is addressed by.
//
// store.ResolveTaskRef does not accept a bare slug: its ref patterns match
// only sync_id, jira_key, "#<local-id>" and "change:<sdd-slug>"
// (projects_tasks.go's taskSyncIDRefPattern/jiraKeyRefPattern/
// localIDRefPattern/changeRefRefPattern), and there is no ListTasks filter
// for the slug column either. This pages through the project's tasks
// looking for a Slug match instead. TODO: add a
// store.GetTaskBySlug(project, slug) — or teach ResolveTaskRef a fifth
// pattern — once the Tasks tab makes this common enough for a linear scan
// to matter.
func (r sqliteTask) TaskBySlug(taskProject, slug string) (TaskDetail, error) {
	if r.store == nil {
		return TaskDetail{}, ErrStoreUnavailable
	}
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return TaskDetail{}, store.ErrUnknownTask
	}
	offset := 0
	for scanned := 0; scanned < taskSlugScanLimit; {
		page, err := r.store.ListTasksPage(taskProject, store.TaskListFilter{
			IncludeArchived: true,
			Limit:           taskSlugScanPageSize,
			Offset:          offset,
		})
		if err != nil {
			return TaskDetail{}, err
		}
		if len(page.Items) == 0 {
			break
		}
		for _, item := range page.Items {
			scanned++
			if item.Slug != nil && *item.Slug == slug {
				return r.taskDetail(item.Task)
			}
		}
		offset += len(page.Items)
		if offset >= page.Total {
			break
		}
	}
	return TaskDetail{}, store.ErrUnknownTask
}

// taskDetail assembles TaskDetail for an already-resolved task: its linked
// observations, its evidence, and every benchmark recorded against it.
func (r sqliteTask) taskDetail(t store.Task) (TaskDetail, error) {
	observations, err := r.store.TaskObservationsForTask(t.ID)
	if err != nil {
		return TaskDetail{}, err
	}
	evidence, _, _, err := r.store.ListEvidence(t.Project, store.EvidenceListFilter{TaskSyncID: t.SyncID})
	if err != nil {
		return TaskDetail{}, err
	}
	benchPage, err := r.store.ListBenchmarks(store.BenchmarkListFilter{Task: t.SyncID})
	if err != nil {
		return TaskDetail{}, err
	}
	return TaskDetail{
		Task:         t,
		Observations: observations,
		Evidence:     evidence,
		Benchmarks:   benchmarksFromDeltas(benchPage.Items),
	}, nil
}

// VaultDir resolves a task's vault checkout directory: ENGRAM_VAULT_ROOT
// joined with the task's own vault_path. It reports ok=false rather than an
// error when either half is missing or the resolved path does not exist on
// this machine — those are not failures, they are "there is nothing to
// open", the same distinction internal/diagnostic's knowledge_ref check
// draws between an unconfigured vault and a broken pointer.
func (r sqliteTask) VaultDir(id int64) (string, bool, error) {
	if r.store == nil {
		return "", false, ErrStoreUnavailable
	}
	t, err := r.store.GetTask(id)
	if err != nil {
		return "", false, err
	}
	if t.VaultPath == nil || strings.TrimSpace(*t.VaultPath) == "" {
		return "", false, nil
	}
	root := strings.TrimSpace(os.Getenv(vaultRootEnv))
	if root == "" {
		return "", false, nil
	}
	dir := filepath.Join(root, filepath.FromSlash(*t.VaultPath))
	info, statErr := os.Stat(dir)
	if statErr != nil || !info.IsDir() {
		return "", false, nil
	}
	return dir, true, nil
}

func (r sqliteTask) UpdateState(id int64, state string) error {
	if r.store == nil {
		return ErrStoreUnavailable
	}
	return r.store.UpdateTaskStateMirror(id, state)
}

func (r sqliteTask) LinkObservation(taskID, observationID int64) error {
	if r.store == nil {
		return ErrStoreUnavailable
	}
	t, err := r.store.GetTask(taskID)
	if err != nil {
		return err
	}
	_, err = r.store.LinkTaskObservation(store.LinkTaskObservationParams{Task: t, ObservationID: observationID})
	return err
}

// ContextPack resolves the task by id to find its project, then delegates to
// internal/project.BuildContextPack — the same function mem_context_pack
// calls — addressing the task by its numeric id ("#<id>", ResolveTaskRef's
// local-id form) so the rendered pack always reflects the exact row S4 has
// on screen.
func (r sqliteTask) ContextPack(taskID int64) (string, error) {
	if r.store == nil {
		return "", ErrStoreUnavailable
	}
	t, err := r.store.GetTask(taskID)
	if err != nil {
		return "", err
	}
	_, markdown, err := project.BuildContextPack(r.store, t.Project, fmt.Sprintf("#%d", t.ID), project.DefaultContextPackOptions())
	return markdown, err
}

// sqliteEvidence adapts the engram SQLite store to EvidenceReader.
//
// The TUI shares the single *store.Store that cmd/engram already opened; it
// does not open a connection of its own.
type sqliteEvidence struct {
	store *store.Store
}

// NewEvidenceReader wraps an engram store as the Evidence tab's reader.
//
// A nil store yields a reader whose queries report ErrStoreUnavailable.
func NewEvidenceReader(s *store.Store) EvidenceReader {
	return sqliteEvidence{store: s}
}

// NewEvidencePageReader wraps an engram store as the Evidence tab's
// paginated reader (EvidencePageReader) — the same sqliteEvidence
// NewEvidenceReader returns, under the interface that carries its newer
// methods.
//
// A nil store yields a reader whose queries report ErrStoreUnavailable.
func NewEvidencePageReader(s *store.Store) EvidencePageReader {
	return sqliteEvidence{store: s}
}

func (r sqliteEvidence) ListEvidence(project string, f store.EvidenceListFilter) ([]store.EvidenceListItem, error) {
	if r.store == nil {
		return nil, ErrStoreUnavailable
	}
	items, _, _, err := r.store.ListEvidence(project, f)
	return items, err
}

func (r sqliteEvidence) ListEvidencePage(project string, f store.EvidenceListFilter) (EvidencePage, error) {
	if r.store == nil {
		return EvidencePage{}, ErrStoreUnavailable
	}
	page, totalBytes, err := r.store.ListEvidencePage(project, f)
	if err != nil {
		return EvidencePage{}, err
	}
	return EvidencePage{Page: pageFrom(page), TotalBytes: totalBytes}, nil
}

// evidenceCategoryScanPageSize and evidenceCategoryScanLimit bound
// Categories's scan the same way TaskBySlug's bound its own: there is no
// "distinct category, count(*)" query in internal/store, so this pages
// through ListEvidencePage and aggregates in Go. TODO: add a
// store.EvidenceCategoryCounts(project) once a project's evidence count
// makes the scan worth replacing with one grouped query.
const (
	evidenceCategoryScanPageSize = 200
	evidenceCategoryScanLimit    = 5000
)

func (r sqliteEvidence) Categories(project string) ([]CategoryCount, error) {
	if r.store == nil {
		return nil, ErrStoreUnavailable
	}
	counts := map[string]int{}
	var order []string
	offset := 0
	for scanned := 0; scanned < evidenceCategoryScanLimit; {
		page, _, err := r.store.ListEvidencePage(project, store.EvidenceListFilter{
			Limit: evidenceCategoryScanPageSize, Offset: offset,
		})
		if err != nil {
			return nil, err
		}
		if len(page.Items) == 0 {
			break
		}
		for _, item := range page.Items {
			scanned++
			category := item.Category
			if _, seen := counts[category]; !seen {
				order = append(order, category)
			}
			counts[category]++
		}
		offset += len(page.Items)
		if offset >= page.Total {
			break
		}
	}
	sort.Strings(order)
	out := make([]CategoryCount, 0, len(order))
	for _, category := range order {
		out = append(out, CategoryCount{Category: category, Count: counts[category]})
	}
	return out, nil
}

// sqliteRunbook adapts the engram SQLite store to RunbookReader.
//
// The TUI shares the single *store.Store that cmd/engram already opened; it
// does not open a connection of its own.
type sqliteRunbook struct {
	store *store.Store
}

// NewRunbookReader wraps an engram store as the Runbooks tab's reader.
//
// A nil store yields a reader whose queries report ErrStoreUnavailable.
func NewRunbookReader(s *store.Store) RunbookReader {
	return sqliteRunbook{store: s}
}

// NewRunbookPageReader wraps an engram store as the Runbooks tab's
// paginated, further-filtered reader (RunbookPageReader) — the same
// sqliteRunbook NewRunbookReader returns, under the interface that carries
// its newer method.
//
// A nil store yields a reader whose queries report ErrStoreUnavailable.
func NewRunbookPageReader(s *store.Store) RunbookPageReader {
	return sqliteRunbook{store: s}
}

func (r sqliteRunbook) ListRunbooks(project string, all bool) ([]store.RunbookIndexRow, error) {
	if r.store == nil {
		return nil, ErrStoreUnavailable
	}
	if all {
		project = ""
	}
	items, _, err := r.store.ListRunbookIndex(project, store.RunbookListFilter{})
	return items, err
}

func (r sqliteRunbook) ListRunbooksPage(project string, all bool, f RunbookFilter) (Page[store.RunbookIndexRow], error) {
	if r.store == nil {
		return Page[store.RunbookIndexRow]{}, ErrStoreUnavailable
	}
	if all {
		project = ""
	}
	page, err := r.store.ListRunbooksPage(project, f)
	if err != nil {
		return Page[store.RunbookIndexRow]{}, err
	}
	return pageFrom(page), nil
}

func (r sqliteRunbook) SearchRunbooks(project string, all bool, query string, limit int) ([]store.RunbookIndexRow, error) {
	if r.store == nil {
		return nil, ErrStoreUnavailable
	}
	if all {
		project = ""
	}
	return r.store.SearchRunbookIndex(query, project, limit)
}

// TaskKey returns the label a task is identified by everywhere in the TUI:
// its Jira key, falling back to its SDD change slug, falling back to its
// sync_id. It is the same precedence internal/project.BuildContextPack uses
// for the context pack's own header, exported through data (rather than
// tabs/tasks importing internal/project directly) so every tab stays on the
// dependency rule in rfc-tui.md §4.1: a tab imports only data, theme and
// shared.
func TaskKey(t store.Task) string {
	return project.TaskKey(t)
}

// JiraURL returns the browse URL for a Jira key, using the same
// ENGRAM_JIRA_BASE_URL configuration internal/project.BuildContextPack reads
// for the context pack's own links.
func JiraURL(jiraKey string) string {
	return project.JiraBaseURL() + jiraKey
}

// ─── Project tree ────────────────────────────────────────────────────────────

// projectTreeStore is the store surface sqliteProjectTree needs — narrower
// than *store.Store, the same way runbooks.Persister narrows
// internal/runbooks's own dependency on internal/store. It exists so a test
// can count exactly how many store calls one ProjectTree() makes by handing
// sqliteProjectTree a counting fake, without instrumenting SQL.
type projectTreeStore interface {
	ProjectTree(root string, includeCounts bool) ([]store.ProjectTreeNode, error)
	GetProjectCard(slug string) (store.ProjectCard, error)
	ProjectCardCounts(slug string) (store.ProjectCardCounts, error)
	ListProjectAliases(slug string) ([]store.ProjectAlias, error)
	SubtreeSlugs(root string) ([]string, error)
	ResolveProjectSlug(raw string) (store.ProjectResolution, error)
}

// sqliteProjectTree adapts the engram store to ProjectTreeReader.
type sqliteProjectTree struct {
	store projectTreeStore
}

// NewProjectTreeReader wraps an engram store as the project tree overlay's
// reader.
//
// A nil store yields a reader whose queries report ErrStoreUnavailable. The
// nil check happens here rather than in every method (as the other readers
// in this file do against *store.Store) because store is an interface field:
// assigning a nil *store.Store to it directly would produce a non-nil
// interface holding a nil pointer, the classic Go trap — returning early
// keeps the field a true nil interface instead.
func NewProjectTreeReader(s *store.Store) ProjectTreeReader {
	if s == nil {
		return sqliteProjectTree{}
	}
	return sqliteProjectTree{store: s}
}

func (r sqliteProjectTree) ProjectTree() ([]ProjectNode, error) {
	if r.store == nil {
		return nil, ErrStoreUnavailable
	}
	// One store call: includeCounts=true batches the counters internally
	// (store.ProjectCardCountsBatch, one grouped query per counted table),
	// so this is the "ProjectTree con counts en lote" the reader is
	// specified to cost at most two store calls for.
	flat, err := r.store.ProjectTree("", true)
	if err != nil {
		return nil, err
	}
	return buildProjectForest(flat), nil
}

func (r sqliteProjectTree) ProjectNode(slug string) (ProjectNode, error) {
	if r.store == nil {
		return ProjectNode{}, ErrStoreUnavailable
	}
	card, err := r.store.GetProjectCard(slug)
	if err != nil {
		return ProjectNode{}, err
	}
	counts, err := r.store.ProjectCardCounts(slug)
	if err != nil {
		return ProjectNode{}, err
	}
	node := projectNodeFromCard(card, counts)
	aliases, err := r.store.ListProjectAliases(slug)
	if err != nil {
		return ProjectNode{}, err
	}
	for _, a := range aliases {
		node.Aliases = append(node.Aliases, a.Alias)
	}
	return node, nil
}

// maxAncestorHops bounds Ancestors's walk. The schema caps depth at three
// (internal/store's maxProjectDepth), so a well-formed card never needs more
// than a handful of hops; the cap only guards against a corrupted parent
// chain turning into an infinite loop.
const maxAncestorHops = 16

func (r sqliteProjectTree) Ancestors(slug string) ([]ProjectNode, error) {
	if r.store == nil {
		return nil, ErrStoreUnavailable
	}
	var chain []ProjectNode
	current := slug
	seen := map[string]bool{current: true}
	for i := 0; i < maxAncestorHops; i++ {
		card, err := r.store.GetProjectCard(current)
		if err != nil {
			return nil, err
		}
		if card.ParentSlug == nil || strings.TrimSpace(*card.ParentSlug) == "" {
			break
		}
		parentSlug := *card.ParentSlug
		if seen[parentSlug] {
			break // defensive: a corrupted chain must not loop forever
		}
		seen[parentSlug] = true
		parentCard, err := r.store.GetProjectCard(parentSlug)
		if err != nil {
			return nil, err
		}
		counts, err := r.store.ProjectCardCounts(parentSlug)
		if err != nil {
			return nil, err
		}
		chain = append(chain, projectNodeFromCard(parentCard, counts))
		current = parentSlug
	}
	// chain was built child-to-root; reverse it to root-first.
	for i, j := 0, len(chain)-1; i < j; i, j = i+1, j-1 {
		chain[i], chain[j] = chain[j], chain[i]
	}
	return chain, nil
}

func (r sqliteProjectTree) Descendants(root string) ([]string, error) {
	if r.store == nil {
		return nil, ErrStoreUnavailable
	}
	return r.store.SubtreeSlugs(root)
}

func (r sqliteProjectTree) ResolveAlias(raw string) (string, error) {
	if r.store == nil {
		return "", ErrStoreUnavailable
	}
	res, err := r.store.ResolveProjectSlug(raw)
	if err != nil {
		return "", err
	}
	if res.Via == store.ProjectResolvedViaUnresolved || res.Slug == "" {
		return "", ErrProjectUnresolved
	}
	return res.Slug, nil
}

// projectNodeFromCard maps a store.ProjectCard and its counters onto a
// ProjectNode, leaving Aliases and Children to whichever caller populates
// them (ProjectTree never fetches aliases; ProjectNode always does).
func projectNodeFromCard(c store.ProjectCard, counts store.ProjectCardCounts) ProjectNode {
	parent := ""
	if c.ParentSlug != nil {
		parent = *c.ParentSlug
	}
	return ProjectNode{
		Slug:        c.Slug,
		ParentSlug:  parent,
		DisplayName: c.DisplayName,
		Kind:        c.Kind,
		Description: derefOr(c.Description, ""),
		Icon:        derefOr(c.Icon, ""),
		Color:       derefOr(c.Color, ""),
		Tags:        parseJSONStringArray(c.Tags),
		Depth:       c.Depth,
		Counts:      counts,
	}
}

// buildProjectForest turns ProjectTree's flat preorder walk into a nested
// forest. It builds bottom-up through a recursive childrenOf lookup rather
// than attaching each row to its parent as it is visited: appending a value
// copy into a parent's Children slice while the child itself still has more
// children coming (a later row in the same walk) would freeze that copy
// before its own subtree was complete.
func buildProjectForest(flat []store.ProjectTreeNode) []ProjectNode {
	base := make(map[string]ProjectNode, len(flat))
	for _, n := range flat {
		var counts store.ProjectCardCounts
		if n.Counts != nil {
			counts = *n.Counts
		}
		base[n.Slug] = projectNodeFromCard(n.ProjectCard, counts)
	}

	childrenOf := map[string][]string{}
	var rootSlugs []string
	for _, n := range flat {
		parent := ""
		if n.ParentSlug != nil {
			parent = *n.ParentSlug
		}
		if parent == "" {
			rootSlugs = append(rootSlugs, n.Slug)
			continue
		}
		if _, ok := base[parent]; !ok {
			// The named parent is not in this walk — should not happen for
			// the full, unscoped tree ProjectTree() asks for, but surface
			// the card as a root rather than dropping it.
			rootSlugs = append(rootSlugs, n.Slug)
			continue
		}
		childrenOf[parent] = append(childrenOf[parent], n.Slug)
	}

	var build func(slug string) ProjectNode
	build = func(slug string) ProjectNode {
		node := base[slug]
		for _, childSlug := range childrenOf[slug] {
			node.Children = append(node.Children, build(childSlug))
		}
		return node
	}

	roots := make([]ProjectNode, 0, len(rootSlugs))
	for _, slug := range rootSlugs {
		roots = append(roots, build(slug))
	}
	return roots
}

// derefOr returns *p, or fallback when p is nil.
func derefOr(p *string, fallback string) string {
	if p == nil {
		return fallback
	}
	return *p
}

// parseJSONStringArray decodes a project_cards.tags-shaped column (nil, or a
// JSON array of strings, per its json_valid CHECK constraint) leniently: a
// nil column or one that fails to parse both yield nil rather than an error,
// since a rendering concern like a tag chip list is not worth failing a
// whole tree walk over.
func parseJSONStringArray(raw *string) []string {
	if raw == nil || strings.TrimSpace(*raw) == "" {
		return nil
	}
	var out []string
	if err := json.Unmarshal([]byte(*raw), &out); err != nil {
		return nil
	}
	return out
}

// ─── Benchmarks ──────────────────────────────────────────────────────────────

// sqliteBenchmark adapts the engram store to BenchmarkReader.
type sqliteBenchmark struct {
	store *store.Store
}

// NewBenchmarkReader wraps an engram store as the Benchmarks tab's reader.
//
// A nil store yields a reader whose queries report ErrStoreUnavailable.
func NewBenchmarkReader(s *store.Store) BenchmarkReader {
	return sqliteBenchmark{store: s}
}

func (r sqliteBenchmark) ListBenchmarks(project string, f BenchmarkFilter) (Page[Benchmark], error) {
	if r.store == nil {
		return Page[Benchmark]{}, ErrStoreUnavailable
	}
	f.Project = project
	page, err := r.store.ListBenchmarks(f)
	if err != nil {
		return Page[Benchmark]{}, err
	}
	return Page[Benchmark]{
		Items: benchmarksFromDeltas(page.Items), Total: page.Total, Limit: page.Limit, Offset: page.Offset,
	}, nil
}

func (r sqliteBenchmark) TaskBenchmarks(taskSyncID string) ([]Benchmark, error) {
	if r.store == nil {
		return nil, ErrStoreUnavailable
	}
	page, err := r.store.ListBenchmarks(store.BenchmarkListFilter{Task: taskSyncID})
	if err != nil {
		return nil, err
	}
	return benchmarksFromDeltas(page.Items), nil
}

func (r sqliteBenchmark) MetricHistory(project, metric string, limit int) ([]Benchmark, error) {
	if r.store == nil {
		return nil, ErrStoreUnavailable
	}
	page, err := r.store.ListBenchmarks(store.BenchmarkListFilter{Project: project, Metric: metric, Limit: limit})
	if err != nil {
		return nil, err
	}
	// ListBenchmarks reads newest first; a history series plots oldest to
	// newest, left to right.
	items := benchmarksFromDeltas(page.Items)
	for i, j := 0, len(items)-1; i < j; i, j = i+1, j-1 {
		items[i], items[j] = items[j], items[i]
	}
	return items, nil
}

// ─── Graph ───────────────────────────────────────────────────────────────────

// sqliteGraph adapts the engram store to GraphReader and GraphSyncer.
type sqliteGraph struct {
	store *store.Store
	// repoDir is the checkout project.SyncGraph resolves graph.json and git
	// HEAD against — see NewGraphSyncer's doc comment.
	repoDir string
}

// NewGraphReader wraps an engram store as the Graph tab's reader.
//
// A nil store yields a reader whose queries report ErrStoreUnavailable.
func NewGraphReader(s *store.Store) GraphReader {
	return sqliteGraph{store: s}
}

// NewGraphSyncer wraps an engram store as the Graph tab's syncer. repoDir is
// the checkout project.SyncGraph resolves graph.json and git HEAD against —
// the same directory cmd/engram and internal/mcp resolve through
// DetectProjectFull(cwd) (or an explicit repo_dir) before calling
// project.SyncGraph themselves. The TUI does not detect a checkout of its
// own yet (tui.New has no such argument today); pass the directory the
// process is running from until it does.
func NewGraphSyncer(s *store.Store, repoDir string) GraphSyncer {
	return sqliteGraph{store: s, repoDir: repoDir}
}

func (r sqliteGraph) GraphState(project string) (GraphState, error) {
	if r.store == nil {
		return GraphState{}, ErrStoreUnavailable
	}
	card, err := r.store.GetProjectCard(project)
	if err != nil {
		return GraphState{}, err
	}
	return graphStateFromCard(project, card), nil
}

func (r sqliteGraph) SyncGraph(proj string) (GraphState, error) {
	if r.store == nil {
		return GraphState{}, ErrStoreUnavailable
	}
	card, err := r.store.GetProjectCard(proj)
	if err != nil {
		return GraphState{}, err
	}
	if _, err := project.SyncGraph(r.store, proj, r.repoDir, card.GraphPath); err != nil {
		return GraphState{}, err
	}
	return r.GraphState(proj)
}

// ObservationRefs pages through the project's graph-linked observations,
// newest first.
//
// Only the "graph" kind is asked for: this is what the Graph tab lists, and a
// knowledge or Jira reference on the same observation belongs to another
// screen. store.ListObservationRefs carries the real total, so a caller can
// render "showing 20 of 340" without counting the page it already has.
func (r sqliteGraph) ObservationRefs(project string, limit, offset int) (Page[ObservationRef], error) {
	if r.store == nil {
		return Page[ObservationRef]{}, ErrStoreUnavailable
	}
	page, err := r.store.ListObservationRefs(project, "graph", limit, offset)
	if err != nil {
		return Page[ObservationRef]{}, err
	}
	items := make([]ObservationRef, 0, len(page.Items))
	for _, row := range page.Items {
		items = append(items, ObservationRef{
			ObservationID: row.ObservationID,
			RefKind:       row.RefKind,
			Ref:           row.Ref,
			GraphCommit:   row.GraphCommit,
		})
	}
	return Page[ObservationRef]{Items: items, Total: page.Total, Limit: page.Limit, Offset: page.Offset}, nil
}

// graphStateFromCard reads GraphState off a project card's Graph* columns,
// parsing the graph_summary JSON blob (internal/project.GraphSummary) for
// the counts and god nodes.
func graphStateFromCard(slug string, card store.ProjectCard) GraphState {
	state := GraphState{Project: slug}
	if card.GraphCommit != nil {
		state.Commit = *card.GraphCommit
	}
	if card.GraphBuiltAt != nil {
		state.BuiltAt = *card.GraphBuiltAt
	}
	if card.GraphSummary != nil {
		var summary project.GraphSummary
		if err := json.Unmarshal([]byte(*card.GraphSummary), &summary); err == nil {
			state.Nodes = summary.NodeCount
			state.Edges = summary.EdgeCount
			state.Communities = summary.CommunityCount
			state.GodNodes = summary.GodNodes
		}
	}
	if card.GraphStaleReason != nil && strings.TrimSpace(*card.GraphStaleReason) != "" {
		state.Stale = true
		state.StaleReason = *card.GraphStaleReason
	}
	if card.GraphChangedFiles != nil {
		state.ChangedFiles = *card.GraphChangedFiles
	}
	if card.GraphCheckedAt != nil {
		state.CheckedAt = *card.GraphCheckedAt
	}
	return state
}

// ─── Themes ──────────────────────────────────────────────────────────────────

// sqliteTheme adapts the engram store to ThemeReader and ThemeWriter.
type sqliteTheme struct {
	store *store.Store
}

// NewThemeReader wraps an engram store as the theme picker's reader.
//
// A nil store yields a reader whose queries report ErrStoreUnavailable.
func NewThemeReader(s *store.Store) ThemeReader {
	return sqliteTheme{store: s}
}

// NewThemeWriter wraps an engram store as the theme picker's writer.
//
// A nil store yields a writer whose calls report ErrStoreUnavailable.
func NewThemeWriter(s *store.Store) ThemeWriter {
	return sqliteTheme{store: s}
}

func (r sqliteTheme) ListThemes() ([]ThemeRecord, error) {
	if r.store == nil {
		return nil, ErrStoreUnavailable
	}
	themes, err := r.store.ListThemes()
	if err != nil {
		return nil, err
	}
	out := make([]ThemeRecord, len(themes))
	for i, t := range themes {
		out[i] = themeRecordFromStore(t)
	}
	return out, nil
}

func (r sqliteTheme) Theme(name string) (ThemeRecord, error) {
	if r.store == nil {
		return ThemeRecord{}, ErrStoreUnavailable
	}
	t, err := r.store.GetTheme(name)
	if err != nil {
		return ThemeRecord{}, err
	}
	return themeRecordFromStore(t), nil
}

func (r sqliteTheme) SaveTheme(rec store.ThemeRecord) error {
	if r.store == nil {
		return ErrStoreUnavailable
	}
	return r.store.SaveTheme(rec)
}

func (r sqliteTheme) DeleteTheme(name string) error {
	if r.store == nil {
		return ErrStoreUnavailable
	}
	return r.store.DeleteTheme(name)
}

func (r sqliteTheme) ResetTheme(name string, palette json.RawMessage) error {
	if r.store == nil {
		return ErrStoreUnavailable
	}
	return r.store.ResetTheme(name, palette)
}

// themeRecordFromStore wraps a store.ThemeRecord and fills Invalid — see
// ThemeRecord's doc comment for what this does and does not check.
func themeRecordFromStore(t store.ThemeRecord) ThemeRecord {
	rec := ThemeRecord{ThemeRecord: t}
	var roles map[string]json.RawMessage
	if err := json.Unmarshal(t.Palette, &roles); err != nil || len(roles) == 0 {
		rec.Invalid = "palette has no color roles"
	}
	return rec
}

// ─── Settings ────────────────────────────────────────────────────────────────

// sqliteSettings adapts the engram store to SettingsReader and
// SettingsWriter.
type sqliteSettings struct {
	store *store.Store
}

// NewSettingsReader wraps an engram store as Ajustes's settings reader.
//
// A nil store yields a reader whose queries report ErrStoreUnavailable.
func NewSettingsReader(s *store.Store) SettingsReader {
	return sqliteSettings{store: s}
}

// NewSettingsWriter wraps an engram store as Ajustes's settings writer.
//
// A nil store yields a writer whose calls report ErrStoreUnavailable.
func NewSettingsWriter(s *store.Store) SettingsWriter {
	return sqliteSettings{store: s}
}

func (r sqliteSettings) Setting(key string) (string, bool, error) {
	if r.store == nil {
		return "", false, ErrStoreUnavailable
	}
	return r.store.Setting(key)
}

func (r sqliteSettings) Settings(prefix string) (map[string]string, error) {
	if r.store == nil {
		return nil, ErrStoreUnavailable
	}
	return r.store.Settings(prefix)
}

func (r sqliteSettings) SetSetting(key, value string) error {
	if r.store == nil {
		return ErrStoreUnavailable
	}
	return r.store.SetSetting(key, value)
}

// ─── Workspace search ────────────────────────────────────────────────────────

// sqliteSearch adapts the engram store to GlobalSearcher.
type sqliteSearch struct {
	store *store.Store
}

// NewGlobalSearcher wraps an engram store as the search palette's (ctrl+k)
// searcher.
//
// A nil store yields a searcher whose queries report ErrStoreUnavailable.
func NewGlobalSearcher(s *store.Store) GlobalSearcher {
	return sqliteSearch{store: s}
}

func (r sqliteSearch) SearchWorkspace(q SearchQuery) ([]SearchHit, error) {
	if r.store == nil {
		return nil, ErrStoreUnavailable
	}
	results, err := r.store.SearchWorkspace(store.SearchWorkspaceParams{
		Query:   q.Text,
		Project: q.Scope.Project,
		Subtree: q.Scope.Subtree,
		Kinds:   q.Kinds,
		PerKind: q.LimitPerKind,
	})
	if err != nil {
		return nil, err
	}
	hits := make([]SearchHit, len(results.Hits))
	for i, h := range results.Hits {
		hits[i] = SearchHit{
			Kind:    h.Kind,
			ID:      h.ID,
			Slug:    h.Ref,
			Project: h.Project,
			Title:   h.Title,
			// Subtitle has no dedicated store column: the project is the
			// one piece of context every kind carries, so it doubles as the
			// palette row's second line until a kind wants a richer one.
			Subtitle: h.Project,
			Snippet:  h.Snippet,
			Rank:     h.Rank,
		}
	}
	return hits, nil
}
