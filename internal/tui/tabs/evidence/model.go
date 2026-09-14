// Package evidence is the Evidence workspace tab: the project's captured
// evidence list and one file's detail, including whatever its sibling
// manifest.json adds (rfc-tui.md §3.1 S6-S7).
//
// It is an isolated Elm sub-model, shaped like tabs/tasks and tabs/memory:
//   - screen constants are a local iota; the root does not know them
//   - one Model struct holds all of the tab's state
//   - vim keys (j/k) navigate, esc/q walk back up
//
// Data reaches the tab through data.EvidenceReader, never through
// *store.Store, and styling through theme.Styles. rfc-tui.md §3.2 puts
// rendering evidence inline out of scope for v1: a file is opened with the
// system viewer, never painted in the terminal (ADR-028 point 4).
package evidence

import (
	"path/filepath"

	"github.com/HoracioEspinosa/engram/internal/store"
	"github.com/HoracioEspinosa/engram/internal/tui/data"
	"github.com/HoracioEspinosa/engram/internal/tui/shared"
	"github.com/HoracioEspinosa/engram/internal/tui/theme"

	tea "github.com/charmbracelet/bubbletea"
)

// ─── Screens ─────────────────────────────────────────────────────────────────

// Screen is the Evidence tab's own screen enum (rfc-tui.md §3.1: S6 list, S7
// detail). Not exported to the root, same as tasks.Screen.
type Screen int

const (
	ScreenList Screen = iota
	ScreenDetail
)

// ─── messages (data loaded) ──────────────────────────────────────────────────

// evidenceLoadedMsg carries loadEvidence's result. project is threaded
// through so a slow load issued before a project switch never clobbers the
// list of the project now on screen — the same guard
// app.dashboardModel.applyLoaded applies to a stale dashboard load. filter
// is echoed back so a filter set by "t"/"a" (or by OpenForTask's deep link)
// is only committed to the model once the rows it produced actually arrive.
type evidenceLoadedMsg struct {
	project string
	filter  store.EvidenceListFilter
	page    data.EvidencePage
	err     error
}

// manifestLoadedMsg carries loadManifest's result for one evidence row.
// evidenceID guards the same way stateUpdatedMsg does in tabs/tasks: a slow
// read for a file the user has since left must not land on whatever is
// selected now.
type manifestLoadedMsg struct {
	evidenceID int64
	entry      *ManifestEntry
	exists     bool
	err        error
}

// ─── Model ───────────────────────────────────────────────────────────────────

// Model is the Evidence tab's state.
type Model struct {
	reader  data.EvidenceSource
	styles  theme.Styles
	project string

	Screen Screen
	Width  int
	Height int

	// Focus is which pane answers the cursor keys at the split breakpoint.
	// Below it there is only the master, and "l" leaves the focus there.
	Focus shared.Pane

	// List (S6).
	Items  []store.EvidenceListItem
	Cursor int
	Scroll int
	// Total is how many evidence rows the filter matches in the store, and
	// TotalBytes how much they weigh together — both counted over the whole
	// match, not over the page on screen.
	Total      int
	TotalBytes int64
	Filter     store.EvidenceListFilter

	// Detail (S7).
	Selected        *store.EvidenceListItem
	Manifest        *ManifestEntry
	ManifestExists  bool
	ManifestChecked bool
	ManifestErr     string

	// Feedback shared by every screen.
	CopyFeedback string
	ErrorMsg     string
}

// New creates the Evidence tab bound to the given reader. The tab starts with
// no project: the root scopes it with WithProject once one is active, exactly
// as it constructs tasks.Model — see app.Model.New and the selector's "enter"
// key.
func New(r data.EvidenceSource) Model {
	return Model{reader: r, styles: theme.Default()}
}

// WithStyles returns a copy of m painted with styles instead of the default
// theme.New built it with — app.New calls this once, right after New, so
// the tab renders under the same resolved palette as the workspace chrome
// around it (rfc-tui.md §8.2's --theme / ENGRAM_TUI_THEME / tui.theme).
func (m Model) WithStyles(styles theme.Styles) Model {
	m.styles = styles
	return m
}

// Styles exposes the tab's current style set for app-level tests that
// assert every tab paints with the same resolved palette instead of its own
// default.
func (m Model) Styles() theme.Styles { return m.styles }

// WithProject returns a copy of m scoped to project, its screen state reset
// to the list. The root calls this whenever the active project changes, so
// the next load queries the right project instead of replaying whatever the
// previous one had on screen.
func (m Model) WithProject(project string) Model {
	m.project = project
	m.Screen = ScreenList
	m.Items = nil
	m.Cursor = 0
	m.Scroll = 0
	m.Total = 0
	m.TotalBytes = 0
	m.Filter = store.EvidenceListFilter{}
	m.Selected = nil
	m.Manifest = nil
	m.ManifestExists = false
	m.ManifestChecked = false
	m.ManifestErr = ""
	m.ErrorMsg = ""
	return m
}

// HasPrevPage reports whether a page of evidence sits before the one on
// screen.
func (m Model) HasPrevPage() bool { return m.Filter.Offset > 0 }

// HasNextPage reports whether a page of evidence sits after the one on
// screen, read from the store's own total rather than guessed from a short
// page.
func (m Model) HasNextPage() bool { return m.Filter.Offset+len(m.Items) < m.Total }

// pageLimit is the page size in force: the filter's own, or the default the
// store applies when it has none.
func (m Model) pageLimit() int {
	if m.Filter.Limit > 0 {
		return m.Filter.Limit
	}
	return pageSize
}

// Title is the label the tab bar shows for this tab.
func (Model) Title() string { return "Evidence" }

// Init loads the evidence list when a project is already active (engram tui
// --project <slug>); otherwise there is nothing to query yet; WithProject's
// caller is responsible for issuing Refresh() once one is chosen.
func (m Model) Init() tea.Cmd {
	if m.project == "" {
		return nil
	}
	return loadEvidence(m.reader, m.project, m.Filter)
}

// Refresh reloads the data behind the current screen: the filtered list on
// S6, or the selected row's manifest.json on S7. The root calls it on "r"
// (from the list) and whenever this tab becomes active.
func (m Model) Refresh() tea.Cmd {
	if m.Screen == ScreenDetail && m.Selected != nil {
		return loadManifest(*m.Selected)
	}
	return loadEvidence(m.reader, m.project, m.Filter)
}

// OpenForTask scopes the evidence list to taskID and reloads it — the deep
// link rfc-tui.md §3.1 S4's "e" key drives via tabs.NavigateMsg.TaskID. It
// mirrors memory.Model.OpenObservation and tasks.Model.OpenTask: state
// changes only once the load comes back through Update, not here.
func (m Model) OpenForTask(taskID int64) tea.Cmd {
	return loadEvidence(m.reader, m.project, store.EvidenceListFilter{TaskID: taskID})
}

// absolutePath resolves a stored evidence path — relative to
// ${CD_EVIDENCE_DIR:-~/.clarodrive/evidence}, per the store's own documented
// contract (internal/mcp/projects_tools.go's `path` argument description) —
// to a real filesystem path. Real registered evidence confirms the
// contract: its rows' `path` values resolve correctly with no project-slug
// segment in between, unlike this same root's context-pack.md convention in
// tabs/tasks/write.go, which nests one deliberately for a file the TUI
// itself writes.
func absolutePath(relative string) string {
	return filepath.Join(shared.EvidenceRoot(), relative)
}
