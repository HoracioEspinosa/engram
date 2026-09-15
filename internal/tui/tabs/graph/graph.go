// Package graph is the Graph tab: what the project's code graph currently
// says about itself, and which observations were written against it.
//
// It renders the store's verdict and never forms one. Staleness is decided
// when the graph is synced and persisted on the card; recomputing it here
// would give the workspace a second opinion that could disagree with the CLI
// and the MCP tools.
package graph

import (
	"errors"
	"fmt"
	"strings"

	"github.com/HoracioEspinosa/engram/internal/tui/data"
	"github.com/HoracioEspinosa/engram/internal/tui/shared"
	"github.com/HoracioEspinosa/engram/internal/tui/tabs"
	"github.com/HoracioEspinosa/engram/internal/tui/theme"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
)

// refPageSize is how many graph-linked observations one page holds.
const refPageSize = 20

// godNodeLimit caps the god nodes on screen. The store already ranks them by
// edge count; a list past the first handful is a different screen.
const godNodeLimit = 8

const (
	// bodyMargin is what the app frame spends either side of a tab's body,
	// minBodyWidth the narrowest body worth laying out, and defaultBodyWidth
	// what a tab with no size yet assumes.
	bodyMargin       = 4
	minBodyWidth     = 24
	defaultBodyWidth = 80

	// labelCells and fileCells are the god-node row's two columns.
	labelCells = 32
	fileCells  = 40
)

var (
	errNoGraphReader = errors.New("no graph reader is bound to this workspace")
	errNoGraphSyncer = errors.New("no graph syncer is bound to this workspace")
)

// Model is the Graph tab's state.
type Model struct {
	reader data.GraphReader
	syncer data.GraphSyncer
	styles theme.Styles

	project string

	Width  int
	Height int

	State data.GraphState
	Refs  []data.ObservationRef
	Total int

	offset int

	// syncing is true from "s" until the sync answers, so the screen says
	// what it is doing instead of looking frozen.
	syncing bool

	Notice shared.Notice
	loaded bool
}

// New creates the Graph tab bound to the reader it summarises and the syncer
// "s" runs.
func New(reader data.GraphReader, syncer data.GraphSyncer) Model {
	return Model{reader: reader, syncer: syncer, styles: theme.Default()}
}

// WithStyles returns a copy of m painted with styles instead of the default.
func (m Model) WithStyles(styles theme.Styles) Model {
	m.styles = styles
	return m
}

// Styles exposes the tab's current style set for app-level tests that assert
// every tab paints with the same resolved palette.
func (m Model) Styles() theme.Styles { return m.styles }

// WithProject returns a copy of m scoped to project, with everything the
// previous one loaded dropped.
func (m Model) WithProject(project string) Model {
	m.project = project
	m.State = data.GraphState{}
	m.Refs = nil
	m.Total = 0
	m.offset = 0
	m.syncing = false
	m.Notice = shared.Notice{}
	m.loaded = false
	return m
}

// Project reports which project the tab is scoped to.
func (m Model) Project() string { return m.project }

// Title is the label the tab bar shows for this tab.
func (Model) Title() string { return "Graph" }

// Init loads the tab's first screen.
func (m Model) Init() tea.Cmd { return m.Refresh() }

// Refresh re-reads the summary and the current page of refs. It is also what
// "r" runs: re-reading is how the tab revalidates, since the verdict itself
// belongs to the store.
func (m Model) Refresh() tea.Cmd {
	if m.project == "" {
		return nil
	}
	return loadGraph(m.reader, m.project, m.offset)
}

// CapturingText is always false: the Graph tab has no text input.
func (Model) CapturingText() bool { return false }

// HasPrevPage and HasNextPage read the store's own total.
func (m Model) HasPrevPage() bool { return m.offset > 0 }
func (m Model) HasNextPage() bool { return m.offset+len(m.Refs) < m.Total }

// ─── messages ────────────────────────────────────────────────────────────────

// graphLoadedMsg carries the summary and one page of refs.
type graphLoadedMsg struct {
	project string
	offset  int
	state   data.GraphState
	refs    data.Page[data.ObservationRef]
	err     error
	// refsErr is kept apart: the store has no paged observation_refs query
	// yet, so a reader that reports "not implemented" must not blank out a
	// summary that was read perfectly well.
	refsErr error
}

// graphSyncedMsg carries the result of "s".
type graphSyncedMsg struct {
	project string
	state   data.GraphState
	err     error
}

func (graphLoadedMsg) TabOwner() tabs.ID { return tabs.Graph }
func (graphSyncedMsg) TabOwner() tabs.ID { return tabs.Graph }

// loadGraph reads the card's graph summary and the refs page at offset.
func loadGraph(reader data.GraphReader, project string, offset int) tea.Cmd {
	return func() tea.Msg {
		msg := graphLoadedMsg{project: project, offset: offset}
		if reader == nil {
			msg.err = errNoGraphReader
			return msg
		}
		state, err := reader.GraphState(project)
		if err != nil {
			msg.err = err
			return msg
		}
		msg.state = state
		msg.refs, msg.refsErr = reader.ObservationRefs(project, refPageSize, offset)
		return msg
	}
}

// syncGraph runs the graph sync for project.
func syncGraph(syncer data.GraphSyncer, project string) tea.Cmd {
	return func() tea.Msg {
		if syncer == nil {
			return graphSyncedMsg{project: project, err: errNoGraphSyncer}
		}
		state, err := syncer.SyncGraph(project)
		return graphSyncedMsg{project: project, state: state, err: err}
	}
}

// ─── Update ──────────────────────────────────────────────────────────────────

// Update advances the Graph tab.
func (m Model) Update(msg tea.Msg) (tabs.Tab, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.Width, m.Height = msg.Width, msg.Height
		return m, nil

	case graphLoadedMsg:
		return m.applyLoaded(msg), nil

	case graphSyncedMsg:
		return m.applySynced(msg), nil

	case tea.KeyMsg:
		return m.handleKey(msg.String())
	}
	return m, nil
}

func (m Model) applyLoaded(msg graphLoadedMsg) Model {
	if msg.project != m.project {
		return m
	}
	if msg.err != nil {
		m.Notice = shared.Error(msg.err.Error())
		return m
	}
	m.Notice = shared.Notice{}
	m.State = msg.state
	m.offset = msg.refs.Offset
	m.Refs = msg.refs.Items
	m.Total = msg.refs.Total
	m.loaded = true
	if msg.refsErr != nil {
		// The summary is real and worth showing; the refs are the half that
		// could not be read, and saying which is what keeps the screen
		// honest.
		m.Notice = shared.Warn("linked observations: " + msg.refsErr.Error())
	}
	return m
}

func (m Model) applySynced(msg graphSyncedMsg) Model {
	if msg.project != m.project {
		return m
	}
	m.syncing = false
	if msg.err != nil {
		m.Notice = shared.Error("graph sync: " + msg.err.Error())
		return m
	}
	m.State = msg.state
	m.loaded = true
	m.Notice = shared.Info("graph synced")
	return m
}

func (m Model) handleKey(pressed string) (tabs.Tab, tea.Cmd) {
	switch pressed {
	case "s":
		if m.project == "" {
			return m, nil
		}
		m.syncing = true
		return m, syncGraph(m.syncer, m.project)
	case "p":
		if !m.HasPrevPage() {
			return m, nil
		}
		offset := m.offset - refPageSize
		if offset < 0 {
			offset = 0
		}
		return m, loadGraph(m.reader, m.project, offset)
	case "n":
		if !m.HasNextPage() {
			return m, nil
		}
		return m, loadGraph(m.reader, m.project, m.offset+refPageSize)
	}
	return m, nil
}

// Help lists the tab's own bindings.
func (m Model) Help() []key.Binding {
	bindings := []key.Binding{
		key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "sync graph")),
	}
	if m.HasPrevPage() {
		bindings = append(bindings, key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "previous page")))
	}
	if m.HasNextPage() {
		bindings = append(bindings, key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "next page")))
	}
	return bindings
}

// ─── View ────────────────────────────────────────────────────────────────────

func (m Model) bodyWidth() int {
	if m.Width <= 0 {
		return defaultBodyWidth
	}
	if w := m.Width - bodyMargin; w >= minBodyWidth {
		return w
	}
	return minBodyWidth
}

// View renders the tab.
func (m Model) View() string {
	var b strings.Builder

	b.WriteString(m.styles.Header.Render("  Code graph"))
	b.WriteString("\n")

	if !m.Notice.Empty() {
		b.WriteString(m.Notice.Render(m.styles))
		b.WriteString("\n")
	}

	switch {
	case m.project == "":
		b.WriteString(m.styles.NoResults.Render("No project is active. Press ctrl+p to pick one."))
		return b.String()
	case m.syncing:
		b.WriteString(shared.Loading(m.styles, spinner.Model{}, "the graph"))
		return b.String()
	case !m.loaded:
		b.WriteString(shared.Loading(m.styles, spinner.Model{}, "the graph summary"))
		return b.String()
	case m.State.Commit == "" && m.State.Nodes == 0:
		b.WriteString(m.styles.NoResults.Render("No graph has been built for this project. Press s to sync one."))
		return b.String()
	}

	b.WriteString(m.viewSummary())
	b.WriteString(m.viewGodNodes())
	b.WriteString(m.viewRefs())
	return b.String()
}

// viewSummary is the card's own numbers and the store's verdict on them.
func (m Model) viewSummary() string {
	g := m.State

	state := m.styles.SuccessInline.Render(m.styles.Icons.Glyph(theme.IconFresh) + " fresh")
	if g.Stale {
		// The reason is printed exactly as the store persisted it: a verdict
		// this build has never heard of still reaches the reader instead of
		// being flattened to "stale".
		reason := g.StaleReason
		if reason == "" {
			reason = "stale"
		}
		state = m.styles.StaleBadge.Render(m.styles.Icons.Glyph(theme.IconStale) + " " + reason)
		if g.ChangedFiles > 0 {
			state += m.styles.Timestamp.Render(fmt.Sprintf("  (%d changed files)", g.ChangedFiles))
		}
	}

	lines := []string{
		"  " + state,
		fmt.Sprintf("  %s nodes  %s edges  %s communities",
			m.styles.Emphasis.Render(fmt.Sprintf("%d", g.Nodes)),
			m.styles.Emphasis.Render(fmt.Sprintf("%d", g.Edges)),
			m.styles.Emphasis.Render(fmt.Sprintf("%d", g.Communities))),
	}
	if g.Commit != "" {
		lines = append(lines, "  "+m.styles.DetailLabel.Render("commit")+m.styles.ID.Render(shared.CutCells(g.Commit, 12)))
	}
	if g.BuiltAt != "" {
		lines = append(lines, "  "+m.styles.DetailLabel.Render("built")+m.styles.Timestamp.Render(shared.LocalTime(g.BuiltAt)))
	}
	if g.CheckedAt != "" {
		lines = append(lines, "  "+m.styles.DetailLabel.Render("checked")+m.styles.Timestamp.Render(shared.LocalTime(g.CheckedAt)))
	}
	return strings.Join(lines, "\n") + "\n"
}

// viewGodNodes lists the most connected nodes: where a change is most likely
// to reach everything else.
func (m Model) viewGodNodes() string {
	if len(m.State.GodNodes) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(m.styles.SectionHeading.Render("  god nodes"))
	b.WriteString("\n")

	nodes := m.State.GodNodes
	if len(nodes) > godNodeLimit {
		nodes = nodes[:godNodeLimit]
	}
	for _, node := range nodes {
		b.WriteString(fmt.Sprintf("  %s %s %s %s\n",
			m.styles.Icons.Glyph(theme.IconGodNode),
			m.styles.Emphasis.Render(shared.Field(node.Label, labelCells)),
			m.styles.ID.Render(shared.Cell(fmt.Sprintf("%d", node.Edges), 6)),
			m.styles.Timestamp.Render(shared.Field(node.File, fileCells))))
	}
	return b.String()
}

// viewRefs lists the observations written against a node of this graph — the
// memory that already knows something about the code a change would touch.
func (m Model) viewRefs() string {
	var b strings.Builder
	b.WriteString(m.styles.SectionHeading.Render("  linked observations"))
	b.WriteString("\n")

	if len(m.Refs) == 0 {
		b.WriteString(m.styles.NoResults.Render("No observation names a node of this graph."))
		b.WriteString("\n")
		return b.String()
	}

	width := m.bodyWidth()
	for _, ref := range m.Refs {
		b.WriteString(fmt.Sprintf("  %s %s %s\n",
			m.styles.ID.Render(shared.Cell(fmt.Sprintf("#%d", ref.ObservationID), 8)),
			m.styles.Emphasis.Render(shared.Field(ref.Ref, labelCells)),
			m.styles.Timestamp.Render(shared.CutCells(ref.GraphCommit, maxCommitCells(width)))))
	}
	b.WriteString(shared.RangeIndicator(m.styles, "observations", m.offset+1, m.offset+len(m.Refs), m.Total))
	return b.String()
}

// maxCommitCells is what is left for the commit after the two columns before
// it, so a narrow terminal drops the hash rather than wrapping the row.
func maxCommitCells(width int) int {
	room := width - 8 - labelCells - 4
	if room < 0 {
		return 0
	}
	if room > 12 {
		return 12
	}
	return room
}
