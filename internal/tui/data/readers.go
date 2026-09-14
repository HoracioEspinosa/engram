// Package data is the only part of the TUI that talks to the engram store.
//
// Tabs depend on the interfaces declared here, never on *store.Store, so a tab
// can be exercised against an in-memory fake and the storage layer can move
// without a view noticing.
package data

import (
	"encoding/json"
	"errors"

	"github.com/HoracioEspinosa/engram/internal/project"
	"github.com/HoracioEspinosa/engram/internal/store"
)

// ErrStoreUnavailable is returned by an adapter built without a store, which
// happens when the TUI is constructed for a render-only test.
var ErrStoreUnavailable = errors.New("store is unavailable")

// ErrNotImplemented is returned by a reader method whose backing store query
// does not exist yet. It is distinct from ErrStoreUnavailable (no store at
// all) and from a real empty result (a query that ran and found nothing): a
// caller that gets this back knows the gap is in this package, not in the
// data.
var ErrNotImplemented = errors.New("not implemented: no store query backs this method yet")

// ErrProjectUnresolved is returned when a raw name does not resolve to any
// known project — no card, no observations, no alias and no folded match.
var ErrProjectUnresolved = errors.New("project name did not resolve to any project")

// vaultRootEnv names the checkout TaskReader.VaultDir resolves a task's
// vault_path against. It is the same variable
// internal/diagnostic.KnowledgeRefVaultDirEnv reads; the two packages do not
// import each other over one shared string.
const vaultRootEnv = "ENGRAM_VAULT_ROOT"

// Page is a page of results with the size of the whole set it came from, the
// same shape store.Page[T] carries plus the two booleans a pager widget
// renders from. It is its own type rather than an alias of store.Page[T]
// because a method can only be added to a type in the package that defines
// it; pageFrom adapts a store.Page[T] into one of these.
type Page[T any] struct {
	Items  []T
	Total  int
	Offset int
	Limit  int
}

// HasPrev reports whether a page before this one exists.
func (p Page[T]) HasPrev() bool { return p.Offset > 0 }

// HasNext reports whether a page after this one exists.
func (p Page[T]) HasNext() bool { return p.Offset+len(p.Items) < p.Total }

// pageFrom adapts a store.Page[T] to this package's own Page[T].
func pageFrom[T any](p store.Page[T]) Page[T] {
	return Page[T]{Items: p.Items, Total: p.Total, Offset: p.Offset, Limit: p.Limit}
}

// EvidencePage is a page of evidence with the total size, in bytes, of every
// item the filter matched — not only the page (rfc-tui.md §9.2's Evidence
// tab footer, "12 items · 4.3 MiB").
type EvidencePage struct {
	Page[store.EvidenceListItem]
	TotalBytes int64
}

// CategoryCount is one row of EvidenceReader.Categories: a vault folder and
// how many evidence rows a project has under it, for the Evidence tab's "t"
// filter and its counts.
type CategoryCount struct {
	Category string
	Count    int
}

// ProjectHealth carries the counters and sync status for a project card.
type ProjectHealth struct {
	store.ProjectCardCounts
	Sync store.ProjectSyncSummary `json:"sync"`
}

// ProjectReader is the surface that the Selector and Dashboard screens need:
// the project list, individual cards, health counters, and the four blocks
// of data that make up the Dashboard (recent tasks, stale runbooks, latest
// evidence).
//
// A project's recent benchmarks — Home's fifth block — are not a fifth
// method here: BenchmarkReader.ListBenchmarks(slug, BenchmarkFilter{Limit:
// n}) already answers the same question, and ProjectReader's method set is
// load-bearing for internal/tui/app and internal/tui/tabs, which implement
// it (and hand-roll test doubles against it) today. Growing it would ask
// every one of those, in a package this change does not touch, to grow a
// method too.
type ProjectReader interface {
	// ListCards returns every project card for the selector's list.
	ListCards() ([]store.ProjectCardListItem, error)
	// Card returns one project card by slug.
	Card(slug string) (store.ProjectCard, error)
	// Health returns the health counters and sync status for one project.
	Health(slug string) (ProjectHealth, error)
	// RecentTasks returns the most recent active tasks for the given project,
	// up to limit.
	RecentTasks(slug string, limit int) ([]store.TaskListItem, error)
	// StaleRunbooks returns the stale runbooks for the given project, up to limit.
	StaleRunbooks(slug string, limit int) ([]store.RunbookIndexRow, error)
	// LatestEvidence returns the most recent evidence entries for the given
	// project, up to limit.
	LatestEvidence(slug string, limit int) ([]store.EvidenceListItem, error)
}

// ProjectNode is one card in a project-tree walk (rfc-tui.md §9.2's project
// tree overlay, ctrl+p): the card's own identity, where it sits in the tree,
// and its children when the walk built them.
//
// Aliases is populated by ProjectNode (the single-card method) but left nil
// by ProjectTreeReader.ProjectTree's bulk result: internal/store has no
// batch alias query, and fetching one project's aliases per node would turn
// the tree's two store calls back into the N+1 this type exists to avoid.
type ProjectNode struct {
	Slug        string
	ParentSlug  string
	DisplayName string
	Kind        string
	Description string
	Icon        string
	Color       string
	Tags        []string
	Aliases     []string
	Depth       int
	Counts      store.ProjectCardCounts
	Children    []ProjectNode
}

// ProjectTreeReader is the surface the project tree overlay and Home's
// breadcrumb need: the whole forest, one node, its ancestors and its
// descendants, and the alias resolver the overlay's "/" filter and the
// workspace search palette both need to turn a typed name into a slug.
type ProjectTreeReader interface {
	// ProjectTree returns every project card as a forest — root cards at the
	// top level, children nested under their parent — built from at most two
	// store calls total.
	ProjectTree() ([]ProjectNode, error)
	// ProjectNode returns one card as a tree node, with its own aliases but
	// no children populated: a caller that wants the subtree calls
	// ProjectTree and looks the slug up there.
	ProjectNode(slug string) (ProjectNode, error)
	// Ancestors returns slug's ancestors, root-first, not including slug
	// itself.
	Ancestors(slug string) ([]ProjectNode, error)
	// Descendants returns every slug under root, root included, in preorder.
	Descendants(root string) ([]string, error)
	// ResolveAlias resolves a raw name — a slug, an alias, or a folded
	// variant — to its canonical slug.
	ResolveAlias(raw string) (string, error)
}

// TaskDetail is the aggregate the Tasks tab's detail screen needs (S4): the
// task row plus its linked observations (task_observations, root_cause
// first) and its evidence.
type TaskDetail struct {
	Task         store.Task
	Observations []store.TaskObservationDetail
	Evidence     []store.EvidenceListItem
	Benchmarks   []Benchmark
}

// TaskReader is the surface the Tasks tab needs: the filtered/searched list
// (S3), one task's aggregate detail (S4), the two writes ADR-028 allows from
// the TUI — the local `state` mirror and linking an observation — and the
// context pack (S5).
//
// ListTasks folds search into the same call rather than exposing a second
// SearchTasks method: store.TaskListFilter.Query already runs against
// tasks_fts (rfc-tui.md §9.2's "S3 search" query), so a second method would
// just be a thinner duplicate of this one.
type TaskReader interface {
	// ListTasks lists tasks for a project applying f (state, kind, query,
	// limit, offset), most recently updated first.
	ListTasks(project string, f store.TaskListFilter) ([]store.TaskListItem, error)
	// Task returns one task's aggregate detail by id.
	Task(id int64) (TaskDetail, error)
	// UpdateState sets a task's local state mirror. Jira remains the source
	// of truth (ADR-028): this never talks to Jira.
	UpdateState(id int64, state string) error
	// LinkObservation links an existing observation to a task by id.
	LinkObservation(taskID, observationID int64) error
	// ContextPack renders the markdown context pack for a task (delegates to
	// internal/project.BuildContextPack, the same function mem_context_pack
	// calls).
	ContextPack(taskID int64) (string, error)
}

// TaskPageReader is TaskReader's paginated and vault-aware surface, kept as
// its own interface rather than folded into TaskReader: TaskReader's method
// set is load-bearing for internal/tui/app and internal/tui/tabs, which
// implement it (and hand-roll test doubles against it) today, so this
// package's own aditivo rule holds here too — an existing data.* interface
// keeps its method set, new capability arrives through a new one.
type TaskPageReader interface {
	// ListTasksPage is ListTasks with the page's own total size alongside
	// it, for a pager that needs to say "showing X of Y" up front.
	ListTasksPage(project string, f store.TaskListFilter) (Page[store.TaskListItem], error)
	// TaskBySlug returns one task's aggregate detail by its project-scoped
	// slug — the vault folder name a task with no Jira key is addressed by.
	TaskBySlug(project, slug string) (TaskDetail, error)
	// VaultDir resolves a task's vault checkout directory: ENGRAM_VAULT_ROOT
	// joined with the task's own vault_path. ok is false when either half is
	// missing, or the resolved path does not exist — "V" (open the vault
	// folder) has nothing to hand tea.ExecProcess in that case.
	VaultDir(id int64) (dir string, ok bool, err error)
}

// EvidenceReader is the surface the Evidence tab needs: the project's
// evidence list, optionally filtered by task and/or attached status (S6).
// S7's detail adds nothing the store must be queried for beyond what a list
// row already carries — manifest.json's positive/negative control pair is
// read from disk, not from SQLite — so unlike TaskReader there is no second
// "get one" method here: the Evidence tab keeps the row it already loaded.
type EvidenceReader interface {
	// ListEvidence lists project's evidence applying f (task, attached-jira
	// and kind filters), most recently captured first (rfc-tui.md §9.2's
	// "S6 Evidence list" query).
	ListEvidence(project string, f store.EvidenceListFilter) ([]store.EvidenceListItem, error)
}

// EvidencePageReader is EvidenceReader's paginated surface, kept as its own
// interface for the same reason TaskPageReader is: EvidenceReader's method
// set is load-bearing for internal/tui/tabs/evidence today.
type EvidencePageReader interface {
	// ListEvidencePage is ListEvidence with the page's total size and byte
	// count alongside it.
	ListEvidencePage(project string, f store.EvidenceListFilter) (EvidencePage, error)
	// Categories returns every vault-folder category project's evidence
	// falls under, with how many rows sit in each, for the "t" filter's
	// counts.
	Categories(project string) ([]CategoryCount, error)
}

// RunbookReader is the surface the Runbooks tab needs: the browsable index
// scoped to a project or every project (S8's "a" toggle), and the same
// shape ranked by symptoms (S8's "/" search).
//
// ListRunbooks and SearchRunbooks share the RunbookIndexRow shape on
// purpose — the Runbooks tab renders both in the exact same table, so a
// single Items field can hold either result — unlike FindRunbooks
// (mem_runbook_find's thinner item for the MCP envelope), which this reader
// does not expose.
//
// Reading a runbook's Markdown file is not part of this interface: like
// Evidence's manifest.json (see EvidenceReader's doc comment), it is a plain
// filesystem read with nothing to query the store for, so the Runbooks tab
// does it directly against shared.VaultRoot(), the same way the Evidence tab
// reads manifest.json directly against shared.EvidenceRoot().
type RunbookReader interface {
	// ListRunbooks lists the runbook index, scoped to project unless all is
	// true, most-stale-first (rfc-tui.md §9.2's "S8 Runbooks index" query).
	ListRunbooks(project string, all bool) ([]store.RunbookIndexRow, error)
	// SearchRunbooks searches the index by title and symptoms via
	// runbook_index_fts, scoped to project unless all is true, ranked by
	// BM25 (rfc-tui.md §9.2's "S8 search by symptoms" query).
	SearchRunbooks(project string, all bool, query string, limit int) ([]store.RunbookIndexRow, error)
}

// RunbookFilter is store.RunbookListFilter under the name the Runbooks tab's
// own reader interface uses.
type RunbookFilter = store.RunbookListFilter

// RunbookPageReader is RunbookReader's paginated and further-filtered
// surface, kept as its own interface for the same reason TaskPageReader is:
// RunbookReader's method set is load-bearing for internal/tui/tabs/runbooks
// today.
type RunbookPageReader interface {
	// ListRunbooksPage is ListRunbooks with f's further filters (stale,
	// category, pattern, status) and the page's own total size alongside it.
	ListRunbooksPage(project string, all bool, f RunbookFilter) (Page[store.RunbookIndexRow], error)
}

// MemoryReader is the surface the Memory tab needs: the observation, session
// and timeline queries behind engram's memory screens, plus the one destructive
// operation those screens expose.
//
// It is named "reader" for the role it plays — every method but DeleteSession
// is a query — and DeleteSession lives here rather than in a second interface
// because the delete prompt is part of the same screen and splitting it would
// buy nothing but a second constructor argument.
type MemoryReader interface {
	// Stats returns the dashboard counters.
	Stats() (*store.Stats, error)
	// Search runs a full-text query over observations.
	Search(query string, opts store.SearchOptions) ([]store.SearchResult, error)
	// RecentObservations returns the newest observations across all projects.
	RecentObservations(limit int) ([]store.Observation, error)
	// Observation returns one observation by id.
	Observation(id int64) (*store.Observation, error)
	// Timeline returns the observations surrounding id inside its session.
	Timeline(id int64, before, after int) (*store.TimelineResult, error)
	// RecentSessions returns the newest sessions across all projects.
	RecentSessions(limit int) ([]store.SessionSummary, error)
	// SessionObservations returns the observations recorded in one session.
	SessionObservations(sessionID string, limit int) ([]store.Observation, error)
	// DeleteSession removes an empty session. Deleting a session that still
	// holds observations must fail rather than orphan them.
	DeleteSession(sessionID string) error
}

// ScopedMemoryReader is MemoryReader's project-scoped and paged surface,
// kept as its own interface for the same reason TaskPageReader is:
// MemoryReader's method set is load-bearing for internal/tui/tabs/memory
// today, which implements it (and hand-rolls test doubles against it).
type ScopedMemoryReader interface {
	// SearchScoped is Search narrowed to scope and paged.
	//
	// Total is the number of hits found within this call's own capped fetch
	// window (offset+limit, itself bounded), not a true count of every
	// match beyond it: store.Search has no Offset and no COUNT(*) OVER() of
	// its own the way ListTasksPage/ListEvidencePage/ListRunbooksPage/
	// ListBenchmarks do. A caller that pages far enough to notice the
	// difference is the signal that store.Search needs one.
	SearchScoped(query string, scope ProjectScope, limit, offset int) (Page[store.SearchResult], error)
	// RecentObservationsScoped is RecentObservations narrowed to scope and
	// paged, with the same capped-window Total as SearchScoped.
	RecentObservationsScoped(scope ProjectScope, limit, offset int) (Page[store.Observation], error)
	// RecentSessionsScoped is RecentSessions narrowed to scope and paged,
	// with the same capped-window Total as SearchScoped.
	RecentSessionsScoped(scope ProjectScope, limit, offset int) (Page[store.SessionSummary], error)
}

// ProjectScope narrows a workspace-wide read to one project, optionally
// widened to its subtree — the Memory tab's "a" three-state toggle
// (rfc-tui.md §9.2: this project / subtree / everything). An empty Project
// means everything, matching every scoped method's unscoped sibling.
type ProjectScope struct {
	Project string
	Subtree bool
}

// Benchmark is one measurement next to the baseline of its own metric, with
// the two readings the Benchmarks tab's Δ column needs already computed
// (rfc-tui.md §9.2's "S9 Benchmarks" table).
//
// It wraps store.BenchmarkDelta rather than aliasing it: Delta and Improved
// are TUI-side readings of the store's own numbers (BaselineValue, DeltaPct,
// Direction), and a method can only be added to a type in the package that
// defines it, which for store.BenchmarkDelta is not this one.
type Benchmark struct {
	store.BenchmarkDelta
}

// Delta returns the percentage change against the metric's baseline —
// store.BenchmarkDelta.DeltaPct itself — nil when the metric has no
// baseline yet.
func (b Benchmark) Delta() *float64 {
	return b.DeltaPct
}

// Improved reports whether this measurement is better than its baseline,
// honoring the metric's Direction (store.BenchmarkDirectionLower vs
// store.BenchmarkDirectionHigher). nil when there is no baseline to compare
// against — "improved" has no answer without one.
func (b Benchmark) Improved() *bool {
	if b.DeltaPct == nil {
		return nil
	}
	improved := *b.DeltaPct < 0
	if b.Direction == store.BenchmarkDirectionHigher {
		improved = *b.DeltaPct > 0
	}
	return &improved
}

// BenchmarkFilter is store.BenchmarkListFilter under the name the Benchmarks
// tab's own reader interface uses.
type BenchmarkFilter = store.BenchmarkListFilter

// BenchmarkReader is the surface the Benchmarks tab needs: the project's
// measurements (S9's table), one task's history, and one metric's series
// across a project (S9's "enter" metric-history view).
type BenchmarkReader interface {
	// ListBenchmarks lists measurements for project (or, via f.Task, one
	// task within it), newest first, each next to the baseline of its own
	// metric.
	ListBenchmarks(project string, f BenchmarkFilter) (Page[Benchmark], error)
	// TaskBenchmarks lists every benchmark recorded against one task, newest
	// first. It takes the task's sync_id — the identity
	// store.BenchmarkListFilter.Task already keys on — rather than its
	// numeric row id, so a caller that already has a Task or TaskListItem in
	// hand does not pay for an extra GetTask round trip.
	TaskBenchmarks(taskSyncID string) ([]Benchmark, error)
	// MetricHistory returns one metric's measurements across a project,
	// oldest first — the series a history chart plots left to right — up to
	// limit.
	MetricHistory(project, metric string, limit int) ([]Benchmark, error)
}

// GraphState is one project's code-graph summary as the card holds it: the
// commit and counts SyncGraph last persisted, and the staleness verdict the
// store computed for them (rfc-tui.md §9.2's "S10 Graph" tab; graph_summary,
// graph_stale_reason and friends on project_cards).
//
// StaleReason is the literal store.StampGraphStaleness persisted —
// "" (fresh), "no_graph", "code_changed", "docs_only" or
// "graph_commit_unreachable" (internal/project's Stale-Reason constants) —
// never recomputed here: the TUI renders the store's verdict, it does not
// form its own.
type GraphState struct {
	Project      string
	Commit       string
	BuiltAt      string
	Nodes        int
	Edges        int
	Communities  int
	GodNodes     []project.GodNode
	Stale        bool
	StaleReason  string
	ChangedFiles int
	CheckedAt    string
}

// ObservationRef is one observation_refs row: an observation linked to a
// graph node label at the commit it was captured under (the "graph" ref
// kind SearchWorkspace and mem_search(graph_ref) both read).
type ObservationRef struct {
	ObservationID int64
	RefKind       string
	Ref           string
	GraphCommit   string
}

// GraphReader is the surface the Graph tab needs: the project's current
// graph state and the observations linked to its nodes.
type GraphReader interface {
	// GraphState returns project's current graph summary and staleness
	// verdict, read off its project card.
	GraphState(project string) (GraphState, error)
	// ObservationRefs pages through project's graph-linked observations.
	ObservationRefs(project string, limit, offset int) (Page[ObservationRef], error)
}

// GraphSyncer runs the graph sync algorithm (RFC §8.3) for a project — the
// same work `engram project <slug> graph sync` and mem_graph_sync do — and
// is kept apart from GraphReader because "s" (sync) is the Graph tab's one
// write among a screen of reads, the same split ProjectReader/TaskReader's
// UpdateState draws.
type GraphSyncer interface {
	// SyncGraph re-reads the project's graph.json, persists the new summary
	// and staleness verdict, and returns the resulting GraphState.
	SyncGraph(project string) (GraphState, error)
}

// ThemeRecord extends store.ThemeRecord with Invalid, the reason its palette
// failed validation — empty for a valid theme.
//
// Full palette validation (13 roles present, hex shape, contrast ratios) is
// internal/tui/theme's job (theme/validate.go, not built yet as of this
// package); this only catches what the store's own json_valid CHECK
// constraint does not: a palette with no color roles in it at all. A theme
// this flags still loads — the picker renders it in Danger and a caller
// falls back to koi-pond, exactly as an internal/tui/theme-validated palette
// would (rfc-tui.md §9.2's theme picker) — Invalid is surfaced, not enforced,
// here.
type ThemeRecord struct {
	store.ThemeRecord
	Invalid string
}

// ThemeReader is the surface the theme picker (Settings tab, ctrl+t) reads
// from: every saved theme and one by name.
type ThemeReader interface {
	// ListThemes returns every theme, builtin and saved, by name.
	ListThemes() ([]ThemeRecord, error)
	// Theme returns one theme by name, or store.ErrThemeNotFound.
	Theme(name string) (ThemeRecord, error)
}

// ThemeWriter is the surface the theme picker writes through: saving a
// theme, deleting one that was added, and resetting a builtin back to a
// palette (the compiled default, once internal/tui/theme exposes one as
// JSON — see ThemeRecord's doc comment).
type ThemeWriter interface {
	// SaveTheme writes a theme somebody chose: imported from a file, or
	// edited in the picker.
	SaveTheme(rec store.ThemeRecord) error
	// DeleteTheme removes a theme somebody added. A builtin is refused
	// (store.ErrBuiltinTheme); ResetTheme is the verb for one of those.
	DeleteTheme(name string) error
	// ResetTheme puts a builtin theme back to palette, or removes a saved
	// one that is not builtin. palette may be nil, which re-marks a builtin
	// row as builtin without changing its colors — the same degradation
	// store.ResetTheme documents for a nil palette.
	ResetTheme(name string, palette json.RawMessage) error
}

// SettingsReader is the surface Ajustes (and everything that resolves a
// `settings.*` value ahead of config.json — rfc-tui.md §9.2's theme/icon/
// mouse precedence) reads from.
type SettingsReader interface {
	// Setting reads one setting. The boolean separates "set to the empty
	// string" from "never set".
	Setting(key string) (string, bool, error)
	// Settings reads every setting under a prefix ("tui." for what the
	// interface remembers), or all of them when prefix is empty.
	Settings(prefix string) (map[string]string, error)
}

// SettingsWriter is the surface Ajustes writes a remembered choice through.
type SettingsWriter interface {
	// SetSetting writes one setting, replacing whatever it held.
	SetSetting(key, value string) error
}

// SearchKind narrows a workspace search to one of the six kinds
// store.SearchWorkspace understands.
type SearchKind = string

// The six kinds a workspace search groups results into, in the order
// store.SearchWorkspace itself groups them: what was written, what is being
// worked on, what proves it, what documents it, what holds it, what
// measures it.
const (
	SearchKindObservation = store.WorkspaceKindObservation
	SearchKindTask        = store.WorkspaceKindTask
	SearchKindEvidence    = store.WorkspaceKindEvidence
	SearchKindRunbook     = store.WorkspaceKindRunbook
	SearchKindCard        = store.WorkspaceKindCard
	SearchKindBenchmark   = store.WorkspaceKindBenchmark
)

// SearchQuery is GlobalSearcher.SearchWorkspace's input: the palette's
// (ctrl+k) live query text, its kind chips, its scope toggle and its
// per-kind cap.
type SearchQuery struct {
	Text         string
	Kinds        []SearchKind
	Scope        ProjectScope
	LimitPerKind int
}

// SearchHit is one grouped result of a workspace search, in the shape the
// palette renders: kind, identity, the two lines of text, and its rank
// within its kind.
//
// MatchedIndexes is always nil here: it names the character positions a
// fuzzy filter (bubbles/list.DefaultFilter, shared.Fuzzy) matched against a
// typed query, and this search is FTS5-ranked, not fuzzy-filtered — FTS5
// has no notion of "which characters matched". The project tree and theme
// picker's own "/" filters are the ones that populate it.
type SearchHit struct {
	Kind           string
	ID             int64
	Slug           string
	Project        string
	Title          string
	Subtitle       string
	Snippet        string
	MatchedIndexes []int
	Rank           float64
}

// GlobalSearcher is the surface the search palette (ctrl+k) needs: one
// query across every kind the workspace holds.
type GlobalSearcher interface {
	// SearchWorkspace runs q across the kinds it names (every kind when
	// Kinds is empty), scoped and capped as q says.
	SearchWorkspace(q SearchQuery) ([]SearchHit, error)
}
