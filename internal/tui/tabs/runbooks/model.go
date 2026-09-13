// Package runbooks is the Runbooks workspace tab: the project's (or every
// project's) runbook index, and one runbook's Markdown rendered with glamour
// (rfc-tui.md §3.1 S8-S9).
//
// It is an isolated Elm sub-model, shaped like tabs/tasks and tabs/evidence:
//   - screen constants are a local iota; the root does not know them
//   - one Model struct holds all of the tab's state
//   - vim keys (j/k) navigate, esc/q walk back up
//
// Data reaches the tab through data.RunbookReader, never through
// *store.Store, and styling through theme.Styles. Editing a runbook is out
// of scope for v1 (ADR-028 point 2): "e" opens the file in $EDITOR, the TUI
// itself never writes to the vault.
package runbooks

import (
	"github.com/HoracioEspinosa/engram/internal/store"
	"github.com/HoracioEspinosa/engram/internal/tui/data"
	"github.com/HoracioEspinosa/engram/internal/tui/theme"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// ─── Screens ─────────────────────────────────────────────────────────────────

// Screen is the Runbooks tab's own screen enum (rfc-tui.md §3.1: S8 index, S9
// Markdown view). Not exported to the root, same as evidence.Screen.
type Screen int

const (
	ScreenIndex Screen = iota
	ScreenView
)

// searchLimit caps SearchRunbooks results, matching rfc-tui.md §9.2's "S8
// search by symptoms" query (`... LIMIT 50`).
const searchLimit = 50

// ─── messages (data loaded) ──────────────────────────────────────────────────

// runbooksLoadedMsg carries loadRunbookIndex's or searchRunbooks' result.
// project is threaded through so a slow load issued before a project switch
// never clobbers the list of the project now on screen — the same guard
// app.dashboardModel.applyLoaded applies to a stale dashboard load.
type runbooksLoadedMsg struct {
	project string
	all     bool
	query   string
	items   []store.RunbookIndexRow
	err     error
}

// markdownLoadedMsg carries loadMarkdown's result for one runbook. id guards
// the same way manifestLoadedMsg's evidenceID does in tabs/evidence: a slow
// read for a runbook the user has since left must not land on whatever is
// selected now.
//
// err and renderErr are deliberately separate: err is a filesystem failure
// other than "not found" (exists already carries that) and blocks showing
// any content; renderErr is glamour failing to style the file it did read —
// raw and rendered are still populated (rendered falls back to the plain
// source, see renderMarkdown), so the runbook stays readable either way.
type markdownLoadedMsg struct {
	id        string
	exists    bool
	raw       string
	rendered  string
	renderErr string
	err       error
}

// editorClosedMsg carries the outcome of execEditor's tea.ExecProcess once
// $EDITOR exits (rfc-tui.md §9.4: "e abre el archivo en $EDITOR").
type editorClosedMsg struct {
	err error
}

// ─── Model ───────────────────────────────────────────────────────────────────

// Model is the Runbooks tab's state.
type Model struct {
	reader   data.RunbookReader
	projects data.ProjectReader
	styles   theme.Styles
	project  string

	Screen Screen
	Width  int
	Height int

	// Index (S8).
	Items       []store.RunbookIndexRow
	Cursor      int
	Scroll      int
	All         bool // "a" toggle: this project only (false) vs every project (true)
	Query       string
	Searching   bool
	SearchInput textinput.Model

	// Markdown view (S9).
	Selected    *store.RunbookIndexRow
	MarkdownRaw string
	Rendered    string
	FileExists  bool
	MarkdownErr string
	ViewScroll  int

	// Feedback shared by every screen.
	CopyFeedback string
	ErrorMsg     string
}

// New creates the Runbooks tab bound to the given readers: reader for the
// runbook index itself, projects for resolving the active project's
// knowledge_hub_path ("o", rfc-tui.md §9.4). The tab starts with no project:
// the root scopes it with WithProject once one is active, exactly as it
// constructs evidence.Model — see app.Model.New and the selector's "enter"
// key.
func New(reader data.RunbookReader, projects data.ProjectReader) Model {
	search := textinput.New()
	search.Placeholder = "Search symptoms..."
	search.CharLimit = 200
	search.Width = 50

	return Model{
		reader:      reader,
		projects:    projects,
		styles:      theme.Default(),
		SearchInput: search,
	}
}

// WithProject returns a copy of m scoped to project, its screen state reset
// to the index. The root calls this whenever the active project changes, so
// the next load queries the right project instead of replaying whatever the
// previous one had on screen.
func (m Model) WithProject(project string) Model {
	m.project = project
	m.Screen = ScreenIndex
	m.Items = nil
	m.Cursor = 0
	m.Scroll = 0
	m.Query = ""
	m.Searching = false
	m.SearchInput.SetValue("")
	m.SearchInput.Blur()
	m.Selected = nil
	m.MarkdownRaw = ""
	m.Rendered = ""
	m.FileExists = false
	m.MarkdownErr = ""
	m.ViewScroll = 0
	m.ErrorMsg = ""
	return m
}

// Title is the label the tab bar shows for this tab.
func (Model) Title() string { return "Runbooks" }

// Init loads the runbook index when a project is already active (engram tui
// --project <slug>); otherwise there is nothing to query yet; WithProject's
// caller is responsible for issuing Refresh() once one is chosen.
func (m Model) Init() tea.Cmd {
	if m.project == "" {
		return nil
	}
	return loadRunbookIndex(m.reader, m.project, m.All)
}

// Refresh reloads the data behind the current screen: the index or the
// active search on S8, or the selected runbook's Markdown on S9. The root
// calls it on "r" and whenever this tab becomes active.
func (m Model) Refresh() tea.Cmd {
	if m.Screen == ScreenView && m.Selected != nil {
		return loadMarkdown(*m.Selected, m.Width)
	}
	if m.Query != "" {
		return searchRunbooks(m.reader, m.project, m.All, m.Query, searchLimit)
	}
	return loadRunbookIndex(m.reader, m.project, m.All)
}
