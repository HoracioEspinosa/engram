// Package tasks is the Tasks workspace tab: the task list, a task's detail,
// and the context pack it can build from there (rfc-tui.md §3.1 S3-S5).
//
// It is an isolated Elm sub-model, shaped like tabs/memory and tabs/cloud:
//   - screen constants are a local iota; the root does not know them
//   - one Model struct holds all of the tab's state
//   - vim keys (j/k) navigate, PrevScreen-style back-navigation walks up
//
// Data reaches the tab through data.TaskReader, never through *store.Store,
// and styling through theme.Styles.
package tasks

import (
	"time"

	tasksdomain "github.com/HoracioEspinosa/engram/internal/tasks"

	"github.com/HoracioEspinosa/engram/internal/store"
	"github.com/HoracioEspinosa/engram/internal/tui/data"
	"github.com/HoracioEspinosa/engram/internal/tui/theme"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// ─── Screens ─────────────────────────────────────────────────────────────────

// Screen is the Tasks tab's own screen enum (rfc-tui.md §3.1: S3 list, S4
// detail, S5 context pack). Not exported to the root, same as memory.Screen.
type Screen int

const (
	ScreenList Screen = iota
	ScreenDetail
	ScreenContextPack
)

// stateOptions lists every value the tasks.state CHECK constraint accepts
// (internal/store/projects_schema.go), in the order the inline state-change
// picker (S4, key "s") offers them. Built from the internal/tasks constants
// so the picker can never drift from the schema it writes against.
var stateOptions = append(append([]string{}, tasksdomain.ActiveStates...), tasksdomain.StateDone, tasksdomain.StateCancelled)

// kindOptions is the fixed set of task kinds the tasks.kind CHECK constraint
// accepts; "" means no kind filter (rfc-tui.md §3.1 S3's "k filtro kind").
var kindOptions = []string{"", "feature", "bugfix", "refactor", "incident", "migration", "spike"}

// pageSize is how many tasks the page keys move by. rfc-tui.md §9.2's list
// query never fixes a page size; store.TaskListFilter defaults to 20 when
// Limit is unset, so paging by the same number keeps one "page" meaning the
// same thing whether or not the user ever presses a page key.
const pageSize = 20

// pageLimit is the page size in force: the filter's own, or the default when
// it has none.
func (m Model) pageLimit() int {
	if m.Filter.Limit > 0 {
		return m.Filter.Limit
	}
	return pageSize
}

// ─── messages (data loaded) ─────────────────────────────────────────────────

type tasksLoadedMsg struct {
	page data.Page[store.TaskListItem]
	err  error
}

type taskDetailLoadedMsg struct {
	detail data.TaskDetail
	err    error
}

// stateUpdatedMsg carries UpdateState's outcome. id is threaded through so a
// slow write for a task the user has since left does not clobber whatever is
// on screen now — the same guard app.dashboardModel.applyLoaded applies to a
// stale dashboard load.
type stateUpdatedMsg struct {
	id  int64
	err error
}

type observationLinkedMsg struct {
	taskID int64
	err    error
}

type contextPackLoadedMsg struct {
	taskID int64
	pack   string
	err    error
}

// ─── Model ───────────────────────────────────────────────────────────────────

// Model is the Tasks tab's state.
type Model struct {
	reader  data.TaskSource
	styles  theme.Styles
	project string

	Screen Screen
	Width  int
	Height int

	// List (S3).
	Items  []store.TaskListItem
	Cursor int
	Scroll int
	// Total is how many tasks the filter matches in the store, not how many
	// came back on this page: the range indicator reports the count the
	// query itself produced, and the page keys need it to know where the
	// last page ends.
	Total       int
	Filter      store.TaskListFilter
	Searching   bool
	SearchInput textinput.Model

	// Detail (S4).
	Detail        *data.TaskDetail
	DetailCursor  int
	DetailScroll  int
	ChangingState bool
	StateCursor   int
	Linking       bool
	LinkInput     textinput.Model

	// Context pack (S5).
	ContextPack       string
	ContextPackBuilt  time.Time
	ContextPackScroll int

	// Feedback shared by every screen.
	CopyFeedback string
	ErrorMsg     string
}

// New creates the Tasks tab bound to the given reader. The tab starts with no
// project: the root scopes it with WithProject once one is active, exactly
// as it constructs app.dashboardModel — see app.Model.New and the selector's
// "enter" key.
func New(r data.TaskSource) Model {
	search := textinput.New()
	search.Placeholder = "Search tasks..."
	search.CharLimit = 200
	search.Width = 50

	link := textinput.New()
	link.Placeholder = "Observation #id..."
	link.CharLimit = 20
	link.Width = 30

	return Model{
		reader:      r,
		styles:      theme.Default(),
		SearchInput: search,
		LinkInput:   link,
	}
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
	m.Filter = store.TaskListFilter{}
	m.Detail = nil
	m.ErrorMsg = ""
	return m
}

// HasPrevPage reports whether a page of tasks sits before the one on screen.
func (m Model) HasPrevPage() bool { return m.Filter.Offset > 0 }

// HasNextPage reports whether a page of tasks sits after the one on screen.
// It reads the store's own total rather than guessing from a short page, so
// the last page stays put instead of wrapping round to the first.
func (m Model) HasNextPage() bool { return m.Filter.Offset+len(m.Items) < m.Total }

// Title is the label the tab bar shows for this tab.
func (Model) Title() string { return "Tasks" }

// Init loads the task list when a project is already active (engram tui
// --project <slug>); otherwise there is nothing to query yet; WithProject's
// caller is responsible for issuing Refresh() once one is chosen.
func (m Model) Init() tea.Cmd {
	if m.project == "" {
		return nil
	}
	return loadTasks(m.reader, m.project, m.Filter)
}

// OpenTask returns the command that loads id's detail. It is what the root
// drives when another tab asks to deep-link into a task (rfc-tui.md §3.1
// S7: Enter on an evidence file opens its task here) — the same command
// loadTaskDetail already issues on the "enter" key from the list screen,
// exposed so a message from outside this package can trigger it too.
func (m Model) OpenTask(id int64) tea.Cmd {
	return loadTaskDetail(m.reader, id)
}

// Refresh reloads the data behind the current screen: the filtered list on
// S3, the task (and its observations/evidence) on S4, or the context pack on
// S5. The root calls it on "r" and whenever this tab becomes active.
func (m Model) Refresh() tea.Cmd {
	switch m.Screen {
	case ScreenDetail:
		if m.Detail != nil {
			return loadTaskDetail(m.reader, m.Detail.Task.ID)
		}
	case ScreenContextPack:
		if m.Detail != nil {
			return loadContextPack(m.reader, m.Detail.Task.ID)
		}
	}
	return loadTasks(m.reader, m.project, m.Filter)
}
