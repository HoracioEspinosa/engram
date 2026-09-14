package data

import (
	"encoding/json"
	"errors"
	"sort"
	"strings"

	"github.com/HoracioEspinosa/engram/internal/store"
)

// pageSlice pages items client-side: Total is the full count before
// slicing, Items is the [offset:offset+limit] window. It backs every new
// *Scoped/*Page fake method in this file, so a test seeding more rows than
// one page reliably sees Total != len(Items).
func pageSlice[T any](items []T, limit, offset int) Page[T] {
	if limit <= 0 {
		limit = len(items)
	}
	if offset < 0 {
		offset = 0
	}
	if offset > len(items) {
		offset = len(items)
	}
	end := offset + limit
	if end > len(items) {
		end = len(items)
	}
	return Page[T]{Items: items[offset:end], Total: len(items), Limit: limit, Offset: offset}
}

// filterByScope keeps the items whose project (per projectOf) is scope.Project,
// or — when scope.Subtree — in subtreeSlugs[scope.Project]. An empty
// scope.Project keeps everything, matching every scoped method's unscoped
// sibling.
func filterByScope[T any](items []T, scope ProjectScope, projectOf func(T) string, subtreeSlugs map[string][]string) []T {
	if scope.Project == "" {
		return items
	}
	allowed := map[string]bool{scope.Project: true}
	if scope.Subtree {
		for _, s := range subtreeSlugs[scope.Project] {
			allowed[s] = true
		}
	}
	var out []T
	for _, it := range items {
		if allowed[projectOf(it)] {
			out = append(out, it)
		}
	}
	return out
}

// FakeMemory is an in-memory MemoryReader for tests: set the fields you care
// about, leave the rest zero.
//
// Err short-circuits every method, which is how a test drives the error banner
// without a broken database.
type FakeMemory struct {
	StatsResult     *store.Stats
	SearchResults   []store.SearchResult
	Observations    []store.Observation
	ObservationByID map[int64]*store.Observation
	TimelineResult  *store.TimelineResult
	Sessions        []store.SessionSummary
	SessionObs      map[string][]store.Observation

	// SubtreeSlugs backs the *Scoped methods' Subtree widening: project ->
	// every slug (itself included) a subtree scope should match. A project
	// missing from this map widens to itself only.
	SubtreeSlugs map[string][]string

	// Err is returned by every method when set.
	Err error

	// Queries and DeletedSessions record what the tab asked for, so a test can
	// assert on the call and not only on the rendered result.
	Queries         []string
	DeletedSessions []string

	// LastScope records the ProjectScope the tab most recently asked for, the
	// same convention FakeTask.LastListFilter follows.
	LastScope ProjectScope
}

var _ MemoryReader = (*FakeMemory)(nil)
var _ ScopedMemoryReader = (*FakeMemory)(nil)

func (f *FakeMemory) Stats() (*store.Stats, error) {
	if f.Err != nil {
		return nil, f.Err
	}
	return f.StatsResult, nil
}

func (f *FakeMemory) Search(query string, _ store.SearchOptions) ([]store.SearchResult, error) {
	f.Queries = append(f.Queries, query)
	if f.Err != nil {
		return nil, f.Err
	}
	return f.SearchResults, nil
}

func (f *FakeMemory) RecentObservations(limit int) ([]store.Observation, error) {
	if f.Err != nil {
		return nil, f.Err
	}
	return capObservations(f.Observations, limit), nil
}

func (f *FakeMemory) Observation(id int64) (*store.Observation, error) {
	if f.Err != nil {
		return nil, f.Err
	}
	return f.ObservationByID[id], nil
}

func (f *FakeMemory) Timeline(_ int64, _, _ int) (*store.TimelineResult, error) {
	if f.Err != nil {
		return nil, f.Err
	}
	return f.TimelineResult, nil
}

func (f *FakeMemory) RecentSessions(limit int) ([]store.SessionSummary, error) {
	if f.Err != nil {
		return nil, f.Err
	}
	if limit > 0 && len(f.Sessions) > limit {
		return f.Sessions[:limit], nil
	}
	return f.Sessions, nil
}

func (f *FakeMemory) SessionObservations(sessionID string, limit int) ([]store.Observation, error) {
	if f.Err != nil {
		return nil, f.Err
	}
	return capObservations(f.SessionObs[sessionID], limit), nil
}

func (f *FakeMemory) DeleteSession(sessionID string) error {
	if f.Err != nil {
		return f.Err
	}
	f.DeletedSessions = append(f.DeletedSessions, sessionID)
	return nil
}

func (f *FakeMemory) SearchScoped(query string, scope ProjectScope, limit, offset int) (Page[store.SearchResult], error) {
	f.Queries = append(f.Queries, query)
	f.LastScope = scope
	if f.Err != nil {
		return Page[store.SearchResult]{}, f.Err
	}
	matched := filterByScope(f.SearchResults, scope, func(r store.SearchResult) string { return derefOr(r.Project, "") }, f.SubtreeSlugs)
	return pageSlice(matched, limit, offset), nil
}

func (f *FakeMemory) RecentObservationsScoped(scope ProjectScope, limit, offset int) (Page[store.Observation], error) {
	f.LastScope = scope
	if f.Err != nil {
		return Page[store.Observation]{}, f.Err
	}
	matched := filterByScope(f.Observations, scope, func(o store.Observation) string { return derefOr(o.Project, "") }, f.SubtreeSlugs)
	return pageSlice(matched, limit, offset), nil
}

func (f *FakeMemory) RecentSessionsScoped(scope ProjectScope, limit, offset int) (Page[store.SessionSummary], error) {
	f.LastScope = scope
	if f.Err != nil {
		return Page[store.SessionSummary]{}, f.Err
	}
	matched := filterByScope(f.Sessions, scope, func(s store.SessionSummary) string { return s.Project }, f.SubtreeSlugs)
	return pageSlice(matched, limit, offset), nil
}

func capObservations(obs []store.Observation, limit int) []store.Observation {
	if limit > 0 && len(obs) > limit {
		return obs[:limit]
	}
	return obs
}

// FakeProject is an in-memory ProjectReader for tests: set the fields you care
// about, leave the rest zero.
//
// Err short-circuits every method, which is how a test drives the error banner
// without a broken database.
type FakeProject struct {
	Cards             []store.ProjectCardListItem
	CardBySlug        map[string]store.ProjectCard
	HealthBySlug      map[string]ProjectHealth
	TasksBySlug       map[string][]store.TaskListItem
	StaleRunbooksSlug map[string][]store.RunbookIndexRow
	EvidenceBySlug    map[string][]store.EvidenceListItem

	// Err is returned by every method when set.
	Err error
}

var _ ProjectReader = (*FakeProject)(nil)

func (f *FakeProject) ListCards() ([]store.ProjectCardListItem, error) {
	if f.Err != nil {
		return nil, f.Err
	}
	return f.Cards, nil
}

func (f *FakeProject) Card(slug string) (store.ProjectCard, error) {
	if f.Err != nil {
		return store.ProjectCard{}, f.Err
	}
	card, ok := f.CardBySlug[slug]
	if !ok {
		return store.ProjectCard{}, errors.New("no such project")
	}
	return card, nil
}

func (f *FakeProject) Health(slug string) (ProjectHealth, error) {
	if f.Err != nil {
		return ProjectHealth{}, f.Err
	}
	// A project with no counters is not a missing project: it is a project
	// whose numbers are all zero, the same way one with no tasks returns an
	// empty slice rather than an error. Only Card reports a slug that does
	// not exist, because the card is what identifies the project at all.
	return f.HealthBySlug[slug], nil
}

func (f *FakeProject) RecentTasks(slug string, limit int) ([]store.TaskListItem, error) {
	if f.Err != nil {
		return nil, f.Err
	}
	tasks := f.TasksBySlug[slug]
	if limit > 0 && len(tasks) > limit {
		return tasks[:limit], nil
	}
	return tasks, nil
}

func (f *FakeProject) StaleRunbooks(slug string, limit int) ([]store.RunbookIndexRow, error) {
	if f.Err != nil {
		return nil, f.Err
	}
	runbooks := f.StaleRunbooksSlug[slug]
	if limit > 0 && len(runbooks) > limit {
		return runbooks[:limit], nil
	}
	return runbooks, nil
}

func (f *FakeProject) LatestEvidence(slug string, limit int) ([]store.EvidenceListItem, error) {
	if f.Err != nil {
		return nil, f.Err
	}
	evidence := f.EvidenceBySlug[slug]
	if limit > 0 && len(evidence) > limit {
		return evidence[:limit], nil
	}
	return evidence, nil
}

// FakeTask is an in-memory TaskReader for tests: set the fields you care
// about, leave the rest zero.
//
// Err short-circuits every method, which is how a test drives the error
// banner without a broken database. UpdateStateCalls and LinkCalls record
// what the tab asked to write, so a test can assert on the call and not only
// on the screen it produced.
type FakeTask struct {
	ItemsByProject map[string][]store.TaskListItem
	DetailByID     map[int64]TaskDetail
	// DetailBySlug is keyed "<project>/<slug>", TaskBySlug's compound
	// identity: a slug is only unique within its project.
	DetailBySlug map[string]TaskDetail
	// VaultDirByID backs VaultDir. A missing or empty entry reports ok=false,
	// the same as a task with no vault_path or no ENGRAM_VAULT_ROOT would.
	VaultDirByID    map[int64]string
	ContextPackByID map[int64]string

	// Err is returned by every method when set.
	Err error

	// LastListFilter records the filter the tab most recently asked for.
	LastListFilter   store.TaskListFilter
	UpdateStateCalls []struct {
		ID    int64
		State string
	}
	LinkCalls []struct {
		TaskID        int64
		ObservationID int64
	}
}

var _ TaskReader = (*FakeTask)(nil)
var _ TaskPageReader = (*FakeTask)(nil)

// filteredTasks applies filter.Query to a project's seeded items, without
// paginating — ListTasks and ListTasksPage both window this same set, so the
// filter is written once.
func (f *FakeTask) filteredTasks(taskProject string, filter store.TaskListFilter) []store.TaskListItem {
	items := f.ItemsByProject[taskProject]
	if filter.Query == "" {
		return items
	}
	var matched []store.TaskListItem
	for _, it := range items {
		if strings.Contains(strings.ToLower(it.Title), strings.ToLower(filter.Query)) {
			matched = append(matched, it)
		}
	}
	return matched
}

func (f *FakeTask) ListTasks(taskProject string, filter store.TaskListFilter) ([]store.TaskListItem, error) {
	f.LastListFilter = filter
	if f.Err != nil {
		return nil, f.Err
	}
	items := f.filteredTasks(taskProject, filter)
	// Mirrors store.ListTasks's own default: an unset limit still caps the
	// page at 20, it does not mean "everything" (see internal/store's
	// projects_tasks.go ListTasks). A fake that returned every row here
	// would let a caller's pagination logic go untested.
	limit := filter.Limit
	if limit <= 0 {
		limit = 20
	}
	offset := filter.Offset
	if offset > len(items) {
		offset = len(items)
	}
	end := offset + limit
	if end > len(items) {
		end = len(items)
	}
	return items[offset:end], nil
}

func (f *FakeTask) ListTasksPage(taskProject string, filter store.TaskListFilter) (Page[store.TaskListItem], error) {
	f.LastListFilter = filter
	if f.Err != nil {
		return Page[store.TaskListItem]{}, f.Err
	}
	items := f.filteredTasks(taskProject, filter)
	limit := filter.Limit
	if limit <= 0 {
		limit = 20
	}
	return pageSlice(items, limit, filter.Offset), nil
}

func (f *FakeTask) Task(id int64) (TaskDetail, error) {
	if f.Err != nil {
		return TaskDetail{}, f.Err
	}
	d, ok := f.DetailByID[id]
	if !ok {
		return TaskDetail{}, errors.New("no such task")
	}
	return d, nil
}

func (f *FakeTask) TaskBySlug(taskProject, slug string) (TaskDetail, error) {
	if f.Err != nil {
		return TaskDetail{}, f.Err
	}
	d, ok := f.DetailBySlug[taskProject+"/"+slug]
	if !ok {
		return TaskDetail{}, errors.New("no such task")
	}
	return d, nil
}

func (f *FakeTask) VaultDir(id int64) (string, bool, error) {
	if f.Err != nil {
		return "", false, f.Err
	}
	dir, ok := f.VaultDirByID[id]
	if !ok || dir == "" {
		return "", false, nil
	}
	return dir, true, nil
}

func (f *FakeTask) UpdateState(id int64, state string) error {
	f.UpdateStateCalls = append(f.UpdateStateCalls, struct {
		ID    int64
		State string
	}{ID: id, State: state})
	if f.Err != nil {
		return f.Err
	}
	return nil
}

func (f *FakeTask) LinkObservation(taskID, observationID int64) error {
	f.LinkCalls = append(f.LinkCalls, struct {
		TaskID        int64
		ObservationID int64
	}{TaskID: taskID, ObservationID: observationID})
	if f.Err != nil {
		return f.Err
	}
	return nil
}

func (f *FakeTask) ContextPack(taskID int64) (string, error) {
	if f.Err != nil {
		return "", f.Err
	}
	return f.ContextPackByID[taskID], nil
}

// FakeEvidence is an in-memory EvidenceReader for tests: set the fields you
// care about, leave the rest zero.
//
// Err short-circuits the call, which is how a test drives the error banner
// without a broken database. Unlike FakeTask, ListEvidence applies every
// filter itself instead of only recording it, because the Evidence tab's own
// "t" (filter by task) and "a" (attached only) keys have nothing else to
// assert against — a fake that just echoed the seeded rows back would let a
// broken filter pass.
type FakeEvidence struct {
	ItemsByProject map[string][]store.EvidenceListItem

	// Err is returned by every method when set.
	Err error

	// LastFilter records the filter the tab most recently asked for, the
	// same convention FakeTask.LastListFilter follows.
	LastFilter store.EvidenceListFilter
}

var _ EvidenceReader = (*FakeEvidence)(nil)
var _ EvidencePageReader = (*FakeEvidence)(nil)

// filteredEvidence applies filter's task, attached-jira, kind and category
// filters to a project's seeded items, without paginating — ListEvidence and
// ListEvidencePage both window this same set.
func (f *FakeEvidence) filteredEvidence(project string, filter store.EvidenceListFilter) []store.EvidenceListItem {
	var filtered []store.EvidenceListItem
	for _, it := range f.ItemsByProject[project] {
		if filter.TaskID != 0 && it.TaskID != filter.TaskID {
			continue
		}
		if filter.TaskSyncID != "" && it.TaskSyncID != filter.TaskSyncID {
			continue
		}
		if filter.AttachedJira != nil && it.AttachedJira != *filter.AttachedJira {
			continue
		}
		if filter.Kind != "" && it.Kind != filter.Kind {
			continue
		}
		if filter.Category != "" && it.Category != filter.Category {
			continue
		}
		filtered = append(filtered, it)
	}
	return filtered
}

func (f *FakeEvidence) ListEvidence(project string, filter store.EvidenceListFilter) ([]store.EvidenceListItem, error) {
	f.LastFilter = filter
	if f.Err != nil {
		return nil, f.Err
	}
	return f.filteredEvidence(project, filter), nil
}

func (f *FakeEvidence) ListEvidencePage(project string, filter store.EvidenceListFilter) (EvidencePage, error) {
	f.LastFilter = filter
	if f.Err != nil {
		return EvidencePage{}, f.Err
	}
	items := f.filteredEvidence(project, filter)
	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}
	var totalBytes int64
	for _, it := range items {
		if it.SizeBytes != nil {
			totalBytes += *it.SizeBytes
		}
	}
	return EvidencePage{Page: pageSlice(items, limit, filter.Offset), TotalBytes: totalBytes}, nil
}

func (f *FakeEvidence) Categories(project string) ([]CategoryCount, error) {
	if f.Err != nil {
		return nil, f.Err
	}
	counts := map[string]int{}
	var order []string
	for _, it := range f.ItemsByProject[project] {
		if _, seen := counts[it.Category]; !seen {
			order = append(order, it.Category)
		}
		counts[it.Category]++
	}
	sort.Strings(order)
	out := make([]CategoryCount, 0, len(order))
	for _, category := range order {
		out = append(out, CategoryCount{Category: category, Count: counts[category]})
	}
	return out, nil
}

// FakeRunbook is an in-memory RunbookReader for tests: set the fields you
// care about, leave the rest zero.
//
// Err short-circuits every method, which is how a test drives the error
// banner without a broken database. Unlike FakeTask's ListTasks, both
// ListRunbooks and SearchRunbooks apply their own filtering here — a fake
// that just echoed the seeded rows back would let a broken "a" toggle or a
// broken search pass — the same convention FakeEvidence follows.
type FakeRunbook struct {
	ItemsByProject map[string][]store.RunbookIndexRow

	// Err is returned by every method when set.
	Err error

	// LastListAll and LastSearch record what the tab most recently asked
	// for, the same convention FakeTask.LastListFilter follows.
	LastListAll bool
	LastSearch  struct {
		Project string
		All     bool
		Query   string
		Limit   int
	}
}

var _ RunbookReader = (*FakeRunbook)(nil)
var _ RunbookPageReader = (*FakeRunbook)(nil)

// allRunbooks flattens every project's seeded rows, in map iteration order —
// callers needing a stable order sort the result themselves, same as a real
// SQL query would require an ORDER BY of its own.
func (f *FakeRunbook) allRunbooks() []store.RunbookIndexRow {
	var all []store.RunbookIndexRow
	for _, items := range f.ItemsByProject {
		all = append(all, items...)
	}
	return all
}

func (f *FakeRunbook) ListRunbooks(project string, all bool) ([]store.RunbookIndexRow, error) {
	f.LastListAll = all
	if f.Err != nil {
		return nil, f.Err
	}
	if all {
		return f.allRunbooks(), nil
	}
	return f.ItemsByProject[project], nil
}

func (f *FakeRunbook) ListRunbooksPage(project string, all bool, filter RunbookFilter) (Page[store.RunbookIndexRow], error) {
	f.LastListAll = all
	if f.Err != nil {
		return Page[store.RunbookIndexRow]{}, f.Err
	}
	pool := f.ItemsByProject[project]
	if all {
		pool = f.allRunbooks()
	}
	var filtered []store.RunbookIndexRow
	for _, r := range pool {
		if filter.Stale != nil && r.Stale != *filter.Stale {
			continue
		}
		if filter.Category != "" && r.Category != filter.Category {
			continue
		}
		if filter.Pattern != "" && (r.Pattern == nil || *r.Pattern != filter.Pattern) {
			continue
		}
		if filter.Status != "" && r.Status != filter.Status {
			continue
		}
		filtered = append(filtered, r)
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}
	return pageSlice(filtered, limit, filter.Offset), nil
}

func (f *FakeRunbook) SearchRunbooks(project string, all bool, query string, limit int) ([]store.RunbookIndexRow, error) {
	f.LastSearch.Project = project
	f.LastSearch.All = all
	f.LastSearch.Query = query
	f.LastSearch.Limit = limit
	if f.Err != nil {
		return nil, f.Err
	}
	pool := f.ItemsByProject[project]
	if all {
		pool = f.allRunbooks()
	}
	var matched []store.RunbookIndexRow
	for _, r := range pool {
		if matchesRunbookQuery(r, query) {
			matched = append(matched, r)
		}
	}
	if limit > 0 && len(matched) > limit {
		matched = matched[:limit]
	}
	return matched, nil
}

// matchesRunbookQuery is FakeRunbook's stand-in for runbook_index_fts: a
// case-insensitive substring match against the title and every symptom line,
// good enough to drive an Update test's assertion on which rows a search
// keeps without pulling FTS5 into a fake.
func matchesRunbookQuery(r store.RunbookIndexRow, query string) bool {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return true
	}
	if strings.Contains(strings.ToLower(r.Title), q) {
		return true
	}
	for _, s := range r.Symptoms {
		if strings.Contains(strings.ToLower(s), q) {
			return true
		}
	}
	return false
}

// FakeProjectTree is an in-memory ProjectTreeReader for tests: set the
// fields you care about, leave the rest zero.
//
// Unlike ProjectTree()'s sqlite adapter, which builds the forest and the
// ancestor chain from flat store rows, the fake takes them pre-built: what
// is under test elsewhere (the tabs that consume ProjectTreeReader) is
// whether a view renders a tree correctly, not whether this fake can also
// reimplement buildProjectForest.
type FakeProjectTree struct {
	Tree              []ProjectNode
	NodeBySlug        map[string]ProjectNode
	AncestorsBySlug   map[string][]ProjectNode
	DescendantsBySlug map[string][]string
	AliasResolution   map[string]string

	// Err is returned by every method when set.
	Err error
}

var _ ProjectTreeReader = (*FakeProjectTree)(nil)

func (f *FakeProjectTree) ProjectTree() ([]ProjectNode, error) {
	if f.Err != nil {
		return nil, f.Err
	}
	return f.Tree, nil
}

func (f *FakeProjectTree) ProjectNode(slug string) (ProjectNode, error) {
	if f.Err != nil {
		return ProjectNode{}, f.Err
	}
	n, ok := f.NodeBySlug[slug]
	if !ok {
		return ProjectNode{}, errors.New("no such project")
	}
	return n, nil
}

func (f *FakeProjectTree) Ancestors(slug string) ([]ProjectNode, error) {
	if f.Err != nil {
		return nil, f.Err
	}
	return f.AncestorsBySlug[slug], nil
}

func (f *FakeProjectTree) Descendants(root string) ([]string, error) {
	if f.Err != nil {
		return nil, f.Err
	}
	return f.DescendantsBySlug[root], nil
}

func (f *FakeProjectTree) ResolveAlias(raw string) (string, error) {
	if f.Err != nil {
		return "", f.Err
	}
	slug, ok := f.AliasResolution[raw]
	if !ok {
		return "", ErrProjectUnresolved
	}
	return slug, nil
}

// FakeBenchmark is an in-memory BenchmarkReader for tests: set the fields
// you care about, leave the rest zero.
type FakeBenchmark struct {
	ByProject    map[string][]Benchmark
	ByTaskSyncID map[string][]Benchmark

	// Err is returned by every method when set.
	Err error

	// LastFilter records the filter ListBenchmarks most recently asked for.
	LastFilter BenchmarkFilter
}

var _ BenchmarkReader = (*FakeBenchmark)(nil)

func (f *FakeBenchmark) ListBenchmarks(project string, filter BenchmarkFilter) (Page[Benchmark], error) {
	f.LastFilter = filter
	if f.Err != nil {
		return Page[Benchmark]{}, f.Err
	}
	items := f.ByProject[project]
	if filter.Metric != "" {
		var matched []Benchmark
		for _, b := range items {
			if b.Metric == filter.Metric {
				matched = append(matched, b)
			}
		}
		items = matched
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}
	return pageSlice(items, limit, filter.Offset), nil
}

func (f *FakeBenchmark) TaskBenchmarks(taskSyncID string) ([]Benchmark, error) {
	if f.Err != nil {
		return nil, f.Err
	}
	return f.ByTaskSyncID[taskSyncID], nil
}

func (f *FakeBenchmark) MetricHistory(project, metric string, limit int) ([]Benchmark, error) {
	if f.Err != nil {
		return nil, f.Err
	}
	var out []Benchmark
	for _, b := range f.ByProject[project] {
		if b.Metric == metric {
			out = append(out, b)
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// FakeGraph is an in-memory GraphReader and GraphSyncer for tests: set the
// fields you care about, leave the rest zero.
type FakeGraph struct {
	StateByProject map[string]GraphState
	// RefsByProject holds each project's graph-linked observations, newest
	// first — the order the sqlite reader returns them in, so a test that
	// asserts on the first row is asserting on the same row the real one
	// would give it.
	RefsByProject map[string][]ObservationRef
	// SyncResult, when a project has an entry, is what SyncGraph returns for
	// it instead of StateByProject's own entry — the state "after" a sync,
	// distinct from the state a plain GraphState read would see before one.
	SyncResult map[string]GraphState

	// Err is returned by every method when set.
	Err error

	// SyncCalls records every project SyncGraph was asked to sync, so a test
	// can assert on the call and not only on the state it returned.
	SyncCalls []string
}

var _ GraphReader = (*FakeGraph)(nil)
var _ GraphSyncer = (*FakeGraph)(nil)

func (f *FakeGraph) GraphState(project string) (GraphState, error) {
	if f.Err != nil {
		return GraphState{}, f.Err
	}
	return f.StateByProject[project], nil
}

func (f *FakeGraph) ObservationRefs(project string, limit, offset int) (Page[ObservationRef], error) {
	if f.Err != nil {
		return Page[ObservationRef]{}, f.Err
	}
	return pageSlice(f.RefsByProject[project], limit, offset), nil
}

func (f *FakeGraph) SyncGraph(project string) (GraphState, error) {
	f.SyncCalls = append(f.SyncCalls, project)
	if f.Err != nil {
		return GraphState{}, f.Err
	}
	if state, ok := f.SyncResult[project]; ok {
		return state, nil
	}
	return f.StateByProject[project], nil
}

// FakeTheme is an in-memory ThemeReader and ThemeWriter for tests: set the
// fields you care about, leave the rest zero.
type FakeTheme struct {
	Themes []ThemeRecord
	ByName map[string]ThemeRecord

	// Err is returned by every method when set.
	Err error

	// Saved, Deleted and ResetCalls record every write, so a test can assert
	// on the call and not only on a subsequent read.
	Saved      []store.ThemeRecord
	Deleted    []string
	ResetCalls []struct {
		Name    string
		Palette json.RawMessage
	}
}

var _ ThemeReader = (*FakeTheme)(nil)
var _ ThemeWriter = (*FakeTheme)(nil)

func (f *FakeTheme) ListThemes() ([]ThemeRecord, error) {
	if f.Err != nil {
		return nil, f.Err
	}
	return f.Themes, nil
}

func (f *FakeTheme) Theme(name string) (ThemeRecord, error) {
	if f.Err != nil {
		return ThemeRecord{}, f.Err
	}
	rec, ok := f.ByName[name]
	if !ok {
		return ThemeRecord{}, store.ErrThemeNotFound
	}
	return rec, nil
}

func (f *FakeTheme) SaveTheme(rec store.ThemeRecord) error {
	f.Saved = append(f.Saved, rec)
	if f.Err != nil {
		return f.Err
	}
	return nil
}

func (f *FakeTheme) DeleteTheme(name string) error {
	f.Deleted = append(f.Deleted, name)
	if f.Err != nil {
		return f.Err
	}
	return nil
}

func (f *FakeTheme) ResetTheme(name string, palette json.RawMessage) error {
	f.ResetCalls = append(f.ResetCalls, struct {
		Name    string
		Palette json.RawMessage
	}{name, palette})
	if f.Err != nil {
		return f.Err
	}
	return nil
}

// FakeSettings is an in-memory SettingsReader and SettingsWriter for tests:
// set Values directly, or let SetSetting populate it.
type FakeSettings struct {
	Values map[string]string

	// Err is returned by every method when set.
	Err error

	// SetCalls records every write, so a test can assert on the call and not
	// only on a subsequent read.
	SetCalls []struct {
		Key, Value string
	}
}

var _ SettingsReader = (*FakeSettings)(nil)
var _ SettingsWriter = (*FakeSettings)(nil)

func (f *FakeSettings) Setting(key string) (string, bool, error) {
	if f.Err != nil {
		return "", false, f.Err
	}
	v, ok := f.Values[key]
	return v, ok, nil
}

func (f *FakeSettings) Settings(prefix string) (map[string]string, error) {
	if f.Err != nil {
		return nil, f.Err
	}
	out := map[string]string{}
	for k, v := range f.Values {
		if prefix == "" || strings.HasPrefix(k, prefix) {
			out[k] = v
		}
	}
	return out, nil
}

func (f *FakeSettings) SetSetting(key, value string) error {
	f.SetCalls = append(f.SetCalls, struct{ Key, Value string }{key, value})
	if f.Err != nil {
		return f.Err
	}
	if f.Values == nil {
		f.Values = map[string]string{}
	}
	f.Values[key] = value
	return nil
}

// FakeSearch is an in-memory GlobalSearcher for tests: set Hits, leave the
// rest zero.
type FakeSearch struct {
	Hits []SearchHit

	// Err is returned by every method when set.
	Err error

	// LastQuery records the query SearchWorkspace most recently asked for.
	LastQuery SearchQuery
}

var _ GlobalSearcher = (*FakeSearch)(nil)

// SearchWorkspace filters Hits by q.Kinds (every kind when empty) — a scope
// or text-match simulation would just duplicate store.SearchWorkspace's own
// FTS5 logic in Go, which is exactly what a fake is not for; a test that
// needs scope-aware behavior seeds Hits already scoped.
func (f *FakeSearch) SearchWorkspace(q SearchQuery) ([]SearchHit, error) {
	f.LastQuery = q
	if f.Err != nil {
		return nil, f.Err
	}
	if len(q.Kinds) == 0 {
		return f.Hits, nil
	}
	kinds := map[string]bool{}
	for _, k := range q.Kinds {
		kinds[k] = true
	}
	var out []SearchHit
	for _, h := range f.Hits {
		if kinds[h.Kind] {
			out = append(out, h)
		}
	}
	return out, nil
}
