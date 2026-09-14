// Package home is the Home tab: the active project seen whole.
//
// It was the root's own dashboard screen. A screen the root drew itself could
// not be reached by a digit, could not declare its keys the way a tab does and
// could not be reloaded by the same freshness rule as everything else — three
// exceptions to remember for one screen. As a tab it is none of those things,
// and the root keeps only the chrome.
package home

import (
	"github.com/HoracioEspinosa/engram/internal/store"
	"github.com/HoracioEspinosa/engram/internal/tui/data"
	"github.com/HoracioEspinosa/engram/internal/tui/shared"
	"github.com/HoracioEspinosa/engram/internal/tui/theme"

	tea "github.com/charmbracelet/bubbletea"
)

// blockLimit caps each of Home's lists. Five is what the S2 wireframe shows
// for recent tasks, applied uniformly so one project with a long tail never
// dwarfs the screen.
const blockLimit = 5

// Model is the Home tab's state.
type Model struct {
	styles theme.Styles

	projects   data.ProjectReader
	graph      data.GraphReader
	syncer     data.GraphSyncer
	benchmarks data.BenchmarkReader

	project string

	width  int
	height int

	card       store.ProjectCard
	health     data.ProjectHealth
	graphState data.GraphState
	tasks      []store.TaskListItem
	bench      []data.Benchmark
	evidence   []store.EvidenceListItem

	// ancestors is the project's place in the forest, handed down by the root
	// rather than queried again: the status bar already reads it for its own
	// breadcrumb, and one chain per project is enough.
	ancestors []data.ProjectNode

	// focus and cursor are the two halves of the block cursor: which column
	// has the keyboard, and where it sits inside each one. Keeping a cursor
	// per column means moving across with h/l and back does not lose the place
	// the reader had in either.
	focus  shared.Pane
	cursor [2]int

	loaded bool
	notice shared.Notice
	// syncing is true from "s" until the sync answers, so the graph block says
	// what it is doing instead of looking frozen.
	syncing bool
}

// New creates the Home tab bound to the reader its card and lists come from.
func New(projects data.ProjectReader) Model {
	return Model{styles: theme.Default(), projects: projects}
}

// WithStyles returns a copy of m painted with styles instead of the default
// theme.New built it with.
func (m Model) WithStyles(styles theme.Styles) Model {
	m.styles = styles
	return m
}

// Styles exposes the tab's current style set for app-level tests that assert
// every tab paints with the same resolved palette.
func (m Model) Styles() theme.Styles { return m.styles }

// WithProject returns a copy of m scoped to project, with everything the
// previous one loaded dropped: a card, a graph and three lists that belong to
// a project nobody is looking at any more are worse than an empty screen,
// because they read as this project's.
func (m Model) WithProject(project string) Model {
	m.project = project
	m.card = store.ProjectCard{}
	m.health = data.ProjectHealth{}
	m.graphState = data.GraphState{}
	m.tasks = nil
	m.bench = nil
	m.evidence = nil
	m.ancestors = nil
	m.focus = shared.PaneMaster
	m.cursor = [2]int{}
	m.loaded = false
	m.notice = shared.Notice{}
	return m
}

// Project reports which project the tab is scoped to.
func (m Model) Project() string { return m.project }

// Health is the project's counters and sync state as Home last read them.
// The status bar shows the sync half in the frame around every tab, and it
// reads it here rather than issuing a query of its own: one answer, one
// place, so the bar and the card can never disagree.
func (m Model) Health() data.ProjectHealth { return m.health }

// GraphState is the code-graph summary Home last read, for the same reason
// Health is exposed.
func (m Model) GraphState() data.GraphState { return m.graphState }

// WithGraph binds the graph block to its reader and to the syncer "s" runs.
// A workspace built without them still opens; the block then says the graph
// has never been read rather than showing a state nobody produced.
func (m Model) WithGraph(reader data.GraphReader, syncer data.GraphSyncer) Model {
	m.graph = reader
	m.syncer = syncer
	return m
}

// WithBenchmarks binds the benchmarks block to its reader.
func (m Model) WithBenchmarks(reader data.BenchmarkReader) Model {
	m.benchmarks = reader
	return m
}

// WithBreadcrumb returns a copy of m that knows where the project sits in the
// forest. The root hands it down after its own ancestor lookup resolves.
func (m Model) WithBreadcrumb(ancestors []data.ProjectNode) Model {
	m.ancestors = ancestors
	return m
}

// Title is the label the tab bar shows for this tab.
func (Model) Title() string { return "Home" }

// Init loads the tab's first screen.
func (m Model) Init() tea.Cmd { return m.Refresh() }

// Refresh reloads every block.
func (m Model) Refresh() tea.Cmd {
	if m.project == "" {
		return nil
	}
	return loadHome(m.projects, m.graph, m.benchmarks, m.project)
}

// CapturingText is always false: Home has no text input.
func (Model) CapturingText() bool { return false }
