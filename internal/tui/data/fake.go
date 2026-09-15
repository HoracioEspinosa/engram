package data

import (
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"sync"

	"github.com/HoracioEspinosa/engram/internal/store"
)

// spy guards the records a fake keeps of what it was asked for. Every reader
// method runs inside a tea.Cmd, on its own goroutine, so the same fake is
// written from Init's command and from Refresh's at the same time and read
// from the test goroutine in between. Seeded data (the ItemsBy* maps and
// their siblings) stays unguarded on purpose: a test fills it before the
// program starts and never writes it again.
type spy struct {
	mu sync.Mutex
}

// failure reads a fake's Err field under the mutex. Err stays exported so a
// fake can be built with one in a struct literal; SetErr is how a test raises
// it once the program is running.
func (s *spy) failure(err *error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return *err
}

func (s *spy) setFailure(dst *error, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	*dst = err
}

// TaskStateCall is one recorded UpdateState call.
type TaskStateCall struct {
	ID    int64
	State string
}

// TaskLinkCall is one recorded LinkObservation call.
type TaskLinkCall struct {
	TaskID        int64
	ObservationID int64
}

// RunbookSearchCall is one recorded SearchRunbooks call.
type RunbookSearchCall struct {
	Project string
	All     bool
	Query   string
	Limit   int
}

// ThemeResetCall is one recorded ResetTheme call.
type ThemeResetCall struct {
	Name    string
	Palette json.RawMessage
}

// SettingCall is one recorded SetSetting call.
type SettingCall struct {
	Key   string
	Value string
}

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
	spy

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

	// queries and deletedSessions record what the tab asked for, so a test can
	// assert on the call and not only on the rendered result. Read them back
	// through Queries and DeletedSessions.
	queries         []string
	deletedSessions []string

	// lastScope records the ProjectScope the tab most recently asked for, the
	// same convention FakeTask's lastListFilter follows.
	lastScope ProjectScope
}

var _ MemoryReader = (*FakeMemory)(nil)
var _ ScopedMemoryReader = (*FakeMemory)(nil)

// SetErr fails every method from now on, safely while a command is in flight.
func (f *FakeMemory) SetErr(err error) { f.setFailure(&f.Err, err) }

// Queries returns the queries Search and SearchScoped were asked for, oldest
// first.
func (f *FakeMemory) Queries() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.queries...)
}

// DeletedSessions returns the session IDs DeleteSession accepted.
func (f *FakeMemory) DeletedSessions() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.deletedSessions...)
}

// LastScope returns the scope the most recent *Scoped call carried.
func (f *FakeMemory) LastScope() ProjectScope {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastScope
}

func (f *FakeMemory) Stats() (*store.Stats, error) {
	if err := f.failure(&f.Err); err != nil {
		return nil, err
	}
	return f.StatsResult, nil
}

func (f *FakeMemory) Search(query string, _ store.SearchOptions) ([]store.SearchResult, error) {
	f.mu.Lock()
	f.queries = append(f.queries, query)
	err := f.Err
	f.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return f.SearchResults, nil
}

func (f *FakeMemory) RecentObservations(limit int) ([]store.Observation, error) {
	if err := f.failure(&f.Err); err != nil {
		return nil, err
	}
	return capObservations(f.Observations, limit), nil
}

func (f *FakeMemory) Observation(id int64) (*store.Observation, error) {
	if err := f.failure(&f.Err); err != nil {
		return nil, err
	}
	return f.ObservationByID[id], nil
}

func (f *FakeMemory) Timeline(_ int64, _, _ int) (*store.TimelineResult, error) {
	if err := f.failure(&f.Err); err != nil {
		return nil, err
	}
	return f.TimelineResult, nil
}

func (f *FakeMemory) RecentSessions(limit int) ([]store.SessionSummary, error) {
	if err := f.failure(&f.Err); err != nil {
		return nil, err
	}
	if limit > 0 && len(f.Sessions) > limit {
		return f.Sessions[:limit], nil
	}
	return f.Sessions, nil
}

func (f *FakeMemory) SessionObservations(sessionID string, limit int) ([]store.Observation, error) {
	if err := f.failure(&f.Err); err != nil {
		return nil, err
	}
	return capObservations(f.SessionObs[sessionID], limit), nil
}

func (f *FakeMemory) DeleteSession(sessionID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return f.Err
	}
	f.deletedSessions = append(f.deletedSessions, sessionID)
	return nil
}

func (f *FakeMemory) SearchScoped(query string, scope ProjectScope, limit, offset int) (Page[store.SearchResult], error) {
	f.mu.Lock()
	f.queries = append(f.queries, query)
	f.lastScope = scope
	err := f.Err
	f.mu.Unlock()
	if err != nil {
		return Page[store.SearchResult]{}, err
	}
	matched := filterByScope(f.SearchResults, scope, func(r store.SearchResult) string { return derefOr(r.Project, "") }, f.SubtreeSlugs)
	return pageSlice(matched, limit, offset), nil
}

func (f *FakeMemory) RecentObservationsScoped(scope ProjectScope, limit, offset int) (Page[store.Observation], error) {
	f.mu.Lock()
	f.lastScope = scope
	err := f.Err
	f.mu.Unlock()
	if err != nil {
		return Page[store.Observation]{}, err
	}
	matched := filterByScope(f.Observations, scope, func(o store.Observation) string { return derefOr(o.Project, "") }, f.SubtreeSlugs)
	return pageSlice(matched, limit, offset), nil
}

func (f *FakeMemory) RecentSessionsScoped(scope ProjectScope, limit, offset int) (Page[store.SessionSummary], error) {
	f.mu.Lock()
	f.lastScope = scope
	err := f.Err
	f.mu.Unlock()
	if err != nil {
		return Page[store.SessionSummary]{}, err
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
	spy

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

// SetErr fails every method from now on, safely while a command is in flight.
func (f *FakeProject) SetErr(err error) { f.setFailure(&f.Err, err) }

func (f *FakeProject) ListCards() ([]store.ProjectCardListItem, error) {
	if err := f.failure(&f.Err); err != nil {
		return nil, err
	}
	return f.Cards, nil
}

func (f *FakeProject) Card(slug string) (store.ProjectCard, error) {
	if err := f.failure(&f.Err); err != nil {
		return store.ProjectCard{}, err
	}
	card, ok := f.CardBySlug[slug]
	if !ok {
		return store.ProjectCard{}, errors.New("no such project")
	}
	return card, nil
}

func (f *FakeProject) Health(slug string) (ProjectHealth, error) {
	if err := f.failure(&f.Err); err != nil {
		return ProjectHealth{}, err
	}
	// A project with no counters is not a missing project: it is a project
	// whose numbers are all zero, the same way one with no tasks returns an
	// empty slice rather than an error. Only Card reports a slug that does
	// not exist, because the card is what identifies the project at all.
	return f.HealthBySlug[slug], nil
}

func (f *FakeProject) RecentTasks(slug string, limit int) ([]store.TaskListItem, error) {
	if err := f.failure(&f.Err); err != nil {
		return nil, err
	}
	tasks := f.TasksBySlug[slug]
	if limit > 0 && len(tasks) > limit {
		return tasks[:limit], nil
	}
	return tasks, nil
}

func (f *FakeProject) StaleRunbooks(slug string, limit int) ([]store.RunbookIndexRow, error) {
	if err := f.failure(&f.Err); err != nil {
		return nil, err
	}
	runbooks := f.StaleRunbooksSlug[slug]
	if limit > 0 && len(runbooks) > limit {
		return runbooks[:limit], nil
	}
	return runbooks, nil
}

func (f *FakeProject) LatestEvidence(slug string, limit int) ([]store.EvidenceListItem, error) {
	if err := f.failure(&f.Err); err != nil {
		return nil, err
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
// banner without a broken database. UpdateStateCalls and LinkCalls report
// what the tab asked to write, so a test can assert on the call and not only
// on the screen it produced.
type FakeTask struct {
	spy

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

	// lastListFilter records the filter the tab most recently asked for;
	// updateStateCalls and linkCalls record every write it attempted.
	lastListFilter   store.TaskListFilter
	updateStateCalls []TaskStateCall
	linkCalls        []TaskLinkCall
}

var _ TaskReader = (*FakeTask)(nil)
var _ TaskPageReader = (*FakeTask)(nil)

// SetErr fails every method from now on, safely while a command is in flight.
func (f *FakeTask) SetErr(err error) { f.setFailure(&f.Err, err) }

// LastListFilter returns the filter the most recent list call carried.
func (f *FakeTask) LastListFilter() store.TaskListFilter {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastListFilter
}

// UpdateStateCalls returns every state write attempted, oldest first,
// including the ones Err then failed.
func (f *FakeTask) UpdateStateCalls() []TaskStateCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]TaskStateCall(nil), f.updateStateCalls...)
}

// LinkCalls returns every link write attempted, oldest first, including the
// ones Err then failed.
func (f *FakeTask) LinkCalls() []TaskLinkCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]TaskLinkCall(nil), f.linkCalls...)
}

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
	f.mu.Lock()
	f.lastListFilter = filter
	err := f.Err
	f.mu.Unlock()
	if err != nil {
		return nil, err
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
	f.mu.Lock()
	f.lastListFilter = filter
	err := f.Err
	f.mu.Unlock()
	if err != nil {
		return Page[store.TaskListItem]{}, err
	}
	items := f.filteredTasks(taskProject, filter)
	limit := filter.Limit
	if limit <= 0 {
		limit = 20
	}
	return pageSlice(items, limit, filter.Offset), nil
}

func (f *FakeTask) Task(id int64) (TaskDetail, error) {
	if err := f.failure(&f.Err); err != nil {
		return TaskDetail{}, err
	}
	d, ok := f.DetailByID[id]
	if !ok {
		return TaskDetail{}, errors.New("no such task")
	}
	return d, nil
}

func (f *FakeTask) TaskBySlug(taskProject, slug string) (TaskDetail, error) {
	if err := f.failure(&f.Err); err != nil {
		return TaskDetail{}, err
	}
	d, ok := f.DetailBySlug[taskProject+"/"+slug]
	if !ok {
		return TaskDetail{}, errors.New("no such task")
	}
	return d, nil
}

func (f *FakeTask) VaultDir(id int64) (string, bool, error) {
	if err := f.failure(&f.Err); err != nil {
		return "", false, err
	}
	dir, ok := f.VaultDirByID[id]
	if !ok || dir == "" {
		return "", false, nil
	}
	return dir, true, nil
}

func (f *FakeTask) UpdateState(id int64, state string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.updateStateCalls = append(f.updateStateCalls, TaskStateCall{ID: id, State: state})
	return f.Err
}

func (f *FakeTask) LinkObservation(taskID, observationID int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.linkCalls = append(f.linkCalls, TaskLinkCall{TaskID: taskID, ObservationID: observationID})
	return f.Err
}

func (f *FakeTask) ContextPack(taskID int64) (string, error) {
	if err := f.failure(&f.Err); err != nil {
		return "", err
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
	spy

	ItemsByProject map[string][]store.EvidenceListItem

	// Err is returned by every method when set.
	Err error

	// lastFilter records the filter the tab most recently asked for, the
	// same convention FakeTask's lastListFilter follows.
	lastFilter store.EvidenceListFilter
}

var _ EvidenceReader = (*FakeEvidence)(nil)
var _ EvidencePageReader = (*FakeEvidence)(nil)

// SetErr fails every method from now on, safely while a command is in flight.
func (f *FakeEvidence) SetErr(err error) { f.setFailure(&f.Err, err) }

// LastFilter returns the filter the most recent list call carried.
func (f *FakeEvidence) LastFilter() store.EvidenceListFilter {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastFilter
}

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
	f.mu.Lock()
	f.lastFilter = filter
	err := f.Err
	f.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return f.filteredEvidence(project, filter), nil
}

func (f *FakeEvidence) ListEvidencePage(project string, filter store.EvidenceListFilter) (EvidencePage, error) {
	f.mu.Lock()
	f.lastFilter = filter
	err := f.Err
	f.mu.Unlock()
	if err != nil {
		return EvidencePage{}, err
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
	if err := f.failure(&f.Err); err != nil {
		return nil, err
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
	spy

	ItemsByProject map[string][]store.RunbookIndexRow

	// Err is returned by every method when set.
	Err error

	// lastListAll and lastSearch record what the tab most recently asked
	// for, the same convention FakeTask's lastListFilter follows.
	lastListAll bool
	lastSearch  RunbookSearchCall
}

var _ RunbookReader = (*FakeRunbook)(nil)
var _ RunbookPageReader = (*FakeRunbook)(nil)

// SetErr fails every method from now on, safely while a command is in flight.
func (f *FakeRunbook) SetErr(err error) { f.setFailure(&f.Err, err) }

// LastListAll reports whether the most recent list call asked for every
// project's runbooks.
func (f *FakeRunbook) LastListAll() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastListAll
}

// LastSearch returns the arguments the most recent SearchRunbooks carried.
func (f *FakeRunbook) LastSearch() RunbookSearchCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastSearch
}

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
	f.mu.Lock()
	f.lastListAll = all
	err := f.Err
	f.mu.Unlock()
	if err != nil {
		return nil, err
	}
	if all {
		return f.allRunbooks(), nil
	}
	return f.ItemsByProject[project], nil
}

func (f *FakeRunbook) ListRunbooksPage(project string, all bool, filter RunbookFilter) (Page[store.RunbookIndexRow], error) {
	f.mu.Lock()
	f.lastListAll = all
	err := f.Err
	f.mu.Unlock()
	if err != nil {
		return Page[store.RunbookIndexRow]{}, err
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
	f.mu.Lock()
	f.lastSearch = RunbookSearchCall{Project: project, All: all, Query: query, Limit: limit}
	err := f.Err
	f.mu.Unlock()
	if err != nil {
		return nil, err
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
	spy

	Tree              []ProjectNode
	NodeBySlug        map[string]ProjectNode
	AncestorsBySlug   map[string][]ProjectNode
	DescendantsBySlug map[string][]string
	AliasResolution   map[string]string

	// Err is returned by every method when set.
	Err error
}

var _ ProjectTreeReader = (*FakeProjectTree)(nil)

// SetErr fails every method from now on, safely while a command is in flight.
func (f *FakeProjectTree) SetErr(err error) { f.setFailure(&f.Err, err) }

func (f *FakeProjectTree) ProjectTree() ([]ProjectNode, error) {
	if err := f.failure(&f.Err); err != nil {
		return nil, err
	}
	return f.Tree, nil
}

func (f *FakeProjectTree) ProjectNode(slug string) (ProjectNode, error) {
	if err := f.failure(&f.Err); err != nil {
		return ProjectNode{}, err
	}
	n, ok := f.NodeBySlug[slug]
	if !ok {
		return ProjectNode{}, errors.New("no such project")
	}
	return n, nil
}

func (f *FakeProjectTree) Ancestors(slug string) ([]ProjectNode, error) {
	if err := f.failure(&f.Err); err != nil {
		return nil, err
	}
	return f.AncestorsBySlug[slug], nil
}

func (f *FakeProjectTree) Descendants(root string) ([]string, error) {
	if err := f.failure(&f.Err); err != nil {
		return nil, err
	}
	return f.DescendantsBySlug[root], nil
}

func (f *FakeProjectTree) ResolveAlias(raw string) (string, error) {
	if err := f.failure(&f.Err); err != nil {
		return "", err
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
	spy

	ByProject    map[string][]Benchmark
	ByTaskSyncID map[string][]Benchmark

	// Err is returned by every method when set.
	Err error

	// lastFilter records the filter ListBenchmarks most recently asked for.
	lastFilter BenchmarkFilter
}

var _ BenchmarkReader = (*FakeBenchmark)(nil)

// SetErr fails every method from now on, safely while a command is in flight.
func (f *FakeBenchmark) SetErr(err error) { f.setFailure(&f.Err, err) }

// LastFilter returns the filter the most recent ListBenchmarks carried.
func (f *FakeBenchmark) LastFilter() BenchmarkFilter {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastFilter
}

func (f *FakeBenchmark) ListBenchmarks(project string, filter BenchmarkFilter) (Page[Benchmark], error) {
	f.mu.Lock()
	f.lastFilter = filter
	err := f.Err
	f.mu.Unlock()
	if err != nil {
		return Page[Benchmark]{}, err
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
	if err := f.failure(&f.Err); err != nil {
		return nil, err
	}
	return f.ByTaskSyncID[taskSyncID], nil
}

func (f *FakeBenchmark) MetricHistory(project, metric string, limit int) ([]Benchmark, error) {
	if err := f.failure(&f.Err); err != nil {
		return nil, err
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
	spy

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

	// syncCalls records every project SyncGraph was asked to sync, so a test
	// can assert on the call and not only on the state it returned.
	syncCalls []string
}

var _ GraphReader = (*FakeGraph)(nil)
var _ GraphSyncer = (*FakeGraph)(nil)

// SetErr fails every method from now on, safely while a command is in flight.
func (f *FakeGraph) SetErr(err error) { f.setFailure(&f.Err, err) }

// SyncCalls returns every project SyncGraph was asked for, oldest first.
func (f *FakeGraph) SyncCalls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.syncCalls...)
}

func (f *FakeGraph) GraphState(project string) (GraphState, error) {
	if err := f.failure(&f.Err); err != nil {
		return GraphState{}, err
	}
	return f.StateByProject[project], nil
}

func (f *FakeGraph) ObservationRefs(project string, limit, offset int) (Page[ObservationRef], error) {
	if err := f.failure(&f.Err); err != nil {
		return Page[ObservationRef]{}, err
	}
	return pageSlice(f.RefsByProject[project], limit, offset), nil
}

func (f *FakeGraph) SyncGraph(project string) (GraphState, error) {
	f.mu.Lock()
	f.syncCalls = append(f.syncCalls, project)
	err := f.Err
	f.mu.Unlock()
	if err != nil {
		return GraphState{}, err
	}
	if state, ok := f.SyncResult[project]; ok {
		return state, nil
	}
	return f.StateByProject[project], nil
}

// FakeTheme is an in-memory ThemeReader and ThemeWriter for tests: set the
// fields you care about, leave the rest zero.
type FakeTheme struct {
	spy

	Themes []ThemeRecord
	ByName map[string]ThemeRecord

	// Err is returned by every method when set.
	Err error

	// saved, deleted and resetCalls record every write, so a test can assert
	// on the call and not only on a subsequent read.
	saved      []store.ThemeRecord
	deleted    []string
	resetCalls []ThemeResetCall
}

var _ ThemeReader = (*FakeTheme)(nil)
var _ ThemeWriter = (*FakeTheme)(nil)

// SetErr fails every method from now on, safely while a command is in flight.
func (f *FakeTheme) SetErr(err error) { f.setFailure(&f.Err, err) }

// Saved returns every record SaveTheme was handed, oldest first.
func (f *FakeTheme) Saved() []store.ThemeRecord {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]store.ThemeRecord(nil), f.saved...)
}

// Deleted returns every name DeleteTheme was handed, oldest first.
func (f *FakeTheme) Deleted() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.deleted...)
}

// ResetCalls returns every ResetTheme call, oldest first.
func (f *FakeTheme) ResetCalls() []ThemeResetCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]ThemeResetCall(nil), f.resetCalls...)
}

func (f *FakeTheme) ListThemes() ([]ThemeRecord, error) {
	if err := f.failure(&f.Err); err != nil {
		return nil, err
	}
	return f.Themes, nil
}

func (f *FakeTheme) Theme(name string) (ThemeRecord, error) {
	if err := f.failure(&f.Err); err != nil {
		return ThemeRecord{}, err
	}
	rec, ok := f.ByName[name]
	if !ok {
		return ThemeRecord{}, store.ErrThemeNotFound
	}
	return rec, nil
}

func (f *FakeTheme) SaveTheme(rec store.ThemeRecord) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.saved = append(f.saved, rec)
	return f.Err
}

func (f *FakeTheme) DeleteTheme(name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleted = append(f.deleted, name)
	return f.Err
}

func (f *FakeTheme) ResetTheme(name string, palette json.RawMessage) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.resetCalls = append(f.resetCalls, ThemeResetCall{Name: name, Palette: palette})
	return f.Err
}

// FakeSettings is an in-memory SettingsReader and SettingsWriter for tests:
// seed Values in the struct literal, or let SetSetting populate it. Values is
// the one seeded field a method also writes, so read it back through Value
// rather than by indexing the map.
type FakeSettings struct {
	spy

	Values map[string]string

	// Err is returned by every method when set.
	Err error

	// setCalls records every write, so a test can assert on the call and not
	// only on a subsequent read.
	setCalls []SettingCall
}

var _ SettingsReader = (*FakeSettings)(nil)
var _ SettingsWriter = (*FakeSettings)(nil)

// SetErr fails every method from now on, safely while a command is in flight.
func (f *FakeSettings) SetErr(err error) { f.setFailure(&f.Err, err) }

// SetCalls returns every SetSetting call, oldest first.
func (f *FakeSettings) SetCalls() []SettingCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]SettingCall(nil), f.setCalls...)
}

// Value returns the stored value for key, empty when unset.
func (f *FakeSettings) Value(key string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.Values[key]
}

func (f *FakeSettings) Setting(key string) (string, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return "", false, f.Err
	}
	v, ok := f.Values[key]
	return v, ok, nil
}

func (f *FakeSettings) Settings(prefix string) (map[string]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
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
	f.mu.Lock()
	defer f.mu.Unlock()
	f.setCalls = append(f.setCalls, SettingCall{Key: key, Value: value})
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
	spy

	Hits []SearchHit

	// Err is returned by every method when set.
	Err error

	// lastQuery records the query SearchWorkspace most recently asked for.
	lastQuery SearchQuery
}

var _ GlobalSearcher = (*FakeSearch)(nil)

// SetErr fails every method from now on, safely while a command is in flight.
func (f *FakeSearch) SetErr(err error) { f.setFailure(&f.Err, err) }

// LastQuery returns the query the most recent SearchWorkspace carried.
func (f *FakeSearch) LastQuery() SearchQuery {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastQuery
}

// SearchWorkspace filters Hits by q.Kinds (every kind when empty) — a scope
// or text-match simulation would just duplicate store.SearchWorkspace's own
// FTS5 logic in Go, which is exactly what a fake is not for; a test that
// needs scope-aware behavior seeds Hits already scoped.
func (f *FakeSearch) SearchWorkspace(q SearchQuery) ([]SearchHit, error) {
	f.mu.Lock()
	f.lastQuery = q
	err := f.Err
	f.mu.Unlock()
	if err != nil {
		return nil, err
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
