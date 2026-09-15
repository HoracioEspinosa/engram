// Package app is the root of the TUI workspace: it owns which tab is active,
// consumes the global key bindings, fans messages out to every tab and draws
// the application frame around whichever tab is showing.
//
// Dependency direction is one-way. app knows tabs/*, theme, data and shared;
// no tab knows app, and no tab knows another tab. Cross-tab navigation travels
// as tabs.NavigateMsg, which is declared in tabs rather than here precisely so
// a tab can emit it without importing the root.
package app

import (
	"github.com/HoracioEspinosa/engram/internal/tui/data"
	"github.com/HoracioEspinosa/engram/internal/tui/tabs"
	"github.com/HoracioEspinosa/engram/internal/tui/tabs/benchmarks"
	"github.com/HoracioEspinosa/engram/internal/tui/tabs/evidence"
	"github.com/HoracioEspinosa/engram/internal/tui/tabs/graph"
	"github.com/HoracioEspinosa/engram/internal/tui/tabs/home"
	"github.com/HoracioEspinosa/engram/internal/tui/tabs/memory"
	"github.com/HoracioEspinosa/engram/internal/tui/tabs/runbooks"
	"github.com/HoracioEspinosa/engram/internal/tui/tabs/settings"
	"github.com/HoracioEspinosa/engram/internal/tui/tabs/tasks"
	"github.com/HoracioEspinosa/engram/internal/tui/theme"

	tea "github.com/charmbracelet/bubbletea"
)

// registered lists the tabs this build implements, in tab-bar order. The IDs
// tabs declares but that no sub-model implements yet are simply absent.
var registered = []tabs.ID{
	tabs.Home, tabs.Memory, tabs.Tasks, tabs.Evidence,
	tabs.Benchmarks, tabs.Runbooks, tabs.Graph, tabs.Settings,
}

// Model is the root workspace model.
//
// Sub-models are typed fields rather than a map so the whole model stays a
// value: copying a Model copies every tab with it, which is what lets Update
// return a new root without the old one observing the change.
type Model struct {
	styles  theme.Styles
	version string

	width  int
	height int

	active     tabs.ID
	home       home.Model
	memory     memory.Model
	tasks      tasks.Model
	evidence   evidence.Model
	benchmarks benchmarks.Model
	runbooks   runbooks.Model
	graph      graph.Model
	settings   settings.Model
	projects   data.ProjectReader

	// freshness decides whether switching to a tab reloads it: a tab whose
	// data is still current is shown as it is.
	freshness tabFreshness

	// treeReader feeds the status bar's breadcrumb and the ctrl+p overlay. It
	// is optional: a workspace built without one shows the project on its own
	// and says so when the overlay is opened.
	treeReader data.ProjectTreeReader
	ancestors  []data.ProjectNode

	project string
	// tree is the ctrl+p project tree. Like showHelp and themePicker it is
	// root state: it rescopes every tab at once, which is something only the
	// root can do.
	tree treeModel

	// showHelp toggles the "?" overlay. It is root state, not per-tab:
	// closing it always returns to whatever screen was showing underneath,
	// untouched.
	showHelp bool

	// themePicker is the ctrl+t overlay. Like showHelp it is root state:
	// picking a theme repaints every tab, which is something only the root
	// can do.
	themePicker themePickerModel

	// palette is the ctrl+k workspace search. It is root state for the same
	// reason the tree is: a hit can be in another project, and moving the
	// whole workspace is something only the root can do.
	palette paletteModel
}

// New builds the root workspace around the readers its tabs consume: mem feeds
// the Memory tab, projects feeds Home (and the Runbooks tab's "o" hub
// lookup), task feeds the Tasks tab, evidenceReader feeds the Evidence tab,
// runbookReader feeds the Runbooks tab. initialProject, when set, scopes Home,
// Tasks, Evidence, Runbooks and Memory to it; without one the workspace opens
// on the project tree so there is something to pick.
//
// The root never opens or wraps a store itself; whoever builds it decides
// which store backs each reader, so a tab can never end up bound to a
// different (or missing) store than its siblings.
func New(mem data.MemorySource, projects data.ProjectReader, task data.TaskSource, evidenceReader data.EvidenceSource, runbookReader data.RunbookSource, version string, styles theme.Styles, initialProject string) Model {
	m := Model{
		version:     version,
		active:      tabs.Home,
		projects:    projects,
		project:     initialProject,
		home:        home.New(projects).WithProject(initialProject),
		memory:      memory.New(mem, version).WithTasks(task).WithProject(initialProject),
		tasks:       tasks.New(task).WithProject(initialProject),
		evidence:    evidence.New(evidenceReader).WithProject(initialProject),
		benchmarks:  benchmarks.New(nil).WithProject(initialProject),
		runbooks:    runbooks.New(runbookReader, projects).WithProject(initialProject),
		graph:       graph.New(nil, nil).WithProject(initialProject),
		settings:    settings.New(),
		tree:        newTreeModel(nil),
		themePicker: newThemePickerModel(styles),
		palette:     newPaletteModel(styles),
	}
	m = m.withStyles(styles)

	// Without a resolvable project the workspace opens on the project tree,
	// composited over Home so closing it lands somewhere real.
	if initialProject == "" {
		m.tree.open = true
	}

	return m
}

// WithUpdateChecker returns a copy of the root whose Memory tab asks check
// for the release banner instead of GitHub. A golden suite driving the real
// program uses it to keep the banner out of the frame it snapshots: whether
// an HTTP response beats tea.Quit is not something a reproducible render can
// depend on.
func (m Model) WithUpdateChecker(check memory.UpdateChecker) Model {
	m.memory = m.memory.WithUpdateChecker(check)
	return m
}

// WithGraph binds Home's graph block to its reader and to the syncer "s"
// runs. A workspace built without them still opens; the block then says the
// graph has never been read.
func (m Model) WithGraph(reader data.GraphReader, syncer data.GraphSyncer) Model {
	m.home = m.home.WithGraph(reader, syncer)
	m.graph = graph.New(reader, syncer).WithProject(m.project).WithStyles(m.styles)
	return m
}

// WithBenchmarks binds Home's benchmarks block to its reader.
func (m Model) WithBenchmarks(reader data.BenchmarkReader) Model {
	m.home = m.home.WithBenchmarks(reader)
	m.benchmarks = benchmarks.New(reader).WithProject(m.project).WithStyles(m.styles)
	return m
}

// withStyles returns a copy of the root repainted in a style set, with every
// tab repainted alongside it.
//
// It is the one place styles fan out. A theme is chosen in one spot — the
// picker, or the resolution that runs before the workspace opens — and a tab
// that missed the fan-out would keep rendering in the palette it was built
// with, which is a bug that looks like a rendering glitch and is invisible to
// any test of that tab alone.
func (m Model) withStyles(s theme.Styles) Model {
	m.styles = s
	m.home = m.home.WithStyles(s)
	m.memory = m.memory.WithStyles(s)
	m.tasks = m.tasks.WithStyles(s)
	m.evidence = m.evidence.WithStyles(s)
	m.benchmarks = m.benchmarks.WithStyles(s)
	m.runbooks = m.runbooks.WithStyles(s)
	m.graph = m.graph.WithStyles(s)
	m.settings = m.settings.WithStyles(s)
	m.themePicker = m.themePicker.withStyles(s)
	m.palette.styles = s
	return m
}

// Init loads every tab's first screen and switches the terminal to the
// alternate screen buffer.
func (m Model) Init() tea.Cmd {
	cmds := make([]tea.Cmd, 0, len(registered)+3)
	for _, id := range registered {
		if tab := m.tab(id); tab != nil {
			cmds = append(cmds, tab.Init())
		}
	}
	if cmd := loadAncestors(m.treeReader, m.project); cmd != nil {
		cmds = append(cmds, cmd)
	}
	// If starting on the tree — no project was resolvable — load the forest
	// too, so it shows real projects instead of an empty list until the user
	// presses "r".
	if m.tree.open {
		cmds = append(cmds, loadTree(m.tree.reader))
	}
	cmds = append(cmds, tea.EnterAltScreen)
	return tea.Batch(cmds...)
}

// tab returns the sub-model registered for id, or nil when this build
// implements no tab for it.
func (m Model) tab(id tabs.ID) tabs.Tab {
	switch id {
	case tabs.Home:
		return m.home
	case tabs.Memory:
		return m.memory
	case tabs.Tasks:
		return m.tasks
	case tabs.Evidence:
		return m.evidence
	case tabs.Benchmarks:
		return m.benchmarks
	case tabs.Runbooks:
		return m.runbooks
	case tabs.Graph:
		return m.graph
	case tabs.Settings:
		return m.settings
	}
	return nil
}

// withTab returns a copy of m with id's sub-model replaced. A tab of an
// unexpected concrete type is dropped rather than stored, which keeps a
// mis-registered tab from corrupting the root.
func (m Model) withTab(id tabs.ID, t tabs.Tab) Model {
	switch id {
	case tabs.Home:
		if updated, ok := t.(home.Model); ok {
			m.home = updated
		}
	case tabs.Memory:
		if updated, ok := t.(memory.Model); ok {
			m.memory = updated
		}
	case tabs.Tasks:
		if updated, ok := t.(tasks.Model); ok {
			m.tasks = updated
		}
	case tabs.Evidence:
		if updated, ok := t.(evidence.Model); ok {
			m.evidence = updated
		}
	case tabs.Benchmarks:
		if updated, ok := t.(benchmarks.Model); ok {
			m.benchmarks = updated
		}
	case tabs.Runbooks:
		if updated, ok := t.(runbooks.Model); ok {
			m.runbooks = updated
		}
	case tabs.Graph:
		if updated, ok := t.(graph.Model); ok {
			m.graph = updated
		}
	case tabs.Settings:
		if updated, ok := t.(settings.Model); ok {
			m.settings = updated
		}
	}
	return m
}
